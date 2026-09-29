package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestContributorSelectionSurvivesNameChangesAndSameNamePeople(t *testing.T) {
	now := time.Now()
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "test", 6, Options{})
	m.width, m.height = 120, 60
	m.allRecords = []git.CommitRecord{
		{Author: "Alice Smith", Email: "alice@test", Date: now.Add(-60 * 24 * time.Hour), Added: 10, RepoName: "r"},
		{Author: "Alice Smith", Email: "alice@test", Date: now.Add(-30 * 24 * time.Hour), Added: 10, RepoName: "r"},
		{Author: "asmith", Email: "alice@test", Date: now.Add(-time.Hour), Added: 3, RepoName: "r"},
		{Author: "Alice Smith", Email: "other@test", Date: now.Add(-time.Hour), Added: 1, RepoName: "r"},
	}
	m.recomputeAuthors()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.activeAuthorID != "email:alice@test" {
		t.Fatalf("selected %q", m.activeAuthorID)
	}
	for i := 0; i < 5; i++ {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
		m = next.(Model)
	}
	out := m.View()
	if strings.Contains(out, "NO SIGNAL") || !strings.Contains(out, "CONTRIBUTOR: ASMITH") {
		t.Fatalf("lost identity after range change:\n%s", out)
	}
	for _, a := range m.authors {
		r := filterRecordsByAuthor(m.filteredRecords(), &a, a.Name, false)
		if len(r) != 1 || stats.IdentityID(r[0]) != a.ID {
			t.Fatalf("detail mixed identities: %+v", r)
		}
	}
	// With no activity for the selected identity, a same-name person must not
	// supply a misleading detail view through the legacy name fallback.
	m.activeAuthorID = "email:absent@test"
	m.activeOperative = "Alice Smith"
	if !strings.Contains(m.View(), "NO SIGNAL") {
		t.Fatal("detail fell back to another person's name")
	}
}

func TestCalendarChartsUseLocalDatesAndUniqueCommits(t *testing.T) {
	zone, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	original := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = original })
	now := time.Date(2026, 2, 1, 1, 0, 0, 0, time.UTC)
	date := now.Add(-time.Hour).In(time.FixedZone("author", 14*60*60))
	a := git.CommitRecord{CommitID: "same", Author: "A", Email: "a@test", Date: date, Added: 3, RepoID: "a", RepoName: "a"}
	b := a
	b.RepoID, b.RepoName = "b", "b"
	records := []git.CommitRecord{a, b}
	author := stats.Aggregate(records)[0]
	filtered := filterRecordsByAuthor(records, &author, author.Name, false)
	months := aggregateByMonth(filtered)
	if len(months) != 1 || months[0].Month.Month() != time.January || months[0].Commits != 1 || months[0].Added != 3 {
		t.Fatalf("incorrect local timeline: %+v", months)
	}
	heatmap := OperativeView{}.renderHeatmap(filtered, 120, now)
	// Count filled cells only, excluding the intensity legend.
	grid, _, _ := strings.Cut(heatmap, "less")
	if strings.Count(grid, "█") != 1 {
		t.Fatalf("past activity vanished from local calendar:\n%s", heatmap)
	}
	if author.FirstCommit.Format("2006-01-02") != "2026-01-31" {
		t.Fatalf("wrong visible date: %v", author.FirstCommit)
	}
}

func TestUnknownCountsAreQualifiedAtWideAndNarrowWidths(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "test", 6, Options{})
	m.height = 50
	m.allRecords = []git.CommitRecord{
		{CommitID: "boundary", Author: "A", Email: "a@test", Date: time.Now(), RepoID: "r", RepoName: "r", LinesUnknown: true},
		{CommitID: "known", Author: "A", Email: "a@test", Date: time.Now(), RepoID: "r", RepoName: "r", Added: 5},
	}
	m.recomputeAuthors()
	for _, width := range []int{60, 80, 120} {
		m.width = width
		out := m.View()
		if !strings.Contains(out, "Shallow history") || !strings.Contains(out, "5?") {
			t.Fatalf("unqualified totals at %d:\n%s", width, out)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if out := m.View(); !strings.Contains(out, "Line totals are incomplete") || !strings.Contains(out, "5?") {
		t.Fatalf("unqualified detail:\n%s", out)
	}
	if out := renderMetricsLine(&stats.AuthorStats{Removed: 100}); !strings.Contains(out, "CHURN 0.00") {
		t.Fatalf("churn display changed: %s", out)
	}
}
