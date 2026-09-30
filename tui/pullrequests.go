package tui

import (
	"context"
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
	if !m.options.GitHubEnabled {
		return nil
	}
	m.cancelPRRefresh()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	m.cancelPRs = cancel
	m.prState.loading = true
	generation := m.prGeneration
	repos := append([]git.Repository(nil), m.loadedRepos...)
	provider := m.prProvider
	return func() tea.Msg {
		defer cancel()
		msg := prsLoadedMsg{generation: generation, repositories: map[string]string{}, errors: map[string]error{}, results: map[string]gh.Result{}}
		aliases := make(map[string]string)
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
				result := provider.Fetch(ctx, remote)
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
	}
}
func (m *Model) applyPRResult(msg prsLoadedMsg) {
	selectedKey := ""
	oldVisible := m.visiblePRs()
	if m.prRow >= 0 && m.prRow < len(oldVisible) {
		selectedKey = oldVisible[m.prRow].Key()
	}
	m.prState.loading = false
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
	if !m.options.GitHubEnabled {
		return "Remote status unknown (no fetch) · GitHub PRs off"
	}
	if m.prState.loading {
		return "GitHub PRs refreshing · previous snapshot shown"
	}
	partial, failed := false, false
	var checked time.Time
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			continue
		}
		if m.prState.errors[repo.ID] != nil {
			failed = true
		}
		snapshot := m.prState.snapshots[m.prState.repositories[repo.ID]]
		partial = partial || snapshot.partial
		failed = failed || snapshot.err != nil
		if snapshot.checked.After(checked) {
			checked = snapshot.checked
		}
	}
	label := fmt.Sprintf("GitHub · %d known open PRs", len(m.allPRs()))
	if failed {
		label += " · STALE/unavailable"
	} else if partial {
		label += " · PARTIAL"
	}
	if !checked.IsZero() {
		label += " · checked " + checked.Local().Format("15:04 MST")
	}
	return label + " · p scope · P all"
}
func (m Model) handlePRKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "esc", "p":
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
		if !m.options.GitHubEnabled {
			lines = append(lines, "  Set github.enabled to true; uses existing gh authentication.")
		}
	} else if m.prDetail {
		pr := prs[min(m.prRow, len(prs)-1)]
		wrapped := m.prDetailLines(pr, width)
		budget := max(1, height-len(lines)-2)
		offset := max(0, min(m.prOffset, max(0, len(wrapped)-budget)))
		lines = append(lines, wrapped[offset:min(len(wrapped), offset+budget)]...)
	} else {
		budget := max(1, height-len(lines)-3)
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
			if err != nil && len(lines) < height-2 {
				lines = append(lines, "  "+displayText(repo.Name)+": "+displayText(err.Error()))
			}
		}
	}
	help := "  ↑↓ scroll · Enter details/back · Esc back · R refresh PRs · q quit"
	if width < 70 {
		help = "↑↓ scroll · Enter detail · Esc back · R"
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
func (m Model) prCountLabel(scope string) string {
	if !m.options.GitHubEnabled {
		return ""
	}
	id := scope
	if m.areaRepoID != "" {
		id = m.areaRepoID
	}
	snapshot, ok := m.prState.snapshots[m.prState.repositories[id]]
	if !ok {
		return " [? PRs]"
	}
	suffix := ""
	if snapshot.partial || snapshot.err != nil || m.prState.errors[id] != nil {
		suffix = "+?"
	}
	return fmt.Sprintf(" [%d%s PRs]", len(m.scopePRs(scope)), suffix)
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
func (m Model) prDetailMaxOffset() int {
	prs := m.visiblePRs()
	if len(prs) == 0 {
		return 0
	}
	pr := prs[min(m.prRow, len(prs)-1)]
	budget := max(1, m.height-len(renderBanner(min(max(1, m.width), bannerMinWidth-1)))-5)
	return max(0, len(m.prDetailLines(pr, max(1, m.width)))-budget)
}

func githubIdentity(login string) string {
	if login == "" {
		return "unavailable"
	}
	return "@" + displayText(login)
}
