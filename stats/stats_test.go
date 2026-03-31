package stats_test

import (
	"testing"
	"time"

	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
)

func TestMergeAuthors(t *testing.T) {
	allNames := []string{"Alice Smith", "alice smith", "Alice S", "Bob Jones"}
	commitCounts := map[string]int{
		"Alice Smith": 10,
		"alice smith": 3,
		"Alice S":     1,
		"Bob Jones":   5,
	}

	// "alice smith" should resolve to "Alice Smith" (highest commit count among similar names)
	canonical := stats.MergeAuthorName("alice smith", allNames, commitCounts)
	if canonical != "Alice Smith" {
		t.Errorf("expected canonical name 'Alice Smith', got %q", canonical)
	}

	// "Alice S" is short (<=5 chars after normalization? no — "alices" is 6, so it should match)
	// "Alice S" normalized = "alice s" — length 7, substring check: "alice s" in "alice smith"? yes
	canonical2 := stats.MergeAuthorName("Alice S", allNames, commitCounts)
	if canonical2 != "Alice Smith" {
		t.Errorf("expected canonical name 'Alice Smith' for 'Alice S', got %q", canonical2)
	}

	// "Bob Jones" should return itself
	canonical3 := stats.MergeAuthorName("Bob Jones", allNames, commitCounts)
	if canonical3 != "Bob Jones" {
		t.Errorf("expected 'Bob Jones', got %q", canonical3)
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
