package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func chooseFilterOption(t *testing.T, m Model, category, value string) Model {
	t.Helper()
	m.openFilters(category)
	for i, option := range m.filterOptions() {
		if option.value == value {
			m.filters.rows[category] = i
			return keyboardContractPress(t, m, " ")
		}
	}
	t.Fatalf("missing %s option %q", category, value)
	return m
}

func TestSharedFiltersStageAllFacetsAndCancelAtomically(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p")
	beforeRange := m.timeIdx
	m = chooseFilterOption(t, m, "repos", "/sample/gateway")
	m = chooseFilterOption(t, m, "repos", "/sample/web-client")
	m = chooseFilterOption(t, m, "author", "contributor01")
	m = chooseFilterOption(t, m, "author", "contributor02")
	m = chooseFilterOption(t, m, "review", "Approved")
	m = chooseFilterOption(t, m, "review", "Needs review")
	m = chooseFilterOption(t, m, "checks", "Passing")
	m = chooseFilterOption(t, m, "checks", "Pending")
	m = chooseFilterOption(t, m, "state", "open")
	m = chooseFilterOption(t, m, "state", "draft")
	if len(m.visiblePRs()) != 48 || len(m.sharedRepos) > 0 || !m.prFilters.empty() || m.timeIdx != beforeRange {
		t.Fatal("staging modified the applied filters")
	}
	draft := m
	m = keyboardContractPress(t, m, "esc")
	if m.filters.open || len(m.visiblePRs()) != 48 || len(m.sharedRepos) > 0 || !m.prFilters.empty() {
		t.Fatal("cancel changed applied filters")
	}
	m = keyboardContractPress(t, draft, "enter")
	prs := m.visiblePRs()
	if m.filters.open || len(m.sharedRepos) != 2 || len(prs) != 2 || prs[0].Number != 100 || prs[1].Number != 101 {
		t.Fatalf("OR within facets and AND across facets returned %+v", prs)
	}
	if len(m.prFilters.author) != 2 || len(m.prFilters.review) != 2 || len(m.prFilters.checks) != 2 || len(m.prFilters.state) != 2 {
		t.Fatal("Enter failed to apply every staged category")
	}
	for _, category := range []string{"author", "review", "checks", "state", "repos"} {
		m = chooseFilterOption(t, m, category, "")
	}
	m = keyboardContractPress(t, m, "enter")
	if len(m.visiblePRs()) != 48 || !m.prFilters.empty() || len(m.sharedRepos) != 0 {
		t.Fatal("All did not clear each multi-select category")
	}
}

func TestSharedFiltersDraftDoesNotAliasAppliedOrPriorModels(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m.sharedRepos = filterValues{"/sample/gateway", "/sample/web-client"}
	m.activityPeople = filterValues{"first", "second"}
	m.prFilters.author = filterValues{"one", "two"}
	m.openFilters("repos")
	m.filters.repos[0], m.filters.people[0], m.filters.prs.author[0] = "changed", "changed", "changed"
	if m.sharedRepos[0] != "/sample/gateway" || m.activityPeople[0] != "first" || m.prFilters.author[0] != "one" {
		t.Fatal("draft slices alias the applied filters")
	}
	prior := m
	m = keyboardContractPress(t, m, "down", "/", "find")
	if prior.filters.rows["repos"] != 0 || prior.filters.queries["repos"] != "" {
		t.Fatal("modal input mutated the previous Bubble Tea model")
	}
	if m.filters.queries["repos"] != "find" {
		t.Fatal("search did not update the current draft")
	}
}

func TestSharedFilterNavigationDoesNotStageHighlightedRows(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m = keyboardContractPress(t, m, "p", "f", "right", "down")
	if !m.filterOptionSelected("") || m.filterOptionSelected(m.filterOptions()[1].value) {
		t.Fatal("cursor was confused with a checked selection")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[✓] All included repositories") || !strings.Contains(view, "› [ ] analytics") {
		t.Fatalf("cursor and checkbox are not visually distinct:\n%s", view)
	}
	m = keyboardContractPress(t, m, "left", "right", "tab")
	if m.filters.category != "shared-reset" {
		t.Fatal("arrows and Tab did not traverse the same categories")
	}
	m = keyboardContractPress(t, m, "enter")
	if len(m.sharedRepos) > 0 || m.timeIdx != DefaultTimeIndex {
		t.Fatal("Enter applied an unselected option or reset")
	}
}

