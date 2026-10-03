package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

// PRProvider is independent of local commit statistics and never mutates GitHub.
type PRProvider interface {
	Resolve(context.Context, string) (string, error)
	Fetch(context.Context, string) gh.Result
}

func defaultPRProvider() PRProvider { return gh.NewProvider(nil) }

type prSnapshot struct {
	prs      []gh.PullRequest
	checked  time.Time
	lastGood time.Time
	partial  bool
	err      error
}
type prState struct {
	repositories map[string]string
	errors       map[string]error
	snapshots    map[string]prSnapshot
	loading      bool
	nextRefresh  time.Time
}
type prsLoadedMsg struct {
	generation   uint64
	repositories map[string]string
	errors       map[string]error
	results      map[string]gh.Result
	checked      time.Time
}

func (m *Model) cancelPRRefresh() {
	if m.cancelPRs != nil {
		m.cancelPRs()
		m.cancelPRs = nil
	}
	m.prGeneration++
	m.prState.loading = false
}
func (m *Model) startPRRefresh() tea.Cmd {
	// Reuse the current snapshot and coalesce repeated refresh keys.
	if m.prState.loading || time.Now().Before(m.prState.nextRefresh) {
		return nil
	}
	m.prState.nextRefresh = time.Now().Add(time.Minute)
	m.cancelPRRefresh()
	parent := m.scanContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	ctx = gh.WithRequestBudget(ctx, gh.MaxRefreshRequests)
	m.cancelPRs = cancel
	m.prState.loading = true
	generation := m.prGeneration
	var repos []git.Repository
	for _, repo := range m.loadedRepos {
		if !m.excludedRepos[repo.ID] {
			repos = append(repos, repo)
		}
	}
	provider := m.prProvider
	return m.commands.wrap(func() tea.Msg {
		defer cancel()
		msg := prsLoadedMsg{generation: generation, repositories: map[string]string{}, errors: map[string]error{}, results: map[string]gh.Result{}}
		aliases := make(map[string]string)
		var unavailable error
		// Serial bounded requests avoid multiplying API budgets across duplicates.
		for _, repo := range repos {
			if ctx.Err() != nil {
				msg.errors[repo.ID] = ctx.Err()
				continue
			}
			remote, err := provider.Resolve(ctx, repo.Path)
			if err != nil {
				msg.errors[repo.ID] = err
				continue
			}
			if canonical, ok := aliases[remote]; ok {
				remote = canonical
			}
			if _, exists := msg.results[remote]; !exists {
				result := gh.Result{Repo: remote, Err: unavailable}
				if unavailable == nil {
					result = provider.Fetch(ctx, remote)
					if errors.Is(result.Err, gh.ErrAuthentication) || errors.Is(result.Err, gh.ErrUnavailable) || errors.Is(result.Err, gh.ErrRateLimited) {
						unavailable = result.Err
					}
					if result.RetryAt.After(time.Now()) {
						unavailable = gh.ErrRateLimited
					}
				}
				canonical, err := gh.CanonicalRepo(result.Repo)
				if err != nil {
					canonical = remote
				}
				aliases[remote] = canonical
				remote = canonical
				msg.results[remote] = result
			}
			msg.repositories[repo.ID] = remote
		}
		msg.checked = time.Now()
		return msg
	})
}
func (m *Model) applyPRResult(msg prsLoadedMsg) {
	selectedKey := ""
	oldVisible := m.visiblePRs()
	if m.prRow >= 0 && m.prRow < len(oldVisible) {
		selectedKey = oldVisible[m.prRow].Key()
	}
	m.prState.loading = false
	if next := msg.checked.Add(time.Minute); next.After(m.prState.nextRefresh) {
		m.prState.nextRefresh = next
	}
	if m.prState.snapshots == nil {
		m.prState.snapshots = map[string]prSnapshot{}
	}
	if m.prState.repositories == nil {
		m.prState.repositories = map[string]string{}
	}
	// A failed origin lookup retains its last known association, visibly stale.
	for id, repo := range msg.repositories {
		m.prState.repositories[id] = repo
	}
	m.prState.errors = msg.errors
	for repo, result := range msg.results {
		if result.RetryAt.After(m.prState.nextRefresh) {
			m.prState.nextRefresh = result.RetryAt
		}
		old := m.prState.snapshots[repo]
		next := prSnapshot{checked: msg.checked, lastGood: old.lastGood, partial: !result.Complete, err: result.Err}
		for _, pr := range result.PRs {
			next.partial = next.partial || !pr.FilesComplete || !pr.ContextComplete
		}
		prior := make(map[string]gh.PullRequest)
		kept := make(map[string]gh.PullRequest)
		for _, pr := range old.prs {
			prior[pr.Key()] = pr
			if !result.Complete {
				kept[pr.Key()] = pr
			}
		}
		// Inventory completeness is independent of supporting context. A
		// complete open inventory removes closed PRs even if files are partial.
		for _, pr := range result.PRs {
			if oldPR, ok := prior[pr.Key()]; ok && !pr.FilesComplete {
				paths := make(map[string]bool)
				for _, f := range pr.Files {
					paths[f.Path] = true
				}
				for _, f := range oldPR.Files {
					if !paths[f.Path] {
						pr.Files = append(pr.Files, f)
					}
				}
			}
			kept[pr.Key()] = pr
		}
		for _, pr := range kept {
			next.prs = append(next.prs, pr)
		}
		if result.Complete && result.Err == nil && !next.partial {
			next.lastGood = msg.checked
		}

		m.prState.snapshots[repo] = next
	}
	visible := m.visiblePRs()
	m.prRow = max(0, min(m.prRow, len(visible)-1))
	found := false
	for i, pr := range visible {
		if pr.Key() == selectedKey {
			m.prRow = i
			found = true
			break
		}
	}
	if !found {
		m.prDetail = false
		m.prOffset = 0
	}
}
func (m Model) prsForRepository(id string) []gh.PullRequest {
	return m.prState.snapshots[m.prState.repositories[id]].prs
}
func (m Model) prAreas(repo git.Repository, pr gh.PullRequest) []stats.WorkArea {
	definition, ok := m.areaDefinitions[repo.ID]
	if !ok {
		definition = stats.DefineWorkAreas(m.recordsInRepository(repo, m.allRecords), m.rulesForRepository(repo))
	}
	changes := make([]git.PathChange, 0, len(pr.Files))
	for _, file := range pr.Files {
		changes = append(changes, git.PathChange{Path: file.Path, Generated: !m.options.IncludeGenerated && git.IsGeneratedPath(file.Path)})
	}
	return definition.ClassifyPaths(changes, !pr.FilesComplete)
}
func (m Model) scopePRs(scope string) []gh.PullRequest {
	if repo, ok := m.areaRepository(); ok {
		var prs []gh.PullRequest
		for _, pr := range m.prsForRepository(repo.ID) {
			for _, area := range m.prAreas(repo, pr) {
				if area.ID == scope {
					prs = append(prs, pr)
					break
				}
			}
		}
		return prs
	}
	return m.prsForRepository(scope)
}
func (m Model) allPRs() []gh.PullRequest {
	unique := map[string]gh.PullRequest{}
	for _, repo := range m.loadedRepos {
		if !m.excludedRepos[repo.ID] {
			for _, pr := range m.prsForRepository(repo.ID) {
				unique[pr.Key()] = pr
			}
		}
	}
	prs := make([]gh.PullRequest, 0, len(unique))
	for _, pr := range unique {
		prs = append(prs, pr)
	}
	return prs
}
func (m Model) visiblePRs() []gh.PullRequest {
	prs := m.allPRs()
	if !m.prAll {
		prs = append([]gh.PullRequest(nil), m.scopePRs(m.selectedScopeID())...)
	}
	sort.Slice(prs, func(i, j int) bool {
		if !prs[i].UpdatedAt.Equal(prs[j].UpdatedAt) {
			return prs[i].UpdatedAt.After(prs[j].UpdatedAt)
		}
		return prs[i].Key() < prs[j].Key()
	})
	return prs
}
func (m Model) prStatus() string {
	partial, failed, hasSnapshot := false, false, false
	reason := "fetch failed"
	refreshHint := ""
	if time.Now().Before(m.prState.nextRefresh) {
		refreshHint = " · R after " + m.prState.nextRefresh.Local().Format("15:04:05")
	}
	var checked time.Time
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			continue
		}
		if err := m.prState.errors[repo.ID]; err != nil {
			failed = true
			if reason == "fetch failed" || reason == "unsupported GitHub origin" {
				reason = prFailureReason(err)
			}
		}
		snapshot, exists := m.prState.snapshots[m.prState.repositories[repo.ID]]
		hasSnapshot = hasSnapshot || exists
		partial = partial || snapshot.partial
		failed = failed || snapshot.err != nil
		if snapshot.err != nil {
			reason = prFailureReason(snapshot.err)
		}
		if snapshot.checked.After(checked) {
			checked = snapshot.checked
		}
	}
	count := len(m.allPRs())
	if failed && count == 0 {
		return "GitHub PRs unavailable · " + reason + refreshHint + " · p details"
	}
	if !hasSnapshot {
		if m.prState.loading {
			return "GitHub PRs refreshing · no snapshot yet"
		}
		return "GitHub PRs unknown" + refreshHint + " · p details"
	}
	// Lead with evidence quality: fixed-width sidebars must never truncate
	// STALE/PARTIAL after counts. A new request cannot make retained data fresh.
	label := "GitHub"
	if failed {
		label += " · STALE"
	} else if partial {
		label += " · PARTIAL"
	}
	if partial && count == 0 {
		label += " · PR count unknown"
	} else {
		label += fmt.Sprintf(" · %d known open PRs", count)
	}
	if failed {
		label += " · " + reason
	}
	if m.prState.loading {
		label += " · refreshing · previous snapshot shown"
	}

	if !checked.IsZero() {
		label += " · checked " + checked.Local().Format("15:04 MST")
	}
	return label + refreshHint + " · p scope · P all"
}

