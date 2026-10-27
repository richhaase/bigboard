package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func filterModalSize(width, height int) (int, int) {
	return max(6, min(112, width-2)), max(10, min(16, height-10))
}

func filterOptionPageSize(width, height int) int {
	_, body := filterModalSize(width, height)
	return max(1, body-6)
}

func (m Model) filterBoardName() string {
	switch m.filters.board {
	case "prs":
		return "PRs"
	case "contributors":
		return "Contributors"
	default:
		return "Activity"
	}
}

func sameFilterValues(a, b filterValues) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range a {
		if !slices.Contains(b, value) {
			return false
		}
	}
	return true
}

func (m Model) filterCategoryChanged(id string) bool {
	f := m.filters
	switch id {
	case "range":
		return f.timeIdx != m.timeIdx || f.customDays != m.customRangeDays || f.sinceLastLook != m.sinceLastLookRange
	case "repos":
		return !sameFilterValues(f.repos, m.sharedRepos)
	case "people":
		return !sameFilterValues(f.people, m.activityPeople)
	case "bots":
		if f.board == "contributors" {
			return f.contributorBots != m.hideBots
		}
		return f.activityBots != m.activityHideBots
	case "mine", "author", "review", "checks", "state":
		return !sameFilterValues(f.prs.values(id), m.prFilters.values(id))
	case "search":
		query := m.glanceQuery()
		switch f.board {
		case "prs":
			query = m.prQuery
		case "contributors":
			query = m.filterQuery
		}
		return f.query != query
	}
	return false
}

func (m Model) filterPendingCount() int {
	count := 0
	for _, category := range m.filterCategories() {
		if m.filterCategoryChanged(category.id) {
			count++
		}
	}
	return count
}

func filterValuesSummary(values filterValues) string {
	if len(values) == 0 {
		return "All"
	}
	if len(values) == 1 {
		return displayText(values[0])
	}
	return fmt.Sprintf("%d selected", len(values))
}

func (m Model) filterCategorySummary(id string) string {
	f := m.filters
	switch id {
	case "range":
		return m.filterDraftModel().rangeLabel()
	case "repos":
		if len(f.repos) == 0 {
			return "All tracked"
		}
		return m.filterDraftModel().sharedRepositoryLabel()
	case "people":
		if len(f.people) == 1 {
			for _, option := range m.filterDraftModel().activityPeopleOptions() {
				if option.value == f.people[0] {
					return option.label
				}
			}
		}
		return filterValuesSummary(f.people)
	case "bots":
		hidden := f.activityBots
		if f.board == "contributors" {
			hidden = f.contributorBots
		}
		if hidden {
			return "Hidden"
		}
		return "Shown"
	case "mine", "author", "review", "checks", "state":
		return filterValuesSummary(f.prs.values(id))
	case "search":
		if f.query == "" {
			return "None"
		}
		return displayText(f.query)
	}
	return ""
}

func (m Model) filterCategoryRows(width, body int) []string {
	rows := []string{StyleDimCyan.Render(padCells("Shared scope · all boards", width))}
	boardGroup := false
	for _, category := range m.filterCategories() {
		if !category.shared && !boardGroup {
			rows = append(rows, StyleDimCyan.Render(padCells(m.filterBoardName()+" only", width)))
			boardGroup = true
		}
		cursor, changed := "  ", " "
		if category.id == m.filters.category {
			cursor = "› "
		}
		if m.filterCategoryChanged(category.id) {
			changed = "*"
		}
		labelWidth := min(14, max(8, width/2-2))
		label := cursor + padCells(category.label, labelWidth) + " "
		value := padCells(m.filterCategorySummary(category.id), max(1, width-ansi.StringWidth(label)-1))
		line := label + value + changed
		style := StyleDimWhite
		if category.id == m.filters.category {
			style = StyleTitle
			if m.filters.categoryFocus {
				style = style.Background(ColorRowSelect)
			}
		} else if changed == "*" {
			style = StyleAmber
		}
		rows = append(rows, style.Render(padCells(line, width)))
	}
	for len(rows) < body {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:min(body, len(rows))]
}

func (m Model) filterDescription() string {
	switch m.filters.category {
	case "range":
		return "All boards: Git author date; PR last-updated time."
	case "repos":
		return "All boards: any selected repo. Tracking stays unchanged."
	case "mine":
		return m.prMineStatus()
	case "people":
		return "Git contributors in Activity only. Match any selected person."
	case "author":
		return "GitHub authors in PRs only. Match any selected account."
	case "review", "checks", "state":
		return "Match any selected value here AND every other active filter."
	case "bots":
		return "Show or hide bot contributors on this board only."
	case "search":
		return "Search this board's current list. / or Space edits the text."
	}
	return ""
}

