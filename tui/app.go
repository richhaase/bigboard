package tui

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	ViewAwareness
)

// Model is the root Bubble Tea model.
type Model struct {
	// Glance navigation is separate from the aggregate statistics view.
	glance          glanceState
	overviewSort    int
	prState         prState
	prProvider      PRProvider
	prGeneration    uint64
	cancelPRs       context.CancelFunc
	showPRs         bool
	prDetail        bool
	prAll           bool
	prRow           int
	prOffset        int
	prBrowserError  string
	prBrowserOpener func(string) error

	showPaths        bool
	pathOffset       int
	areaDefinitions  map[string]stats.WorkAreaDefinition
	areaRepoID       string
	selectedAreaID   string
	selectedRepoID   string
	personID         string
	returnView       ViewMode
	scannedAt        map[string]time.Time
	staleRepos       map[string]bool
	allRecords       []git.CommitRecord
	authors          []stats.AuthorStats
	repositories     []git.Repository
	loadedRepos      []git.Repository
	failedRepos      []string
	scanErrors       map[string]string
	showScanErrors   bool
	scanErrorOffset  int
	excludedRepos    map[string]bool
	overlayExcluded  map[string]bool
	overlayCursor    int
	viewMode         ViewMode
	selectedRow      int
	scrollOffset     int
	filterQuery      string
	searching        bool
	sortAsc          bool
	hideBots         bool
	activeOperative  string
	activeAuthorID   string
	statDetailOffset int
	sortField        stats.SortField
	timeIdx          int
	customRangeDays  int
	rangePicker      rangePickerState
	version          string
	width            int
	height           int
	loading          bool
	refreshing       bool
	quitting         bool
	scanGeneration   uint64
	inFlight         map[string]bool
	err              error
	options          Options
	scanContext      context.Context
	cancelScans      context.CancelFunc
	commands         *commandGroup

	pendingScannedAt  map[string]time.Time
	pendingStaleRepos map[string]bool
	pendingRecords    []git.CommitRecord
	pendingRepos      []git.Repository
	pendingFailed     []string
	pendingScanErrors map[string]string
	pendingRemaining  int
	activeScans       int
	nextRepo          int
	bootLines         []string
}

// RepoLoadedMsg is emitted as each repository finishes scanning, so the loader
// can stream a live scan log instead of blocking on the whole set.
type RepoLoadedMsg struct {
	Generation uint64
	Repository git.Repository
	Records    []git.CommitRecord
	Err        error
}

// DefaultTimeIndex is the TimePresets index used when no since config value is given.
const DefaultTimeIndex = 2

const maxConcurrentRepoScans = 8

// Options controls model behavior without process-wide package state.
type Options struct {
	PRProvider PRProvider

	FuzzyMatching    bool
	IncludeGenerated bool
	AIIdentities     []string
	BotIdentities    []string
	WorkAreas        map[string][]stats.WorkAreaRule
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
		viewMode:      ViewAwareness,
		overviewSort:  1,
		scannedAt:     make(map[string]time.Time),
		staleRepos:    make(map[string]bool),
		repositories:  append([]git.Repository(nil), repositories...),
		sortField:     initialSort,
		timeIdx:       initialTimeIdx,
		loading:       true,
		excludedRepos: normalizeExcluded(repositories, excluded),
		version:       version,
		options:       options,
		scanContext:   scanContext,
		cancelScans:   cancelScans,
		commands:      &commandGroup{},
	}
	m.prProvider = options.PRProvider
	if m.prProvider == nil {
		m.prProvider = defaultPRProvider()
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

func loadRepoCmd(ctx context.Context, repository git.Repository, options Options, generation uint64) tea.Cmd {
	return func() tea.Msg {
		records, err := git.ScanRepository(ctx, repository, git.CollectOptions{
			IncludeGenerated: options.IncludeGenerated,
			AIIdentities:     options.AIIdentities,
		})
		return RepoLoadedMsg{Generation: generation, Repository: repository, Records: records, Err: err}
	}
}

func (m Model) loadCmds() tea.Cmd {
	if m.activeScans == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, m.activeScans)
	for i := range m.activeScans {
		cmds[i] = m.commands.wrap(loadRepoCmd(m.scanContext, m.repositories[i], m.options, m.scanGeneration))
	}
	return tea.Batch(cmds...)
}

