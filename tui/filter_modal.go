package tui

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type sharedFilterState struct {
	open                          bool
	board, category               string
	rows                          map[string]int
	queries                       map[string]string
	edit, input, prior, error     string
	priorRow                      int
	replace, overflow             bool
	timeIdx, customDays           int
	repos, people                 filterValues
	activityBots, contributorBots bool
	prs                           prFilters
	query                         string
	resetShared, resetBoard       bool
}

type filterCategory struct {
	id, label string
	shared    bool
}

func (m Model) filtersTextEditing() bool {
	return m.filters.open && m.filters.edit != ""
}

func (m Model) filterCategories() []filterCategory {
	categories := []filterCategory{{"range", "Date range", true}, {"repos", "Repositories", true}, {"shared-reset", "Reset shared", true}}
	switch m.filters.board {
	case "prs":
		categories = append(categories, filterCategory{"author", "Authors", false}, filterCategory{"review", "Review", false}, filterCategory{"checks", "Checks", false}, filterCategory{"state", "State", false})
	case "activity":
		categories = append(categories, filterCategory{"people", "Contributors", false}, filterCategory{"bots", "Bots", false})
	default:
		categories = append(categories, filterCategory{"bots", "Bots", false})
	}
	return append(categories, filterCategory{"search", "Search", false}, filterCategory{"board-reset", "Reset board", false})
}

func (m *Model) openFilters(category string) {
	if !m.filters.open {
		board, query := "activity", m.glanceQuery()
		if m.showPRs {
			board, query = "prs", m.prQuery
		} else if m.viewMode == ViewAggregate || m.viewMode == ViewOperative {
			board, query = "contributors", m.filterQuery
		}
		m.filters = sharedFilterState{
			open: true, board: board, rows: map[string]int{}, queries: map[string]string{},
			timeIdx: m.timeIdx, customDays: m.customRangeDays, repos: m.sharedRepos.clone(),
			people: m.activityPeople.clone(), activityBots: m.activityHideBots, contributorBots: m.hideBots,
			prs: m.prFilters.clone(), query: query,
		}
	}
	m.filters.rows = maps.Clone(m.filters.rows)
	m.filters.queries = maps.Clone(m.filters.queries)
	if category == "repo" {
		category = "repos"
	}
	if category == "" {
		category = "range"
	}
	m.filters.category = "range"
	for _, item := range m.filterCategories() {
		if item.id == category {
			m.filters.category = category
			break
		}
	}
	m.filters.edit, m.filters.error = "", ""
	if m.filters.category == "range" {
		m.filters.rows["range"] = max(0, min(m.filters.timeIdx, len(TimePresets)-1))
		if m.filters.customDays > 0 {
			m.filters.rows["range"] = len(TimePresets)
		}
	}
}

func (m Model) filterDraftModel() Model {
	m.sharedRepos = m.filters.repos.clone()
	m.activityPeople = m.filters.people.clone()
	m.activityHideBots, m.hideBots = m.filters.activityBots, m.filters.contributorBots
	m.timeIdx, m.customRangeDays = m.filters.timeIdx, m.filters.customDays
	m.prFilters = m.filters.prs.clone()
	return m
}

