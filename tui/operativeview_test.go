package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

func TestRenderHeatmap(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) // Tuesday
	recs := []git.CommitRecord{
		{Author: "A", Date: now.AddDate(0, 0, -1), Added: 100, Removed: 0}, // hottest
		{Author: "A", Date: now.AddDate(0, 0, -8), Added: 5, Removed: 0},
	}
	out := OperativeView{}.renderHeatmap(recs, 100, now)
	if !strings.Contains(out, "Sun") || !strings.Contains(out, "Sat") {
		t.Errorf("heatmap missing weekday labels:\n%s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("heatmap missing hottest cell:\n%s", out)
	}
	if !strings.Contains(out, "less") || !strings.Contains(out, "more") {
		t.Errorf("heatmap missing legend:\n%s", out)
	}
}

func TestRenderHeatmapEmpty(t *testing.T) {
	// No panic, renders a grid even with a single record.
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	out := OperativeView{}.renderHeatmap([]git.CommitRecord{{Date: now}}, 40, now)
	if out == "" {
		t.Error("expected a heatmap grid")
	}
}