func (m *Model) resetPending() {
	m.scanGeneration++
	m.refreshing = true
	m.inFlight = make(map[string]bool)
	for _, repo := range m.repositories[:min(len(m.repositories), maxConcurrentRepoScans)] {
		m.inFlight[repo.ID] = true
	}
	m.pendingScannedAt = make(map[string]time.Time)
	m.pendingStaleRepos = make(map[string]bool)
	m.pendingRemaining = len(m.repositories)
	m.pendingRecords = nil
	m.pendingRepos = nil
	m.pendingFailed = nil
	m.pendingScanErrors = make(map[string]string)
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
	m.inFlight[repository.ID] = true
	return m.commands.wrap(loadRepoCmd(m.scanContext, repository, m.options, m.scanGeneration))
}

func (m *Model) finalizeLoad() {
	initial := m.loading
	sort.Slice(m.pendingRepos, func(i, j int) bool {
		return m.pendingRepos[i].Name < m.pendingRepos[j].Name
	})
	sort.Strings(m.pendingFailed)
	m.allRecords = m.pendingRecords
	m.loadedRepos = m.pendingRepos
	m.failedRepos = m.pendingFailed
	m.scanErrors = m.pendingScanErrors
	// Commit freshness with the snapshot, never advertise new timestamps while
	// still displaying old records from an unfinished batch.
	scannedAt := make(map[string]time.Time)
	maps.Copy(scannedAt, m.scannedAt)
	maps.Copy(scannedAt, m.pendingScannedAt)
	m.scannedAt = scannedAt
	staleRepos := make(map[string]bool)
	maps.Copy(staleRepos, m.staleRepos)
	for id, stale := range m.pendingStaleRepos {
		if stale {
			staleRepos[id] = true
		} else {
			delete(staleRepos, id)
		}
	}
	m.staleRepos = staleRepos
	if len(m.loadedRepos) == 0 && len(m.failedRepos) > 0 {
		m.err = fmt.Errorf("all %d repositories failed to scan", len(m.failedRepos))
	} else {
		m.err = nil
	}
	m.loading = false
	m.refreshing = false
	m.rebuildAreaDefinitions()
	personID := m.personID
	showPaths, pathOffset := m.showPaths, m.pathOffset
	m.recomputeAuthors()
	if !initial {
		// A refresh is not a navigation action. Retain vanished-person/commit
		// intent so the existing detail can explain its empty evidence instead
		// of silently broadening to everybody or closing an inspector.
		m.personID = personID
		m.showPaths, m.pathOffset = showPaths, pathOffset
	}
	if initial {
		m.openSingleRepositoryAreas()
	}
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
	if len(m.repositories) == 0 {
		return nil
	}
	return m.loadCmds()
}

