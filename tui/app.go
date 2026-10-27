package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
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
	settings           settingsState
	settingsContext    context.Context
	cancelSettings     context.CancelFunc
	settingsGeneration uint64
	cacheGeneration    uint64
	cacheInfo          git.CacheInfo
	cacheError         string
	cacheRemoveConfirm string
	cacheRemoving      bool
	copyStatus         string
	// Glance navigation is separate from the aggregate statistics view.
	glance             glanceState
	overviewSort       int
	overviewReverse    bool
	repositorySort     int
	directory          directoryState
	contributorPane    activityContributorPane
	filters            sharedFilterState
	identityMerge      identityMergeState
	sharedRepos        filterValues
	activityPeople     filterValues
	activityHideBots   bool
	contributorsView   ViewMode
	searchBefore       string
	prSearchBefore     string
	glanceSearchBefore string
	prContext          prContextState
	prState            prState
	prProvider         PRProvider
	prGeneration       uint64
	cancelPRs          context.CancelFunc
	showPRs            bool
	prDetail           bool
	prRepoID           string
	prFilters          prFilters
	prFilterKind       string
	prFilterQuery      string
	prQuery            string
	prSearching        bool
	prSort             int
	prReverse          bool
	overlayPRs         bool
	prRow              int
	prOffset           int
	prBrowserError     string
	commitBrowserError string
	prBrowserOpener    func(string) error

	showPaths          bool
	showHelp           bool
	helpOffset         int
	pathOffset         int
	areaDefinitions    map[string]stats.WorkAreaDefinition
	areaRepoID         string
	selectedAreaID     string
	selectedRepoID     string
	personID           string
	returnView         ViewMode
	local              localSnapshot
	scan               scanBatch
	startup            startupProgress
	bookmarkSeenAt     time.Time
	refreshAnimation   refreshAnimation
	authors            []stats.AuthorStats
	repositories       []git.Repository
	showScanErrors     bool
	scanErrorOffset    int
	scanErrorDetails   bool
	excludedRepos      map[string]bool
	overlayExcluded    map[string]bool
	overlayCursor      int
	viewMode           ViewMode
	selectedRow        int
	scrollOffset       int
	filterQuery        string
	searching          bool
	sortAsc            bool
	hideBots           bool
	activeOperative    string
	activeAuthorID     string
	statDetailOffset   int
	sortField          stats.SortField
	timeIdx            int
	customRangeDays    int
	sinceLastLookRange bool
	version            string
	width              int
	height             int
	loading            bool
	quitting           bool
	err                error
	options            Options
	scanContext        context.Context
	cancelScans        context.CancelFunc
	localScanContext   context.Context
	cancelLocalScans   context.CancelFunc
	identityReselect   string
	commands           *commandGroup

	bootLines []string
}

// RepoLoadedMsg is emitted as each repository finishes scanning, so the loader
// can stream a live scan log instead of blocking on the whole set.
type RepoLoadedMsg struct {
	Generation uint64
	Repository git.Repository
	Records    []git.CommitRecord
	Err        error
	FetchErr   error
	Revision   string
	Skipped    bool
	Retained   bool
}

// DefaultTimeIndex is the TimePresets index used when no since config value is given.
const DefaultTimeIndex = 2

const maxConcurrentRepoScans = 8

