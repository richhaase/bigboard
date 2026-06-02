package stats_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestMergeAuthors(t *testing.T) {
	allNames := []string{"Alice Smith", "alice smith", "Alice S", "Bob Jones"}
	commitCounts := map[string]int{
		"Alice Smith": 10,
		"alice smith": 3,
		"Alice S":     1,
		"Bob Jones":   5,
	}

	// Exact normalized-name equality always merges, regardless of fuzzy setting.
	canonical := stats.MergeAuthorName("alice smith", allNames, commitCounts)
	if canonical != "Alice Smith" {
		t.Errorf("expected canonical name 'Alice Smith', got %q", canonical)
	}

	// "Bob Jones" should return itself
	canonical3 := stats.MergeAuthorName("Bob Jones", allNames, commitCounts)
	if canonical3 != "Bob Jones" {
		t.Errorf("expected 'Bob Jones', got %q", canonical3)
	}

	// Substring merging ("Alice S" → "Alice Smith") is gated behind FuzzyMatching.
	if got := stats.MergeAuthorName("Alice S", allNames, commitCounts); got != "Alice S" {
		t.Errorf("with fuzzy off, 'Alice S' should stay separate, got %q", got)
	}
	stats.FuzzyMatching = true
	t.Cleanup(func() { stats.FuzzyMatching = false })
	if got := stats.MergeAuthorName("Alice S", allNames, commitCounts); got != "Alice Smith" {
		t.Errorf("with fuzzy on, 'Alice S' should merge to 'Alice Smith', got %q", got)
	}
}

// TestAggregateNoOverMerge guards the headline accuracy fix: distinct people
// with distinct emails and merely similar names must NOT collapse into one row
// under the default (fuzzy-off) policy.
func TestAggregateNoOverMerge(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Daniel", Email: "daniel@x.com", Date: now, Added: 10, RepoName: "r"},
		{Author: "Daniela", Email: "daniela@y.com", Date: now, Added: 20, RepoName: "r"},
		{Author: "Martin", Email: "martin@x.com", Date: now, Added: 5, RepoName: "r"},
		{Author: "Martinez", Email: "martinez@y.com", Date: now, Added: 7, RepoName: "r"},
	}
	result := stats.Aggregate(records)
	if len(result) != 4 {
		names := make([]string, len(result))
		for i, a := range result {
			names[i] = a.Name
		}
		t.Fatalf("expected 4 distinct authors with fuzzy off, got %d: %v", len(result), names)
	}
}

func TestAreSimilarNames(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		// Case-insensitive exact match
		{"Alice Smith", "alice smith", true},
		// Whitespace normalization
		{"Alice  Smith", "Alice Smith", true},
		// Substring: "alice s" is in "alice smith", both >5 chars
		{"Alice S", "Alice Smith", true},
		// Completely different names
		{"Bob Jones", "Alice Smith", false},
		// Short name (<= 5 chars), no substring matching
		{"Al", "Alice Smith", false},
		// Both long, no substring relation
		{"Charlie Brown", "Alice Smith", false},
		// One is substring of other, both >5 chars
		{"alice smith", "alice smithson", true},
	}

	for _, tc := range cases {
		got := stats.AreSimilarNames(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("AreSimilarNames(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAggregate(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice Smith", Date: now, Added: 100, Removed: 20, RepoName: "repo-a"},
		{Author: "Alice Smith", Date: now, Added: 50, Removed: 10, RepoName: "repo-a"},
		{Author: "Alice Smith", Date: now, Added: 30, Removed: 5, RepoName: "repo-b"},
		{Author: "Bob Jones", Date: now, Added: 200, Removed: 80, RepoName: "repo-a"},
	}

	result := stats.Aggregate(records)

	if len(result) != 2 {
		t.Fatalf("expected 2 authors, got %d", len(result))
	}

	// Find Alice and Bob
	var alice, bob *stats.AuthorStats
	for i := range result {
		switch result[i].Name {
		case "Alice Smith":
			alice = &result[i]
		case "Bob Jones":
			bob = &result[i]
		}
	}

	if alice == nil {
		t.Fatal("Alice Smith not found in results")
	}
	if bob == nil {
		t.Fatal("Bob Jones not found in results")
	}

	// Alice: 3 commits, 180 added, 35 removed
	if alice.Commits != 3 {
		t.Errorf("Alice commits: expected 3, got %d", alice.Commits)
	}
	if alice.Added != 180 {
		t.Errorf("Alice added: expected 180, got %d", alice.Added)
	}
	if alice.Removed != 35 {
		t.Errorf("Alice removed: expected 35, got %d", alice.Removed)
	}
	if alice.Net != 145 {
		t.Errorf("Alice net: expected 145, got %d", alice.Net)
	}
	if alice.TotalChange != 215 {
		t.Errorf("Alice total: expected 215, got %d", alice.TotalChange)
	}

	// Alice per-repo
	if len(alice.PerRepo) != 2 {
		t.Errorf("Alice per-repo: expected 2 repos, got %d", len(alice.PerRepo))
	}
	repoA, ok := alice.PerRepo["repo-a"]
	if !ok {
		t.Fatal("Alice missing repo-a contribution")
	}
	if repoA.Commits != 2 {
		t.Errorf("Alice repo-a commits: expected 2, got %d", repoA.Commits)
	}
	if repoA.Added != 150 {
		t.Errorf("Alice repo-a added: expected 150, got %d", repoA.Added)
	}

	repoB, ok := alice.PerRepo["repo-b"]
	if !ok {
		t.Fatal("Alice missing repo-b contribution")
	}
	if repoB.Commits != 1 {
		t.Errorf("Alice repo-b commits: expected 1, got %d", repoB.Commits)
	}

	// Bob: 1 commit, 200 added, 80 removed
	if bob.Commits != 1 {
		t.Errorf("Bob commits: expected 1, got %d", bob.Commits)
	}
	if bob.Added != 200 {
		t.Errorf("Bob added: expected 200, got %d", bob.Added)
	}
	if bob.Net != 120 {
		t.Errorf("Bob net: expected 120, got %d", bob.Net)
	}
}

