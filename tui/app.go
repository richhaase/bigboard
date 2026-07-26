package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// ViewMode controls which screen is displayed.
type ViewMode int

const (
	ViewAggregate ViewMode = iota
	ViewOperative
	ViewRepoOverlay
)

// Model is the root Bubble Tea model.
type Model struct {
	allRecords      []git.CommitRecord
	authors         []stats.AuthorStats
	repositories    []git.Repository
	loadedRepos     []git.Repository
	failedRepos     []string
	excludedRepos   map[string]bool
	overlayExcluded map[string]bool
	overlayCursor   int
	viewMode        ViewMode
	selectedRow     int
	scrollOffset    int
	filterQuery     string
	searching       bool
	sortAsc         bool
	activeOperative string
	sortField       stats.SortField
	timeIdx         int
	version         string
	width           int
	height          int
	loading         bool
	err             error
	options         Options
	scanContext     context.Context
	cancelScans     context.CancelFunc

	pendingRecords   []git.CommitRecord
	pendingRepos     []git.Repository
	pendingFailed    []string
	pendingRemaining int
	activeScans      int
	nextRepo         int
	bootLines        []string
}

// RepoLoadedMsg is emitted as each repository finishes scanning, so the loader
// can stream a live scan log instead of blocking on the whole set.
type RepoLoadedMsg struct {
	Repository git.Repository
	Records    []git.CommitRecord
	Err        error
}

// DefaultTimeIndex is the TimePresets index used when no --since/since is given.
const DefaultTimeIndex = 2

const maxConcurrentRepoScans = 8

// Options controls model behavior without process-wide package state.
type Options struct {
	FuzzyMatching    bool
	IncludeGenerated bool
}

// NewModel creates an initial Model ready to display the loading state.
func NewModel(repoPaths []string, initialSort stats.SortField, excluded map[string]bool, version string, initialTimeIdx int) Model {
	options := Options{
		FuzzyMatching:    stats.FuzzyMatching,
		IncludeGenerated: !git.FilterGeneratedPaths,
	}
	return NewModelWithOptions(git.NewRepositories(repoPaths), initialSort, excluded, version, initialTimeIdx, options)
}

// NewModelWithOptions creates a model from explicit repository identities and
// scan options.
func NewModelWithOptions(repositories []git.Repository, initialSort stats.SortField, excluded map[string]bool, version string, initialTimeIdx int, options Options) Model {
	if initialTimeIdx < 0 || initialTimeIdx >= len(TimePresets) {
		initialTimeIdx = DefaultTimeIndex
	}
	scanContext, cancelScans := context.WithCancel(context.Background())
	m := Model{
		repositories:  append([]git.Repository(nil), repositories...),
		sortField:     initialSort,
		timeIdx:       initialTimeIdx,
		loading:       true,
		excludedRepos: normalizeExcluded(repositories, excluded),
		version:       version,
		options:       options,
		scanContext:   scanContext,
		cancelScans:   cancelScans,
	}
	m.resetPending()
	if len(repositories) == 0 {
		m.finalizeLoad()
	}
	return m
}

func normalizeExcluded(repositories []git.Repository, excluded map[string]bool) map[string]bool {
	normalized := make(map[string]bool)
	for key, value := range excluded {
		if value {
			normalized[key] = true
		}
	}
	for _, repo := range repositories {
		if excluded[repo.Name] || excluded[filepath.Base(repo.Path)] {
			normalized[repo.ID] = true
		}
	}
	for _, repo := range repositories {
		if repo.Name != repo.ID {
			delete(normalized, repo.Name)
		}
		if base := filepath.Base(repo.Path); base != repo.ID {
			delete(normalized, base)
		}
	}
	return normalized
}

func loadRepoCmd(ctx context.Context, repository git.Repository, options Options) tea.Cmd {
	return func() tea.Msg {
		records, err := git.ScanRepository(ctx, repository, git.CollectOptions{
			IncludeGenerated: options.IncludeGenerated,
		})
		return RepoLoadedMsg{Repository: repository, Records: records, Err: err}
	}
}

func (m Model) loadCmds() tea.Cmd {
	if m.activeScans == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, m.activeScans)
	for i := range m.activeScans {
		cmds[i] = loadRepoCmd(m.scanContext, m.repositories[i], m.options)
	}
	return tea.Batch(cmds...)
}

