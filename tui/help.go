package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type helpBinding struct {
	key, action string
}

type helpSection struct {
	title    string
	bindings []helpBinding
}

type screenHelp struct {
	title    string
	sections []helpSection
}

func (m Model) helpSearchActive() bool {
	if m.loading || m.err != nil || m.showScanErrors || m.viewMode == ViewRepoOverlay {
		return false
	}
	if m.filters.open {
		return m.filtersTextEditing()
	}
	if m.showPRs {
		return m.prSearching
	}
	return m.viewMode == ViewAwareness && m.glance.searching || m.viewMode == ViewAggregate && m.searching
}

func (m Model) currentHelp() screenHelp {
	help := m.screenHelp()
	if !m.filters.open && !m.helpSearchActive() && !m.loading && m.err == nil && !m.showScanErrors && m.viewMode != ViewRepoOverlay {
		help.sections = append(help.sections, helpSection{"Filtering", []helpBinding{{"f", "Edit shared scope and board filters"}, {"t", "Open shared date range"}, {"x", "Clear this board filters/search; keep scope and location"}}})
	}
	help.sections = append([]helpSection{{"Boards", []helpBinding{
		{"c", "Resume Contributors"}, {"a", "Resume Activity"}, {"p", "Resume pull requests"},
	}}}, help.sections...)
	if len(m.scanErrors) > 0 && !m.showScanErrors && !m.loading && m.err == nil && m.viewMode != ViewRepoOverlay && !m.helpSearchActive() {
		help.sections = append(help.sections, helpSection{"Diagnostics", []helpBinding{{"e", "Read repository update errors"}}})
	}
	return help
}