func TestAggregateAICommits(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Email: "alice@example.com", Date: now, Added: 10, RepoName: "repo-a", AIAssisted: true},
		{Author: "Alice", Email: "alice@example.com", Date: now, Added: 20, RepoName: "repo-a", AIAssisted: false},
		{Author: "Alice", Email: "alice@example.com", Date: now, Added: 30, RepoName: "repo-b", AIAssisted: true},
		{Author: "Bob", Email: "bob@example.com", Date: now, Added: 40, RepoName: "repo-a", AIAssisted: false},
		{Author: "Bob", Email: "bob@example.com", Date: now, Added: 50, RepoName: "repo-a", AIAssisted: true},
	}

	result := stats.Aggregate(records)

	var alice, bob *stats.AuthorStats
	for i := range result {
		switch result[i].Name {
		case "Alice":
			alice = &result[i]
		case "Bob":
			bob = &result[i]
		}
	}

	if alice == nil || bob == nil {
		t.Fatal("expected both Alice and Bob in results")
	}

	if alice.AICommits != 2 {
		t.Errorf("Alice AICommits: expected 2, got %d", alice.AICommits)
	}
	if bob.AICommits != 1 {
		t.Errorf("Bob AICommits: expected 1, got %d", bob.AICommits)
	}

	if alice.PerRepo["repo-a"].AICommits != 1 {
		t.Errorf("Alice repo-a AICommits: expected 1, got %d", alice.PerRepo["repo-a"].AICommits)
	}
	if alice.PerRepo["repo-b"].AICommits != 1 {
		t.Errorf("Alice repo-b AICommits: expected 1, got %d", alice.PerRepo["repo-b"].AICommits)
	}
}

func TestFilterByTime(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Date: now, Added: 1, RepoName: "repo"},
		{Author: "Alice", Date: now.Add(-60 * 24 * time.Hour), Added: 2, RepoName: "repo"},
		{Author: "Alice", Date: now.Add(-400 * 24 * time.Hour), Added: 3, RepoName: "repo"},
	}

	// 30 days: only the first record
	filtered30 := stats.FilterByTime(records, 30*24*time.Hour)
	if len(filtered30) != 1 {
		t.Errorf("30d filter: expected 1 record, got %d", len(filtered30))
	}

	// 90 days: first two records
	filtered90 := stats.FilterByTime(records, 90*24*time.Hour)
	if len(filtered90) != 2 {
		t.Errorf("90d filter: expected 2 records, got %d", len(filtered90))
	}

	// Duration 0: all records
	filteredAll := stats.FilterByTime(records, 0)
	if len(filteredAll) != 3 {
		t.Errorf("0 (all) filter: expected 3 records, got %d", len(filteredAll))
	}
}

