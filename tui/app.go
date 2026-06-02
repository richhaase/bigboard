package tui

import (
	"fmt"
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
)

// Model is the root Bubble Tea model.
type Model struct {
	allRecords      []git.CommitRecord
	authors         []stats.AuthorStats
	repoNames       []string
	failedRepos     []string
	repoPaths       []string // retained so refresh/watch can re-scan
	watchInterval   time.Duration
	excludedRepos   map[string]bool
	overlayExcluded map[string]bool // working copy while overlay is open
	overlayCursor   int
	viewMode        ViewMode
	selectedRow     int
	scrollOffset    int    // index of the first visible leaderboard row
	filterQuery     string // active incremental author filter ("" = none)
	searching       bool   // true while the '/' search input is open
	sortAsc         bool   // false = descending (default), true = ascending
	activeOperative string
	sortField       stats.SortField
	timeIdx         int
	version         string
	width           int
	height          int
	loading         bool
	err             error
	animate         bool // run the glitch-line animation
	frame           int  // animation frame counter

	// Streaming load state (accumulated as per-repo results arrive).
	pendingRecords   []git.CommitRecord
	pendingNames     []string
	pendingFailed    []string
	pendingRemaining int
	bootLines        []string
}

// watchTickMsg fires on the --watch interval to trigger a background refresh.
type watchTickMsg struct{}

func watchTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return watchTickMsg{} })
}

// animTickMsg drives the glitch-line animation frame counter.
type animTickMsg struct{}

func animTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

// animFrame returns the current frame when animating, or -1 for a static render.
func (m Model) animFrame() int {
	if m.animate {
		return m.frame
	}
	return -1
}

// RepoLoadedMsg is emitted as each repository finishes scanning, so the loader
// can stream a "JACKING IN" boot log instead of blocking on the whole set.
type RepoLoadedMsg struct {
	RepoName string
	Records  []git.CommitRecord
	Err      error
}

// NewModel creates an initial Model ready to display the loading state.
func NewModel(repoPaths []string, initialSort stats.SortField, excluded map[string]bool, version string, watchInterval time.Duration, animate bool) Model {
	return Model{
		repoPaths:        repoPaths,
		watchInterval:    watchInterval,
		animate:          animate,
		sortField:        initialSort,
		timeIdx:          2, // 14d
		loading:          true,
		pendingRemaining: len(repoPaths),
		excludedRepos:    excluded,
		version:          version,
	}
}

// loadRepoCmd scans one repository and returns its result.
func loadRepoCmd(path string) tea.Cmd {
	return func() tea.Msg {
		// Derive the repo name from the path, not from records[0], so a repo
		// with zero non-merge commits still registers in the overlay.
		name := filepath.Base(path)
		ref := git.DetectDefaultBranch(path)
		records, err := git.CollectCommits(path, ref)
		return RepoLoadedMsg{RepoName: name, Records: records, Err: err}
	}
}

// loadCmds returns one scan command per repo; they run concurrently and stream
// RepoLoadedMsg results back. (Pure — does not mutate m.)
func (m Model) loadCmds() tea.Cmd {
	if len(m.repoPaths) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, len(m.repoPaths))
	for i, p := range m.repoPaths {
		cmds[i] = loadRepoCmd(p)
	}
	return tea.Batch(cmds...)
}

// resetPending clears the streaming accumulator ahead of a (re)load.
func (m *Model) resetPending() {
	m.pendingRemaining = len(m.repoPaths)
	m.pendingRecords = nil
	m.pendingNames = nil
	m.pendingFailed = nil
	m.bootLines = nil
}

