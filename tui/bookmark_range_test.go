package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestBookmarkRangeStagesCancelsAndResetsAcrossBoards(t *testing.T) {
	m := prRangeFixture()
	m.options.Bookmark = &BookmarkOptions{Time: time.Now().Add(-48 * time.Hour)}
	m = keyboardContractPress(t, m, "t", "end", " ")
	if m.sinceLastLookRange || !m.filters.sinceLastLook || !m.filterOptionSelected("since-last-look") {
		t.Fatal("bookmark range was not staged independently")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.sinceLastLookRange || m.rangeLabel() != "14 days" {
		t.Fatal("cancel changed the applied range")
	}
	m = keyboardContractPress(t, m, "t", "end", " ", "enter")
	for _, board := range []string{"a", "c", "p"} {
		m = keyboardContractPress(t, m, board)
		if !m.sinceLastLookRange || !strings.Contains(ansi.Strip(m.View()), "Shared: Since last look") {
			t.Fatalf("board %s lost the shared bookmark range", board)
		}
	}
	m = keyboardContractPress(t, m, "x", "t")
	if !m.sinceLastLookRange || m.filters.rows["range"] != len(TimePresets)+1 {
		t.Fatal("board reset cleared bookmark range or reopen selected another range")
	}
	m = keyboardContractPress(t, m, "up", " ", "2", "enter", "enter")
	if m.sinceLastLookRange || m.customRangeDays != 2 {
		t.Fatal("custom range did not replace bookmark range")
	}
	m = keyboardContractPress(t, m, "t", "end", " ", "enter", "t", "home", " ", "enter")
	if m.sinceLastLookRange || m.timeIdx != 0 {
		t.Fatal("preset did not replace bookmark range")
	}
	m = keyboardContractPress(t, m, "t", "end", " ", "enter")
	m.openFilters("shared-reset")
	m.stageFilterOption("reset")
	m.applyFilters()
	if m.sinceLastLookRange || m.rangeLabel() != "All time" {
		t.Fatal("shared reset did not clear bookmark range")
	}
}

func TestBookmarkRangeUnavailableDoesNotAddAnUnusablePreset(t *testing.T) {
	for _, bookmark := range []*BookmarkOptions{nil, {}, {Error: "Cannot read bookmark"}} {
		m := awarenessFixture()
		m.options.Bookmark = bookmark
		m.openFilters("range")
		if slices.ContainsFunc(m.filterOptions(), func(option prFilterOption) bool { return option.value == "since-last-look" }) {
			t.Fatal("missing bookmark exposed an unusable date preset")
		}
		m.stageFilterOption("since-last-look")
		if m.filters.sinceLastLook {
			t.Fatal("missing bookmark could still be selected")
		}
	}
}

func TestBookmarkRangeUsesExactStableCutoffForGitAndPRs(t *testing.T) {
	cutoff := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	dates := []time.Time{cutoff.Add(-time.Nanosecond), cutoff, cutoff.Add(time.Nanosecond), time.Now().Add(24 * time.Hour), {}}
	m := prRangeFixture()
	m.options.Bookmark = &BookmarkOptions{Time: cutoff}
	m.sinceLastLookRange = true
	m.timeIdx = len(TimePresets) - 1
	m.local.records = nil
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = nil
	for i, at := range dates {
		m.local.records = append(m.local.records, git.CommitRecord{RepoID: "/api", CommitID: string(rune('a' + i)), Date: at})
		snapshot.prs = append(snapshot.prs, gh.PullRequest{Repo: "org/repo", Number: i + 1, UpdatedAt: at})
	}
	m.prState.snapshots["org/repo"] = snapshot
	for _, now := range []time.Time{time.Now(), time.Now().Add(24 * time.Hour)} {
		records := m.filteredRecordsAt(now)
		if len(records) != 2 || records[0].CommitID != "c" || records[1].CommitID != "d" {
			t.Fatalf("Git range rounded/drifted its cutoff or lost future records: %+v", records)
		}
		if start, bounded := m.rangeCutoffAt(now); !bounded || !start.Equal(cutoff) {
			t.Fatal("bookmark cutoff changed with wall clock")
		}
	}
	var numbers []int
	for _, pr := range m.prsInViewForRepository("/api") {
		numbers = append(numbers, pr.Number)
	}
	if !reflect.DeepEqual(numbers, []int{3, 4}) {
		t.Fatalf("PR range disagreed with Git at the bookmark boundary: %v", numbers)
	}
}

func TestBookmarkRangeChartsUseBookmarkCalendarBoundary(t *testing.T) {
	location, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	cutoff := time.Date(2026, 10, 31, 23, 30, 0, 0, location)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, location)
	m := rangeChartDetailModel(now, []git.CommitRecord{
		{RepoID: "/keep", Author: "Ada", Email: "ada@example.com", Date: cutoff, Added: 100},
		{RepoID: "/keep", Author: "Ada", Email: "ada@example.com", Date: cutoff.Add(time.Nanosecond), Added: 5},
		{RepoID: "/keep", Author: "Ada", Email: "ada@example.com", Date: now.AddDate(0, 0, 1), Added: 7},
	})
	m.options.Bookmark = &BookmarkOptions{Time: cutoff}
	m.sinceLastLookRange = true
	window := m.activityWindowAt(now)
	if window.timelineLabel() != "daily" || !window.startsAt().Equal(cutoff) {
		t.Fatal("chart bounds did not use the saved instant")
	}
	_, first, last, _ := window.heatmapDates(80)
	if first.Format("2006-01-02") != "2026-10-31" || last.Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("DST changed bookmark calendar coverage: %v–%v", first, last)
	}
	timeline := rangeChartTimeline(t, m.operativeDetailContentAt(now))
	requireRangeChartCount(t, timeline, "Oct 31 2026", 1)
	requireRangeChartCount(t, timeline, "Nov 01 2026", 0)
	requireRangeChartCount(t, timeline, "Nov 03 2026", 1)
}