func (m Model) filterOptions() []prFilterOption {
	draft := m.filterDraftModel()
	var options []prFilterOption
	switch m.filters.category {
	case "range":
		for i := range TimePresets {
			options = append(options, prFilterOption{strconv.Itoa(i), presetRangeLabel(i)})
		}
		label := "Custom days…"
		if m.filters.customDays > 0 {
			label = "Custom: " + daysRangeLabel(m.filters.customDays)
		}
		options = append(options, prFilterOption{"custom", label})
	case "repos":
		labels := map[string]string{}
		for _, repo := range append(slices.Clone(m.repositories), m.local.repositories...) {
			if !m.excludedRepos[repo.ID] {
				labels[repo.ID] = displayText(repo.Name)
			}
		}
		for _, id := range m.filters.repos {
			if _, ok := labels[id]; !ok {
				labels[id] = displayText(id) + " (unavailable)"
			}
		}
		options = []prFilterOption{{"", "All included repositories"}}
		for value, label := range labels {
			options = append(options, prFilterOption{value, label})
		}
		sort.Slice(options[1:], func(i, j int) bool {
			a, b := options[i+1], options[j+1]
			if a.label != b.label {
				return a.label < b.label
			}
			return a.value < b.value
		})
	case "people":
		options = draft.activityPeopleOptions()
	case "bots":
		options = []prFilterOption{{"shown", "Show bot contributors"}, {"hidden", "Hide bot contributors"}}
	case "author", "review", "checks", "state":
		draft.prFilterKind, draft.prFilterQuery = m.filters.category, ""
		options = draft.prFilterOptions()
	case "search":
		options = []prFilterOption{{"edit", "Edit this board's search"}, {"clear", "Clear this search"}}
	case "shared-reset":
		options = []prFilterOption{{"reset", "Reset shared scope"}}
	case "board-reset":
		options = []prFilterOption{{"reset", "Reset this board's filters"}}
	}
	query := strings.ToLower(m.filters.queries[m.filters.category])
	if query == "" || m.filters.category == "search" || strings.HasSuffix(m.filters.category, "reset") {
		return options
	}
	filtered := make([]prFilterOption, 0, len(options))
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.label), query) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func (m Model) filterOptionSelected(value string) bool {
	f := m.filters
	var values filterValues
	switch f.category {
	case "range":
		if value == "custom" {
			return f.customDays > 0
		}
		return f.customDays == 0 && value == strconv.Itoa(f.timeIdx)
	case "repos":
		values = f.repos
	case "people":
		values = f.people
	case "author", "review", "checks", "state":
		values = f.prs.values(f.category)
	case "bots":
		hide := f.activityBots
		if f.board == "contributors" {
			hide = f.contributorBots
		}
		return hide == (value == "hidden")
	case "search":
		return value == "clear" && f.query == ""
	case "shared-reset":
		return f.resetShared
	case "board-reset":
		return f.resetBoard
	}
	return value == "" && len(values) == 0 || value != "" && slices.Contains(values, value)
}

func (m *Model) beginFilterEdit(kind string) {
	f := &m.filters
	f.edit, f.error, f.overflow, f.replace = kind, "", false, false
	f.priorRow = f.rows[f.category]
	switch kind {
	case "options":
		f.input = f.queries[f.category]
	case "query":
		f.input = f.query
	case "custom":
		days := f.customDays
		if days == 0 {
			days = 14
		}
		f.input, f.replace = strconv.Itoa(days), true
	}
	f.prior = f.input
}

func (m *Model) stageFilterOption(value string) {
	f := &m.filters
	switch f.category {
	case "range":
		if value == "custom" {
			m.beginFilterEdit("custom")
		} else if index, err := strconv.Atoi(value); err == nil {
			f.timeIdx, f.customDays = index, 0
		}
	case "repos":
		f.repos = f.repos.toggle(value)
	case "people":
		f.people = f.people.toggle(value)
	case "author", "review", "checks", "state":
		f.prs.setValues(f.category, f.prs.values(f.category).toggle(value))
	case "bots":
		if f.board == "contributors" {
			f.contributorBots = value == "hidden"
		} else {
			f.activityBots = value == "hidden"
		}
	case "search":
		if value == "edit" {
			m.beginFilterEdit("query")
		} else {
			f.query = ""
		}
	case "shared-reset":
		f.timeIdx, f.customDays, f.repos, f.resetShared = len(TimePresets)-1, 0, nil, true
	case "board-reset":
		f.query, f.resetBoard = "", true
		switch f.board {
		case "activity":
			f.people, f.activityBots = nil, false
		case "contributors":
			f.contributorBots = false
		case "prs":
			f.prs = prFilters{}
		}
	}
}

func (m Model) handleFilterText(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.filters
	switch msg.String() {
	case "esc":
		if f.edit == "options" {
			f.queries[f.category] = f.prior
		}
		f.rows[f.category] = f.priorRow
		f.edit, f.error = "", ""
	case "enter":
		switch f.edit {
		case "custom":
			if f.overflow {
				f.error = "Too many digits; clear or edit"
				return m, nil
			}
			days, err := strconv.Atoi(f.input)
			if err != nil || days < 1 || days > maxCustomRangeDays {
				f.error = "Enter days from 1 to 3650"
				return m, nil
			}
			f.customDays = days
		case "query":
			f.query = f.input
		}
		f.edit, f.error = "", ""
	case "backspace":
		if f.replace {
			f.input = ""
		} else if runes := []rune(f.input); len(runes) > 0 {
			f.input = string(runes[:len(runes)-1])
		}
		f.replace, f.overflow, f.error = false, false, ""
	case "ctrl+u":
		f.input, f.error, f.replace, f.overflow = "", "", false, false
	default:
		if len(msg.Runes) > 0 {
			input := string(msg.Runes)
			if f.edit == "custom" && strings.IndexFunc(input, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				f.error = "Use digits only"
				return m, nil
			}
			if f.replace {
				f.input = ""
			}
			f.replace, f.error = false, ""
			input = f.input + input
			if f.edit == "custom" {
				f.overflow = f.overflow || len(input) > 6
				input = input[:min(6, len(input))]
				if f.overflow {
					f.error = "Too many digits; clear or edit"
				}
			}
			f.input = input
		}
	}
	if f.edit == "options" {
		f.queries[f.category] = f.input
		f.rows[f.category] = 0
	}
	return m, nil
}

