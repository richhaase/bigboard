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
	m.viewMode = ViewAggregate
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
	m = send(m, "t", "home", "down", "enter")
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

func TestMonthTimelineAcrossMidnightDSTTransition(t *testing.T) {
	zone, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		t.Fatal(err)
	}
	original := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = original })

	// October 1, 2023 starts with a skipped midnight in this timezone.
	for _, tt := range []struct {
		name    string
		months  []time.Month
		labels  []string
		commits []int
	}{
		{"single month", []time.Month{time.October}, []string{"Oct 2023"}, []int{1}},
		{"gap across transition", []time.Month{time.September, time.December}, []string{"Sep 2023", "Oct 2023", "Nov 2023", "Dec 2023"}, []int{1, 0, 0, 1}},
		{"activity across transition", []time.Month{time.September, time.October, time.December}, []string{"Sep 2023", "Oct 2023", "Nov 2023", "Dec 2023"}, []int{1, 1, 0, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var records []git.CommitRecord
			for _, month := range tt.months {
				records = append(records, git.CommitRecord{
					Author: "A", Email: "a@test", RepoName: "r", Added: 3,
					Date: time.Date(2023, month, 15, 12, 0, 0, 0, time.UTC),
				})
			}
			months := aggregateByMonth(records)
			if len(months) != len(tt.labels) {
				t.Fatalf("expected %d months, got %+v", len(tt.labels), months)
			}
			for i, month := range months {
				if label := month.Month.Format("Jan 2006"); label != tt.labels[i] {
					t.Errorf("month %d: expected %s, got %s", i, tt.labels[i], label)
				}
				if month.Commits != tt.commits[i] || month.Added != 3*tt.commits[i] {
					t.Errorf("month %d: expected %d commits and %d additions, got %+v", i, tt.commits[i], 3*tt.commits[i], month)
				}
			}
			author := stats.Aggregate(records)[0]
			out := OperativeView{}.RenderOperativeDetail(author.Name, &author, records, 120, DefaultTimeIndex, 1, 0)
			for _, label := range tt.labels {
				if strings.Count(out, label) != 1 {
					t.Errorf("expected exactly one %s label in detail:\n%s", label, out)
				}
			}
		})
	}
}

func TestUnknownCountsAreQualifiedAtWideAndNarrowWidths(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "test", 6, Options{})
	m.viewMode = ViewAggregate
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