// Options controls model behavior without process-wide package state.
type Options struct {
	Bookmark               *BookmarkOptions
	Settings               *SettingsOptions
	PrepareRepository      func(context.Context, git.Repository) error
	RepositoryRevision     func(context.Context, git.Repository) (string, error)
	ScanRepository         func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error)
	CacheInfo              func() (git.CacheInfo, error)
	RemoveCachedRepository func(string) error
	PRProvider             PRProvider
	PRRequestBudget        int

	FuzzyMatching    bool
	IncludeGenerated bool
	AIIdentities     []string
	BotIdentities    []string
	Identities       map[string]git.Identity
	SaveIdentities   func(map[string]git.Identity) error
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
		local:         localSnapshot{scannedAt: make(map[string]time.Time), staleRepos: make(map[string]bool)},
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
	if options.RepositoryRevision != nil && len(repositories) > 0 {
		m.startup.pending = true
		m.scan.phase = scanCached
	}
	if len(repositories) == 0 {
		m.finalizeLoad()
	}
	if options.Settings != nil && options.Settings.Required {
		m.prepareSettings(true)
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

func loadRepoCmd(ctx context.Context, repository git.Repository, options Options, generation uint64, fetch bool) tea.Cmd {
	return func() tea.Msg {
		var fetchErr error
		if fetch {
			if options.PrepareRepository != nil {
				fetchErr = options.PrepareRepository(ctx, repository)
			} else {
				fetchErr = git.FetchRepository(ctx, repository)
			}
		}
		scan := options.ScanRepository
		if scan == nil {
			scan = git.ScanRepository
		}
		records, err := scan(ctx, repository, git.CollectOptions{
			IncludeGenerated: options.IncludeGenerated,
			AIIdentities:     options.AIIdentities,
			Identities:       options.Identities,
		})
		return RepoLoadedMsg{Generation: generation, Repository: repository, Records: records, Err: err, FetchErr: fetchErr}
	}
}

func (m Model) loadCmds() tea.Cmd {
	if m.scan.active == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, m.scan.active)
	for i := range m.scan.active {
		cmds[i] = m.repoLoadCmd(m.repositories[i])
	}
	return tea.Batch(cmds...)
}

func (m *Model) resetPending() {
	if m.cancelLocalScans != nil {
		m.cancelLocalScans()
	}
	parent := m.scanContext
	if parent == nil {
		parent = context.Background()
	}
	m.localScanContext, m.cancelLocalScans = context.WithCancel(parent)
	m.scan = newScanBatch(m.repositories, m.scan.generation+1, m.loading)
	m.bootLines = nil
}

func (m *Model) nextLoadCmd() tea.Cmd {
	repository, ok := m.scan.scheduleNext(m.repositories)
	if !ok {
		return nil
	}
	return m.repoLoadCmd(repository)
}

func (m Model) repoLoadCmd(repository git.Repository) tea.Cmd {
	ctx := m.localScanContext
	if ctx == nil {
		ctx = m.scanContext
	}
	if m.scan.phase != scanDefault || (m.scan.localOnly && m.startup.pending) {
		return m.commands.wrap(m.startupRepoCmd(ctx, repository))
	}
	return m.commands.wrap(loadRepoCmd(ctx, repository, m.options, m.scan.generation, !m.scan.localOnly && !m.excludedRepos[repository.ID]))
}

func (m *Model) finalizeLoad() {
	snapshot, ready := m.scan.publish(m.local)
	if !ready {
		return
	}
	initial := m.loading
	m.local = snapshot
	committedAt := time.Now()
	if m.scan.phase == scanCached {
		m.startup.cachedAt = committedAt
	}
	if m.scan.phase == scanStartupFetch {
		m.startup.fetchedAt = committedAt
		m.startup.pending = false
	}
	if m.scan.phase != scanCached && !m.scan.localOnly && len(snapshot.repositories) > 0 {
		m.bookmarkSeenAt = committedAt
	}
	m.err = m.local.loadError()
	m.loading = false
	m.rebuildAreaDefinitions()
	personID := m.personID
	showPaths, pathOffset := m.showPaths, m.pathOffset
	m.recomputeAuthors()
	if m.identityReselect != "" {
		m.selectContributor(m.identityReselect)
		m.identityReselect = ""
	}
	m.normalizeContributorDetail()
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
	if m.settings.open && m.settings.required {
		return m.settingsAccountCmd()
	}
	if len(m.repositories) == 0 {
		return m.cacheInfoCmd()
	}
	return tea.Batch(m.loadCmds(), m.cacheInfoCmd())
}

