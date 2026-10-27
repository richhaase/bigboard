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
	CurrentLogin(context.Context) (string, error)
	Fetch(context.Context, string) gh.Result
}

func defaultPRProvider() PRProvider { return gh.NewProvider(nil) }

type prSnapshot struct {
	prs      []gh.PullRequest
	checked  time.Time
	lastGood time.Time
	partial  bool
	err      error
	filesErr error
}
type prState struct {
	aliases           map[string]string
	repositories      map[string]string
	errors            map[string]error
	snapshots         map[string]prSnapshot
	loading           bool
	nextRefresh       time.Time
	login             string
	loginErr          error
	accountRequests   int
	inventoryRequests int
	fileRequests      int
	requestBudget     int
}
type prsLoadedMsg struct {
	aliases           map[string]string
	generation        uint64
	repositories      map[string]string
	errors            map[string]error
	results           map[string]gh.Result
	checked           time.Time
	login             string
	loginErr          error
	accountRequests   int
	inventoryRequests int
	fileRequests      int
	requestBudget     int
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
	budget := m.options.PRRequestBudget
	if budget <= 0 {
		budget = gh.MaxRefreshRequests
	}
	ctx = gh.WithRequestBudget(ctx, budget)
	m.cancelPRs = cancel
	m.prState.loading = true
	generation := m.prGeneration
	var repos []git.Repository
	for _, repo := range m.local.repositories {
		if !m.excludedRepos[repo.ID] {
			repos = append(repos, repo)
		}
	}
	provider := m.prProvider
	snapshots := maps.Clone(m.prState.snapshots)
	aliases := maps.Clone(m.prState.aliases)
	if aliases == nil {
		aliases = make(map[string]string)
	}
	return m.commands.wrap(func() tea.Msg {
		defer cancel()
		msg := prsLoadedMsg{generation: generation, repositories: map[string]string{}, errors: map[string]error{}, results: map[string]gh.Result{}}
		msg.aliases = aliases
		msg.requestBudget = budget
		var requests []gh.RepositoryRequest
		seen := make(map[string]bool)
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
			aliases[origin] = remote
			msg.repositories[repo.ID] = remote
			if !seen[remote] {
				seen[remote] = true
				requests = append(requests, gh.RepositoryRequest{Repo: remote, Previous: snapshots[remote].prs})
			}
		}
		var results []gh.Result
		if len(requests) > 0 {
			before := gh.RequestCount(ctx)
			msg.login, msg.loginErr = provider.CurrentLogin(ctx)
			msg.accountRequests = gh.RequestCount(ctx) - before
			if msg.loginErr != nil {
				msg.login = ""
			} else if msg.login == "" {
				msg.loginErr = gh.ErrResponse
			}
		}
		if errors.Is(msg.loginErr, gh.ErrAuthentication) || errors.Is(msg.loginErr, gh.ErrUnavailable) {
			for _, request := range requests {
				results = append(results, gh.Result{Repo: request.Repo, Err: msg.loginErr})
			}
		} else {
			results = fetchPRRepositories(ctx, provider, requests)
		}
		for i, result := range results {
			remote := requests[i].Repo
			canonical, err := gh.CanonicalRepo(result.Repo)
			if err != nil {
				canonical = remote
			}
			aliases[remote] = canonical
			msg.results[canonical] = result
			msg.inventoryRequests += result.InventoryRequests
			msg.fileRequests += result.FileRequests
		}
		for id, remote := range msg.repositories {
			if canonical := aliases[remote]; canonical != "" {
				msg.repositories[id] = canonical
			}
		}
		msg.checked = time.Now()
		return msg
	})
}
func fetchPRRepositories(ctx context.Context, provider PRProvider, requests []gh.RepositoryRequest) []gh.Result {
	if batch, ok := provider.(interface {
		FetchAll(context.Context, []gh.RepositoryRequest) []gh.Result
	}); ok {
		return batch.FetchAll(ctx, requests)
	}
	results := make([]gh.Result, 0, len(requests))
	known := make(map[string]gh.Result)
	var unavailable error
	for _, request := range requests {
		if previous, ok := known[request.Repo]; ok {
			previous.InventoryRequests, previous.FileRequests = 0, 0
			results = append(results, previous)
			continue
		}
		result := gh.Result{Repo: request.Repo, Err: unavailable}
		if unavailable == nil {
			result = provider.Fetch(ctx, request.Repo)
			if errors.Is(result.Err, gh.ErrAuthentication) || errors.Is(result.Err, gh.ErrUnavailable) || errors.Is(result.Err, gh.ErrRateLimited) {
				unavailable = result.Err
			}
			if result.RetryAt.After(time.Now()) {
				unavailable = gh.ErrRateLimited
			}
		}
		known[request.Repo], known[result.Repo] = result, result
		results = append(results, result)
	}
	return results
}

