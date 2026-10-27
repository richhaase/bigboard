package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func rangeChartDetailModel(now time.Time, records []git.CommitRecord) Model {
	m := Model{
		local:    localSnapshot{records: records, repositories: []git.Repository{{ID: "/keep", Name: "keep"}}},
		viewMode: ViewOperative, activeAuthorID: "email:ada@example.com", activeOperative: "Ada",
		timeIdx: DefaultTimeIndex, width: 80, height: 28,
	}
	m.authors = stats.Aggregate(m.filteredRecordsAt(now))
	return m
}

func rangeChartTimeline(t *testing.T, content string) string {
	t.Helper()
	_, timeline, ok := strings.Cut(ansi.Strip(content), "ACTIVITY TIMELINE")
	if !ok {
		t.Fatalf("detail is missing its timeline:\n%s", ansi.Strip(content))
	}
	timeline, _, ok = strings.Cut(timeline, "ACTIVITY MATRIX")
	if !ok {
		t.Fatal("detail is missing its activity matrix")
	}
	return timeline
}

func requireRangeChartCount(t *testing.T, timeline, date string, count int) {
	t.Helper()
	pattern := fmt.Sprintf(`(?m)^\s+%s\s+%d(?:\s|$)`, regexp.QuoteMeta(date), count)
	if !regexp.MustCompile(pattern).MatchString(timeline) {
		t.Fatalf("missing %q with %d commits in timeline:\n%s", date, count, timeline)
	}
}

func TestRangeAwareContributorDetailUsesSelectedInterval(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.Local)
	records := []git.CommitRecord{
		{CommitID: "recent", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-24 * time.Hour), Added: 10},
		{CommitID: "prior", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: time.Date(2026, time.September, 26, 12, 0, 0, 0, time.Local), Added: 20},
		{CommitID: "spring", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: time.Date(2026, time.May, 20, 12, 0, 0, 0, time.Local), Added: 30},
		{CommitID: "ancient", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: time.Date(2025, time.January, 20, 12, 0, 0, 0, time.Local), Added: 40},
	}
	for _, tc := range []struct {
		name, interval, empty, active string
		days                          int
	}{
		{name: "14-day preset", interval: "daily", empty: "Oct 07 2026", active: "Oct 20 2026"},
		{name: "31-day custom", interval: "weekly", days: 31, empty: "Week of Sep 27 2026", active: "Week of Sep 20 2026"},
		{name: "181-day custom", interval: "monthly", days: 181, empty: "Jun 2026", active: "May 2026"},
		{name: "All time", interval: "monthly", days: -1, empty: "Nov 2025", active: "May 2026"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := rangeChartDetailModel(now, records)
			if tc.days < 0 {
				m.timeIdx = len(TimePresets) - 1
			} else {
				m.customRangeDays = tc.days
			}
			m.authors = stats.Aggregate(m.filteredRecordsAt(now))
			timeline := rangeChartTimeline(t, m.operativeDetailContentAt(now))
			if !strings.Contains(timeline, "· "+tc.interval) {
				t.Fatalf("range %q did not select %s buckets:\n%s", m.rangeLabel(), tc.interval, timeline)
			}
			requireRangeChartCount(t, timeline, tc.empty, 0)
			requireRangeChartCount(t, timeline, tc.active, 1)
		})
	}
}

func TestRangeAwareContributorDetailKeepsScopedCanonicalAndFutureCounts(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.Local)
	records := []git.CommitRecord{
		{CommitID: "shared", Author: "Legacy", Email: "legacy@example.com", RepoID: "/mirror", RepoName: "mirror", Date: now.Add(-24 * time.Hour), LinesUnknown: true},
		{CommitID: "shared", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-24 * time.Hour), Added: 11},
		{CommitID: "alias", Author: "A. Contributor", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-48 * time.Hour), Added: 7, AIAssisted: true},
		{CommitID: "future", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(12 * 24 * time.Hour), Added: 23},
		{CommitID: "namesake", Author: "Ada", Email: "other@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-11 * 24 * time.Hour), Added: 1000},
		{CommitID: "excluded", Author: "Ada", Email: "ada@example.com", RepoID: "/excluded", RepoName: "excluded-repo", Date: now.Add(-10 * 24 * time.Hour), Added: 2000},
		{CommitID: "unselected", Author: "Ada", Email: "ada@example.com", RepoID: "/outside", RepoName: "outside-repo", Date: now.Add(-9 * 24 * time.Hour), Added: 3000},
		{CommitID: "inside-cutoff", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-14*24*time.Hour + time.Hour), Added: 2},
		{CommitID: "cutoff", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-14 * 24 * time.Hour), Added: 4000},
		{CommitID: "old", Author: "Ada", Email: "ada@example.com", RepoID: "/keep", RepoName: "keep", Date: now.Add(-15 * 24 * time.Hour), Added: 5000},
	}
	m := rangeChartDetailModel(now, records)
	m.width = 40
	m.sharedRepos = filterValues{"/keep", "/mirror", "/excluded"}
	m.excludedRepos = map[string]bool{"/excluded": true}
	m.authors = stats.Aggregate(m.filteredRecordsAt(now))
	content := ansi.Strip(m.operativeDetailContentAt(now))
	for _, want := range []string{"COMMITS 4", "ADDED   43", "AI CO-AUTHORED 25% (1)", "mirror", "LAST 2026-11-02"} {
		if !strings.Contains(content, want) {
			t.Errorf("detail missing %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"excluded-repo", "outside-repo", "Line totals are incomplete"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("detail leaked %q", unwanted)
		}
	}
	timeline := rangeChartTimeline(t, content)
	for _, day := range []string{"Oct 10 2026", "Oct 11 2026", "Oct 12 2026"} {
		requireRangeChartCount(t, timeline, day, 0)
	}
	for _, day := range []string{"Oct 07 2026", "Oct 19 2026", "Oct 20 2026", "Nov 02 2026"} {
		requireRangeChartCount(t, timeline, day, 1)
	}
	if strings.Contains(timeline, "Oct 06 2026") || strings.Contains(timeline, "?") || !strings.Contains(timeline, "◆1") {
		t.Fatalf("timeline lost scoped canonical evidence:\n%s", timeline)
	}
}

