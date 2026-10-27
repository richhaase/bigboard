package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFilterOverviewGroupsEveryApplicableCategory(t *testing.T) {
	for _, board := range []struct {
		key, label string
		categories []string
	}{
		{"a", "Activity only", []string{"range", "repos", "people", "bots", "search"}},
		{"c", "Contributors only", []string{"range", "repos", "bots", "search"}},
		{"p", "PRs only", []string{"range", "repos", "mine", "author", "review", "checks", "state", "search"}},
	} {
		m := boardLayoutFixture()
		m.width, m.height = 120, 36
		m = keyboardContractPress(t, m, board.key, "f")
		if !m.filters.open || !m.filters.categoryFocus || m.filters.category != "range" {
			t.Fatalf("%s did not open the category overview", board.key)
		}
		var ids []string
		view := ansi.Strip(m.View())
		for _, category := range m.filterCategories() {
			ids = append(ids, category.id)
			if !strings.Contains(view, category.label) {
				t.Fatalf("%s overview hid %s:\n%s", board.key, category.label, view)
			}
		}
		if !slices.Equal(ids, board.categories) {
			t.Fatalf("%s categories = %v, want %v", board.key, ids, board.categories)
		}
		for _, want := range []string{"Shared scope", board.label, "No pending changes"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%s overview missing %q:\n%s", board.key, want, view)
			}
		}
	}
}

func TestFilterPaneFocusPreservesCategoryAndOptionPosition(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p", "f", "down", " ", "down", "down")
	if m.filters.categoryFocus || m.filters.category != "repos" || m.filters.rows["repos"] != 2 || len(m.filters.repos) != 0 {
		t.Fatal("entering options or moving the cursor staged a choice")
	}
	m = keyboardContractPress(t, m, "left", "down", "up", "right")
	if m.filters.categoryFocus || m.filters.category != "repos" || m.filters.rows["repos"] != 2 {
		t.Fatal("category navigation discarded the option position")
	}
	m = keyboardContractPress(t, m, "tab")
	if !m.filters.categoryFocus || m.filters.category != "repos" {
		t.Fatal("Tab did not focus categories")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if cmd != nil || m.filters.categoryFocus || m.filters.category != "repos" || m.filters.rows["repos"] != 2 {
		t.Fatal("Shift+Tab did not return to the selected option")
	}
	m = keyboardContractPress(t, m, "left", "end")
	if m.filters.category != "search" || !m.filters.categoryFocus {
		t.Fatal("End did not select the last category")
	}
	m = keyboardContractPress(t, m, "home")
	if m.filters.category != "range" {
		t.Fatal("Home did not select the first category")
	}
	m = keyboardContractPress(t, m, "pgdown")
	if m.filters.category == "range" || !m.filters.categoryFocus {
		t.Fatal("PageDown did not move the category selection")
	}
	m = keyboardContractPress(t, m, "pgup")
	if m.filters.category != "range" || len(m.filters.repos) != 0 || m.filters.timeIdx != m.timeIdx {
		t.Fatal("category paging staged a choice or failed to return to the first category")
	}
}

func TestDirectFilterShortcutsFocusTheirOptions(t *testing.T) {
	for _, shortcut := range []struct{ key, category string }{{"t", "range"}, {"u", "author"}, {"v", "review"}, {"k", "checks"}, {"d", "state"}} {
		m := boardLayoutFixture()
		m.width, m.height = 80, 24
		m = keyboardContractPress(t, m, "p", shortcut.key)
		if !m.filters.open || m.filters.categoryFocus || m.filters.category != shortcut.category {
			t.Fatalf("%s did not focus %s options", shortcut.key, shortcut.category)
		}
	}
}

func TestFilterPendingIndicatorsFollowActualDraftChanges(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "p", "f", "down", "right", "down")
		if !strings.Contains(ansi.Strip(m.View()), "No pending changes") {
			t.Fatalf("%v cursor movement appeared as a pending change", size)
		}
		m = keyboardContractPress(t, m, " ", "left")
		view := ansi.Strip(m.View())
		if strings.Contains(view, "No pending changes") || !strings.Contains(view, "1 pending") {
			t.Fatalf("%v staging did not display one pending change:\n%s", size, view)
		}
		marked := false
		for _, line := range strings.Split(view, "\n") {
			marked = marked || strings.Contains(line, "Repositories") && strings.Contains(line, "*")
		}
		if !marked {
			t.Fatalf("%v changed category was not marked:\n%s", size, view)
		}
		m = keyboardContractPress(t, m, "right", " ")
		if !strings.Contains(ansi.Strip(m.View()), "No pending changes") || len(m.sharedRepos) != 0 {
			t.Fatalf("%v undoing the choice did not clear the pending indicator", size)
		}
		m = keyboardContractPress(t, m, " ", "enter", "f")
		if len(m.sharedRepos) != 1 || !strings.Contains(ansi.Strip(m.View()), "No pending changes") {
			t.Fatalf("%v reopening an applied draft still showed pending changes", size)
		}
	}
}