func (m *Model) resetPending() {
	m.pendingRemaining = len(m.repositories)
	m.pendingRecords = nil
	m.pendingRepos = nil
	m.pendingFailed = nil
	m.bootLines = nil
	m.activeScans = min(len(m.repositories), maxConcurrentRepoScans)
	m.nextRepo = m.activeScans
}

func (m *Model) nextLoadCmd() tea.Cmd {
	if m.nextRepo >= len(m.repositories) {
		return nil
	}
	repository := m.repositories[m.nextRepo]
	m.nextRepo++
	m.activeScans++
	return loadRepoCmd(m.scanContext, repository, m.options)
}

func (m *Model) finalizeLoad() {
	sort.Slice(m.pendingRepos, func(i, j int) bool {
		return m.pendingRepos[i].Name < m.pendingRepos[j].Name
	})
	sort.Strings(m.pendingFailed)
	m.allRecords = m.pendingRecords
	m.loadedRepos = m.pendingRepos
	m.failedRepos = m.pendingFailed
	if len(m.loadedRepos) == 0 && len(m.failedRepos) > 0 {
		m.err = fmt.Errorf("all %d repositories failed to scan", len(m.failedRepos))
	} else {
		m.err = nil
	}
	m.loading = false
	m.recomputeAuthors()
}

func bootLine(repo string, ok bool) string {
	repo = displayText(repo)
	if ok {
		return "  " + StyleGreen.Render("▸ ") + StyleAuthor.Render(repo) + StyleGreen.Render("  ✓")
	}
	red := lipgloss.NewStyle().Foreground(ColorRed)
	return "  " + red.Render("▸ ") + StyleAuthor.Render(repo) + red.Render("  ✗ unreadable")
}

// Init kicks off the initial concurrent load.
func (m Model) Init() tea.Cmd {
	return m.loadCmds()
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case RepoLoadedMsg:
		m.activeScans--
		if msg.Err != nil {
			m.pendingFailed = append(m.pendingFailed, msg.Repository.Name)
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.Repository.Name, false))
			}
		} else {
			m.pendingRecords = append(m.pendingRecords, msg.Records...)
			m.pendingRepos = append(m.pendingRepos, msg.Repository)
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.Repository.Name, true))
			}
		}
		m.pendingRemaining--
		if m.pendingRemaining <= 0 {
			m.finalizeLoad()
		} else {
			return m, m.nextLoadCmd()
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m.quit()

	case "q":
		if m.viewMode == ViewAggregate && m.filterQuery != "" {
			m.filterQuery = ""
			m.clampScroll()
			return m, nil
		}
		return m.quit()

	case "R":
		if m.viewMode == ViewAggregate && !m.loading {
			m.loading = true
			m.resetPending()
			return m, m.loadCmds()
		}

	case "/":
		if m.viewMode == ViewAggregate {
			m.searching = true
			m.filterQuery = ""
			m.selectedRow = 0
			m.scrollOffset = 0
		}

	case "esc":
		switch m.viewMode {
		case ViewRepoOverlay:
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
		case ViewOperative:
			m.viewMode = ViewAggregate
			m.activeOperative = ""
		default:
			if m.filterQuery != "" {
				m.filterQuery = ""
				m.clampScroll()
			} else {
				return m.quit()
			}
		}

	case "up", "k":
		switch m.viewMode {
		case ViewRepoOverlay:
			if m.overlayCursor > 0 {
				m.overlayCursor--
			}
		case ViewOperative:
			m.stepOperative(-1)
		default:
			if m.selectedRow > 0 {
				m.selectedRow--
				m.clampScroll()
			}
		}

	case "down", "j":
		switch m.viewMode {
		case ViewRepoOverlay:
			if m.overlayCursor < len(m.loadedRepos)-1 {
				m.overlayCursor++
			}
		case ViewOperative:
			m.stepOperative(1)
		default:
			if m.selectedRow < len(m.displayedAuthors())-1 {
				m.selectedRow++
				m.clampScroll()
			}
		}

	case "left", "h":
		if m.viewMode == ViewAggregate || m.viewMode == ViewOperative {
			if m.timeIdx > 0 {
				m.timeIdx--
				m.recomputeAuthors()
			}
		}

	case "right", "l":
		if m.viewMode == ViewAggregate || m.viewMode == ViewOperative {
			if m.timeIdx < len(TimePresets)-1 {
				m.timeIdx++
				m.recomputeAuthors()
			}
		}

	case "s":
		if m.viewMode == ViewAggregate {
			m.sortField = stats.NextSortField(m.sortField)
			m.sortAuthors()
			m.selectedRow = 0
			m.scrollOffset = 0
		}

	case "S":
		if m.viewMode == ViewAggregate {
			m.sortAsc = !m.sortAsc
			m.sortAuthors()
			m.selectedRow = 0
			m.scrollOffset = 0
		}

	case "r":
		if m.viewMode == ViewAggregate {
			m.overlayExcluded = make(map[string]bool)
			for k, v := range m.excludedRepos {
				m.overlayExcluded[k] = v
			}
			m.overlayCursor = 0
			m.viewMode = ViewRepoOverlay
		}

	case " ":
		if m.viewMode == ViewRepoOverlay && m.overlayCursor < len(m.loadedRepos) {
			id := m.loadedRepos[m.overlayCursor].ID
			if m.overlayExcluded[id] {
				delete(m.overlayExcluded, id)
			} else {
				m.overlayExcluded[id] = true
			}
		}

	case "enter":
		switch m.viewMode {
		case ViewRepoOverlay:
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
		case ViewAggregate:
			disp := m.displayedAuthors()
			if m.selectedRow < len(disp) {
				m.activeOperative = disp[m.selectedRow].Name
				m.viewMode = ViewOperative
			}
		}
	}

	return m, nil
}

