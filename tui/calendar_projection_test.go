package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/richhaase/bigboard/git"
)

func TestCalendarRenderersKeepPresentationWindowAndFullInputHeatScale(t *testing.T) {
	originalZone, originalProfile := time.Local, lipgloss.ColorProfile()
	time.Local = time.UTC
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() {
		time.Local = originalZone
		lipgloss.SetColorProfile(originalProfile)
	})
	now := time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)
	records := []git.CommitRecord{
		{Date: now.AddDate(0, -16, 0), Added: 500},
		{Date: now.AddDate(0, 0, -1), Added: 10, AIAssisted: true},
		{Date: now.AddDate(0, 0, -8), Added: 99999, LinesUnknown: true},
		{Date: now.AddDate(0, 0, 1), Added: 1000},
	}
	view := OperativeView{}
	timeline := view.renderTimeline(records, 80)
	if strings.Count(timeline, "\n") != 11 || strings.Contains(timeline, "Feb 2025") || !strings.Contains(timeline, "Jul 2025") || !strings.Contains(timeline, "Jun 2026") {
		t.Fatalf("timeline did not keep its last-12-month presentation window:\n%s", timeline)
	}
	if !strings.Contains(timeline, " ?") || !strings.Contains(timeline, "◆1") {
		t.Fatalf("timeline lost unknown or AI evidence:\n%s", timeline)
	}
	for _, width := range []int{20, 40, 120} {
		grid, _, _ := strings.Cut(view.renderHeatmap(records, width, now), "\n\n")
		if strings.Count(grid, "?") != 1 || strings.Count(grid, "░") != 1 || strings.ContainsAny(grid, "▒▓█") {
			t.Fatalf("width %d changed full-input scale, unknown evidence or future masking:\n%s", width, grid)
		}
	}
}