func TestSharedFilterTextOwnsKeysAndEscapeRestoresDraft(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m.openFilters("repos")
	m = keyboardContractPress(t, m, "/", "gate", "enter")
	m = keyboardContractPress(t, m, "/", "a", "c", "p", "q", "?", " ")
	if !m.filtersTextEditing() || m.filters.input != "gateacpq? " || m.showPRs || m.quitting || m.showHelp {
		t.Fatal("option text leaked a shortcut")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.filters.queries["repos"] != "gate" || !m.filters.open || m.filtersTextEditing() {
		t.Fatal("Escape did not restore the previous option search")
	}
	m.openFilters("search")
	m = keyboardContractPress(t, m, " ", "literal acp", "enter")
	if m.glanceQuery() != "" || m.filters.query != "literal acp" || !m.filters.open {
		t.Fatal("board query applied before the modal transaction")
	}
	m = keyboardContractPress(t, m, " ", " discarded", "esc", "enter")
	if m.glanceQuery() != "literal acp" {
		t.Fatal("Escape failed to undo only the pending query edit")
	}
}

func TestSharedFiltersCustomDaysValidateAndRemainStaged(t *testing.T) {
	for _, input := range []string{"0", "3651", "9999999999999999999999999"} {
		m := boardLayoutFixture()
		m.width, m.height = 80, 24
		m = chooseFilterOption(t, m, "range", "custom")
		m = keyboardContractPress(t, m, input, "enter")
		if !m.filtersTextEditing() || m.filters.error == "" || m.customRangeDays != 0 {
			t.Fatalf("invalid custom days %q applied", input)
		}
		m = keyboardContractPress(t, m, "ctrl+u", "3650", "enter")
		if m.filtersTextEditing() || m.filters.customDays != 3650 || m.customRangeDays != 0 {
			t.Fatal("valid custom days were not staged")
		}
		m = keyboardContractPress(t, m, "enter")
		if m.customRangeDays != 3650 || m.filters.open {
			t.Fatal("custom days were not applied")
		}
	}
}

func TestSharedFiltersResetScopeAndBoardIndependently(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		m := boardLayoutFixture()
		m.width, m.height = 80, 24
		m = keyboardContractPress(t, m, board)
		m.sharedRepos = filterValues{"/sample/gateway"}
		m.activityPeople, m.activityHideBots = filterValues{"missing-person"}, true
		m.hideBots, m.filterQuery = true, "kept-contributor-search"
		m.prFilters.review, m.prQuery = filterValues{"Approved"}, "kept-pr-search"
		m.setGlanceQuery("kept-activity-search")
		m.personID, m.prRepoID = "context-person", "/sample/gateway"
		beforeRange := m.timeIdx
		m = chooseFilterOption(t, m, "board-reset", "reset")
		m = keyboardContractPress(t, m, "enter")
		if !reflect.DeepEqual(m.sharedRepos, filterValues{"/sample/gateway"}) || m.timeIdx != beforeRange || m.personID != "context-person" || m.prRepoID != "/sample/gateway" {
			t.Fatalf("%s reset discarded shared scope or navigation", board)
		}
		switch board {
		case "a":
			if len(m.activityPeople) > 0 || m.activityHideBots || m.glanceQuery() != "" || !m.hideBots || m.prQuery != "kept-pr-search" {
				t.Fatal("Activity reset affected the wrong board")
			}
		case "c":
			if m.hideBots || m.filterQuery != "" || !m.activityHideBots || m.prQuery != "kept-pr-search" {
				t.Fatal("Contributors reset affected the wrong board")
			}
		case "p":
			if !m.prFilters.empty() || m.prQuery != "" || !m.hideBots || !m.activityHideBots {
				t.Fatal("PR reset affected the wrong board")
			}
		}
		m = chooseFilterOption(t, m, "shared-reset", "reset")
		m = keyboardContractPress(t, m, "enter")
		if len(m.sharedRepos) != 0 || m.timeIdx != len(TimePresets)-1 || m.customRangeDays != 0 || m.prRepoID != "/sample/gateway" || m.personID != "context-person" {
			t.Fatal("shared reset changed navigation or failed to clear shared scope")
		}
	}
}

func TestSharedFiltersPreserveSelectionsAndCloseHiddenPRInspector(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p", "down", "enter")
	key := m.selectedPRKey()
	m = chooseFilterOption(t, m, "repos", "/sample/gateway")
	m = keyboardContractPress(t, m, "enter")
	if m.selectedPRKey() != key || !m.prDetail {
		t.Fatal("visible PR selection or inspector was lost")
	}
	m = chooseFilterOption(t, m, "review", "Changes req.")
	m = keyboardContractPress(t, m, "enter")
	if m.selectedPRKey() == key || m.prDetail {
		t.Fatal("hidden PR inspector remained open")
	}
	m = keyboardContractPress(t, m, "c", "down")
	selected := m.selectedContributorID()
	m.openFilters("range")
	m = keyboardContractPress(t, m, "enter")
	if m.selectedContributorID() != selected {
		t.Fatal("unchanged contributor selection was lost")
	}
}

func TestActivityContributorFiltersSupportMultiplePeople(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m.openFilters("people")
	options := m.filterOptions()
	first, second := options[1].value, options[2].value
	m = chooseFilterOption(t, m, "people", first)
	m = chooseFilterOption(t, m, "people", second)
	m = chooseFilterOption(t, m, "bots", "hidden")
	if len(m.activityPeople) != 0 || m.activityHideBots {
		t.Fatal("Activity filters applied before Enter")
	}
	m = keyboardContractPress(t, m, "enter")
	if len(m.activityPeople) != 2 || !slices.Contains(m.activityPeople, first) || !slices.Contains(m.activityPeople, second) || !m.activityHideBots || m.hideBots {
		t.Fatal("Activity filters were not independent of Contributors")
	}
}

func TestSharedFilterModalFitsEveryBoardAndCategory(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		for _, board := range []string{"a", "c", "p"} {
			m := boardLayoutFixture()
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, board, "f")
			for _, category := range m.filterCategories() {
				m.openFilters(category.id)
				view := ansi.Strip(m.View())
				lines := strings.Split(view, "\n")
				if len(lines) != m.height {
					t.Fatalf("%s/%s/%v modal height changed", board, category.id, size)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("%s/%s/%v modal overflowed", board, category.id, size)
					}
				}
				for _, want := range []string{"Shared:", "This board:", "↑↓ move", "Enter apply", "Esc cancel"} {
					if !strings.Contains(view, want) {
						t.Fatalf("%s/%s/%v missing %q:\n%s", board, category.id, size, want, view)
					}
				}
			}
		}
	}
}
