package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func TestActivityTimelineRangeThresholds(t *testing.T) {
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		days  int
		label string
	}{
		{1, "daily"}, {14, "daily"}, {30, "daily"},
		{31, "weekly"}, {90, "weekly"}, {180, "weekly"},
		{181, "monthly"}, {365, "monthly"}, {0, "monthly"},
	} {
		t.Run(fmt.Sprint(tc.days), func(t *testing.T) {
			window := activityChartWindow{now: now, duration: time.Duration(tc.days) * 24 * time.Hour}
			if got := window.timelineLabel(); got != tc.label {
				t.Fatalf("%d days: got %s, want %s", tc.days, got, tc.label)
			}
			records := []git.CommitRecord{{Author: "Example", Date: now, Added: 12}}
			out := OperativeView{}.renderOperativeDetail("Example", nil, records, 100, window)
			if !strings.Contains(ansi.Strip(out), "ACTIVITY TIMELINE · "+tc.label) {
				t.Fatalf("interval missing from detail:\n%s", out)
			}
			if tc.label == "weekly" && !strings.Contains(out, "Week of ") {
				t.Fatalf("weekly rows lack an explicit week label:\n%s", out)
			}
		})
	}
}

func TestShortHeatmapIncludesOnlySelectedCalendarDays(t *testing.T) {
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	window := activityChartWindow{now: now, duration: 14 * 24 * time.Hour}
	records := []git.CommitRecord{
		{Date: now.Add(-window.duration).Add(time.Minute), Added: 10},
		{Date: now, LinesUnknown: true},
		{Date: now.AddDate(0, 0, 1), Added: 1000},
	}
	for _, width := range []int{40, 80, 160} {
		out := ansi.Strip(OperativeView{}.renderHeatmapWindow(records, width, window))
		grid, _, _ := strings.Cut(out, "\n\n")
		if !strings.Contains(grid, "Oct 12, 2026 – Oct 26, 2026") {
			t.Fatalf("width %d lost the covered dates:\n%s", width, grid)
		}
		cells := 0
		for _, symbol := range []string{"·", "░", "▒", "▓", "█", "?"} {
			cells += strings.Count(grid, symbol)
		}
		if cells != 15 || strings.Count(grid, "?") != 1 || strings.Count(grid, "░") != 1 {
			t.Fatalf("width %d changed partial days, unknown counts or future scale: %d cells\n%s", width, cells, grid)
		}
		if strings.ContainsAny(grid, "▒▓█") {
			t.Fatalf("width %d included a future cell or changed normalization:\n%s", width, grid)
		}
	}
}

func TestShortHeatmapNarrowWindowLabelsVisibleCoverage(t *testing.T) {
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	window := activityChartWindow{now: now, duration: 180 * 24 * time.Hour}
	_, first, last, weeks := window.heatmapDates(40)
	if first.Format("2006-01-02") != "2026-04-29" || last.Format("2006-01-02") != "2026-10-26" || weeks != 27 {
		t.Fatalf("40-column window omitted in-range dates: %s–%s, %d weeks", first, last, weeks)
	}
	_, first, _, weeks = window.heatmapDates(20)
	if weeks != 10 || first.Format("2006-01-02") != "2026-08-23" {
		t.Fatalf("narrow view should show its latest 10 weeks: %s, %d", first, weeks)
	}
	out := ansi.Strip(OperativeView{}.renderHeatmapWindow(nil, 20, window))
	if !strings.Contains(out, "Aug 23, 2026 – Oct 26, 2026") {
		t.Fatalf("narrow matrix hid the actual covered interval:\n%s", out)
	}
}

func TestShortTimelinePreservesEvidenceAtNarrowWidths(t *testing.T) {
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	records := []git.CommitRecord{
		{Date: now.Add(-24 * time.Hour), Added: 20, Removed: 10, AIAssisted: true},
		{Date: now, LinesUnknown: true},
		{Date: now.AddDate(0, 0, 1), Added: 30},
	}
	for _, days := range []int{14, 90} {
		for _, width := range []int{40, 80, 120} {
			window := activityChartWindow{now: now, duration: time.Duration(days) * 24 * time.Hour}
			out := OperativeView{}.renderTimelineWindow(records, width, window)
			for _, want := range []string{" ?", "◆1", "2026"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%d-day timeline lost %q:\n%s", days, want, out)
				}
			}
			if days == 14 && !strings.Contains(out, "Oct 27 2026") {
				t.Fatalf("daily timeline lost future activity:\n%s", out)
			}
			for _, line := range strings.Split(out, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%d-day timeline exceeds %d columns: %q", days, width, line)
				}
			}
		}
	}
}
