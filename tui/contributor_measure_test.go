package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/stats"
)

func TestContributorBarsKeepLinesWhenSortedByCommits(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "Large changes", Commits: 1, Added: 800, Removed: 200, Net: 600, TotalChange: 1000},
		{Name: "Frequent commits", Commits: 10, Added: 8, Removed: 2, Net: 6, TotalChange: 10},
	}
	for _, width := range []int{80, 120, 220} {
		for _, field := range []stats.SortField{stats.SortByTotal, stats.SortByCommits} {
			out := AggregateView{}.RenderTable(authors, TableState{VisibleRows: 2, Width: width, SortField: field})
			bars := map[string]int{}
			for _, line := range strings.Split(ansi.Strip(out), "\n") {
				if ansi.StringWidth(line) > width-2 {
					t.Fatalf("width %d sort %d: row exceeds terminal: %q", width, field, line)
				}
				for _, author := range authors {
					if strings.Contains(line, author.Name) {
						for _, cell := range line {
							if strings.ContainsRune("█▓▒░", cell) {
								bars[author.Name]++
							}
						}
					}
				}
			}
			large, frequent := bars["Large changes"], bars["Frequent commits"]
			if large == 0 || frequent == 0 {
				t.Fatalf("width %d sort %d: missing bars: %v", width, field, bars)
			}
			if large <= frequent {
				t.Fatalf("bars should always show larger line changes: %v", bars)
			}
		}
	}
}

func TestContributorMeasureRetainsUnknownLineEvidence(t *testing.T) {
	author := stats.AuthorStats{Name: "Boundary history", Commits: 5, Added: 80, Removed: 20, Net: 60, TotalChange: 100, UnknownLineCommits: 1}
	for _, field := range []stats.SortField{stats.SortByTotal, stats.SortByCommits} {
		out := ansi.Strip(AggregateView{}.RenderTable([]stats.AuthorStats{author}, TableState{VisibleRows: 1, Width: 120, SortField: field}))
		for _, value := range []int{author.Added, author.Removed, author.Net} {
			if !strings.Contains(out, formatLineCount(value, author.UnknownLineCommits)) {
				t.Fatalf("sort %d lost known-count qualifier:\n%s", field, out)
			}
		}
	}
}

func TestNarrowContributorsHeadingShowsSort(t *testing.T) {
	for _, width := range []int{40, 60, 80} {
		m := boardLayoutFixture()
		m.width, m.height = width, 24
		m = keyboardContractPress(t, m, "c")
		for _, label := range []string{"Lines +/-", "Commits", "Added"} {
			view := ansi.Strip(m.View())
			lines := strings.Split(view, "\n")
			if len(lines) != m.height || !strings.Contains(lines[screenHeaderRows], label) {
				t.Fatalf("width %d should show %q in fixed heading:\n%s", width, label, view)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width %d overflows: %q", width, line)
				}
			}
			m = keyboardContractPress(t, m, "s")
		}
	}
}
