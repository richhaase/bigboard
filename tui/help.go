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
	if m.identityMerge.open {
		return m.identityMerge.edit != ""
	}
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
	if !m.identityMerge.open && !m.filters.open && !m.helpSearchActive() && !m.loading && m.err == nil && !m.showScanErrors && m.viewMode != ViewRepoOverlay {
		help.sections = append(help.sections, helpSection{"Filters", []helpBinding{{"f", "Filters: shared scope and this board's filters"}, {"t", "Date range: shared across all boards"}, {"x", "Clear board filters and search; keep scope and location"}}})
	}
	if !m.identityMerge.open && !m.filters.open && !m.showScanErrors && !m.loading && m.err == nil && m.viewMode != ViewRepoOverlay && !m.helpSearchActive() {
		help.sections = append(help.sections, helpSection{"Data status", []helpBinding{{"e", "Data status: Git/PR freshness, coverage and diagnostics"}}})
	}
	boards := "Boards"
	if m.identityMerge.open || m.filters.open || m.viewMode == ViewRepoOverlay {
		boards = "Boards · cancel pending choices"
	} else if m.helpSearchActive() {
		boards = "Boards · from help"
	}
	help.sections = append(help.sections, helpSection{boards, []helpBinding{
		{"c", "Resume Contributors"}, {"a", "Resume Activity"}, {"p", "Resume PRs"},
	}})
	return help
}