func (m Model) filterOptionRows(width, body int) []string {
	title := ""
	for _, category := range m.filterCategories() {
		if category.id == m.filters.category {
			title = category.label
		}
	}
	style := StyleTitle
	if !m.filters.categoryFocus {
		style = style.Background(ColorRowSelect)
	}
	rows := []string{style.Render(padCells(title+" · choices", width))}
	description := strings.Split(ansi.Wrap(m.filterDescription(), width, ""), "\n")
	for i := range 2 {
		line := ""
		if i < len(description) {
			line = description[i]
			if i == 1 {
				line = strings.Join(description[i:], " ")
			}
		}
		rows = append(rows, StyleDimWhite.Render(padCells(line, width)))
	}
	selection := "Selected: " + m.filterCategorySummary(m.filters.category)
	if m.filterCategoryChanged(m.filters.category) {
		selection = "Pending: " + m.filterCategorySummary(m.filters.category)
	}
	rows = append(rows, StyleCyan.Render(padCells(selection, width)))
	field := "/ Find options"
	if query := m.filters.queries[m.filters.category]; query != "" {
		field = "/ " + displayText(query)
	}
	if m.filters.category == "search" {
		field = "/ Edit search"
	}
	if m.filtersTextEditing() {
		prefix := "/ "
		switch m.filters.edit {
		case "query":
			prefix = "Search: "
		case "custom":
			prefix = "Days: "
		}
		field = prefix + ansi.Truncate(displayText(m.filters.input), max(1, width-ansi.StringWidth(prefix)-1), "…") + "▌"
	}
	rows = append(rows, lipgloss.NewStyle().Foreground(ColorBrightWht).Background(ColorRowEven).Render(padCells(field, width)))
	options := m.filterOptions()
	selected := max(0, min(m.filters.rows[m.filters.category], len(options)-1))
	page := max(1, body-6)
	start := max(0, min(selected-page/2, len(options)-page))
	end := min(len(options), start+page)
	for i := start; i < end; i++ {
		cursor, mark := "  ", "[ ] "
		if i == selected && !m.filters.categoryFocus {
			cursor = "› "
		}
		if m.filterOptionSelected(options[i].value) {
			mark = "[✓] "
		}
		line := padCells(cursor+mark+displayText(options[i].label), width)
		style := StyleDimWhite
		if i == selected && !m.filters.categoryFocus {
			style = StyleTitle.Background(ColorRowSelect)
		}
		rows = append(rows, style.Render(line))
	}
	if len(options) == 0 {
		rows = append(rows, StyleAmber.Render(padCells("No matching options", width)))
	}
	for len(rows) < body-1 {
		rows = append(rows, strings.Repeat(" ", width))
	}
	position := fmt.Sprintf("%d–%d of %d", start+1, end, len(options))
	if len(options) == 0 {
		position = "0 options · / change search"
	}
	if m.filters.error != "" {
		position = m.filters.error
	}
	rows = append(rows, StyleDimWhite.Render(padCells(position, width)))
	return rows
}

func (m Model) renderFiltersModal(background string, width, height int) string {
	if width < 40 || height < 18 {
		return renderCenteredModal(background, []string{"Filters", "Resize to 40×18", "Esc cancels"}, max(6, min(30, width)), width, height)
	}
	panel, body := filterModalSize(width, height)
	inner := panel - 4
	content := []string{StyleTitle.Render(padCells("Filters · "+m.filterBoardName(), inner))}
	if width >= 80 {
		leftWidth := min(38, inner/2)
		left := m.filterCategoryRows(leftWidth, body)
		right := m.filterOptionRows(inner-leftWidth-3, body)
		for i := range body {
			content = append(content, left[i]+StyleDimCyan.Render(" │ ")+right[i])
		}
	} else if m.filters.categoryFocus {
		content = append(content, m.filterCategoryRows(inner, body)...)
	} else {
		content = append(content, m.filterOptionRows(inner, body)...)
	}
	pending := "No pending changes"
	if count := m.filterPendingCount(); count > 0 {
		pending = fmt.Sprintf("%d pending changes · * marks edits", count)
	} else if m.filters.resetBoard {
		pending = "Board reset staged · Enter applies"
	}
	content = append(content, StyleAmber.Render(padCells(pending, inner)))
	if m.filtersTextEditing() {
		content = append(content, StyleTitle.Render(padCells("Enter keep edit · Esc cancel edit", inner)), StyleDimWhite.Render(padCells("Ctrl+U clear text · F1 help", inner)), StyleDimWhite.Render(padCells("Apply after finishing this edit", inner)))
	} else {
		navigation := "↑↓ category · →/Tab choices"
		if !m.filters.categoryFocus {
			navigation = "↑↓ · Space select · ←/Tab back"
		}
		if inner >= 48 {
			navigation += " · / find"
		}
		content = append(content, StyleDimWhite.Render(padCells(navigation, inner)), StyleDimWhite.Render(padCells("x reset board · X reset shared", inner)), StyleTitle.Render(padCells("Enter apply · Esc cancel · ? help", inner)))
	}
	return renderCenteredModal(background, content, panel, width, height)
}
