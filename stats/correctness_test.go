package stats_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestSharedCommitsCountOnceWithOverlappingRepositories(t *testing.T) {
	now := time.Now()
	a := git.CommitRecord{CommitID: "shared", Author: "Ada", Email: "ada@test", Date: now, Added: 3, Removed: 1, RepoID: "a", RepoName: "a", AIAssisted: true}
	b := a
	b.RepoID, b.RepoName = "b", "b"
	c := a
	c.CommitID = "distinct"
	c.Added = 2
	records := []git.CommitRecord{a, b, c, a}
	result := stats.Aggregate(records)
	if len(result) != 1 {
		t.Fatalf("authors: %+v", result)
	}
	s := result[0]
	if s.Commits != 2 || s.Added != 5 || s.Removed != 2 || s.AICommits != 2 || s.ActiveDays != 1 {
		t.Fatalf("duplicated totals: %+v", s)
	}
	if s.PerRepo["a"].Commits != 2 || s.PerRepo["b"].Commits != 1 {
		t.Fatalf("lost associations: %+v", s.PerRepo)
	}
	filtered := stats.Aggregate(stats.FilterByRepo(records, map[string]bool{"a": true}))
	if len(filtered) != 1 || filtered[0].Commits != 1 || filtered[0].Added != 3 || len(filtered[0].PerRepo) != 1 {
		t.Fatalf("wrong filtered scope: %+v", filtered)
	}
	if len(stats.UniqueRecords(records)) != 2 {
		t.Fatal("detail charts would double-count")
	}
}

func TestFullCopySuppliesMissingShallowCounts(t *testing.T) {
	boundary := git.CommitRecord{CommitID: "one", Author: "Ada", Email: "ada@test", RepoID: "a", RepoName: "shallow", LinesUnknown: true, Added: 999}
	full := boundary
	full.RepoID, full.RepoName = "b", "full"
	full.LinesUnknown = false
	full.Added = 4
	partial := stats.Aggregate([]git.CommitRecord{boundary})[0]
	if partial.Commits != 1 || partial.Added != 0 || partial.UnknownLineCommits != 1 || partial.PerRepo["shallow"].UnknownLineCommits != 1 {
		t.Fatalf("unknown count fabricated: %+v", partial)
	}
	result := stats.Aggregate([]git.CommitRecord{boundary, full})[0]
	if result.Commits != 1 || result.Added != 4 || result.UnknownLineCommits != 0 || result.PerRepo["shallow"].Added != 4 {
		t.Fatalf("full copy not used: %+v", result)
	}
}

func TestConflictingCloneMailmapsAreDeterministic(t *testing.T) {
	a := git.CommitRecord{CommitID: "same", Author: "Alice", Email: "a@test", RepoID: "a", RepoName: "a", Added: 3}
	b := a
	b.Author, b.Email, b.RepoID, b.RepoName = "Bob", "b@test", "b", "b"
	first := stats.Aggregate([]git.CommitRecord{a, b})
	second := stats.Aggregate([]git.CommitRecord{b, a})
	if !reflect.DeepEqual(first, second) || len(first) != 1 || first[0].ID != "email:a@test" || first[0].Commits != 1 {
		t.Fatalf("arrival-order attribution: %+v %+v", first, second)
	}
}

func TestMissingEmailIdentitiesAreRepositoryScoped(t *testing.T) {
	records := []git.CommitRecord{{Author: "Alex", RepoID: "a"}, {Author: "Alex", RepoID: "b"}, {Author: "Alex", RepoID: "a"}}
	if got := stats.Aggregate(records); len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("missing emails joined repositories: %+v", got)
	}
}

func TestCalendarUsesLocalTimezoneIncludingDST(t *testing.T) {
	zone, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	original := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = original })
	parse := func(s string) time.Time {
		d, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, dates := range [][2]string{
		{"2026-02-01T00:30:00+01:00", "2026-02-01T00:30:00Z"},
		{"2026-11-01T07:30:00Z", "2026-11-01T08:30:00Z"},
	} {
		r := []git.CommitRecord{{Author: "A", Email: "a@test", Date: parse(dates[0])}, {Author: "Alias", Email: "a@test", Date: parse(dates[1])}}
		got := stats.Aggregate(r)[0]
		if got.ActiveDays != 1 || got.FirstCommit.Location() != zone || got.LastCommit.Location() != zone {
			t.Fatalf("inconsistent local dates: %+v", got)
		}
	}
}

func TestAIRankingUsesExactRatiosWithoutOverflow(t *testing.T) {
	for _, pair := range [][2]stats.AuthorStats{
		{{Name: "higher", Commits: 200, AICommits: 101, TotalChange: 1}, {Name: "lower", Commits: 100, AICommits: 50, TotalChange: 100}},
		{{Name: "higher", Commits: math.MaxInt, AICommits: math.MaxInt - 1}, {Name: "lower", Commits: math.MaxInt, AICommits: math.MaxInt - 2, TotalChange: 100}},
	} {
		rows := []stats.AuthorStats{pair[1], pair[0]}
		stats.Sort(rows, stats.SortByAI)
		if rows[0].Name != "higher" {
			t.Fatalf("rounded/overflowed ranking: %+v", rows)
		}
	}
}

func TestFutureCommitsAndChurnConventionsStayUnchanged(t *testing.T) {
	r := []git.CommitRecord{{Author: "A", Email: "a@test", Date: time.Now().Add(24 * time.Hour), Added: 3}}
	for _, window := range []time.Duration{0, 7 * 24 * time.Hour} {
		got := stats.Aggregate(stats.FilterByTime(r, window))
		if len(got) != 1 || got[0].Commits != 1 || got[0].Added != 3 {
			t.Fatal("future-dated commit was dropped")
		}
	}
	for _, value := range []stats.AuthorStats{{Removed: 100}, {Added: 100}, {}} {
		if value.ChurnRatio() != 0 {
			t.Fatalf("churn convention changed: %+v", value)
		}
	}
	if got := (stats.AuthorStats{Added: 100, Removed: 50}).ChurnRatio(); got != 0.5 {
		t.Fatalf("churn formula changed: %v", got)
	}
}