// finalizeLoad swaps the accumulated scan results into the live model once all
// repos have reported, then re-aggregates.
func (m *Model) finalizeLoad() {
	seen := map[string]bool{}
	var names []string
	for _, n := range m.pendingNames {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	sort.Strings(m.pendingFailed)
	m.allRecords = m.pendingRecords
	m.repoNames = names
	m.failedRepos = m.pendingFailed
	if len(m.repoNames) == 0 && len(m.failedRepos) > 0 {
		m.err = fmt.Errorf("all %d repositories failed to scan", len(m.failedRepos))
	} else {
		m.err = nil
	}
	m.loading = false
	m.recomputeAuthors()
}

// bootLine formats one repo's line in the JACKING IN boot log.
func bootLine(repo string, ok bool) string {
	red := lipgloss.NewStyle().Foreground(ColorRed)
	if ok {
		return "  " + StyleGreen.Render("▸ JACKING IN ") + StyleAuthor.Render(repo) + StyleGreen.Render(" … ✓")
	}
	return "  " + red.Render("▸ JACKING IN ") + StyleAuthor.Render(repo) + red.Render(" … ✗ LINK SEVERED")
}

// Init kicks off the initial concurrent load and arms the watch/animation timers.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadCmds()}
	if m.watchInterval > 0 {
		cmds = append(cmds, watchTick(m.watchInterval))
	}
	if m.animate {
		cmds = append(cmds, animTick())
	}
	return tea.Batch(cmds...)
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case animTickMsg:
		if !m.animate {
			return m, nil
		}
		m.frame++
		return m, animTick()

	case watchTickMsg:
		// Background refresh: re-scan silently (no boot splash), then re-arm.
		m.resetPending()
		return m, tea.Batch(m.loadCmds(), watchTick(m.watchInterval))

	case RepoLoadedMsg:
		if msg.Err != nil {
			m.pendingFailed = append(m.pendingFailed, msg.RepoName)
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.RepoName, false))
			}
		} else {
			m.pendingRecords = append(m.pendingRecords, msg.Records...)
			m.pendingNames = append(m.pendingNames, msg.RepoName)
			if m.loading {
				m.bootLines = append(m.bootLines, bootLine(msg.RepoName, true))
			}
		}
		m.pendingRemaining--
		if m.pendingRemaining <= 0 {
			m.finalizeLoad()
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the search prompt is open, all input feeds the filter query.
	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "q":
		// In aggregate, q quits; an active filter is cleared first.
		if m.viewMode == ViewAggregate && m.filterQuery != "" {
			m.filterQuery = ""
			m.clampScroll()
			return m, nil
		}
		return m, tea.Quit

	case "R":
		// Manual refresh: re-scan all repos and show the boot splash.
		if !m.loading {
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
				return m, tea.Quit
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
			if m.overlayCursor < len(m.repoNames)-1 {
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
		if m.timeIdx > 0 {
			m.timeIdx--
			m.recomputeAuthors()
		}

	case "right", "l":
		if m.timeIdx < len(TimePresets)-1 {
			m.timeIdx++
			m.recomputeAuthors()
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
		if m.viewMode == ViewRepoOverlay && m.overlayCursor < len(m.repoNames) {
			name := m.repoNames[m.overlayCursor]
			if m.overlayExcluded[name] {
				delete(m.overlayExcluded, name)
			} else {
				m.overlayExcluded[name] = true
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

// filteredRecords returns allRecords with the current repo-exclusion and
// time-range filters applied, in that order. Both the leaderboard and the
// operative detail view derive from this single helper so they can never drift.
func (m *Model) filteredRecords() []git.CommitRecord {
	filtered := stats.FilterByRepo(m.allRecords, m.excludedRepos)
	return stats.FilterByTime(filtered, TimePresets[m.timeIdx].Duration)
}

// recomputeAuthors re-filters and re-aggregates from allRecords.
func (m *Model) recomputeAuthors() {
	m.authors = stats.Aggregate(m.filteredRecords())
	m.sortAuthors()
	// Keep the cursor and scroll window in range when the list changes.
	m.clampScroll()
}

// sortAuthors sorts by the active field, reversing for ascending order.
func (m *Model) sortAuthors() {
	stats.Sort(m.authors, m.sortField)
	if m.sortAsc {
		for i, j := 0, len(m.authors)-1; i < j; i, j = i+1, j-1 {
			m.authors[i], m.authors[j] = m.authors[j], m.authors[i]
		}
	}
}

// displayedAuthors applies the active incremental filter to the sorted authors.
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

// tableViewport returns how many leaderboard rows fit given the terminal height,
// by measuring the surrounding chrome so the table never overflows the screen.
func (m Model) tableViewport() int {
	above := []string{RenderHeader(m.width, len(m.repoNames), len(m.excludedRepos), -1, m.version)}
	if len(m.failedRepos) > 0 {
		above = append(above, "warn")
	}
	above = append(above, "", RenderTimePicker(m.timeIdx), "", RenderStatBoxes(0, 0, 0, 0), "")
	help := RenderHelpBar(HelpContext{View: "aggregate"})
	// 6 = table-internal chrome (section header, blank, column header, rule) +
	// the blank before the help bar + the count/status footer line.
	budget := m.height - lipgloss.Height(strings.Join(above, "\n")) - lipgloss.Height(help) - 6
	if budget < 3 {
		budget = 3
	}
	return budget
}

// clampScroll keeps selectedRow in range and scrolls the window so the cursor
// stays visible.
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

// stepOperative moves the active contributor by delta within the displayed list.
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

// handleSearchKey processes input while the '/' filter prompt is open.
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
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

// View renders the current UI state.
func (m Model) View() string {
	if m.loading {
		return m.renderBootSequence()
	}

	if m.err != nil {
		lines := renderBanner(m.width)
		lines = append(lines,
			"",
			StyleClassification.Render("  ◈ SYSTEM FAULT // CONNECTION SEVERED"),
			"",
			lipgloss.NewStyle().Foreground(ColorRed).Render("  "+m.err.Error()),
			"",
			StyleDimCyan.Render("  ▐")+StyleHelpKey.Render("q")+StyleDimCyan.Render("▌")+StyleHelpDesc.Render("disconnect"),
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

// renderBootSequence renders the streaming "JACKING IN" loader.
func (m Model) renderBootSequence() string {
	lines := renderBanner(m.width)
	lines = append(lines, "", StyleClassification.Render("  ◈ JACKING INTO THE METAVERSE"), "")

	done := len(m.bootLines)
	total := done + m.pendingRemaining

	const maxShown = 14
	shown := m.bootLines
	if len(shown) > maxShown {
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  … %d earlier nodes", len(shown)-maxShown)))
		shown = shown[len(shown)-maxShown:]
	}
	lines = append(lines, shown...)
	lines = append(lines, "", StyleDimCyan.Render(fmt.Sprintf("  ▐ linking %d/%d nodes ▌", done, total)))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderAggregateView composes the full aggregate screen.
func (m Model) renderAggregateView() string {
	var sections []string

	// Header (banner + status + glitch line)
	sections = append(sections, RenderHeader(m.width, len(m.repoNames), len(m.excludedRepos), m.animFrame(), m.version))

	// Surface repos that failed to scan so the totals aren't silently incomplete.
	if len(m.failedRepos) > 0 {
		sections = append(sections, StyleAmber.Render(
			fmt.Sprintf("  ⚠ %d repo(s) unreadable: %s", len(m.failedRepos), strings.Join(m.failedRepos, ", "))))
	}
	sections = append(sections, "")

	// Time picker
	sections = append(sections, RenderTimePicker(m.timeIdx))
	sections = append(sections, "")

	// Stat boxes
	var totalCommits, totalAdded, totalRemoved, totalAI int
	for _, a := range m.authors {
		totalCommits += a.Commits
		totalAdded += a.Added
		totalRemoved += a.Removed
		totalAI += a.AICommits
	}
	sections = append(sections, RenderStatBoxes(totalCommits, totalAdded, totalRemoved, totalAI))
	sections = append(sections, "")

	// Aggregate table (includes its own section header + status footer)
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

	// Help bar at bottom
	sections = append(sections, "")
	sortLabel := strings.ToLower(stats.SortFieldLabel(m.sortField))
	sections = append(sections, RenderHelpBar(HelpContext{View: "aggregate", Sort: sortLabel}))

	return strings.Join(sections, "\n")
}

// renderOperativeView composes the operative detail screen.
func (m Model) renderOperativeView() string {
	// Find the author stats for the active operative
	var as *stats.AuthorStats
	for i := range m.authors {
		if m.authors[i].Name == m.activeOperative {
			as = &m.authors[i]
			break
		}
	}

	// Use the same repo+time filter as the leaderboard so the timeline and the
	// summary boxes agree when repos are excluded.
	filtered := m.filteredRecords()

	detail := OperativeView{}.RenderOperativeDetail(m.activeOperative, as, filtered, m.width, m.timeIdx, len(m.repoNames), len(m.excludedRepos))
	helpBar := RenderHelpBar(HelpContext{View: "operative"})
	return strings.Join([]string{detail, "", helpBar}, "\n")
}