func (m *Model) startLocalRefresh() tea.Cmd {
	if m.quitting || len(m.repositories) == 0 || m.scan.running {
		return nil
	}
	m.resetPending()
	return m.loadCmds()
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(refreshTickMsg); ok {
		return m.updateRefreshTick(tick)
	}
	next, cmd := m.update(msg)
	updated := next.(Model)
	updated.refreshAnimation = m.refreshAnimation
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		updated.refreshAnimation.ready = true
	}
	updated.normalizeActivityContributors()
	animation := updated.refreshTickCmd()
	return updated, tea.Batch(cmd, animation)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, nil
	}
	switch msg := msg.(type) {
	case settingsAccountMsg:
		return m.applySettingsAccount(msg), nil
	case settingsRepositoriesMsg:
		return m.applySettingsRepositories(msg), nil
	case settingsSavedMsg:
		return m.applySavedSettings(msg)
	case settingsReloadMsg:
		if msg.generation == m.cacheGeneration {
			return m, m.Init()
		}
		return m, nil
	case cacheInfoMsg:
		if msg.generation != m.cacheGeneration {
			return m, nil
		}
		if m.cacheRemoving && !msg.removal {
			return m, nil
		}
		m.cacheInfo = msg.info
		m.cacheError = ""
		if msg.err != nil {
			m.cacheError = displayText(msg.err.Error())
		}
		if msg.removal {
			m.cacheRemoving, m.cacheRemoveConfirm = false, ""
			m.overlayCursor = max(0, min(m.overlayCursor, len(m.repositoryControlRows())-1))
		}
		return m, nil
	case clipboardResult:
		m.copyStatus = fmt.Sprintf("Clipboard request sent: %d rows as Markdown · terminal support required", msg.rows)
		if msg.err != nil {
			m.copyStatus = "Clipboard request failed: " + displayText(msg.err.Error())
		}
		return m, nil
	case identitySavedMsg:
		return m.applyIdentitySave(msg)
	case prsLoadedMsg:
		if msg.generation == m.prGeneration {
			m.applyPRResult(msg)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		if m.viewMode == ViewOperative {
			m.clampStatDetailScroll()
		}

	case RepoLoadedMsg:
		if !m.scan.admit(msg, time.Now()) {
			return m, nil
		}
		if m.loading {
			line := bootLine(msg.Repository.Name, msg.Err == nil && msg.FetchErr == nil)
			if msg.Skipped {
				line = StyleDimWhite.Render("  " + displayText(msg.Repository.Name) + " · not cached yet")
			}
			m.bootLines = append(slices.Clip(m.bootLines), line)
		}
		if m.scan.remaining == 0 {
			if m.scan.phase == scanCached {
				return m.finishCachedStartup()
			}
			localOnly := m.scan.localOnly
			startupFetch := m.scan.phase == scanStartupFetch
			m.finalizeLoad()
			if localOnly {
				if m.startup.pending {
					cmd := m.startStartupFetch()
					return m, cmd
				}
				return m, nil
			}
			if startupFetch {
				return m, m.cacheInfoCmd()
			}
			return m, tea.Batch(m.startPRRefresh(), m.cacheInfoCmd())
		}
		return m, m.nextLoadCmd()

	case commitBrowserResult:
		if msg.err != nil {
			m.commitBrowserError = "Could not open commit: " + displayText(msg.err.Error())
		}
		return m, nil

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
	m.copyStatus = ""
	if m.settings.open {
		if m.settings.busy == "saving" {
			return m.handleSettingsKey(msg)
		}
		if m.showHelp {
			return m.handleHelpKey(msg)
		}
		if msg.String() == "f1" || msg.String() == "?" && m.settings.edit == "" {
			m.showHelp, m.helpOffset = true, 0
			return m, nil
		}
		return m.handleSettingsKey(msg)
	}
	if msg.String() == "," && m.options.Settings != nil && !m.helpSearchActive() && !m.identityMerge.saving && !m.cacheRemoving {
		if m.viewMode == ViewRepoOverlay {
			m.closeRepositoryControls(false)
		}
		m.filters, m.identityMerge = sharedFilterState{}, identityMergeState{}
		m.showHelp, m.showScanErrors = false, false
		m.prepareSettings(false)
		return m, m.settingsAccountCmd()
	}
	if msg.String() == "y" && m.canCopyMarkdown() {
		return m.copyMarkdown()
	}
	if m.identityMerge.saving {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, nil
	}
	switch msg.String() {
	case "c", "l", "a", "w", "p":
		if m.helpSearchActive() && !m.showHelp {
			break
		}
		return m.switchBoard(msg.String()), nil
	}
	if m.showHelp {
		return m.handleHelpKey(msg)
	}
	if msg.String() == "f1" || msg.String() == "?" && !m.helpSearchActive() {
		m.showHelp, m.helpOffset = true, 0
		return m, nil
	}
	if m.showScanErrors {
		return m.handleScanErrorKey(msg)
	}
	if m.identityMerge.open {
		return m.handleIdentityMergeKey(msg)
	}
	if m.err != nil && !m.loading {
		m.filters = sharedFilterState{}
		switch msg.String() {
		case "q", "ctrl+c":
			return m.quit()
		case "r", "R":
			return m, m.startLocalRefresh()
		case "e":
			m.showScanErrors, m.scanErrorOffset = true, 0
		}
		return m, nil
	}
	if m.filters.open {
		return m.handleFiltersKey(msg)
	}
	if m.helpSearchActive() {
		if m.showPRs {
			return m.handlePRSearch(msg)
		}
		if m.viewMode == ViewAwareness {
			return m.handleGlanceSearch(msg)
		}
		return m.handleSearchKey(msg)
	}
	if m.viewMode == ViewRepoOverlay {
		return m.handleRepositoryControls(msg)
	}
	if !m.loading && m.err == nil {
		switch msg.String() {
		case "f":
			m.openFilters("")
			return m, nil
		case "t":
			m.openFilters("range")
			return m, nil
		case "x":
			m.clearBoardFilters()
			return m, nil
		}
	}
	switch msg.String() {
	case "m":
		if m.viewMode == ViewAggregate && !m.showPRs && !m.loading {
			m.openIdentityMerge()
			return m, nil
		}
	case "q", "ctrl+c":
		return m.quit()
	case "r", "R":
		return m, m.startLocalRefresh()
	case "i":
		m.openRepositoryControls()
		return m, nil
	}
	if msg.String() == "e" {
		m.showScanErrors, m.scanErrorOffset = true, 0
		return m, nil
	}
	if m.showPRs {
		return m.handlePRKey(msg)
	}
	if m.viewMode == ViewAwareness {
		return m.handleAwarenessKey(msg)
	}
	switch msg.String() {
	case "/":
		if m.viewMode == ViewAggregate {
			m.searchBefore = m.filterQuery
			m.searching = true
			selectedID := m.selectedContributorID()
			m.selectContributor(selectedID)
		}

	case "t":
		if !m.loading && m.err == nil && (m.viewMode == ViewAggregate || m.viewMode == ViewOperative) {
			m.openRangePicker()
		}

	case "esc", "left":
		switch m.viewMode {
		case ViewOperative:
			m.viewMode = ViewAggregate
			m.activeOperative = ""
			m.activeAuthorID = ""
		default:
		}

	case "up":
		switch m.viewMode {
		case ViewOperative:
			m.statDetailOffset--
			m.clampStatDetailScroll()
		default:
			if m.selectedRow > 0 {
				m.selectedRow--
				m.clampScroll()
			}
		}

	case "down":
		switch m.viewMode {
		case ViewOperative:
			m.statDetailOffset++
			m.clampStatDetailScroll()
		default:
			if m.selectedRow < len(m.displayedAuthors())-1 {
				m.selectedRow++
				m.clampScroll()
			}
		}

	case "pgup", "pgdown", "home", "end":
		if m.viewMode == ViewAggregate {
			m.selectedRow, _ = glanceMove(msg.String(), m.selectedRow, len(m.displayedAuthors()), m.tableViewport())
			m.clampScroll()
		}
		if m.viewMode == ViewOperative {
			m.clampStatDetailScroll()
			switch msg.String() {
			case "pgup":
				m.statDetailOffset -= m.statisticsBodyBudget()
			case "pgdown":
				m.statDetailOffset += m.statisticsBodyBudget()
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
			selectedID := m.selectedContributorID()
			m.sortAuthors()
			m.selectContributor(selectedID)
		}

	case "S":
		if m.viewMode == ViewAggregate {
			m.sortAsc = !m.sortAsc
			selectedID := m.selectedContributorID()
			m.sortAuthors()
			m.selectContributor(selectedID)
		}

	case "b":
		if m.viewMode == ViewAggregate {
			m.hideBots = !m.hideBots
			m.recomputeAuthors()
		}

	case "enter", "right":
		if msg.String() != "enter" && m.viewMode != ViewAggregate {
			return m, nil
		}
		switch m.viewMode {
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
	return m.filteredRecordsAt(time.Now())
}

func (m *Model) filteredRecordsAt(now time.Time) []git.CommitRecord {
	filtered := stats.FilterByRepo(m.local.records, m.excludedRepos)
	filtered = m.filterSharedRecords(filtered)
	if m.timeIdx < 0 || m.timeIdx >= len(TimePresets) {
		m.timeIdx = DefaultTimeIndex
	}
	return m.filterRangeRecordsAt(filtered, now)
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

func (m Model) selectedContributorID() string {
	list := m.displayedAuthors()
	if m.selectedRow >= 0 && m.selectedRow < len(list) {
		return list[m.selectedRow].ID
	}
	return ""
}

func (m *Model) selectContributor(id string) {
	for i, author := range m.displayedAuthors() {
		if id != "" && author.ID == id {
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

func (m Model) leaderboardSummary() []string {
	commits, added, removed, ai := m.aggregateTotals()
	summary := []string{FormatNumber(commits) + " commits", "+" + formatLineCount(added, m.unknownLineCommits()), "−" + formatLineCount(removed, m.unknownLineCommits())}
	if ai > 0 {
		summary = append(summary, "AI "+aiBoxValue(commits, ai))
	}
	return summary
}

func (m Model) tableViewport() int {
	chrome := AggregateView{}.RenderTable(m.displayedAuthors(), TableState{Width: m.width, Searching: m.searching, Query: m.filterQuery})
	chromeHeight := lipgloss.Height(wrapStatistics(chrome, m.width))
	rowHeight := aggregateRowHeight(m.width, m.displayedAuthors())
	return max(1, (m.statisticsBodyBudget()-1-chromeHeight)/rowHeight)
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

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	selectedID := m.selectedContributorID()
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		m.searching = false
	case "esc":
		m.searching = false
		m.filterQuery = m.searchBefore
	case "ctrl+u":
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
	m.selectContributor(selectedID)
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	m.scan.stop()
	if m.cancelPRs != nil {
		m.cancelPRs()
	}
	if m.cancelScans != nil {
		m.cancelScans()
	}
	if m.cancelLocalScans != nil {
		m.cancelLocalScans()
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
	if m.filters.open && m.err == nil && !m.loading {
		content = m.renderFiltersModal(content, max(1, width), max(1, height))
	}
	if m.identityMerge.open {
		content = m.renderIdentityMerge(content, max(1, width), max(1, height))
	}
	if m.showScanErrors {
		content = m.renderScanErrors(content, max(1, width), max(1, height))
	}
	if m.settings.open {
		content = m.renderSettings(content, max(1, width), max(1, height))
	}
	if m.showHelp {
		content = m.renderHelpModal(content, max(1, width), max(1, height))
	}
	return worklistCanvas(content, max(1, width), max(1, height))
}

func (m Model) viewContent() string {
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
			StyleHelpKey.Render("  ? help · ")+StyleDimCyan.Render("▐")+StyleHelpKey.Render("q")+StyleDimCyan.Render("▌")+StyleHelpDesc.Render("quit · r retry · e errors"),
		)
		if details := m.scanErrorLines(); len(details) > 0 {
			lines = append(lines, "", details[0])
		}
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
	label := "SCANNING REPOSITORIES"
	if m.options.PrepareRepository != nil {
		label = "PREPARING CACHE · CLONING / FETCHING / SCANNING"
	}
	switch m.scan.phase {
	case scanCached:
		label = "READING CACHED HISTORY · OFFLINE"
	case scanStartupFetch:
		label = "PREPARING CACHE · CLONING / FETCHING / SCANNING"
	}
	lines = append(lines, "", StyleSubtitle.Render("  ◈ "+label), "")

	done := len(m.bootLines)
	total := done + m.scan.remaining

	const maxShown = 14
	shown := m.bootLines
	if len(shown) > maxShown {
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  … %d earlier", len(shown)-maxShown)))
		shown = shown[len(shown)-maxShown:]
	}
	lines = append(lines, shown...)
	lines = append(lines, "", StyleDimCyan.Render(fmt.Sprintf("  %s ▐ %d/%d repos ▌", m.refreshGlyph(), done, total)))
	lines = append(lines, StyleHelpKey.Render("  ? help · q quit"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderAggregateView() string {
	lines := m.screenHeader("Contributors", m.width)
	total := len(m.displayedAuthors())
	end := min(total, m.scrollOffset+m.tableViewport())
	label := []string{"Lines +/-", "Commits", "Added", "Removed", "Net", "AI"}[m.sortField]
	direction := "↓"
	if m.sortAsc {
		direction = "↑"
	}
	lines = append(lines, boardListHeading("CONTRIBUTORS", label+" "+direction, m.leaderboardSummary(), m.scrollOffset, end, total, m.width))
	table := AggregateView{}.RenderTable(m.displayedAuthors(), TableState{
		SelectedRow: m.selectedRow, ScrollOffset: m.scrollOffset,
		VisibleRows: m.tableViewport(), SortField: m.sortField, SortAsc: m.sortAsc,
		Width: m.width, Searching: m.searching, Query: m.filterQuery,
	})
	lines = append(lines, strings.Split(wrapStatistics(table, m.width), "\n")...)
	return screenFinish(lines, m.statisticsFooter(), m.copyCoverage(m.screenCoverage()), m.width, m.height)
}

func (m Model) operativeDetailContent() string {
	return m.operativeDetailContentAt(time.Now())
}

func (m Model) operativeDetailContentAt(now time.Time) string {
	var as *stats.AuthorStats
	for i := range m.authors {
		if (m.activeAuthorID != "" && m.authors[i].ID == m.activeAuthorID) || (m.activeAuthorID == "" && m.authors[i].Name == m.activeOperative) {
			as = &m.authors[i]
			break
		}
	}

	filtered := m.filteredRecordsAt(now)
	name := m.activeOperative
	if as != nil {
		name = as.Name
	} else if m.activeAuthorID != "" {
		// A selected identity with no activity in range must not fall back to
		// a different person who happens to have the same display name.
		filtered = nil
	}

	detail := OperativeView{FuzzyMatching: m.options.FuzzyMatching}.renderOperativeDetail(
		name,
		as,
		filtered,
		m.width,
		m.activityWindowAt(now),
	)
	return detail
}

func (m *Model) clampStatDetailScroll() {
	lines := strings.Split(m.operativeDetailContent(), "\n")
	m.statDetailOffset = max(0, min(m.statDetailOffset, len(lines)-m.statisticsBodyBudget()))
}

func (m Model) renderOperativeView() string {
	lines := m.screenHeader("Contributors / "+m.activeOperative, m.width)
	body := strings.Split(m.operativeDetailContent(), "\n")
	budget := m.statisticsBodyBudget()
	offset := max(0, min(m.statDetailOffset, len(body)-budget))
	end := min(len(body), offset+budget)
	lines = append(lines, body[offset:end]...)
	coverage := m.screenCoverage()
	if len(body) > budget {
		coverage = fmt.Sprintf("lines %d–%d/%d · %s", offset+1, end, len(body), coverage)
	}
	return screenFinish(lines, m.statisticsFooter(), m.copyCoverage(coverage), m.width, m.height)
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

func (m Model) unknownLineCommits() int {
	n := 0
	for _, a := range m.authors {
		n += a.UnknownLineCommits
	}
	return n
}
