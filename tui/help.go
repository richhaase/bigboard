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
	if m.loading || m.err != nil || m.showScanErrors || m.rangePicker.open || m.showPRs {
		return false
	}
	return m.viewMode == ViewAwareness && m.glance.searching || m.viewMode == ViewAggregate && m.searching
}

func (m Model) currentHelp() screenHelp {
	if m.showScanErrors {
		return screenHelp{"Repository errors", []helpSection{{"Actions", []helpBinding{
			{"↑ / ↓", "Scroll errors"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"e / Esc", "Return to the previous screen"}, {"R", "Retry repository refresh"},
		}}}}
	}
	if m.loading {
		return screenHelp{"Loading repositories", []helpSection{{"Actions", []helpBinding{
			{"R", "Join the current refresh"},
		}}}}
	}
	if m.err != nil {
		return screenHelp{"Repositories unavailable", []helpSection{{"Recovery", []helpBinding{
			{"R", "Retry repository refresh"}, {"e", "Read error details"},
		}}}}
	}
	if m.rangePicker.open {
		if m.rangePicker.custom {
			return screenHelp{"Custom time range", []helpSection{{"Edit days", []helpBinding{
				{"0–9", "Enter 1–3650 days"}, {"Backspace", "Edit the value"}, {"Ctrl+U", "Clear the value"},
				{"Enter", "Apply this range"}, {"Esc", "Cancel without changing the range"},
			}}}}
		}
		return screenHelp{"Time range", []helpSection{{"Choose a range", []helpBinding{
			{"↑ / ↓", "Select a range"}, {"c / End", "Select custom days"}, {"Home", "Select the first preset"},
			{"Enter", "Apply preset / edit custom days"}, {"Esc", "Cancel without changing the range"},
		}}}}
	}
	if m.helpSearchActive() {
		return screenHelp{"Search", []helpSection{{"Edit the search", []helpBinding{
			{"Type", "Filter the current list"}, {"Backspace", "Remove the last character"},
			{"Enter", "Keep the filter and return to the list"}, {"Esc", "Clear the filter and return"},
			{"?", "Type a question mark"}, {"F1", "Open this help while typing"}, {"Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.showPRs {
		title, move, enter, back := "Pull requests", "Select a pull request", "Read PR details", "Return to the previous screen"
		if m.prDetail {
			title, move, enter, back = "Pull request details", "Scroll PR details", "Return to the PR list", "Return to the PR list"
		}
		return screenHelp{title, []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", move}, {"Enter", enter}, {"Esc", back},
		}}, {"Actions", []helpBinding{
			{"p / P", "Selected scope / all repositories"}, {"o", "Open the selected PR in your browser"},
			{"R", "Refresh repos and PRs"},
		}}}}
	}
	switch m.viewMode {
	case ViewRepoOverlay:
		return screenHelp{"Repository controls", []helpSection{{"Choose repositories", []helpBinding{
			{"↑ / ↓", "Select a repository"}, {"Space", "Include / exclude the repository"},
			{"Enter / Esc", "Apply choices and return"},
		}}}}
	case ViewOperative:
		return screenHelp{"Contributor details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Previous / next contributor"}, {"PgUp / PgDn", "Scroll details"}, {"Home / End", "First / last page"},
			{"← / Esc", "Return to the leaderboard"},
		}}, {"Actions", []helpBinding{{"t", "Change the time range"}, {"R", "Refresh repos and PRs"}}}}}
	case ViewAggregate:
		back, quit := "Quit BigBoard", "Quit BigBoard"
		if m.filterQuery != "" {
			back, quit = "Clear the search filter", "Clear the search filter"
		}
		return screenHelp{"Contributor leaderboard", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Select a contributor"}, {"Enter / →", "Read contributor details"}, {"Esc", back},
		}}, {"Actions", []helpBinding{
			{"/", "Search contributors"}, {"s / S", "Change sort / reverse order"}, {"t", "Change the time range"},
			{"b", "Show / hide bots"}, {"r", "Include repositories"}, {"l", "Return to the Worklist"},
			{"R", "Refresh repos and PRs"}, {"q", quit},
		}}}}
	}
	if m.showPaths {
		return screenHelp{"Commit details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Scroll commit evidence"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"Enter / ← / Esc", "Return to the current lens"}, {"Tab / Shift+Tab", "Next / previous lens"},
		}}}}
	}
	title, open, back := "Worklist · Repositories", "Open repository areas", "Quit BigBoard"
	if m.areaRepoID != "" {
		title, open, back = "Worklist · Areas", "Open area details", "Return to repositories"
	}
	if m.glanceQuery() != "" {
		back = "Clear the search filter"
	}
	navigation := []helpBinding{{"↑ / ↓", "Select a row"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last row"}, {"Enter / →", open}}
	if m.areaRepoID != "" || m.glanceQuery() != "" || m.glance.detailOpen {
		navigation = append(navigation, helpBinding{"Esc", back})
	}
	actions := []helpBinding{{"/", "Search this list"}, {"s", "Cycle overview sort"}}
	if m.glance.detailOpen {
		title = "Worklist · " + glanceTabs[m.glance.frame.tab]
		navigation[3].action = []string{"Read commit details", "Show this contributor's activity", "Open related evidence", "Open the selected subarea"}[m.glance.frame.tab]
		navigation[4] = helpBinding{"← / Esc", "Clear filter / return to the parent view"}
		navigation = append(navigation, helpBinding{"Tab / Shift+Tab", "Next / previous lens"}, helpBinding{"1–4", "Activity / People / Related / Subareas"})
		actions = []helpBinding{{"/", "Search this lens"}}
		if m.glance.frame.tab == glanceRelated {
			actions = append(actions, helpBinding{"o", "Shared commits / contributor overlap"})
		}
		if m.glance.frame.tab == glanceSubareas {
			actions = append(actions, helpBinding{"s", "Cycle overview sort"})
		}
	} else if m.areaRepoID != "" {
		navigation = append(navigation, helpBinding{"Tab / 1–4", "Open an area lens"})
	}
	actions = append(actions, helpBinding{"t", "Change the time range"}, helpBinding{"b", "Show / hide bots"},
		helpBinding{"p / P", "Scoped / all open PRs"}, helpBinding{"r", "Include repositories"},
		helpBinding{"l", "Contributor stats"}, helpBinding{"R", "Refresh repos and PRs"})
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
