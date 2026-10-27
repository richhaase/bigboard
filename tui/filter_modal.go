package tui

import (
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sharedFilterState struct {
	open                          bool
	board, category               string
	categoryFocus                 bool
	rows                          map[string]int
	queries                       map[string]string
	edit, input, prior, error     string
	priorRow                      int
	replace, overflow             bool
	timeIdx, customDays           int
	sinceLastLook                 bool
	repos, people                 filterValues
	activityBots, contributorBots bool
	prs                           prFilters
	query                         string
	resetBoard                    bool
}

type filterCategory struct {
	id, label string
	shared    bool
}

func (m Model) filtersTextEditing() bool {
	return m.filters.open && m.filters.edit != ""
}

func (m Model) filterCategories() []filterCategory {
	categories := []filterCategory{{"range", "Date range", true}, {"repos", "Repositories", true}}
	switch m.filters.board {
	case "prs":
		categories = append(categories, filterCategory{"mine", "Mine", false}, filterCategory{"author", "Authors", false}, filterCategory{"review", "Review", false}, filterCategory{"checks", "Checks", false}, filterCategory{"state", "State", false})
	case "activity":
		categories = append(categories, filterCategory{"people", "Contributors", false}, filterCategory{"bots", "Bots", false})
	default:
		categories = append(categories, filterCategory{"bots", "Bots", false})
	}
	return append(categories, filterCategory{"search", "Search", false})
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
			sinceLastLook: m.sinceLastLookRange,
			people:        m.activityPeople.clone(), activityBots: m.activityHideBots, contributorBots: m.hideBots,
			prs: m.prFilters.clone(), query: query,
		}
	}
	m.filters.rows = maps.Clone(m.filters.rows)
	m.filters.queries = maps.Clone(m.filters.queries)
	m.filters.categoryFocus = category == ""
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
		if m.filters.sinceLastLook && m.bookmarkRangeAvailable() {
			m.filters.rows["range"] = len(TimePresets) + 1
		}
	}
}

func (m Model) filterDraftModel() Model {
	m.sharedRepos = m.filters.repos.clone()
	m.activityPeople = m.filters.people.clone()
	m.activityHideBots, m.hideBots = m.filters.activityBots, m.filters.contributorBots
	m.timeIdx, m.customRangeDays = m.filters.timeIdx, m.filters.customDays
	m.sinceLastLookRange = m.filters.sinceLastLook
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
		if m.bookmarkRangeAvailable() {
			options = append(options, prFilterOption{"since-last-look", "Since last look"})
		}
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
	case "mine", "author", "review", "checks", "state":
		draft.prFilterKind, draft.prFilterQuery = m.filters.category, ""
		options = draft.prFilterOptions()
	case "search":
		options = []prFilterOption{{"edit", "Edit this board's search"}, {"clear", "Clear this search"}}
	}
	query := strings.ToLower(m.filters.queries[m.filters.category])
	if query == "" || m.filters.category == "search" {
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
		if value == "since-last-look" {
			return f.sinceLastLook
		}
		if value == "custom" {
			return !f.sinceLastLook && f.customDays > 0
		}
		return !f.sinceLastLook && f.customDays == 0 && value == strconv.Itoa(f.timeIdx)
	case "repos":
		values = f.repos
	case "people":
		values = f.people
	case "mine", "author", "review", "checks", "state":
		values = f.prs.values(f.category)
	case "bots":
		hide := f.activityBots
		if f.board == "contributors" {
			hide = f.contributorBots
		}
		return hide == (value == "hidden")
	case "search":
		return value == "clear" && f.query == ""
	}
	return value == "" && len(values) == 0 || value != "" && slices.Contains(values, value)
}

func (m *Model) beginFilterEdit(kind string) {
	f := &m.filters
	f.categoryFocus = false
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
		} else if value == "since-last-look" && m.bookmarkRangeAvailable() {
			f.sinceLastLook, f.customDays = true, 0
		} else if index, err := strconv.Atoi(value); err == nil {
			f.timeIdx, f.customDays = index, 0
			f.sinceLastLook = false
		}
	case "repos":
		f.repos = f.repos.toggle(value)
	case "people":
		f.people = f.people.toggle(value)
	case "mine", "author", "review", "checks", "state":
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
	}
}

func (m *Model) stageSharedFilterReset() {
	f := &m.filters
	f.timeIdx, f.customDays, f.repos = len(TimePresets)-1, 0, nil
	f.sinceLastLook = false
}

func (m *Model) stageBoardFilterReset() {
	f := &m.filters
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
			f.sinceLastLook = false
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
	m.sinceLastLookRange = f.sinceLastLook
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
	case "left":
		m.filters.categoryFocus = true
		return m, nil
	case "right":
		m.filters.categoryFocus = false
		return m, nil
	case "tab", "shift+tab":
		m.filters.categoryFocus = !m.filters.categoryFocus
		return m, nil
	case "x":
		m.stageBoardFilterReset()
		return m, nil
	case "X":
		m.stageSharedFilterReset()
		return m, nil
	case "/":
		kind := "options"
		if m.filters.category == "search" {
			kind = "query"
		}
		m.beginFilterEdit(kind)
		return m, nil
	}
	if m.filters.categoryFocus {
		if msg.String() == " " {
			m.filters.categoryFocus = false
			return m, nil
		}
		categories := m.filterCategories()
		index := 0
		for i, category := range categories {
			if category.id == m.filters.category {
				index = i
				break
			}
		}
		_, page := filterModalSize(m.width, m.height)
		if next, ok := glanceMove(msg.String(), index, len(categories), page); ok {
			m.filters.category = categories[next].id
		}
		return m, nil
	}
	options := m.filterOptions()
	row := max(0, min(m.filters.rows[m.filters.category], len(options)-1))
	if msg.String() == " " && len(options) > 0 {
		m.stageFilterOption(options[row].value)
		return m, nil
	}
	page := filterOptionPageSize(m.width, m.height)
	if next, ok := glanceMove(msg.String(), row, len(options), page); ok {
		m.filters.rows[m.filters.category] = next
	}
	return m, nil
}