func (m *Model) filteredRecords() []git.CommitRecord {
	filtered := stats.FilterByRepo(m.allRecords, m.excludedRepos)
	if m.timeIdx < 0 || m.timeIdx >= len(TimePresets) {
		m.timeIdx = DefaultTimeIndex
	}
	return stats.FilterByTime(filtered, TimePresets[m.timeIdx].Duration)
}

func (m *Model) recomputeAuthors() {
	m.authors = stats.AggregateWithOptions(m.filteredRecords(), stats.AggregateOptions{
		FuzzyMatching: m.options.FuzzyMatching,
	})
	m.sortAuthors()
	m.clampScroll()
}

func (m *Model) sortAuthors() {
	stats.Sort(m.authors, m.sortField)
	if m.sortAsc {
		for i, j := 0, len(m.authors)-1; i < j; i, j = i+1, j-1 {
			m.authors[i], m.authors[j] = m.authors[j], m.authors[i]
		}
	}
}

func (m Model) displayedAuthors() []stats.AuthorStats {
	if m.filterQuery == "" {
		return m.authors
	}
	q := strings.ToLower(m.filterQuery)
	out := make([]stats.AuthorStats, 0, len(m.authors))
	for _, a := range m.authors {
		if strings.Contains(strings.ToLower(a.Name), q) {
			out = append(out, a)
		}
	}
	return out
}

const tableChromeLines = 6

func (m Model) failedReposLine() string {
	const maxNames = 3
	names := m.failedRepos
	suffix := ""
	if len(names) > maxNames {
		suffix = fmt.Sprintf(", +%d more", len(names)-maxNames)
		names = names[:maxNames]
	}
	line := fmt.Sprintf("  ⚠ %d repo(s) unreadable: %s%s", len(m.failedRepos), strings.Join(names, ", "), suffix)
	return Truncate(line, m.width)
}

func (m Model) tableViewport() int {
	totalCommits, totalAdded, totalRemoved, totalAI := m.aggregateTotals()
	above := []string{RenderHeader(m.width, len(m.loadedRepos), m.excludedRepoCount(), m.version)}
	if len(m.failedRepos) > 0 {
		above = append(above, m.failedReposLine())
	}
	above = append(above, "", RenderTimePicker(m.timeIdx), "", RenderStatBoxes(totalCommits, totalAdded, totalRemoved, totalAI, m.width), "")
	help := RenderHelpBar(HelpContext{View: "aggregate"})
	budget := m.height - lipgloss.Height(strings.Join(above, "\n")) - lipgloss.Height(help) - tableChromeLines
	if budget < 3 {
		budget = 3
	}
	return budget
}

