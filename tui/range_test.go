package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		m.activeAuthorID = selected
		m.activeOperative = m.displayedAuthors()[1].Name
		m.statDetailOffset = 5
		m = rangeKey(t, m, "t")
		m = rangeKey(t, m, "up")
		if m.timeIdx != DefaultTimeIndex {
			t.Fatal("browsing must not apply a range")
		}
		m = rangeKey(t, m, "esc")
		if m.rangePicker.open || m.timeIdx != DefaultTimeIndex || m.statDetailOffset != 5 {
			t.Fatal("cancel changed range or viewport")
		}
		m = rangeKey(t, m, "t")
		m = rangeKey(t, m, "down")
		m = rangeKey(t, m, "enter")
		if m.rangePicker.open || m.timeIdx != 3 || m.customRangeDays != 0 || m.statDetailOffset != 0 {
			t.Fatal("preset failed to apply")
		}
		if m.viewMode != view || m.selectedRepoID != "/api" || m.displayedAuthors()[m.selectedRow].ID != selected || m.activeAuthorID != selected {
			t.Fatal("range lost navigation identity")
		}
	}
}

func TestRangeCustomValidationAndSharedFiltering(t *testing.T) {
	m := awarenessFixture()
	m.timeIdx = DefaultTimeIndex
	m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: "old", Author: "Old", Email: "old@x", RepoID: "/api", RepoName: "api", Date: time.Now().Add(-20 * 24 * time.Hour)})
	m.recomputeAuthors()
	m = rangeKey(t, m, "t")
	m = rangeKey(t, m, "c")
	m = rangeKey(t, m, "enter")
	for _, input := range []string{"0", "3651", "99999999999", "0036501", ""} {
		m = rangeKey(t, m, "ctrl+u")
		if input != "" {
			m = rangeKey(t, m, input)
		}
		m = rangeKey(t, m, "enter")
		if !m.rangePicker.open || m.rangePicker.error == "" || m.customRangeDays != 0 || m.timeIdx != DefaultTimeIndex {
			t.Fatalf("invalid custom input %q was applied", input)
		}
	}
	m = rangeKey(t, m, "ctrl+u")
	m = rangeKey(t, m, "abc")
	if m.rangePicker.error == "" {
		t.Fatal("nonnumeric entry lacked feedback")
	}
	m = rangeKey(t, m, "21")
	m = rangeKey(t, m, "enter")
	if m.customRangeDays != 21 || m.rangePicker.open || len(m.filteredRecords()) != 4 {
		t.Fatal("custom range did not update common filter")
	}
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate, ViewOperative} {
		m.viewMode = view
		if !strings.Contains(ansi.Strip(m.viewContent()), "Range: 21 days · t to change") {
			t.Fatalf("view %v does not show custom range", view)
		}
	}
	m = rangeKey(t, m, "t")
	if m.rangePicker.cursor != len(TimePresets) {
		t.Fatal("custom range not selected on reopen")
	}
	m = rangeKey(t, m, "enter")
	m = rangeKey(t, m, "1")
	m = rangeKey(t, m, "esc")
	if m.customRangeDays != 21 {
		t.Fatal("custom cancel altered applied range")
	}
	m = rangeKey(t, m, "t")
	m = rangeKey(t, m, "home")
	m = rangeKey(t, m, "enter")
	if m.timeIdx != 0 || m.customRangeDays != 0 || len(m.filteredRecords()) != 3 {
		t.Fatal("preset did not replace custom filter")
	}
}

func TestRangePickerOwnsKeysWithoutRefreshOrNavigation(t *testing.T) {
	m := glanceBusyFixture()
	m = rangeKey(t, m, "right")
	before := m.glance
	m = rangeKey(t, m, "t")
	for _, k := range []string{"left", "right", "R", "r", "v", "p", "q", "t", "/", "e"} {
		m = rangeKey(t, m, k)
	}
	if !m.rangePicker.open || m.quitting || !reflect.DeepEqual(before, m.glance) {
		t.Fatal("picker leaked a key to underlying view")
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
		m = rangeKey(t, m, "/")
		m = rangeKey(t, m, "t")
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
		if cmd != nil || m.rangePicker.open || (view == ViewAwareness && m.glanceQuery() != "t") || (view == ViewAggregate && m.filterQuery != "t") {
			t.Fatal("range/nav keys escaped search")
		}
	}
	for _, mode := range []string{"help", "paths", "prs", "repos", "errors"} {
		m := glanceBusyFixture()
		switch mode {
		case "help":
			m.glance.help = true
		case "paths":
			m = rangeKey(t, m, "right")
			m = rangeKey(t, m, "right")
		case "prs":
			m.showPRs = true
		case "repos":
			m.viewMode = ViewRepoOverlay
		case "errors":
			m.showScanErrors = true
		}
		m = rangeKey(t, m, "t")
		if m.rangePicker.open {
			t.Fatalf("picker opened over %s", mode)
		}
	}
}

