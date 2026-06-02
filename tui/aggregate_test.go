package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/stats"
)

func sampleAuthors() []stats.AuthorStats {
	return []stats.AuthorStats{
		{Name: "Ada Lovelace", Commits: 120, Added: 4000, Removed: 1200, Net: 2800, TotalChange: 5200, AICommits: 30},
		{Name: "Grace Hopper", Commits: 80, Added: 2000, Removed: 3000, Net: -1000, TotalChange: 5000},
		{Name: "Alan Turing", Commits: 40, Added: 900, Removed: 100, Net: 800, TotalChange: 1000, AICommits: 1},
	}
}

func TestRenderTableAdapts(t *testing.T) {
	authors := sampleAuthors()
	for _, width := range []int{60, 80, 120} {
		ts := TableState{VisibleRows: len(authors), Width: width, SortField: stats.SortByTotal}
		out := AggregateView{}.RenderTable(authors, ts)
		limit := width - chromeInset + 2
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > limit {
				t.Errorf("width %d: line %q display width %d exceeds %d", width, line, w, limit)
			}
		}
	}
}

func TestRenderTableColumnTiers(t *testing.T) {
	authors := sampleAuthors()

	wide := AggregateView{}.RenderTable(authors, TableState{VisibleRows: len(authors), Width: 120, SortField: stats.SortByTotal})
	if !strings.Contains(wide, "ADDED") || !strings.Contains(wide, "REMOVED") {
		t.Errorf("wide tier should keep ADDED/REMOVED:\n%s", wide)
	}
	if !strings.Contains(wide, "IMPACT") {
		t.Errorf("wide tier should keep the impact bar header:\n%s", wide)
	}

	mid := AggregateView{}.RenderTable(authors, TableState{VisibleRows: len(authors), Width: 80, SortField: stats.SortByTotal})
	if strings.Contains(mid, "ADDED") || strings.Contains(mid, "REMOVED") {
		t.Errorf("mid tier should drop ADDED/REMOVED:\n%s", mid)
	}

	narrow := AggregateView{}.RenderTable(authors, TableState{VisibleRows: len(authors), Width: 60, SortField: stats.SortByTotal})
	if strings.Contains(narrow, "IMPACT") {
		t.Errorf("narrow tier should drop the impact bar header:\n%s", narrow)
	}
	if !strings.Contains(narrow, "CONTRIBUTOR") || !strings.Contains(narrow, "NET") || !strings.Contains(narrow, "AI%") {
		t.Errorf("narrow tier should keep CONTRIBUTOR/NET/AI%%:\n%s", narrow)
	}
}