func (m *Model) applyFilters() {
	f := m.filters
	selectedPR, selectedContributor, person := m.selectedPRKey(), m.selectedContributorID(), m.personID
	selectedCommit := ""
	if m.showPaths {
		ids := m.glanceDetailIDs()
		if len(ids) > 0 {
			selectedCommit = ids[m.glanceSelected(ids)]
		}
	}
	m.filters = sharedFilterState{}
	if f.resetBoard {
		m.clearBoardFilters()
	}
	m.timeIdx, m.customRangeDays, m.sharedRepos = f.timeIdx, f.customDays, f.repos.clone()
	switch f.board {
	case "activity":
		m.activityPeople, m.activityHideBots = f.people.clone(), f.activityBots
		m.setGlanceQuery(f.query)
	case "contributors":
		m.hideBots, m.filterQuery = f.contributorBots, f.query
	case "prs":
		m.prFilters, m.prQuery = f.prs.clone(), f.query
	}
	m.searching, m.prSearching, m.glance.searching = false, false, false
	m.recomputeAuthors()
	m.personID = person
	m.selectContributor(selectedContributor)
	m.selectPR(selectedPR)
	if m.prDetail && (selectedPR == "" || m.selectedPRKey() != selectedPR) {
		m.prDetail, m.prOffset = false, 0
	}
	if m.showPaths && (selectedCommit == "" || !slices.Contains(m.glanceDetailIDs(), selectedCommit)) {
		m.showPaths, m.pathOffset = false, 0
	}
	m.normalizeContributorDetail()
}

func (m Model) handleFiltersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.filters.rows = maps.Clone(m.filters.rows)
	m.filters.queries = maps.Clone(m.filters.queries)
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	if m.filtersTextEditing() {
		return m.handleFilterText(msg)
	}
	switch msg.String() {
	case "esc":
		m.filters = sharedFilterState{}
		return m, nil
	case "q":
		return m.quit()
	case "enter":
		m.applyFilters()
		return m, nil
	case "left", "right", "tab", "shift+tab":
		categories := m.filterCategories()
		index := 0
		for i, category := range categories {
			if category.id == m.filters.category {
				index = i
			}
		}
		step := 1
		if msg.String() == "left" || msg.String() == "shift+tab" {
			step = -1
		}
		m.filters.category = categories[(index+step+len(categories))%len(categories)].id
		return m, nil
	case "/":
		kind := "options"
		if m.filters.category == "search" {
			kind = "query"
		}
		m.beginFilterEdit(kind)
		return m, nil
	}
	options := m.filterOptions()
	row := max(0, min(m.filters.rows[m.filters.category], len(options)-1))
	if msg.String() == " " && len(options) > 0 {
		m.stageFilterOption(options[row].value)
		return m, nil
	}
	_, page := filterModalSize(m.width, m.height)
	if next, ok := glanceMove(msg.String(), row, len(options), page); ok {
		m.filters.rows[m.filters.category] = next
	}
	return m, nil
}

func filterModalSize(width, height int) (int, int) {
	return max(6, min(84, width-2)), max(1, min(12, height-13))
}

func (m Model) filterCategoryLine(shared bool, width int) string {
	var categories []filterCategory
	selected := 0
	for _, category := range m.filterCategories() {
		if category.shared == shared {
			if category.id == m.filters.category {
				selected = len(categories)
			}
			categories = append(categories, category)
		}
	}
	group := "This board: "
	if shared {
		group = "Shared: "
	}
	labels := make([]string, len(categories))
	for i, category := range categories {
		label := category.label
		if category.id == "repos" {
			label = "Repos"
		} else if strings.HasSuffix(category.id, "reset") {
			label = "Reset"
		}
		if category.id == m.filters.category {
			label = "[" + label + "]"
		}
		labels[i] = label
	}
	start, end := selected, selected+1
	text := func() string {
		left, right := "", ""
		if start > 0 {
			left = "‹ "
		}
		if end < len(labels) {
			right = " ›"
		}
		return group + left + strings.Join(labels[start:end], " ") + right
	}
	for end < len(labels) {
		end++
		if ansi.StringWidth(text()) > width {
			end--
			break
		}
	}
	for start > 0 {
		start--
		if ansi.StringWidth(text()) > width {
			start++
			break
		}
	}
	line := text()
	if strings.Contains(line, "[") {
		return StyleTitle.Render(padCells(line, width))
	}
	return StyleDimWhite.Render(padCells(line, width))
}