// selectPRScope uses the visible Worklist selection and only retained PR data.
func (m Model) selectPRScope(all bool) Model {
	if !all && !m.glance.detailOpen {
		// View resolves the highlighted row on a value copy. Persist it before
		// the overlay reads its scope, including after opening all PRs first.
		rows := m.glanceRepositories()
		if len(rows) == 0 {
			m.setSelectedScopeID("")
		} else {
			m.selectedRepository(rows)
		}
	}
	m.showPRs = true
	m.prBrowserError = ""
	m.prAll = all
	m.prDetail = false
	m.prRow = 0
	m.prOffset = 0
	return m
}

func (m Model) handlePRKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "o":
		return m.openSelectedPR()
	case "q", "ctrl+c":
		return m.quit()
	case "p", "P":
		m = m.selectPRScope(msg.String() == "P")
	case "esc":
		if m.prDetail {
			m.prDetail = false
			m.prOffset = 0
		} else {
			m.showPRs = false
		}
	case "enter":
		m.prDetail = !m.prDetail
		m.prOffset = 0
	case "up", "k":
		if m.prDetail {
			m.prOffset = max(0, min(m.prOffset-1, m.prDetailMaxOffset()))
		} else {
			m.prRow = max(0, m.prRow-1)
		}
	case "down", "j":
		if m.prDetail {
			m.prOffset = min(m.prOffset+1, m.prDetailMaxOffset())
		} else {
			m.prRow = min(max(0, len(m.visiblePRs())-1), m.prRow+1)
		}
	case "R":
		return m, m.startPRRefresh()
	}
	return m, nil
}
func (m Model) renderPRs() string {
	width, height := max(1, m.width), max(1, m.height)
	if width < 40 || height < 12 {
		return ansi.Truncate("Resize to 40×12 · Esc back · q quit", width, "…")
	}
	lines := renderBanner(min(width, bannerMinWidth-1))
	prs := m.visiblePRs()
	label := "OPEN PULL REQUESTS · selected scope"
	if m.prAll {
		label = "OPEN PULL REQUESTS · all included repositories"
	}
	if m.prDetail && len(prs) > 0 {
		pr := prs[min(m.prRow, len(prs)-1)]
		label = fmt.Sprintf("PR #%d · %s", pr.Number, displayText(pr.Repo))
	}
	lines = append(lines, RenderSectionHeader(label, width), StyleDimWhite.Render("  "+m.prStatus()), "  All open PRs · independent of local date, person and bot filters")
	if len(prs) == 0 {
		lines = append(lines, "  No PRs to display in this snapshot/scope.")
	} else if m.prDetail {
		pr := prs[min(m.prRow, len(prs)-1)]
		wrapped := m.prDetailLines(pr, width)
		budget := m.prDetailBudget()
		offset := max(0, min(m.prOffset, max(0, len(wrapped)-budget)))
		lines = append(lines, wrapped[offset:min(len(wrapped), offset+budget)]...)
	} else {
		budget := max(1, height-len(lines)-5)
		selected := max(0, min(m.prRow, len(prs)-1))
		start := max(0, min(selected-budget/2, len(prs)-budget))
		for i := start; i < min(len(prs), start+budget); i++ {
			pr := prs[i]
			draft := ""
			if pr.Draft {
				draft = " DRAFT"
			}
			row := fmt.Sprintf("#%d%s %s · %s · %s · %s", pr.Number, draft, displayText(pr.Title), githubIdentity(pr.Author), displayText(pr.ReviewDecision), displayText(pr.CheckState))
			if m.prAll {
				row = displayText(pr.Repo) + " " + row
			}
			lines = append(lines, awarenessRow(row, i == selected, i, width))
		}
	}
	if len(prs) == 0 || !m.prDetail {
		scopeRepoID := m.selectedScopeID()
		if m.areaRepoID != "" {
			scopeRepoID = m.areaRepoID
		}
		for _, repo := range m.loadedRepos {
			if !m.prAll && repo.ID != scopeRepoID {
				continue
			}
			if m.excludedRepos[repo.ID] {
				continue
			}
			err := m.prState.errors[repo.ID]
			if err == nil {
				err = m.prState.snapshots[m.prState.repositories[repo.ID]].err
			}
			if err != nil && len(lines) < height-4 {
				lines = append(lines, "  "+displayText(repo.Name)+": "+displayText(err.Error()))
			}
		}
	}
	help := "  ↑↓ scroll · Enter details/back · Esc back · R refresh PRs · q quit"
	if width < 70 {
		help = "↑↓ scroll · Enter detail · Esc back · R"
	}
	if m.prBrowserError != "" {
		lines = append(lines, "  "+m.prBrowserError)
	}
	if len(prs) > 0 && prBrowserURL(prs[max(0, min(m.prRow, len(prs)-1))]) != "" {
		lines = append(lines, "  o open PR in browser")
	}
	lines = append(lines, help)
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}
func (m Model) prDetailLines(pr gh.PullRequest, width int) []string {
	detail := []string{fmt.Sprintf("%s #%d · %s", pr.Repo, pr.Number, pr.Title), pr.URL, "Author " + githubIdentity(pr.Author), fmt.Sprintf("Draft: %t · review: %s · checks: %s · mergeability: %s", pr.Draft, pr.ReviewDecision, pr.CheckState, pr.Mergeable), "Requested reviewers: " + strings.Join(pr.RequestedReviewers, ", "), "Updated " + pr.UpdatedAt.Local().Format("2006-01-02 15:04 MST") + " (any PR activity)"}
	detail = append(detail, "Mergeability reports conflicts only; check rollup covers reported checks. Branch protection/readiness is not evaluated.")
	snapshot := m.prState.snapshots[pr.Repo]
	if !snapshot.checked.IsZero() {
		detail = append(detail, "Snapshot checked "+snapshot.checked.Local().Format("2006-01-02 15:04:05 MST"))
	}
	if snapshot.err != nil || snapshot.partial {
		detail = append(detail, "STALE/PARTIAL snapshot · retained evidence may no longer be current")
		if !snapshot.lastGood.IsZero() {
			detail = append(detail, "Last complete fetch "+snapshot.lastGood.Local().Format("2006-01-02 15:04:05 MST"))
		}
		if snapshot.err != nil {
			detail = append(detail, snapshot.err.Error())
		}
	}
	if !pr.FilesComplete {
		detail = append(detail, "PARTIAL files · unmapped paths may touch other areas")
	}
	if !pr.ContextComplete {
		detail = append(detail, "PARTIAL review/context data · unknown is not approval")
	}
	if !pr.PreviousPathsComplete {
		detail = append(detail, "Rename origins unavailable · current paths shown")
	}
	for _, review := range pr.Reviews {
		detail = append(detail, "Reviewer "+githubIdentity(review.Author)+": "+review.State)
	}
	for _, repo := range m.loadedRepos {
		if !m.excludedRepos[repo.ID] && m.prState.repositories[repo.ID] == pr.Repo {
			var names []string
			for _, area := range m.prAreas(repo, pr) {
				names = append(names, area.Name)
			}
			detail = append(detail, "Work areas ("+repo.Name+"): "+strings.Join(names, ", "))
		}
	}
	detail = append(detail, "Changed paths:")
	for _, file := range pr.Files {
		path := file.Path
		if !m.options.IncludeGenerated && git.IsGeneratedPath(file.Path) {
			path += " [excluded]"
		}
		detail = append(detail, path)
	}
	var wrapped []string
	for _, line := range detail {
		line = displayText(line)
		if line == "" {
			wrapped = append(wrapped, "  ")
			continue
		}
		for len(line) > 0 {
			part := cutToWidth(line, max(1, width-4))
			if part == "" {
				break
			}
			wrapped = append(wrapped, "  "+part)
			line = line[len(part):]
		}
	}

	return wrapped
}

// Reserve the three header rows and four footer/error/hint rows consistently.
func (m Model) prDetailBudget() int {
	return max(1, m.height-len(renderBanner(min(max(1, m.width), bannerMinWidth-1)))-7)
}

func (m Model) prDetailMaxOffset() int {
	prs := m.visiblePRs()
	if len(prs) == 0 {
		return 0
	}
	pr := prs[min(m.prRow, len(prs)-1)]
	budget := m.prDetailBudget()
	return max(0, len(m.prDetailLines(pr, max(1, m.width)))-budget)
}

func githubIdentity(login string) string {
	if login == "" {
		return "unavailable"
	}
	return "@" + displayText(login)
}

func prFailureReason(err error) string {
	switch {
	case errors.Is(err, gh.ErrAuthentication):
		return "gh not signed in"
	case errors.Is(err, gh.ErrUnavailable):
		return "gh not installed"
	case errors.Is(err, gh.ErrUnsupportedOrigin):
		return "unsupported GitHub origin"
	case errors.Is(err, gh.ErrRateLimited):
		return "rate limited"
	case errors.Is(err, gh.ErrRepository):
		return "repository unavailable"
	default:
		return "fetch failed"
	}
}
