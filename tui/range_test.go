package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func rangeKey(t *testing.T, m Model, k string) Model {
	t.Helper()
	next, cmd := m.Update(key(k))
	if cmd != nil {
		t.Fatalf("range/navigation key %q started a command", k)
	}
	return next.(Model)
}

func TestRangePickerPresetConfirmCancelAndSelection(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate, ViewOperative} {
		m := awarenessFixture()
		m.viewMode, m.timeIdx = view, DefaultTimeIndex
		m.selectedRow = 1
		selected := m.displayedAuthors()[1].ID
		m.activeAuthorID, m.activeOperative = selected, m.displayedAuthors()[1].Name
		m = keyboardContractPress(t, m, "t", "down", " ")
		if m.timeIdx != DefaultTimeIndex || !m.filters.open {
			t.Fatal("staging changed applied range")
		}
		m = rangeKey(t, m, "esc")
		if m.filters.open || m.timeIdx != DefaultTimeIndex {
			t.Fatal("cancel changed range")
		}
		m = keyboardContractPress(t, m, "t", "down", " ", "enter")
		if m.filters.open || m.timeIdx != 3 || m.customRangeDays != 0 {
			t.Fatal("preset failed to apply")
		}
		if m.viewMode != view || m.selectedRepoID != "/api" || m.selectedContributorID() != selected || m.activeAuthorID != selected {
			t.Fatal("range lost navigation identity")
		}
	}
}

func TestRangeCustomValidationAndSharedFiltering(t *testing.T) {
	m := awarenessFixture()
	m.timeIdx = DefaultTimeIndex
	m.local.records = append(m.local.records, git.CommitRecord{CommitID: "old", Author: "Old", Email: "old@x", RepoID: "/api", RepoName: "api", Date: time.Now().Add(-20 * 24 * time.Hour)})
	m.recomputeAuthors()
	m = keyboardContractPress(t, m, "t", "end", " ")
	for _, input := range []string{"0", "3651", "99999999999", "0036501", ""} {
		m = rangeKey(t, m, "ctrl+u")
		if input != "" {
			m = rangeKey(t, m, input)
		}
		m = rangeKey(t, m, "enter")
		if !m.filters.open || m.filters.error == "" || m.customRangeDays != 0 || m.timeIdx != DefaultTimeIndex {
			t.Fatalf("invalid custom input %q was applied", input)
		}
	}
	m = keyboardContractPress(t, m, "ctrl+u", "abc")
	if m.filters.error == "" {
		t.Fatal("nonnumeric entry lacked feedback")
	}
	m = keyboardContractPress(t, m, "21", "enter")
	if m.customRangeDays != 0 || m.filters.customDays != 21 {
		t.Fatal("custom editor bypassed pending changes")
	}
	m = rangeKey(t, m, "enter")
	if m.customRangeDays != 21 || m.filters.open || len(m.filteredRecords()) != 4 {
		t.Fatal("custom range did not update shared filter")
	}
	for _, board := range []string{"a", "c", "p"} {
		m = rangeKey(t, m, board)
		if !strings.Contains(ansi.Strip(m.View()), "Shared: 21 days") {
			t.Fatalf("%s does not show shared custom range", board)
		}
	}
	m = rangeKey(t, m, "t")
	if m.filters.rows["range"] != len(TimePresets) {
		t.Fatal("custom range not selected on reopen")
	}
	m = keyboardContractPress(t, m, " ", "1", "esc", "esc")
	if m.customRangeDays != 21 {
		t.Fatal("cancel altered applied custom range")
	}
	m = keyboardContractPress(t, m, "t", "home", " ", "enter")
	if m.timeIdx != 0 || m.customRangeDays != 0 || len(m.filteredRecords()) != 3 {
		t.Fatal("preset did not replace custom filter")
	}
}

func TestRangePickerOwnsKeysWithoutRefreshOrNavigation(t *testing.T) {
	m := glanceBusyFixture()
	m = rangeKey(t, m, "right")
	before := m.glance
	m = rangeKey(t, m, "t")
	for _, k := range []string{"left", "right", "R", "r", "v", "t", "e"} {
		m = rangeKey(t, m, k)
	}
	if !m.filters.open || m.quitting || !reflect.DeepEqual(before, m.glance) {
		t.Fatal("filter modal leaked navigation or refresh")
	}
	m = rangeKey(t, m, "esc")
	if !reflect.DeepEqual(before, m.glance) {
		t.Fatal("cancel changed detail identity")
	}
}

func TestRangeKeysRespectSearchAndOtherModals(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		m := awarenessFixture()
		m.viewMode = view
		m = keyboardContractPress(t, m, "/", "t")
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
		if cmd != nil || m.filters.open || (view == ViewAwareness && m.glanceQuery() != "t") || (view == ViewAggregate && m.filterQuery != "t") {
			t.Fatal("range/navigation escaped search")
		}
	}
	for _, mode := range []string{"help", "repos", "errors"} {
		m := glanceBusyFixture()
		switch mode {
		case "help":
			m.showHelp = true
		case "repos":
			m.openRepositoryControls()
		case "errors":
			m.showScanErrors = true
		}
		m = rangeKey(t, m, "t")
		if m.filters.open {
			t.Fatalf("filters opened over %s", mode)
		}
	}
}

func TestArrowDrillDownBackAndRootDoesNotQuit(t *testing.T) {
	m := awarenessFixture()
	initial := m.timeIdx
	m = rangeKey(t, m, "left")
	if m.quitting {
		t.Fatal("root Back quit")
	}
	m = keyboardContractPress(t, m, "right", "right", "right")
	if !m.showPaths || !m.glance.detailOpen || m.areaRepoID == "" {
		t.Fatal("right did not drill to inspector")
	}
	m = keyboardContractPress(t, m, "left", "left", "left")
	if m.showPaths || m.glance.detailOpen || m.areaRepoID != "" || m.selectedRepoID != "/api" || m.timeIdx != initial {
		t.Fatal("Back did not unwind with original selection/range")
	}
	m = rangeKey(t, m, "left")
	if m.quitting {
		t.Fatal("root Back quit")
	}
}

func TestRangeChangePreservesNestedAreaAndCommit(t *testing.T) {
	m := glanceBusyFixture()
	m = keyboardContractPress(t, m, "right", "4", "right")
	m.glanceSelected(m.glanceDetailIDs())
	before := m.glance
	m = keyboardContractPress(t, m, "t", "home", "down", " ", "enter")
	if !reflect.DeepEqual(before, m.glance) {
		t.Fatal("range change lost nested selection")
	}
}

func TestRangeChangeKeepsFilteredOutPersonWithoutBroaderEvidence(t *testing.T) {
	m := awarenessFixture()
	m.local.records[2].Date = time.Now().Add(-10 * 24 * time.Hour)
	m.recomputeAuthors()
	m = keyboardContractPress(t, m, "right", "right")
	m.personID = "email:grace@x"
	m = keyboardContractPress(t, m, "t", "home", " ", "enter")
	if m.personID != "email:grace@x" || len(m.glanceEvidence()) != 0 || strings.Contains(m.View(), "Fix login") {
		t.Fatal("range change broadened filtered-out person")
	}
	m = rangeKey(t, m, "left")
	if m.personID != "" || m.glance.frame.tab != glancePeople {
		t.Fatal("Back failed to return from person activity to People")
	}
}