func TestSort(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "Alice", Commits: 5, Added: 100, Removed: 20, Net: 80, TotalChange: 120},
		{Name: "Bob", Commits: 10, Added: 50, Removed: 10, Net: 40, TotalChange: 60},
		{Name: "Charlie", Commits: 1, Added: 200, Removed: 50, Net: 150, TotalChange: 250},
	}

	// Sort by commits descending: Bob(10), Alice(5), Charlie(1)
	a := make([]stats.AuthorStats, len(authors))
	copy(a, authors)
	stats.Sort(a, stats.SortByCommits)
	if a[0].Name != "Bob" || a[1].Name != "Alice" || a[2].Name != "Charlie" {
		t.Errorf("SortByCommits: got %v, %v, %v", a[0].Name, a[1].Name, a[2].Name)
	}

	// Sort by total descending: Charlie(250), Alice(120), Bob(60)
	b := make([]stats.AuthorStats, len(authors))
	copy(b, authors)
	stats.Sort(b, stats.SortByTotal)
	if b[0].Name != "Charlie" || b[1].Name != "Alice" || b[2].Name != "Bob" {
		t.Errorf("SortByTotal: got %v, %v, %v", b[0].Name, b[1].Name, b[2].Name)
	}

	// Sort by added descending: Charlie(200), Alice(100), Bob(50)
	c := make([]stats.AuthorStats, len(authors))
	copy(c, authors)
	stats.Sort(c, stats.SortByAdded)
	if c[0].Name != "Charlie" || c[1].Name != "Alice" || c[2].Name != "Bob" {
		t.Errorf("SortByAdded: got %v, %v, %v", c[0].Name, c[1].Name, c[2].Name)
	}
}

func TestSortFieldFromString(t *testing.T) {
	cases := []struct {
		s    string
		want stats.SortField
	}{
		{"commits", stats.SortByCommits},
		{"added", stats.SortByAdded},
		{"removed", stats.SortByRemoved},
		{"net", stats.SortByNet},
		{"total", stats.SortByTotal},
		{"unknown", stats.SortByTotal}, // default
	}
	for _, tc := range cases {
		got := stats.SortFieldFromString(tc.s)
		if got != tc.want {
			t.Errorf("SortFieldFromString(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestFilterByRepo(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Date: now, Added: 10, RepoName: "repo-a"},
		{Author: "Alice", Date: now, Added: 20, RepoName: "repo-b"},
		{Author: "Bob", Date: now, Added: 30, RepoName: "repo-c"},
		{Author: "Bob", Date: now, Added: 40, RepoName: "repo-a"},
	}

	excluded := map[string]bool{"repo-b": true}
	filtered := stats.FilterByRepo(records, excluded)

	if len(filtered) != 3 {
		t.Errorf("expected 3 records, got %d", len(filtered))
	}
	for _, r := range filtered {
		if r.RepoName == "repo-b" {
			t.Error("repo-b should be excluded")
		}
	}

	// Empty exclusion set returns all records
	all := stats.FilterByRepo(records, nil)
	if len(all) != 4 {
		t.Errorf("expected 4 records with nil exclusions, got %d", len(all))
	}

	// Exclude all repos
	allExcluded := map[string]bool{"repo-a": true, "repo-b": true, "repo-c": true}
	none := stats.FilterByRepo(records, allExcluded)
	if len(none) != 0 {
		t.Errorf("expected 0 records, got %d", len(none))
	}
}

func TestSortTiebreakerDeterministic(t *testing.T) {
	// Three authors tied on Commits; ties must break by TotalChange then Name.
	authors := []stats.AuthorStats{
		{Name: "Charlie", Commits: 5, TotalChange: 10},
		{Name: "Alice", Commits: 5, TotalChange: 30},
		{Name: "Bob", Commits: 5, TotalChange: 30},
	}
	stats.Sort(authors, stats.SortByCommits)
	// TotalChange 30 (Alice, Bob) before 10 (Charlie); Alice before Bob by name.
	got := []string{authors[0].Name, authors[1].Name, authors[2].Name}
	want := []string{"Alice", "Bob", "Charlie"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tiebreak order = %v, want %v", got, want)
		}
	}
}

func TestAggregateDeterministicOrder(t *testing.T) {
	now := time.Now()
	mk := func() []git.CommitRecord {
		return []git.CommitRecord{
			{Author: "Zoe", Email: "zoe@x.com", Date: now, Added: 1, RepoName: "r"},
			{Author: "Amy", Email: "amy@x.com", Date: now, Added: 1, RepoName: "r"},
			{Author: "Max", Email: "max@x.com", Date: now, Added: 1, RepoName: "r"},
		}
	}
	first := stats.Aggregate(mk())
	// Aggregate output is name-sorted regardless of input/map order.
	for i := 0; i < 20; i++ {
		again := stats.Aggregate(mk())
		if len(again) != len(first) {
			t.Fatalf("length varied: %d vs %d", len(again), len(first))
		}
		for j := range first {
			if again[j].Name != first[j].Name {
				t.Fatalf("Aggregate order non-deterministic at %d: %q vs %q", j, again[j].Name, first[j].Name)
			}
		}
	}
	if first[0].Name != "Amy" || first[2].Name != "Zoe" {
		t.Errorf("expected name-sorted order, got %q..%q", first[0].Name, first[2].Name)
	}
}

func TestAggregateMergesSameNameDifferentEmail(t *testing.T) {
	now := time.Now()
	// Same person, two emails (work + personal), identical name → one row.
	records := []git.CommitRecord{
		{Author: "Rich Haase", Email: "rich@work.com", Date: now, Added: 10, RepoName: "r"},
		{Author: "Rich Haase", Email: "rich@personal.com", Date: now, Added: 20, RepoName: "r"},
	}
	result := stats.Aggregate(records)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged author, got %d", len(result))
	}
	if result[0].Added != 30 {
		t.Errorf("expected merged Added=30, got %d", result[0].Added)
	}
}