func TestFilterResetActionsRemainStagedAndPreserveFocus(t *testing.T) {
	for _, focusCategories := range []bool{false, true} {
		m := boardLayoutFixture()
		m.width, m.height = 80, 24
		m = keyboardContractPress(t, m, "p")
		m.sharedRepos = filterValues{"/sample/gateway"}
		m.prFilters.review, m.prQuery = filterValues{"Approved"}, "Change"
		m.openFilters("review")
		if focusCategories {
			m = keyboardContractPress(t, m, "left")
		}
		m = keyboardContractPress(t, m, "x")
		if m.filters.category != "review" || m.filters.categoryFocus != focusCategories || !m.filters.prs.empty() || m.filters.query != "" {
			t.Fatal("board reset did not preserve focus and clear the board draft")
		}
		if !slices.Equal(m.filters.repos, m.sharedRepos) || m.filters.timeIdx != m.timeIdx || !slices.Equal(m.prFilters.review, filterValues{"Approved"}) || m.prQuery != "Change" {
			t.Fatal("board reset changed shared scope or applied values before Enter")
		}
		m = keyboardContractPress(t, m, "X")
		if m.filters.category != "review" || m.filters.categoryFocus != focusCategories || len(m.filters.repos) != 0 || m.filters.timeIdx != len(TimePresets)-1 {
			t.Fatal("shared reset did not preserve focus and clear the shared draft")
		}
		m = keyboardContractPress(t, m, "esc", "f")
		if !slices.Equal(m.filters.repos, filterValues{"/sample/gateway"}) || !slices.Equal(m.filters.prs.review, filterValues{"Approved"}) || m.filters.query != "Change" {
			t.Fatal("canceled reset leaked into a reopened draft")
		}
	}
}

func TestFilterApplyCancelAndBoardSwitchUseFreshDrafts(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p")
	m = chooseFilterOption(t, m, "review", "Approved")
	m = keyboardContractPress(t, m, "enter")
	for _, exit := range []string{"esc", "a", "c", "p"} {
		m = chooseFilterOption(t, m, "review", "Needs review")
		m = chooseFilterOption(t, m, "repos", "/sample/gateway")
		m = keyboardContractPress(t, m, exit)
		if m.filters.open || !slices.Equal(m.prFilters.review, filterValues{"Approved"}) || len(m.sharedRepos) != 0 {
			t.Fatalf("%s applied or retained an abandoned filter draft", exit)
		}
		m = keyboardContractPress(t, m, "p", "f")
		if !m.filters.categoryFocus || !slices.Equal(m.filters.prs.review, filterValues{"Approved"}) || len(m.filters.repos) != 0 || !strings.Contains(ansi.Strip(m.View()), "No pending changes") {
			t.Fatalf("%s did not reopen a clean draft of the applied filters", exit)
		}
		m = keyboardContractPress(t, m, "enter")
	}
	m = chooseFilterOption(t, m, "review", "Needs review")
	m = keyboardContractPress(t, m, "enter", "f")
	if len(m.prFilters.review) != 2 || !slices.Equal(m.filters.prs.review, m.prFilters.review) || !strings.Contains(ansi.Strip(m.View()), "No pending changes") {
		t.Fatal("a later Apply reused stale values from a canceled draft")
	}
}
