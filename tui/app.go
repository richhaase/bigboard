package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
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
	excludedRepos   map[string]bool
	overlayExcluded map[string]bool // working copy while overlay is open
	overlayCursor   int
	viewMode        ViewMode
	selectedRow     int
	activeOperative string
	sortField       stats.SortField
	timeIdx         int
	width           int
	height          int
	loading         bool
	err             error
}

// DataLoadedMsg is sent after background data collection completes.
// Exported so main.go can construct it.
type DataLoadedMsg struct {
	Records   []git.CommitRecord
	RepoNames []string
	Err       error
}

// NewModel creates an initial Model ready to display the loading state.
func NewModel(repoPaths []string, initialSort stats.SortField, excluded map[string]bool) Model {
	return Model{
		sortField:     initialSort,
		timeIdx:       2, // 14d
		loading:       true,
		excludedRepos: excluded,
	}
}

// LoadDataCmd returns a Cmd that concurrently collects commits from all repos.
func LoadDataCmd(repoPaths []string) tea.Cmd {
	return func() tea.Msg {
		type result struct {
			records  []git.CommitRecord
			repoName string
		}

		ch := make(chan result, len(repoPaths))
		var wg sync.WaitGroup

		for _, path := range repoPaths {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				ref := git.DetectDefaultBranch(p)
				records, err := git.CollectCommits(p, ref)
				if err != nil {
					ch <- result{}
					return
				}
				repoName := ""
				if len(records) > 0 {
					repoName = records[0].RepoName
				}
				ch <- result{records: records, repoName: repoName}
			}(path)
		}

		wg.Wait()
		close(ch)

		var allRecords []git.CommitRecord
		repoSet := make(map[string]struct{})
		var repoNames []string

		for r := range ch {
			allRecords = append(allRecords, r.records...)
			if r.repoName != "" {
				if _, seen := repoSet[r.repoName]; !seen {
					repoSet[r.repoName] = struct{}{}
					repoNames = append(repoNames, r.repoName)
				}
			}
		}

		return DataLoadedMsg{
			Records:   allRecords,
			RepoNames: repoNames,
		}
	}
}

// Init satisfies tea.Model; data loading is kicked off by the caller via Cmd.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case DataLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.loading = false
			return m, nil
		}
		m.allRecords = msg.Records
		m.repoNames = msg.RepoNames
		sort.Strings(m.repoNames)
		m.loading = false
		m.recomputeAuthors()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		switch m.viewMode {
		case ViewRepoOverlay:
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
			m.selectedRow = 0
		case ViewOperative:
			m.viewMode = ViewAggregate
			m.activeOperative = ""
		default:
			return m, tea.Quit
		}

	case "up", "k":
		if m.viewMode == ViewRepoOverlay {
			if m.overlayCursor > 0 {
				m.overlayCursor--
			}
		} else if m.selectedRow > 0 {
			m.selectedRow--
		}

	case "down", "j":
		if m.viewMode == ViewRepoOverlay {
			if m.overlayCursor < len(m.repoNames)-1 {
				m.overlayCursor++
			}
		} else {
			maxRow := len(m.authors) - 1
			if maxRow > 19 {
				maxRow = 19
			}
			if m.selectedRow < maxRow {
				m.selectedRow++
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
			m.sortField = (m.sortField + 1) % 5
			stats.Sort(m.authors, m.sortField)
			m.selectedRow = 0
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
		if m.viewMode == ViewRepoOverlay {
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
			m.selectedRow = 0
		} else if m.viewMode == ViewAggregate && m.selectedRow < len(m.authors) {
			m.activeOperative = m.authors[m.selectedRow].Name
			m.viewMode = ViewOperative
		}
	}

	return m, nil
}

// recomputeAuthors re-filters and re-aggregates from allRecords.
func (m *Model) recomputeAuthors() {
	filtered := stats.FilterByRepo(m.allRecords, m.excludedRepos)
	filtered = stats.FilterByTime(filtered, TimePresets[m.timeIdx].Duration)
	m.authors = stats.Aggregate(filtered)
	stats.Sort(m.authors, m.sortField)
}

// View renders the current UI state.
func (m Model) View() string {
	if m.loading {
		title := StyleTitle.Render("⟐ BIG BOARD ⟐")
		scanning := StyleSubtitle.Render("// SCANNING REPOSITORIES...")
		return lipgloss.JoinVertical(lipgloss.Left, title, scanning)
	}

	if m.err != nil {
		return lipgloss.NewStyle().Foreground(ColorMagenta).Render(
			fmt.Sprintf("error: %v", m.err),
		)
	}

	if m.viewMode == ViewRepoOverlay {
		return m.renderRepoOverlay()
	}

	if m.viewMode == ViewOperative {
		return m.renderOperativeView()
	}

	return m.renderAggregateView()
}

// renderAggregateView composes the full aggregate screen.
func (m Model) renderAggregateView() string {
	var sections []string

	// Header
	sections = append(sections, RenderHeader(m.width))

	// Time picker + repo count on the same line
	timePicker := RenderTimePicker(m.timeIdx)
	repoCount := RenderRepoCount(len(m.repoNames), len(m.excludedRepos))
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, timePicker, repoCount))

	// Stat boxes
	var totalCommits, totalAdded, totalRemoved int
	for _, a := range m.authors {
		totalCommits += a.Commits
		totalAdded += a.Added
		totalRemoved += a.Removed
	}
	sections = append(sections, RenderStatBoxes(totalCommits, totalAdded, totalRemoved))

	// Aggregate table
	sections = append(sections, AggregateView{}.RenderTable(m.authors, m.selectedRow, m.sortField, m.width))

	// Help bar
	sections = append(sections, RenderHelpBar(HelpContext{View: "aggregate"}))

	// Footer
	sections = append(sections, RenderFooter(len(m.repoNames), len(m.excludedRepos), m.width))

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

	// Filter records by current time range for the timeline
	filtered := stats.FilterByTime(m.allRecords, TimePresets[m.timeIdx].Duration)

	// Time picker
	timePicker := RenderTimePicker(m.timeIdx)

	detail := OperativeView{}.RenderOperativeDetail(m.activeOperative, as, filtered, m.width)
	helpBar := RenderHelpBar(HelpContext{View: "operative"})
	footer := RenderFooter(len(m.repoNames), len(m.excludedRepos), m.width)
	return strings.Join([]string{timePicker, detail, "", helpBar, footer}, "\n")
}
