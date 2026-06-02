package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestRenderOperativeDetailEmptyState(t *testing.T) {
	out := OperativeView{}.RenderOperativeDetail("Ada Lovelace", nil, nil, 100, DefaultTimeIndex, 3, 0)
	if !strings.Contains(out, "NO SIGNAL") {
		t.Errorf("empty detail should show NO SIGNAL:\n%s", out)
	}
	if !strings.Contains(out, "ADA LOVELACE") {
		t.Errorf("empty detail should still name the contributor:\n%s", out)
	}
	if strings.Contains(out, "REPO CONTRIBUTIONS") || strings.Contains(out, "ACTIVITY TIMELINE") {
		t.Errorf("empty detail should not render a body:\n%s", out)
	}
}

func TestRenderOperativeDetailRendersBody(t *testing.T) {
	as := &stats.AuthorStats{
		Name:    "Ada Lovelace",
		Commits: 5,
		Added:   100,
		Removed: 10,
		Net:     90,
		PerRepo: map[string]*stats.RepoContribution{
			"engine": {Commits: 5, Added: 100, Removed: 10, Net: 90, TotalChange: 110},
		},
	}
	out := OperativeView{}.RenderOperativeDetail("Ada Lovelace", as, nil, 100, DefaultTimeIndex, 3, 0)
	if !strings.Contains(out, "REPO CONTRIBUTIONS") {
		t.Errorf("non-empty stats should render the repo breakdown:\n%s", out)
	}
	if strings.Contains(out, "NO SIGNAL") {
		t.Errorf("non-empty stats should not show NO SIGNAL:\n%s", out)
	}
}

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
