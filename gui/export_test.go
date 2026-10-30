package gui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestExportMatchesCanonicalAllTimeSchemaWithoutNetwork(t *testing.T) {
	s, cache, provider := testService(t, "cache")
	old := s.now().AddDate(-2, 0, 0)
	records := []git.CommitRecord{
		{CommitID: "duplicate", RepoID: "example/engine", RepoName: "example/engine", Author: "Person", Email: "person@example.test", Date: old, Added: 10, Removed: 4, AIAssisted: true},
		{CommitID: "duplicate", RepoID: "example/library", RepoName: "example/library", Author: "Person", Email: "person@example.test", Date: old, Added: 10, Removed: 4, AIAssisted: true},
		{CommitID: "unknown", RepoID: "example/engine", RepoName: "example/engine", Author: "Person", Email: "person@example.test", Date: s.now(), LinesUnknown: true},
		{CommitID: "bot", RepoID: "example/engine", RepoName: "example/engine", Author: "Release robot", Email: "release@example.test", Date: s.now().Add(-time.Hour), Added: 3},
	}
	s.analysis.BotIdentities = []string{"release@example.test"}
	s.snapshot.Repositories = []Repository{{ID: "example/engine", Name: "example/engine"}, {ID: "example/library", Name: "example/library"}}
	for _, record := range records {
		s.snapshot.Commits = append(s.snapshot.Commits, s.commit(record))
	}
	s.snapshot.PullRequests = []PR{{Repo: "example/engine", Number: 99}}
	actual, err := s.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	authors := stats.AggregateWithOptions(records, stats.AggregateOptions{BotIdentities: s.analysis.BotIdentities})
	stats.Sort(authors, stats.SortByTotal)
	expected, err := json.MarshalIndent(authors, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if actual != string(expected)+"\n" {
		t.Fatalf("export differs from the contributor schema/counts:\n%s\n%s", actual, expected)
	}
	if provider.calls != 0 || cache.refreshes != 0 {
		t.Fatal("export performed remote work")
	}
}

func TestExportRequiresLoadedRepositorySelection(t *testing.T) {
	s, _, _ := testService(t, "cache")
	if _, err := s.ExportJSON(); err == nil {
		t.Fatal("empty unloaded selection exported")
	}
}