func TestAggregateMergesNormalizedNameTiedCounts(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice Smith", Email: "alice@work.com", Date: now, Added: 10, RepoName: "r"},
		{Author: "alice smith", Email: "alice@home.com", Date: now, Added: 20, RepoName: "r"},
	}
	result := stats.Aggregate(records)
	if len(result) != 1 {
		names := make([]string, len(result))
		for i, a := range result {
			names[i] = a.Name
		}
		t.Fatalf("normalized-equal names with tied counts did not merge: got %d rows %v", len(result), names)
	}
	if result[0].Added != 30 {
		t.Errorf("merged Added = %d, want 30", result[0].Added)
	}
}

func TestAggregateAliases(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Rich Haase", Email: "rich@x.com", Date: now, Added: 10, RepoName: "r"},
		{Author: "rhaase", Email: "rich@x.com", Date: now, Added: 5, RepoName: "r"},
	}
	result := stats.Aggregate(records)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged author, got %d", len(result))
	}
	a := result[0]
	if a.Name != "Rich Haase" {
		t.Errorf("canonical name = %q, want %q", a.Name, "Rich Haase")
	}
	if !a.Aliases["Rich Haase"] || !a.Aliases["rhaase"] {
		t.Errorf("aliases = %v, want both spellings", a.Aliases)
	}
}

func TestSortByAIUsesPercentNotCount(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "HighCount", Commits: 100, AICommits: 10},
		{Name: "HighPercent", Commits: 4, AICommits: 4},
	}
	stats.Sort(authors, stats.SortByAI)
	if authors[0].Name != "HighPercent" {
		t.Errorf("SortByAI ranked by raw count: got %q first, want HighPercent", authors[0].Name)
	}
}

func TestMergeAuthorNameDeterministicTieBreak(t *testing.T) {
	stats.FuzzyMatching = true
	t.Cleanup(func() { stats.FuzzyMatching = false })
	cases := []struct {
		name   string
		target string
		all    []string
		counts map[string]int
		want   string
	}{
		{
			name:   "tied counts break lexicographically at equal length",
			target: "Andrew",
			all:    []string{"xAndrew", "Andrew", "Andrews"},
			counts: map[string]int{"Andrew": 1, "Andrews": 5, "xAndrew": 5},
			want:   "Andrews",
		},
		{
			name:   "tied counts prefer the longer name",
			target: "Stephen",
			all:    []string{"Stephe", "Stephen", "Stephens"},
			counts: map[string]int{"Stephen": 1, "Stephens": 5, "Stephe": 5},
			want:   "Stephens",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stats.MergeAuthorName(tc.target, tc.all, tc.counts); got != tc.want {
				t.Errorf("MergeAuthorName(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

func TestAggregateFuzzyDeterministic(t *testing.T) {
	stats.FuzzyMatching = true
	t.Cleanup(func() { stats.FuzzyMatching = false })
	now := time.Now()
	mk := func() []git.CommitRecord {
		recs := []git.CommitRecord{
			{Author: "Andrew", Email: "andrew@x.com", Date: now, Added: 1, RepoName: "r"},
		}
		for i := 0; i < 5; i++ {
			recs = append(recs,
				git.CommitRecord{Author: "Andrews", Email: "andrews@x.com", Date: now, Added: 1, RepoName: "r"},
				git.CommitRecord{Author: "xAndrew", Email: "xandrew@x.com", Date: now, Added: 1, RepoName: "r"},
			)
		}
		return recs
	}
	want := snapshot(stats.Aggregate(mk()))
	for i := 0; i < 50; i++ {
		if got := snapshot(stats.Aggregate(mk())); got != want {
			t.Fatalf("Aggregate non-deterministic under fuzzy: run %d gave %q, want %q", i, got, want)
		}
	}
}

func snapshot(authors []stats.AuthorStats) string {
	var b strings.Builder
	for _, a := range authors {
		fmt.Fprintf(&b, "%s=%d;", a.Name, a.Commits)
	}
	return b.String()
}