func (m *Model) clampScroll() {
	n := len(m.displayedAuthors())
	if m.selectedRow > n-1 {
		m.selectedRow = n - 1
	}
	if m.selectedRow < 0 {
		m.selectedRow = 0
	}
	visible := m.tableViewport()
	if m.selectedRow < m.scrollOffset {
		m.scrollOffset = m.selectedRow
	}
	if m.selectedRow >= m.scrollOffset+visible {
		m.scrollOffset = m.selectedRow - visible + 1
	}
	if maxOffset := n - visible; m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m *Model) stepOperative(delta int) {
	list := m.displayedAuthors()
	if len(list) == 0 {
		return
	}
	idx := 0
	for i, a := range list {
		if a.Name == m.activeOperative {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx > len(list)-1 {
		idx = len(list) - 1
	}
	m.activeOperative = list[idx].Name
	m.selectedRow = idx
	m.clampScroll()
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		m.searching = false
	case "esc":
		m.searching = false
		m.filterQuery = ""
	case "backspace":
		if r := []rune(m.filterQuery); len(r) > 0 {
			m.filterQuery = string(r[:len(r)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.filterQuery += string(msg.Runes)
		}
	}
	m.selectedRow = 0
	m.scrollOffset = 0
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.cancelScans != nil {
		m.cancelScans()
	}
	return m, tea.Quit
}

// View renders the current UI state.
func (m Model) View() string {
	if m.loading {
		return m.renderBootSequence()
	}

	if m.err != nil {
		lines := renderBanner(m.width)
		lines = append(lines,
			"",
			lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render("  ◈ ERROR"),
			"",
			lipgloss.NewStyle().Foreground(ColorRed).Render("  "+displayText(m.err.Error())),
			"",
			StyleDimCyan.Render("  ▐")+StyleHelpKey.Render("q")+StyleDimCyan.Render("▌")+StyleHelpDesc.Render("quit"),
		)
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	if m.viewMode == ViewRepoOverlay {
		return m.renderRepoOverlay()
	}

	if m.viewMode == ViewOperative {
		return m.renderOperativeView()
	}

	return m.renderAggregateView()
}

func (m Model) renderBootSequence() string {
	lines := renderBanner(m.width)
	lines = append(lines, "", StyleSubtitle.Render("  ◈ SCANNING REPOSITORIES"), "")

	done := len(m.bootLines)
	total := done + m.pendingRemaining

	const maxShown = 14
	shown := m.bootLines
	if len(shown) > maxShown {
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  … %d earlier", len(shown)-maxShown)))
		shown = shown[len(shown)-maxShown:]
	}
	lines = append(lines, shown...)
	lines = append(lines, "", StyleDimCyan.Render(fmt.Sprintf("  ▐ %d/%d repos ▌", done, total)))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderAggregateView() string {
	var sections []string

	sections = append(sections, RenderHeader(m.width, len(m.loadedRepos), m.excludedRepoCount(), m.version))

	if len(m.failedRepos) > 0 {
		sections = append(sections, StyleAmber.Render(m.failedReposLine()))
	}
	sections = append(sections, "")

	sections = append(sections, RenderTimePicker(m.timeIdx))
	sections = append(sections, "")

	totalCommits, totalAdded, totalRemoved, totalAI := m.aggregateTotals()
	sections = append(sections, RenderStatBoxes(totalCommits, totalAdded, totalRemoved, totalAI, m.width))
	sections = append(sections, "")

	sections = append(sections, AggregateView{}.RenderTable(m.displayedAuthors(), TableState{
		SelectedRow:  m.selectedRow,
		ScrollOffset: m.scrollOffset,
		VisibleRows:  m.tableViewport(),
		SortField:    m.sortField,
		SortAsc:      m.sortAsc,
		Width:        m.width,
		Searching:    m.searching,
		Query:        m.filterQuery,
	}))

	sections = append(sections, "")
	sortLabel := strings.ToLower(stats.SortFieldLabel(m.sortField))
	sections = append(sections, RenderHelpBar(HelpContext{View: "aggregate", Sort: sortLabel}))

	return strings.Join(sections, "\n")
}

func (m Model) renderOperativeView() string {
	var as *stats.AuthorStats
	for i := range m.authors {
		if m.authors[i].Name == m.activeOperative {
			as = &m.authors[i]
			break
		}
	}

	filtered := m.filteredRecords()

	detail := OperativeView{FuzzyMatching: m.options.FuzzyMatching}.RenderOperativeDetail(
		m.activeOperative,
		as,
		filtered,
		m.width,
		m.timeIdx,
		len(m.loadedRepos),
		m.excludedRepoCount(),
	)
	helpBar := RenderHelpBar(HelpContext{View: "operative"})
	return strings.Join([]string{detail, "", helpBar}, "\n")
}

func (m Model) aggregateTotals() (commits, added, removed, ai int) {
	for _, author := range m.authors {
		commits += author.Commits
		added += author.Added
		removed += author.Removed
		ai += author.AICommits
	}
	return commits, added, removed, ai
}

func (m Model) excludedRepoCount() int {
	count := 0
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			count++
		}
	}
	return count
}