func rangeChartViewportModel(t *testing.T, now time.Time) Model {
	t.Helper()
	var records []git.CommitRecord
	for day := range 14 {
		records = append(records, git.CommitRecord{
			CommitID: fmt.Sprintf("day-%d", day), Author: "Ada", Email: "ada@example.com",
			RepoID: "/keep", RepoName: "keep", Date: now.Add(-time.Duration(day)*24*time.Hour - time.Hour), Added: day + 1,
		})
	}
	m := rangeChartDetailModel(now, records)
	m.viewMode = ViewAggregate
	m.sharedRepos, m.hideBots, m.filterQuery = filterValues{"/keep"}, true, "Ada"
	m.recomputeAuthors()
	return keyboardContractPress(t, m, "enter")
}

func TestRangeAwareContributorDetailPagesEveryDailyRowAfterResize(t *testing.T) {
	now := time.Now()
	for _, width := range []int{40, 80, 120} {
		for _, height := range []int{16, 28} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := rangeChartViewportModel(t, now)
				next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
				m = next.(Model)
				var seen strings.Builder
				for page := 0; ; page++ {
					checkStatisticsViewportBounds(t, m)
					seen.WriteString(ansi.Strip(m.View()))
					seen.WriteByte('\n')
					before := m.statDetailOffset
					m = keyboardContractPress(t, m, "pgdown")
					if m.statDetailOffset == before {
						break
					}
					if page > 100 {
						t.Fatal("PageDown never reached the bottom")
					}
				}
				for day := range 15 {
					date := now.Add(-time.Duration(day) * 24 * time.Hour).Format("Jan 02 2006")
					if !strings.Contains(seen.String(), date) {
						t.Errorf("daily row %q was unreachable", date)
					}
				}
				for _, section := range []string{"CONTRIBUTOR: ADA", "ACTIVITY TIMELINE", "ACTIVITY MATRIX", "less", "back"} {
					if !strings.Contains(seen.String(), section) {
						t.Errorf("section %q was unreachable", section)
					}
				}
				bottom := m.statDetailOffset
				m = keyboardContractPress(t, m, "pgup")
				if m.statDetailOffset >= bottom {
					t.Fatal("PageUp did not move away from the bottom")
				}
				m = keyboardContractPress(t, m, "home")
				if m.statDetailOffset != 0 || !strings.Contains(ansi.Strip(m.View()), "CONTRIBUTOR: ADA") {
					t.Fatal("Home did not return to the contributor summary")
				}
				m = keyboardContractPress(t, m, "end")
				next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 100})
				m = next.(Model)
				checkStatisticsViewportBounds(t, m)
				if m.statDetailOffset != 0 || !strings.Contains(ansi.Strip(m.View()), "ACTIVITY MATRIX") {
					t.Fatal("resize did not clamp detail into the expanded viewport")
				}
				if m.activeAuthorID != "email:ada@example.com" || m.filterQuery != "Ada" || !m.hideBots || !reflect.DeepEqual(m.sharedRepos, filterValues{"/keep"}) {
					t.Fatal("paging or resizing changed the selected contributor or filters")
				}
			})
		}
	}
}

func TestRangeAwareContributorDetailRangeChangesClampAndPreserveSelection(t *testing.T) {
	m := rangeChartViewportModel(t, time.Now())
	m = keyboardContractPress(t, m, "t", "home", "down", "down", "down", " ", "enter", "end")
	for _, tc := range []struct {
		name, interval string
		keys           []string
	}{
		{name: "14 days", interval: "daily", keys: []string{"t", "home", "down", "down", " ", "enter"}},
		{name: "31 days", interval: "weekly", keys: []string{"t", "end", " ", "ctrl+u", "31", "enter", "enter"}},
		{name: "181 days", interval: "monthly", keys: []string{"t", "end", " ", "ctrl+u", "181", "enter", "enter"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := m.statDetailOffset
			m = keyboardContractPress(t, m, tc.keys...)
			if m.viewMode != ViewOperative || m.activeAuthorID != "email:ada@example.com" || m.filterQuery != "Ada" || !m.hideBots || !reflect.DeepEqual(m.sharedRepos, filterValues{"/keep"}) {
				t.Fatal("range change lost contributor identity or existing filters")
			}
			if m.rangeLabel() != tc.name || !strings.Contains(rangeChartTimeline(t, m.operativeDetailContent()), "· "+tc.interval) {
				t.Fatal("range selection did not update contributor charts")
			}
			if m.statDetailOffset >= before || !strings.Contains(ansi.Strip(m.View()), "less") {
				t.Fatal("shorter chart content did not clamp to a visible final page")
			}
			after := m.statDetailOffset
			m = keyboardContractPress(t, m, "end")
			if m.statDetailOffset != after {
				t.Fatal("range change left scroll beyond the new final page")
			}
			checkStatisticsViewportBounds(t, m)
		})
	}
}
