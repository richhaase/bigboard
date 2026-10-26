package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	aliases      map[string]string
	repositories map[string]string
	errors       map[string]error
	snapshots    map[string]prSnapshot
	loading      bool
	nextRefresh  time.Time
}
type prsLoadedMsg struct {
	aliases      map[string]string
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
	aliases := maps.Clone(m.prState.aliases)
	if aliases == nil {
		aliases = make(map[string]string)
	}
	return m.commands.wrap(func() tea.Msg {
		defer cancel()
		msg := prsLoadedMsg{generation: generation, repositories: map[string]string{}, errors: map[string]error{}, results: map[string]gh.Result{}}
		msg.aliases = aliases
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
			origin := remote
			for i := 0; i < len(aliases) && aliases[remote] != "" && aliases[remote] != remote; i++ {
				remote = aliases[remote]
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
				aliases[origin] = canonical
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
	if len(msg.results) > 0 {
		if next := msg.checked.Add(time.Minute); next.After(m.prState.nextRefresh) {
			m.prState.nextRefresh = next
		}
	}
	if msg.aliases != nil {
		m.prState.aliases = msg.aliases
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
	for id, err := range msg.errors {
		if errors.Is(err, gh.ErrUnsupportedOrigin) {
			delete(m.prState.repositories, id)
		}
	}
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
	kept := prs[:0]
	query := strings.ToLower(m.prQuery)
	duration := m.rangeDuration()
	cutoff := time.Now().Add(-duration)
	for _, pr := range prs {
		if duration > 0 && !pr.UpdatedAt.After(cutoff) {
			continue
		}
		if !m.prFilters.matches(pr) {
			continue
		}
		if m.prRepoID != "" && (m.excludedRepos[m.prRepoID] || pr.Repo != m.prState.repositories[m.prRepoID]) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s #%d", pr.Repo, pr.Title, pr.Author, pr.Number)), query) {
			continue
		}
		kept = append(kept, pr)
	}
	prs = kept
	sort.Slice(prs, func(i, j int) bool {
		a, b := prs[i], prs[j]
		comparison := 0
		switch m.prSort {
		case 0:
			comparison = b.UpdatedAt.Compare(a.UpdatedAt)
		case 1:
			comparison = strings.Compare(strings.ToLower(a.Repo), strings.ToLower(b.Repo))
		case 2:
			comparison = strings.Compare(strings.ToLower(a.Author), strings.ToLower(b.Author))
		case 3:
			comparison = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		}
		if comparison == 0 {
			comparison = strings.Compare(a.Key(), b.Key())
		}
		if m.prReverse {
			return comparison > 0
		}
		return comparison < 0
	})
	return prs
}
func (m Model) prStatus() string {
	partial, failed, hasSnapshot := false, false, false
	reason := "fetch failed"
	refreshHint := ""
	if time.Now().Before(m.prState.nextRefresh) {
		refreshHint = " · r after " + m.prState.nextRefresh.Local().Format("15:04:05")
	}
	var checked time.Time
	eligible := 0
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			continue
		}
		if err := m.prState.errors[repo.ID]; err != nil {
			if errors.Is(err, gh.ErrUnsupportedOrigin) {
				continue
			}
			failed = true
			if reason == "fetch failed" || reason == "unsupported GitHub origin" {
				reason = prFailureReason(err)
			}
		}
		eligible++
		snapshot, exists := m.prState.snapshots[m.prState.repositories[repo.ID]]
		hasSnapshot = hasSnapshot || exists
		partial = partial || snapshot.partial || !exists
		failed = failed || snapshot.err != nil
		if snapshot.err != nil {
			reason = prFailureReason(snapshot.err)
		}
		if snapshot.checked.After(checked) {
			checked = snapshot.checked
		}
	}
	count := len(m.allPRs())
	if eligible == 0 && !m.prState.loading {
		return "GitHub PRs not applicable · no supported origins · p details"
	}
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
	return label + refreshHint + " · p PRs"
}

func (m Model) handlePRKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prRepoPicker {
		return m.handlePRScopeKey(msg)
	}
	key := msg.String()
	if !m.prDetail {
		if row, ok := glanceMove(key, m.prRow, len(m.visiblePRs()), m.prListPageSize()); ok {
			m.prRow = row
			return m, nil
		}
	} else {
		page := m.prDetailBudget()
		switch key {
		case "up":
			m.prOffset--
		case "down":
			m.prOffset++
		case "pgup":
			m.prOffset -= page
		case "pgdown":
			m.prOffset += page
		case "home":
			m.prOffset = 0
		case "end":
			m.prOffset = m.prDetailMaxOffset()
		}
		m.prOffset = max(0, min(m.prOffset, m.prDetailMaxOffset()))
	}
	switch key {
	case "t":
		m.openRangePicker()
	case "o":
		return m.openSelectedPR()
	case "f":
		m.openPRFilter("repo")
	case "u":
		m.openPRFilter("author")
	case "v":
		m.openPRFilter("review")
	case "k":
		m.openPRFilter("checks")
	case "d":
		m.openPRFilter("state")
	case "x":
		m.clearPRFilters()
	case "esc", "left":
		if m.prDetail {
			m.prDetail, m.prOffset = false, 0
		} else if m.prQuery != "" {
			selected := m.selectedPRKey()
			m.prQuery = ""
			m.selectPR(selected)
		} else if m.prRepoID != "" {
			selected := m.selectedPRKey()
			m.prRepoID = ""
			m.selectPR(selected)
		} else if m.prFilters != (prFilters{}) {
			m.clearPRFilters()
		}
	case "enter", "right":
		if len(m.visiblePRs()) > 0 {
			m.prDetail = true
		}
	case "/":
		if !m.prDetail {
			m.prSearching = true
		}
	case "s", "S":
		if !m.prDetail {
			selected := m.selectedPRKey()
			if key == "s" {
				m.prSort = (m.prSort + 1) % len(prSortLabels)
			} else {
				m.prReverse = !m.prReverse
			}
			m.selectPR(selected)
		}
	}
	return m, nil
}
func (m Model) renderPRs() string {
	width, height := max(1, m.width), max(1, m.height)
	if width < 40 || height < 12 {
		return ansi.Truncate("Resize to 40×12 · Esc back · q quit", width, "…")
	}
	if m.prRepoPicker {
		background := m
		background.prRepoPicker = false
		return m.renderPRScopePicker(background.renderPRs(), width, height)
	}
	title := "Pull requests"
	if m.prRepoID != "" {
		title += " / " + m.prScopeLabel()
	}
	prs := m.visiblePRs()
	if m.prDetail && len(prs) > 0 {
		background := m
		background.prDetail = false
		return m.renderPRDetailModal(background.renderPRs(), prs[max(0, min(m.prRow, len(prs)-1))], width, height)
	}
	lines := m.screenHeader(title, width)
	lines = append(lines, m.renderPRBoardRows(prs, width)...)
	if m.prBrowserError != "" {
		lines = append(lines, "  "+m.prBrowserError)
	}
	return screenFinish(lines, m.prFooter(prs), m.prBoardCoverage(), width, height)
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