func (m Model) screenHelp() screenHelp {
	if m.identityMerge.open {
		if m.identityMerge.edit != "" {
			return screenHelp{"Contributor merge text", []helpSection{{"Edit", []helpBinding{
				{"Type / Backspace", "Edit text; shortcut letters stay literal"}, {"Ctrl+U", "Clear text"}, {"Enter", "Keep edit; return to choices"}, {"Esc", "Cancel edit"}, {"F1", "Show help while typing"}, {"Ctrl+C", "Quit BigBoard"},
			}}}}
		}
		if m.identityMerge.review {
			return screenHelp{"Review contributor merge", []helpSection{{"Confirm", []helpBinding{
				{"Enter", "Merge the listed emails and save for future sessions"}, {"Esc / ←", "Return to choices without saving"}, {"↑ / ↓", "Review selected emails"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last email"}, {"q / Ctrl+C", "Quit without confirming"},
			}}}}
		}
		return screenHelp{"Merge contributors", []helpSection{{"Choose identities", []helpBinding{
			{"↑ / ↓", "Select a contributor identity"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last identity"}, {"Space", "Stage identity for merging"}, {"k", "Keep highlighted email as the primary identity"}, {"n", "Edit optional display name"}, {"/", "Find by name or email"}, {"Enter", "Review the merge before saving"}, {"Esc / ←", "Cancel without saving"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}, {"Identity", []helpBinding{{"Scope", "All collected identities are available, independently of date and board filters"}, {"[P]", "Primary email retained by the merged contributor"}, {"[—]", "Missing or invalid email; cannot be merged"}, {"Saving", "Updates future sessions and rescans cached local history without fetching"}}}}}
	}
	if m.filters.open {
		if m.filtersTextEditing() {
			return screenHelp{"Filter text", []helpSection{{"Edit", []helpBinding{
				{"Type / Backspace", "Edit text; shortcut letters stay literal"}, {"Ctrl+U", "Clear the text"}, {"Enter", "Keep this edit; return to pending choices"}, {"Esc", "Cancel this edit; return to pending choices"}, {"F1", "Show help while typing"}, {"Ctrl+C", "Quit BigBoard"},
			}}}}
		}
		return screenHelp{"Filters", []helpSection{{"Choose", []helpBinding{
			{"← / → / Tab", "Change category; Shift+Tab moves backward"}, {"↑ / ↓", "Select an option"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last option"},
			{"Space", "Change pending selection"}, {"/", "Find options"}, {"Enter", "Apply all pending changes"}, {"Esc", "Cancel pending changes; return to the board"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}, {"Scope", []helpBinding{
			{"Shared", "Date range and viewed repositories affect every board; tracking is unchanged"}, {"This board", "Other filters stay on this board"}, {"Selections", "Any selected value; all categories must match"},
			{"Reset shared", "All time and all tracked repositories"}, {"Reset this board", "Clear board filters and search; keep your location"},
		}}}}
	}
	if m.showScanErrors {
		return screenHelp{"Data status", []helpSection{{"Actions", []helpBinding{
			{"↑ / ↓", "Scroll freshness and coverage"}, {"d", "Show / hide repository diagnostics"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"e / ← / Esc", "Close Data status; return to the previous view"}, {"r", "Refresh Git and PRs"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.loading {
		return screenHelp{"Loading repositories", []helpSection{{"Actions", []helpBinding{
			{"r", "Join the current refresh"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.err != nil {
		return screenHelp{"Repositories unavailable", []helpSection{{"Recovery", []helpBinding{
			{"r", "Retry repository refresh"}, {"e", "Data status: read error details"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.helpSearchActive() {
		return screenHelp{"Search", []helpSection{{"Edit the search", []helpBinding{
			{"Type", "Find in the current list; shortcut letters stay literal"}, {"Backspace", "Remove the last character"}, {"Ctrl+U", "Clear the text"},
			{"Enter", "Keep the search and return to the list"}, {"Esc", "Restore the previous search and return"},
			{"?", "Type a question mark"}, {"F1", "Open this help while typing"}, {"Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.showPRs {
		title, move := "Pull requests", "Select a pull request"
		if m.prDetail {
			title, move = "Pull request details", "Scroll PR details"
		}
		back := "Stay at the PRs root; keep filters"
		if m.prDetail {
			back = "Close details; return to the PR list; keep filters"
		} else if m.prContext.active {
			back = "Return to Activity; restore saved PR filters"
		}
		navigation := []helpBinding{{"↑ / ↓", move}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last row or page"}, {"← / Esc", back}}
		actions := []helpBinding{{"u", "Filters: PR authors"}, {"v", "Filters: review states"}, {"k", "Filters: check states"}, {"d", "Filters: draft/open states"}, {"i", "Repository controls: choose tracked repositories"}, {"o", "Open the selected PR in your browser"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh Git and PRs"}}
		if !m.prDetail {
			navigation = append(navigation, helpBinding{"Enter / →", "Read PR details"})
			actions = append([]helpBinding{{"/", "Find by repo, title, author or PR number"}, {"s / S", "Change sort / reverse order"}}, actions...)
		}
		return screenHelp{title, []helpSection{{"Navigate", navigation}, {"Actions", actions}, {"Legend", []helpBinding{{"◇ / ●", "Draft / open non-draft; drafts have subdued titles"}, {"Green", "Approved review or passing checks; not a merge-readiness verdict"}, {"Date range", "PR last-updated time; local history uses author dates"}}}}}
	}

	switch m.viewMode {
	case ViewRepoOverlay:
		return screenHelp{"Repository controls", []helpSection{{"Choose repositories", []helpBinding{
			{"↑ / ↓", "Select a repository"}, {"Space", "Stage include / exclude for this repository"},
			{"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last repository"},
			{"Enter", "Apply tracking choices and return"}, {"Esc / ←", "Cancel pending choices and return"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}, {"Tracking", []helpBinding{{"Included", "Used for data and refreshes in this session"}, {"Filters", "Narrow the view without changing repository tracking"}}}}}
	case ViewOperative:
		return screenHelp{"Contributor details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Scroll contributor details"}, {"PgUp / PgDn", "Scroll details"}, {"Home / End", "First / last page"},
			{"← / Esc", "Return to Contributors"},
		}}, {"Actions", []helpBinding{{"i", "Repository controls: choose tracked repositories"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh Git and PRs"}}}}}
	case ViewAggregate:
		back := "Stay at the Contributors root"
		return screenHelp{"Contributors", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Select a contributor"}, {"PgUp / PgDn", "Move a page"}, {"Home / End", "First / last contributor"}, {"Enter / →", "Read contributor details"}, {"← / Esc", back},
		}}, {"Actions", []helpBinding{
			{"/", "Find contributors"}, {"s / S", "Change sort / reverse order"},
			{"b", "Show / hide bots"}, {"m", "Merge contributor identities; review before saving"}, {"i", "Repository controls: choose tracked repositories"},
			{"r", "Refresh Git and PRs"}, {"q / Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.showPaths {
		return screenHelp{"Commit details", []helpSection{{"Navigate", []helpBinding{
			{"↑ / ↓", "Scroll commit evidence"}, {"PgUp / PgDn", "Scroll a page"}, {"Home / End", "First / last page"},
			{"← / Esc", "Return to the current lens"}, {"Tab / Shift+Tab", "Next / previous lens"},
		}}, {"Actions", []helpBinding{{"o", "Open this exact commit on GitHub"}, {"v", "Open PRs for this repository"}, {"i", "Repository controls: choose tracked repositories"}, {"q / Ctrl+C", "Quit BigBoard"}, {"r", "Refresh Git and PRs"}}}}}
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
	actions := []helpBinding{{"/", "Find in this list"}, {"s / S", "Change sort / reverse order"}}
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
		actions = []helpBinding{{"/", "Find in this lens"}}
		if m.glance.frame.tab == glanceActivity {
			actions = append(actions, helpBinding{"o", "Open the selected exact commit on GitHub"})
		}
		if m.glance.frame.tab == glanceRelated {
			actions = append(actions, helpBinding{"m", "Shared commits / contributor overlap"})
		}
		if m.glance.frame.tab == glanceSubareas {
			actions = append(actions, helpBinding{"s / S", "Change sort / reverse order"})
		}
	} else if m.directory.active || m.areaRepoID == "" {
		if activityContributorsHeight(m.width, m.height) > 0 {
			navigation = append(navigation, helpBinding{"Tab / Shift+Tab", "Move focus between ranking and contributors"})
		}
		if m.directory.active {
			navigation = append(navigation, helpBinding{"1–3", "Read scope commits / people / related evidence"})
		} else {
			navigation = append(navigation, helpBinding{"Enter, then 2", "Read all contributors for the repository"})
		}
		if m.contributorPane.focused {
			title = "Activity · Contributors pane"
			navigation = []helpBinding{{"↑ / ↓", "Scroll contributors"}, {"PgUp / PgDn", "Scroll a visible page"}, {"Home / End", "First / last contributor page"}, {"Tab / Shift+Tab", "Return focus to ranking"}, {"← / Esc", "Return focus to ranking"}}
			actions = nil
		}
	} else if m.areaRepoID != "" {
		navigation = append(navigation, helpBinding{"Tab / 1–4", "Open an area lens"})
	}
	actions = append(actions, helpBinding{"b", "Show / hide bots"},
		helpBinding{"i", "Repository controls: choose tracked repositories"}, helpBinding{"v", "Open PRs for this repository"},
		helpBinding{"q / Ctrl+C", "Quit BigBoard"}, helpBinding{"r", "Refresh Git and PRs"})
	sections := []helpSection{{"Navigate", navigation}, {"Actions", actions}}
	if !m.glance.detailOpen {
		sections = append(sections, helpSection{"PR counts", []helpBinding{
			{"Open", "Known open PRs in the shared date and repository scope"},
			{"Failing", "PRs with failing or errored checks"},
			{"Changes", "PRs with changes requested; may also have failing checks"},
			{"— / blank", "No open PRs / zero failing or changes-requested counts"},
			{"?", "Incomplete or stale PR evidence; directories also need complete file lists"},
			{"Range", "PR last-updated time; Git contributor/bot filters do not affect PR counts"},
		}})
	}
	return screenHelp{title, sections}
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
