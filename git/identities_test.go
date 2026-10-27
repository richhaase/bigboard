package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestIdentityAliasesFollowMailmapAcrossRepositoriesAndClones(t *testing.T) {
	source, other := t.TempDir(), t.TempDir()
	makeTestRepo(t, source)
	makeTestRepo(t, other)
	writeAndCommit(t, source, "one.go", "one\n", "source change")
	gitConfig(t, source, "user.name", "Preferred Name")
	gitConfig(t, source, "user.email", "canonical@example.com")
	writeAndCommit(t, source, "two.go", "two\n", "canonical change")
	gitConfig(t, other, "user.name", "Preferred Name")
	gitConfig(t, other, "user.email", "other@example.com")
	writeAndCommit(t, other, "three.go", "three\n", "other change")
	if err := os.WriteFile(filepath.Join(source, ".mailmap"), []byte("Mapped Name <mapped@example.com> <test@example.com>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	gitOutput(t, source, "clone", "--local", source, clone)
	options := git.CollectOptions{Identities: map[string]git.Identity{
		"canonical@example.com":      {Aliases: []string{"mapped@example.com", "test@example.com", "other@example.com"}},
		"must-not-match@example.com": {Aliases: []string{"missing@example.com"}, Name: "Preferred Name"},
	}}
	repositories := git.NewRepositories([]string{source, other, clone})
	var records []git.CommitRecord
	for _, repo := range repositories {
		got, err := git.ScanRepository(t.Context(), repo, options)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range got {
			if record.Email != "canonical@example.com" {
				t.Fatalf("alias mapping did not follow mailmap: %+v", record)
			}
			if repo.Path == source && record.Subject == "source change" && record.Author != "Mapped Name" {
				t.Fatalf("mailmapped display name was lost: %+v", record)
			}
		}
		records = append(records, got...)
	}
	authors := stats.AggregateWithOptions(records, stats.AggregateOptions{FuzzyMatching: true})
	if len(authors) != 1 || authors[0].ID != "email:canonical@example.com" || authors[0].Name != "Preferred Name" || authors[0].Commits != 3 || authors[0].Added != 3 || len(authors[0].PerRepo) != 3 {
		t.Fatalf("aliases or clone deduplication changed totals: %+v", authors)
	}
	for _, repo := range repositories {
		excluded := make(map[string]bool)
		for _, other := range repositories {
			excluded[other.ID] = other.ID != repo.ID
		}
		local := stats.Aggregate(stats.FilterByRepo(records, excluded))
		if len(local) != 1 || local[0].ID != authors[0].ID {
			t.Fatalf("scoped identity changed for %s: %+v", repo.Name, local)
		}
	}
	options.Identities["canonical@example.com"] = git.Identity{Aliases: []string{"mapped@example.com"}, Name: "Override Name"}
	got, err := git.CollectRepository(repositories[0], "main", options)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range got {
		if record.Author != "Override Name" || record.Email != "canonical@example.com" {
			t.Fatalf("display override did not apply to both alias and canonical author: %+v", record)
		}
	}
}