func (m *Model) startLocalRefresh() tea.Cmd {
	if m.quitting || len(m.repositories) == 0 || m.refreshing {
		// Repeated requests join the initial or manual scan already in flight.
		return nil
	}
	m.resetPending()
	return m.loadCmds()
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, nil
	}
	switch msg := msg.(type) {
	case prsLoadedMsg:
		if msg.generation == m.prGeneration {
			m.applyPRResult(msg)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.viewMode == ViewOperative {
			m.clampStatDetailScroll()
		}

	case RepoLoadedMsg:
		if !m.refreshing || msg.Generation != m.scanGeneration || !m.inFlight[msg.Repository.ID] {
			return m, nil
		}
		m.inFlight = maps.Clone(m.inFlight)
		m.pendingScannedAt = maps.Clone(m.pendingScannedAt)
		m.pendingStaleRepos = maps.Clone(m.pendingStaleRepos)
		m.pendingScanErrors = maps.Clone(m.pendingScanErrors)
		delete(m.inFlight, msg.Repository.ID)
		m.activeScans--
		if msg.Err != nil {
			m.pendingFailed = append(m.pendingFailed, msg.Repository.Name)
			m.pendingScanErrors[msg.Repository.ID] = displayText(msg.Err.Error())
			m.pendingStaleRepos[msg.Repository.ID] = true
			for _, repo := range m.loadedRepos {
				if repo.ID == msg.Repository.ID {
					m.pendingRepos = append(m.pendingRepos, repo)
					for _, record := range m.allRecords {
						if record.RepoID == repo.ID {
							m.pendingRecords = append(m.pendingRecords, record)
						}
					}
					break
				}
			}
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.Repository.Name, false))
			}
		} else {
			m.pendingScannedAt[msg.Repository.ID] = time.Now()
			m.pendingStaleRepos[msg.Repository.ID] = false
			m.pendingRecords = append(m.pendingRecords, msg.Records...)
			m.pendingRepos = append(m.pendingRepos, msg.Repository)
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.Repository.Name, true))
			}
		}
		m.pendingRemaining--
		if m.pendingRemaining <= 0 {
			m.finalizeLoad()
			return m, m.startPRRefresh()
		} else {
			return m, m.nextLoadCmd()
		}

	case prBrowserResult:
		if msg.err != nil {
			m.prBrowserError = "Could not open browser: " + displayText(msg.err.Error())
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.rangePicker.open {
		return m.handleRangeKey(msg)
	}
	if m.showScanErrors {
		return m.handleScanErrorKey(msg)
	}
	if msg.String() == "e" && !m.searching && !m.glance.searching && len(m.scanErrors) > 0 {
		m.showScanErrors, m.scanErrorOffset = true, 0
		return m, nil
	}
	if m.showPRs {
		return m.handlePRKey(msg)
	}
	if m.viewMode == ViewAwareness {
		return m.handleAwarenessKey(msg)
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "l":
		if m.viewMode == ViewAggregate {
			m.viewMode = ViewAwareness
			return m, nil
		}
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
		if m.viewMode == ViewAggregate {
			cmd := m.startLocalRefresh()
			return m, cmd
		}

	case "/":
		if m.viewMode == ViewAggregate {
			m.searching = true
			m.filterQuery = ""
			m.selectedRow = 0
			m.scrollOffset = 0
		}

	case "t":
		if !m.loading && m.err == nil && (m.viewMode == ViewAggregate || m.viewMode == ViewOperative) {
			m.openRangePicker()
		}

	case "esc", "left":
		if msg.String() != "esc" && m.viewMode == ViewRepoOverlay {
			return m, nil
		}
		switch m.viewMode {
		case ViewRepoOverlay:
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = m.returnView
			m.recomputeAuthors()
		case ViewOperative:
			m.viewMode = ViewAggregate
			m.activeOperative = ""
			m.activeAuthorID = ""
		default:
			if m.filterQuery != "" {
				m.filterQuery = ""
				m.clampScroll()
			} else if msg.String() == "esc" {
				return m.quit()
			}
		}

	case "up":
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

	case "down":
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

	case "pgup", "pgdown", "home", "end":
		if m.viewMode == ViewOperative {
			m.clampStatDetailScroll()
			switch msg.String() {
			case "pgup":
				m.statDetailOffset -= max(1, m.height-3)
			case "pgdown":
				m.statDetailOffset += max(1, m.height-3)
			case "home":
				m.statDetailOffset = 0
			case "end":
				m.statDetailOffset = len(strings.Split(m.operativeDetailContent(), "\n"))
			}
			m.clampStatDetailScroll()
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

	case "b":
		if m.viewMode == ViewAggregate {
			m.hideBots = !m.hideBots
			m.recomputeAuthors()
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
			m.returnView = ViewAggregate
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

	case "enter", "right":
		if msg.String() != "enter" && m.viewMode != ViewAggregate {
			return m, nil
		}
		switch m.viewMode {
		case ViewRepoOverlay:
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = m.returnView
			m.recomputeAuthors()
		case ViewAggregate:
			disp := m.displayedAuthors()
			if m.selectedRow < len(disp) {
				m.activeOperative = disp[m.selectedRow].Name
				m.activeAuthorID = disp[m.selectedRow].ID
				m.viewMode = ViewOperative
				m.statDetailOffset = 0
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
	duration := TimePresets[m.timeIdx].Duration
	if m.customRangeDays > 0 {
		duration = time.Duration(m.customRangeDays) * 24 * time.Hour
	}
	return stats.FilterByTime(filtered, duration)
}

func (m *Model) recomputeAuthors() {
	selectedID := ""
	if list := m.displayedAuthors(); m.selectedRow >= 0 && m.selectedRow < len(list) {
		selectedID = list[m.selectedRow].ID
	}
	authors := stats.AggregateWithOptions(m.filteredRecords(), stats.AggregateOptions{
		FuzzyMatching: m.options.FuzzyMatching,
		BotIdentities: m.options.BotIdentities,
	})
	if m.hideBots {
		kept := make([]stats.AuthorStats, 0, len(authors))
		for _, a := range authors {
			if !a.Bot {
				kept = append(kept, a)
			}
		}
		authors = kept
	}
	m.authors = authors
	m.normalizeAwarenessFocus()
	m.sortAuthors()
	for i, author := range m.displayedAuthors() {
		if selectedID != "" && author.ID == selectedID {
			m.selectedRow = i
			break
		}
	}
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

const tableChromeLines = 7

func (m Model) failedReposLine() string {
	const maxNames = 3
	names := m.failedRepos
	suffix := ""
	if len(names) > maxNames {
		suffix = fmt.Sprintf(", +%d more", len(names)-maxNames)
		names = names[:maxNames]
	}
	line := fmt.Sprintf("  ⚠ %d repo(s) unreadable: %s%s", len(m.failedRepos), strings.Join(names, ", "), suffix)
	return Truncate(displayText(line)+" · e errors", m.width)
}

func (m Model) tableViewport() int {
	totalCommits, totalAdded, totalRemoved, totalAI := m.aggregateTotals()
	above := []string{RenderHeader(m.width, len(m.loadedRepos), m.excludedRepoCount(), m.version)}
	if len(m.failedRepos) > 0 {
		above = append(above, m.failedReposLine())
	}
	if notice := m.historyNotice(); notice != "" {
		above = append(above, notice)
	}
	above = append(above, "", rangeControl(m.rangeLabel()), "", renderStatBoxes(totalCommits, totalAdded, totalRemoved, totalAI, m.width, m.unknownLineCommits()), "")
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
	idx := -1
	for i, a := range list {
		if (m.activeAuthorID != "" && a.ID == m.activeAuthorID) || (m.activeAuthorID == "" && a.Name == m.activeOperative) {
			idx = i
			break
		}
	}
	if idx == -1 {
		idx = m.selectedRow
	} else {
		idx += delta
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(list)-1 {
		idx = len(list) - 1
	}
	m.statDetailOffset = 0
	m.activeOperative = list[idx].Name
	m.activeAuthorID = list[idx].ID
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
	m.quitting = true
	m.refreshing = false
	if m.cancelPRs != nil {
		m.cancelPRs()
	}
	if m.cancelScans != nil {
		m.cancelScans()
	}
	return m, tea.Quit
}

// View renders the current UI state.
func (m Model) View() string {
	content := m.viewContent()
	width, height := m.width, m.height
	if width < 1 {
		width = lipgloss.Width(content)
	}
	if height < 1 {
		height = lipgloss.Height(content)
	}
	return worklistCanvas(content, max(1, width), max(1, height))
}

func (m Model) viewContent() string {
	if m.showScanErrors {
		return m.renderScanErrors()
	}
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
			StyleDimCyan.Render("  ▐")+StyleHelpKey.Render("q")+StyleDimCyan.Render("▌")+StyleHelpDesc.Render("quit · R retry · e errors"),
		)
		if details := m.scanErrorLines(); len(details) > 0 {
			lines = append(lines, "", details[0])
		}
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	if m.rangePicker.open {
		return m.renderRangePicker()
	}
	if m.showPRs {
		return m.renderPRs()
	}
	if m.viewMode == ViewAwareness {
		return m.renderAwareness()
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

	if notice := m.historyNotice(); notice != "" {
		sections = append(sections, StyleAmber.Render(notice))
	}
	sections = append(sections, rangeControl(m.rangeLabel()))
	sections = append(sections, "")

	totalCommits, totalAdded, totalRemoved, totalAI := m.aggregateTotals()
	sections = append(sections, renderStatBoxes(totalCommits, totalAdded, totalRemoved, totalAI, m.width, m.unknownLineCommits()))
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
	botsLabel := "on"
	if m.hideBots {
		botsLabel = "off"
	}
	sections = append(sections, StyleCyan.Render("  l worklist"), RenderHelpBar(HelpContext{View: "aggregate", Sort: sortLabel, Bots: botsLabel}))

	return strings.Join(sections, "\n")
}

func (m Model) operativeDetailContent() string {
	var as *stats.AuthorStats
	for i := range m.authors {
		if (m.activeAuthorID != "" && m.authors[i].ID == m.activeAuthorID) || (m.activeAuthorID == "" && m.authors[i].Name == m.activeOperative) {
			as = &m.authors[i]
			break
		}
	}

	filtered := m.filteredRecords()
	name := m.activeOperative
	if as != nil {
		name = as.Name
	} else if m.activeAuthorID != "" {
		// A selected identity with no activity in range must not fall back to
		// a different person who happens to have the same display name.
		filtered = nil
	}

	detail := OperativeView{FuzzyMatching: m.options.FuzzyMatching, RangeLabel: m.rangeLabel()}.RenderOperativeDetail(
		name,
		as,
		filtered,
		m.width,
		m.timeIdx,
		len(m.loadedRepos),
		m.excludedRepoCount(),
	)
	return detail
}

func (m *Model) clampStatDetailScroll() {
	lines := strings.Split(m.operativeDetailContent(), "\n")
	m.statDetailOffset = max(0, min(m.statDetailOffset, len(lines)-max(1, m.height-2)))
}

func (m Model) renderOperativeView() string {
	if m.height > 0 && m.height < 3 {
		return fitGlanceLines([]string{"Resize terminal · Esc back · q quit"}, max(1, m.width), m.height)
	}
	detail := m.operativeDetailContent()
	help := RenderHelpBar(HelpContext{View: "operative"})
	if m.width > 0 && m.width < 100 {
		help = StyleHelpKey.Render("  PgUp/PgDn scroll · ↑↓ person · ←/Esc back · t range")
	}
	if m.width > 0 && m.width < 60 {
		help = StyleHelpKey.Render("  PgUp/PgDn · ← back · t range")
	}
	lines := strings.Split(detail, "\n")
	if m.height <= 0 || len(lines)+2 <= m.height {
		return strings.Join([]string{detail, "", help}, "\n")
	}
	// Keep navigation visible while paging the full detail, including the
	// timeline and matrix. Arrow keys continue switching contributors.
	budget := max(1, m.height-2)
	offset := max(0, min(m.statDetailOffset, len(lines)-budget))
	end := min(len(lines), offset+budget)
	visible := append([]string(nil), lines[offset:end]...)
	visible = append(visible, StyleDimWhite.Render(fmt.Sprintf("  %s · lines %d–%d/%d", displayText(m.activeOperative), offset+1, end, len(lines))), help)
	return fitGlanceLines(visible, max(1, m.width), m.height)
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

func (m Model) unknownLineCommits() int {
	n := 0
	for _, a := range m.authors {
		n += a.UnknownLineCommits
	}
	return n
}

func (m Model) historyNotice() string {
	for _, r := range m.allRecords {
		if r.LinesUnknown && !m.excludedRepos[r.RepoID] {
			return Truncate("  ⚠ Shallow history; ? marks unknown line counts.", m.width)
		}
	}
	return ""
}