func TestRangePickerFitsTerminalAndKeepsApplyCancelVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}, {20, 8}} {
		m := awarenessFixture()
		m.width, m.height = size[0], size[1]
		m.timeIdx = DefaultTimeIndex
		for _, mode := range []string{"preset", "custom", "invalid"} {
			m = rangeKey(t, m, "t")
			if mode != "preset" {
				m = rangeKey(t, m, "c")
				m = rangeKey(t, m, "enter")
			}
			if mode == "invalid" {
				m = rangeKey(t, m, "0")
				m = rangeKey(t, m, "enter")
			}
			view := ansi.Strip(m.View())
			if lipgloss.Height(view) > m.height {
				t.Fatal("range modal exceeds height")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("range modal exceeds width")
				}
			}
			if size[0] >= 40 && (!strings.Contains(view, "Enter apply") || !strings.Contains(view, "Esc cancel")) {
				t.Fatalf("%v %s hides confirm/cancel", size, mode)
			}
			m = rangeKey(t, m, "esc")
		}
	}
}

func TestArrowDrillDownBackAndRootDoesNotQuit(t *testing.T) {
	m := awarenessFixture()
	initial := m.timeIdx
	for _, k := range []string{"left", "h"} {
		m = rangeKey(t, m, k)
		if m.quitting {
			t.Fatal("left at root quit")
		}
	}
	for i := 0; i < 3; i++ {
		m = rangeKey(t, m, "right")
	}
	if !m.showPaths || !m.glance.detailOpen || m.areaRepoID == "" {
		t.Fatal("right did not drill to inspector")
	}
	for i := 0; i < 3; i++ {
		m = rangeKey(t, m, "left")
	}
	if m.showPaths || m.glance.detailOpen || m.areaRepoID != "" || m.selectedRepoID != "/api" || m.timeIdx != initial {
		t.Fatal("left did not unwind with original selection/range")
	}
	m = rangeKey(t, m, "left")
	if m.quitting {
		t.Fatal("left after unwinding quit")
	}
}

func TestRangeChangePreservesNestedAreaAndCommit(t *testing.T) {
	m := glanceBusyFixture()
	m = rangeKey(t, m, "right")
	m = rangeKey(t, m, "4")
	m = rangeKey(t, m, "right")
	ids := m.glanceDetailIDs()
	m.glanceSelected(ids)
	before := m.glance
	m = rangeKey(t, m, "t")
	m = rangeKey(t, m, "home")
	m = rangeKey(t, m, "down")
	m = rangeKey(t, m, "enter")
	if !reflect.DeepEqual(before, m.glance) {
		t.Fatal("changing range lost nested selection")
	}
}

func TestRangeChangeKeepsFilteredOutPersonWithoutBroaderEvidence(t *testing.T) {
	m := awarenessFixture()
	m.allRecords[2].Date = time.Now().Add(-10 * 24 * time.Hour)
	m.recomputeAuthors()
	m = rangeKey(t, m, "right")
	m = rangeKey(t, m, "right")
	m.personID = "email:grace@x"
	m = rangeKey(t, m, "t")
	m = rangeKey(t, m, "home")
	m = rangeKey(t, m, "enter")
	if m.personID != "email:grace@x" || len(m.glanceEvidence()) != 0 {
		t.Fatal("range change broadened a filtered-out person's evidence")
	}
	if strings.Contains(m.View(), "Fix login") {
		t.Fatal("another person's commit appeared in filtered detail")
	}
	m = rangeKey(t, m, "left")
	if m.personID != "" || m.glance.frame.tab != glancePeople {
		t.Fatal("left should explicitly clear the empty person filter")
	}
}