func (m Model) renderFiltersModal(background string, width, height int) string {
	if width < 40 || height < 18 {
		return renderCenteredModal(background, []string{"Filters", "Resize to 40×18", "Esc cancels"}, max(6, min(30, width)), width, height)
	}
	panelWidth, page := filterModalSize(width, height)
	inner := panelWidth - 4
	board := map[string]string{"activity": "Activity", "contributors": "Contributors", "prs": "PRs"}[m.filters.board]
	content := []string{StyleTitle.Render("Filters · " + board), m.filterCategoryLine(true, inner), m.filterCategoryLine(false, inner)}
	description := "Space stages choices · Enter applies all"
	switch m.filters.category {
	case "range":
		description = "Date range applies to all three boards"
	case "repos":
		description = "Repository scope applies to all boards"
	case "shared-reset":
		description = "All time + all included repositories"
	case "board-reset":
		description = "Clear board filters; keep shared scope"
	case "people", "author", "review", "checks", "state":
		description = "Choose any · selections combine across tabs"
	}
	content = append(content, StyleDimWhite.Render(padCells(description, inner)))
	query := m.filters.queries[m.filters.category]
	field := "/ Find options"
	if query != "" {
		field = "/ " + displayText(query)
	}
	if m.filters.category == "search" {
		field = "Search: " + displayText(m.filters.query)
	}
	if m.filtersTextEditing() {
		prefix := "/ "
		switch m.filters.edit {
		case "query":
			prefix = "Search: "
		case "custom":
			prefix = "Days (1–3650): "
		}
		field = prefix + ansi.Truncate(displayText(m.filters.input), max(1, inner-ansi.StringWidth(prefix)-1), "…") + "▌"
	}
	content = append(content, lipgloss.NewStyle().Foreground(ColorBrightWht).Background(ColorRowEven).Render(padCells(field, inner)), StyleDimCyan.Render(strings.Repeat("─", inner)))
	options := m.filterOptions()
	selected := max(0, min(m.filters.rows[m.filters.category], len(options)-1))
	start := max(0, min(selected-page/2, len(options)-page))
	end := min(len(options), start+page)
	for i := start; i < end; i++ {
		cursor, mark := "  ", "[ ] "
		if i == selected {
			cursor = "› "
		}
		if m.filterOptionSelected(options[i].value) {
			mark = "[✓] "
		}
		line := padCells(cursor+mark+displayText(options[i].label), inner)
		if i == selected {
			line = selectedRowBackground(StyleTitle.Background(ColorRowSelect).Render(line))
		}
		content = append(content, line)
	}
	if len(options) == 0 {
		content = append(content, StyleAmber.Render("No matching options"))
	}
	for len(content) < 6+page {
		content = append(content, "")
	}
	position := fmt.Sprintf("%d–%d of %d · staged choices", start+1, end, len(options))
	if len(options) == 0 {
		position = "0 options · staged choices"
	}
	if m.filters.error != "" {
		position = m.filters.error
	}
	content = append(content, StyleAmber.Render(padCells(position, inner)))
	if m.filtersTextEditing() {
		content = append(content, StyleTitle.Render(padCells("Enter keep edit · Esc cancel edit", inner)), StyleDimWhite.Render(padCells("Ctrl+U clear · F1 help", inner)))
	} else {
		actions := "Space toggle · Enter apply · Esc cancel"
		if ansi.StringWidth(actions) > inner {
			actions = "Space · Enter apply · Esc cancel"
		}
		hints := "↑↓ select · ←→/Tab category · / find · ? help"
		if ansi.StringWidth(hints) > inner {
			hints = "↑↓ · ←→/Tab · / find · ? help"
		}
		content = append(content, StyleTitle.Render(padCells(actions, inner)), StyleDimWhite.Render(padCells(hints, inner)))
	}
	return renderCenteredModal(background, content, panelWidth, width, height)
}