func (m *Model) applyPRResult(msg prsLoadedMsg) {
	selectedKey := ""
	oldVisible := m.visiblePRs()
	if m.prRow >= 0 && m.prRow < len(oldVisible) {
		selectedKey = oldVisible[m.prRow].Key()
	}
	m.prState.loading = false
	m.prState.login, m.prState.loginErr = msg.login, msg.loginErr
	m.prState.accountRequests = msg.accountRequests
	m.prState.inventoryRequests = msg.inventoryRequests
	m.prState.fileRequests = msg.fileRequests
	m.prState.requestBudget = msg.requestBudget
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
		inventoryErr := result.InventoryErr
		if inventoryErr == nil && result.FilesErr == nil {
			inventoryErr = result.Err
		}
		next := prSnapshot{checked: msg.checked, lastGood: old.lastGood, partial: !result.Complete, err: inventoryErr, filesErr: result.FilesErr}
		kept := make(map[string]gh.PullRequest)
		for _, pr := range old.prs {
			if !result.Complete {
				kept[pr.Key()] = pr
			}
		}
		// Inventory completeness is independent of supporting context. A
		// complete open inventory removes closed PRs even if files are partial.
		for _, pr := range result.PRs {
			kept[pr.Key()] = pr
		}
		for _, pr := range kept {
			next.prs = append(next.prs, pr)
		}
		if result.Complete && inventoryErr == nil {
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
		definition = stats.DefineWorkAreas(m.recordsInRepository(repo, m.local.records), m.rulesForRepository(repo))
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
		for _, pr := range m.prsInViewForRepository(repo.ID) {
			if scope == repo.ID {
				prs = append(prs, pr)
				continue
			}
			if strings.HasPrefix(scope, "dir:") || strings.HasPrefix(scope, "file:") || scope == "unknown" || scope == "empty" {
				if m.prTouchesLiteralScope(pr, scope) {
					prs = append(prs, pr)
				}
				continue
			}
			for _, area := range m.prAreas(repo, pr) {
				if area.ID == scope {
					prs = append(prs, pr)
					break
				}
			}
		}
		return prs
	}
	return m.prsInViewForRepository(scope)
}
func (m Model) allPRs() []gh.PullRequest {
	unique := map[string]gh.PullRequest{}
	for _, repo := range m.local.repositories {
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
	prs := m.sharedPRs()
	kept := prs[:0]
	query := strings.ToLower(m.prQuery)
	for _, pr := range prs {
		if !m.prFilters.matches(pr, m.prState.login) {
			continue
		}
		if m.prRepoID != "" && (!m.repoInView(m.prRepoID) || pr.Repo != m.prState.repositories[m.prRepoID]) {
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
func (m Model) handlePRKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.openFilters("repos")
	case "u":
		m.openFilters("author")
	case "v":
		m.openFilters("review")
	case "k":
		m.openFilters("checks")
	case "d":
		m.openFilters("state")
	case "x":
		m.clearBoardFilters()
	case "esc", "left":
		if m.prDetail {
			m.prDetail, m.prOffset = false, 0
		} else if m.prContext.active {
			m.closeContextPRs(true)
		}
	case "enter", "right":
		if len(m.visiblePRs()) > 0 {
			m.prDetail = true
		}
	case "/":
		if !m.prDetail {
			m.prSearchBefore = m.prQuery
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
	title := "Pull requests"
	if m.prContext.active {
		title = "PRs from Activity"
	}
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
	return screenFinish(lines, m.prFooter(prs), m.copyCoverage(m.prBoardCoverage()), width, height)
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
