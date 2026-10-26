package tui

import (
	"strings"
	"testing"
	"time"

	gh "github.com/richhaase/bigboard/github"
)

func prRangeFixture() Model {
	m := prFixture()
	m.width, m.height = 120, 36
	m.timeIdx = DefaultTimeIndex
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = []gh.PullRequest{
		{Repo: "org/repo", Number: 1, Title: "Recent", UpdatedAt: time.Now().Add(-10 * time.Hour)},
		{Repo: "org/repo", Number: 2, Title: "Older", UpdatedAt: time.Now().Add(-20 * 24 * time.Hour)},
		{Repo: "org/repo", Number: 3, Title: "Unknown"},
		{Repo: "org/repo", Number: 4, Title: "Future", UpdatedAt: time.Now().Add(time.Hour)},
	}
	m.prState.snapshots["org/repo"] = snapshot
	return m
}

func TestPRRangePresetCustomAndAllTime(t *testing.T) {
	m := prRangeFixture()
	m = rangeKey(t, m, "p")
	if len(m.visiblePRs()) != 2 || len(m.allPRs()) != 4 {
		t.Fatal("default range did not filter updated time independently of snapshot inventory")
	}
	m = keyboardContractPress(t, m, "t", "d", "enter", "21", "enter")
	if m.rangePicker.open || m.customRangeDays != 21 || len(m.visiblePRs()) != 3 {
		t.Fatal("custom range did not apply from the PR board")
	}
	for _, board := range []string{"a", "l", "p"} {
		m = rangeKey(t, m, board)
		if !strings.Contains(m.View(), "Range: 21 days") {
			t.Fatalf("%s did not share the range", board)
		}
	}
	m = keyboardContractPress(t, m, "t", "end", "up", "enter")
	if m.rangeDuration() != 0 || len(m.visiblePRs()) != 4 {
		t.Fatal("All time did not restore old and unknown dates")
	}
	m = keyboardContractPress(t, m, "a", "t", "home", "enter", "p")
	if len(m.visiblePRs()) != 2 {
		t.Fatal("range set on another board did not carry into PRs")
	}
	m.prFilters.review, m.prQuery, m.hideBots = "Unknown", "Recent", true
	if prs := m.visiblePRs(); len(prs) != 1 || prs[0].Number != 1 {
		t.Fatal("range did not compose with PR filters and search")
	}
	m = rangeKey(t, m, "x")
	if m.rangeDuration() != 24*time.Hour || len(m.visiblePRs()) != 2 {
		t.Fatal("clearing PR filters changed the global range")
	}
}

func TestPRRangeSelectionCancelAndFilteredDetail(t *testing.T) {
	m := prRangeFixture()
	m.prSort = 3
	m = keyboardContractPress(t, m, "p", "end", "enter")
	selected := m.selectedPRKey()
	m = keyboardContractPress(t, m, "t", "end", "up", "esc")
	if !m.prDetail || m.selectedPRKey() != selected || m.rangeDuration() != 14*24*time.Hour {
		t.Fatal("cancel changed range or detail selection")
	}
	m = keyboardContractPress(t, m, "t", "end", "up", "enter")
	if !m.prDetail || m.selectedPRKey() != selected || m.prRow != 2 {
		t.Fatal("widening the range silently changed detail identity")
	}
	m = keyboardContractPress(t, m, "esc", "home", "down", "enter", "t", "home", "enter")
	if m.prDetail || len(m.visiblePRs()) != 2 {
		t.Fatal("filtered-out detail switched to another PR instead of closing")
	}
	m = keyboardContractPress(t, m, "/", "t")
	if m.rangePicker.open || m.prQuery != "t" {
		t.Fatal("range key escaped PR search")
	}
}