func (m Model) screenHelp() screenHelp {
	if m.filters.open {
		if m.filtersTextEditing() {
			return screenHelp{"Filter text", []helpSection{{"Edit", []helpBinding{
				{"Type / Backspace", "Edit the text"}, {"Ctrl+U", "Clear the text"}, {"Enter", "Keep this edit in the pending choices"}, {"Esc", "Cancel this edit"}, {"F1", "Show help while typing"},
			}}}}
		}
		return screenHelp{"Filters", []helpSection{{"Choose", []helpBinding{
			{"← / → / Tab", "Change category"}, {"↑ / ↓", "Select an option"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last option"},
			{"Space", "Change pending selection"}, {"/", "Find options"}, {"Enter", "Apply all pending changes"}, {"Esc", "Cancel pending changes"},
		}}, {"Scope", []helpBinding{
			{"Shared", "Date range and viewed repositories affect every board"}, {"This board", "Other filters stay on this board"}, {"Selections", "Any selected value; all categories must match"},
			{"Reset shared", "All time and all tracked repositories"}, {"Reset this board", "Clear board filters and search; keep your location"},
		}}}}
	}
	if m.showScanErrors {
		return screenHelp{"Repository errors", []helpSection{{"Actions", []helpBinding{
			{"↑ / ↓", "Scroll errors"}, {"d", "Show / hide technical details"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"e / ← / Esc", "Return to the previous screen"}, {"r", "Retry repository refresh"},
		}}}}
	}
	if m.loading {
		return screenHelp{"Loading repositories", []helpSection{{"Actions", []helpBinding{
			{"r", "Join the current refresh"},
		}}}}
	}
	if m.err != nil {
		return screenHelp{"Repositories unavailable", []helpSection{{"Recovery", []helpBinding{
			{"r", "Retry repository refresh"}, {"e", "Read error details"},
		}}}}
	}
	if m.helpSearchActive() {
		return screenHelp{"Search", []helpSection{{"Edit the search", []helpBinding{
			{"Type", "Filter the current list"}, {"Backspace", "Remove the last character"},
			{"Enter", "Keep the search and return to the list"}, {"Esc", "Restore the previous search and return"},
			{"?", "Type a question mark"}, {"F1", "Open this help while typing"}, {"Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.showPRs {
		title, move := "Pull requests", "Select a pull request"
		if m.prDetail {
			title, move = "Pull request details", "Scroll PR details"
		}
		back := "Close details; stay at board root; keep filters"
		if m.prContext.active {
			back = "Close details / return to Activity; keep saved PR filters"
		}
		navigation := []helpBinding{{"↑ / ↓", move}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last row or page"}, {"← / Esc", back}}
		actions := []helpBinding{{"f", "Edit shared scope and PR filters"}, {"u", "Edit PR authors"}, {"v", "Edit review states"}, {"k", "Edit check states"}, {"d", "Edit draft/open states"}, {"x", "Clear PR filters and search; keep shared scope"}, {"i", "Manage tracked repositories"}, {"o", "Open the selected PR in your browser"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh repos and PRs"}}
		actions = append([]helpBinding{{"t", "Change global range (PR last-updated time)"}}, actions...)
		if !m.prDetail {
			navigation = append(navigation, helpBinding{"Enter / →", "Read PR details"})
			actions = append([]helpBinding{{"/", "Search repo, title, author or PR number"}, {"s / S", "Change sort / reverse order"}}, actions...)
		}
		return screenHelp{title, []helpSection{{"Navigate", navigation}, {"Actions", actions}, {"Legend", []helpBinding{{"◇ / ●", "Draft / open non-draft; drafts have subdued titles"}, {"Green", "Approved review or passing checks; not a merge-readiness verdict"}}}}}
	}

	switch m.viewMode {
	case ViewRepoOverlay:
		return screenHelp{"Repository controls", []helpSection{{"Choose repositories", []helpBinding{
			{"↑ / ↓", "Select a repository"}, {"Space", "Include / exclude the repository"},
			{"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last repository"},
			{"Enter", "Apply choices and return"}, {"Esc / ←", "Cancel choices and return"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	case ViewOperative:
		return screenHelp{"Contributor details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Scroll contributor details"}, {"PgUp / PgDn", "Scroll details"}, {"Home / End", "First / last page"},
			{"← / Esc", "Return to Contributors"},
		}}, {"Actions", []helpBinding{{"t", "Edit the shared time range"}, {"i", "Choose included repositories"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh repos and PRs"}}}}}
	case ViewAggregate:
		back := "Stay at the Contributors root"
		return screenHelp{"Contributors", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Select a contributor"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last contributor"}, {"Enter / →", "Read contributor details"}, {"← / Esc", back},
		}}, {"Actions", []helpBinding{
			{"/", "Search contributors"}, {"s / S", "Change sort / reverse order"}, {"t", "Edit the shared time range"},
			{"b", "Show / hide bots"}, {"i", "Include repositories"},
			{"r", "Refresh repos and PRs"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.showPaths {
		return screenHelp{"Commit details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Scroll commit evidence"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"← / Esc", "Return to the current lens"}, {"Tab / Shift+Tab", "Next / previous lens"},
		}}, {"Actions", []helpBinding{{"v", "Open PRs for this repository"}, {"i", "Choose included repositories"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh repos and PRs"}}}}}
	}
	title, open, back := "Activity · Repositories", "Open repository directories", "Stay at the Activity root"
	if m.areaRepoID != "" {
		title, open, back = "Activity · Areas", "Open area details", "Return to repositories"
	}
	if m.directory.active {
		title, open, back = "Activity · Directories", "Open directory / file history", "Return to parent directory / repositories"
	}
	navigation := []helpBinding{{"↑ / ↓", "Select a row"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last row"}, {"Enter / →", open}}
	navigation = append(navigation, helpBinding{"← / Esc", back})
	actions := []helpBinding{{"/", "Search this list"}, {"s / S", "Change sort / reverse order"}}
	if m.glance.detailOpen {
		title = "Activity · " + glanceTabs[m.glance.frame.tab]
		navigation[3].action = []string{"Read commit details", "Show this contributor's activity", "Open related evidence", "Open the selected subarea"}[m.glance.frame.tab]
		navigation[4] = helpBinding{"← / Esc", "Return to the parent view; keep applied filters"}
		navigation = append(navigation, helpBinding{"Tab / Shift+Tab", "Next / previous lens"}, helpBinding{"1–4", "Activity / People / Related / Subareas"})
		if m.glance.frame.literal {
			if m.glance.frame.tab == glanceActivity {
				title = "Activity · Commits"
			}
			navigation[len(navigation)-1] = helpBinding{"1–3 / 4", "Commits / People / Related; return to directories"}
		}
		actions = []helpBinding{{"/", "Search this lens"}}
		if m.glance.frame.tab == glanceRelated {
			actions = append(actions, helpBinding{"m", "Shared commits / contributor overlap"})
		}
		if m.glance.frame.tab == glanceSubareas {
			actions = append(actions, helpBinding{"s / S", "Change sort / reverse order"})
		}
	} else if m.directory.active {
		navigation = append(navigation, helpBinding{"Tab / 1–3", "Read scope commits / people / related evidence"})
	} else if m.areaRepoID != "" {
		navigation = append(navigation, helpBinding{"Tab / 1–4", "Open an area lens"})
	}
	actions = append(actions, helpBinding{"t", "Edit the shared time range"}, helpBinding{"b", "Show / hide bots"},
		helpBinding{"i", "Choose included repositories"}, helpBinding{"v", "Open PRs for this repository"},
		helpBinding{"q / Ctrl+C", "Quit BigBoard"}, helpBinding{"r", "Refresh repos and PRs"})
	return screenHelp{title, []helpSection{{"Navigate", navigation}, {"Actions", actions}}}
}

func (m Model) helpLines(width int) []string {
	var lines []string
	keyWidth := 0
	for _, section := range m.currentHelp().sections {
		for _, binding := range section.bindings {
			keyWidth = max(keyWidth, ansi.StringWidth(binding.key))
		}
	}
	keyWidth = min(keyWidth, max(1, width/2))
	for i, section := range m.currentHelp().sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, StyleDimCyan.Bold(true).Render(section.title))
		for _, binding := range section.bindings {
			actions := strings.Split(ansi.Wrap(binding.action, max(1, width-keyWidth-2), ""), "\n")
			for j, action := range actions {
				key := ""
				if j == 0 {
					key = binding.key
				}
				lines = append(lines, StyleHelpKey.Render(padCells(key, keyWidth))+"  "+action)
			}
		}
	}
	return lines
}

func helpSize(width, height int) (int, int) {
	return max(6, min(66, width-2)), max(1, height-9)
}

func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	panelWidth, page := helpSize(width, height)
	last := max(0, len(m.helpLines(panelWidth-4))-page)
	m.helpOffset = min(m.helpOffset, last)
	switch msg.String() {
	case "?", "f1", "esc", "enter":
		m.showHelp, m.helpOffset = false, 0
	case "q", "ctrl+c":
		return m.quit()
	case "up":
		m.helpOffset--
	case "down":
		m.helpOffset++
	case "pgup":
		m.helpOffset -= page
	case "pgdown":
		m.helpOffset += page
	case "home":
		m.helpOffset = 0
	case "end":
		m.helpOffset = last
	}
	m.helpOffset = max(0, min(m.helpOffset, last))
	return m, nil
}

func (m Model) renderHelpModal(background string, width, height int) string {
	if width < 30 || height < 8 {
		return fitGlanceLines([]string{"Help · " + m.currentHelp().title, "Resize to read shortcuts", "Esc / ? / F1 close help"}, width, height)
	}
	panelWidth, page := helpSize(width, height)
	lines := m.helpLines(panelWidth - 4)
	offset := max(0, min(m.helpOffset, len(lines)-page))
	end := min(len(lines), offset+page)
	content := []string{StyleTitle.Render(ansi.Truncate("Help · "+m.currentHelp().title, panelWidth-4, "…")), ""}
	content = append(content, lines[offset:end]...)
	content = append(content, "")
	if len(lines) > page {
		content = append(content, StyleDimWhite.Render(fmt.Sprintf("%d–%d of %d · ↑↓ / PgUp / PgDn", offset+1, end, len(lines))))
	}
	content = append(content, StyleHelpKey.Render("Esc / ? / F1")+StyleHelpDesc.Render("  close help · q quit"))
	return renderCenteredModal(background, content, panelWidth, width, height)
}

func renderCenteredModal(background string, content []string, panelWidth, width, height int) string {
	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorCyanMid).
		Foreground(ColorBrightWht).Background(lipgloss.Color("#172B3A")).Padding(0, 1).Width(panelWidth - 2).
		Render(strings.Join(content, "\n"))
	panelBase := lipgloss.NewStyle().Foreground(ColorBrightWht).Background(lipgloss.Color("#172B3A"))
	prefix, _, _ := strings.Cut(panelBase.Render("X"), "X")
	if prefix != "" {
		panel = strings.ReplaceAll(panel, "\x1b[0m", "\x1b[0m"+prefix)
	}
	panelLines := strings.Split(fitGlanceLines(strings.Split(panel, "\n"), width, height), "\n")
	panelWidth = min(width, lipgloss.Width(panel))
	x, y := max(0, (width-panelWidth)/2), max(0, (height-len(panelLines))/2)
	backdrop := strings.Split(background, "\n")
	result := make([]string, height)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#58748B")).Background(ColorRowEven)
	for i := range result {
		line := ""
		if i < len(backdrop) {
			line = ansi.Strip(backdrop[i])
		}
		line = padCells(line, width)
		if i >= y && i < y+len(panelLines) {
			result[i] = dim.Render(padCells(ansi.Cut(line, 0, x), x)) + panelLines[i-y] + dim.Render(ansi.Cut(line, x+panelWidth, width))
		} else {
			result[i] = dim.Render(line)
		}
	}
	return strings.Join(result, "\n")
}
