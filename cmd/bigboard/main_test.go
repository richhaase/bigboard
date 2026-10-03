package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestFullPipeline(t *testing.T) {
	dir := t.TempDir()

	// Set up git repo
	setupCmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "alice@example.com"},
		{"git", "config", "user.name", "Alice"},
		{"git", "config", "commit.gpgSign", "false"},
		{"git", "config", "core.hooksPath", t.TempDir()},
	}
	for _, args := range setupCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s %v", args, out, err)
		}
	}

	// Alice makes 2 commits
	for i, content := range []string{"line1\nline2\nline3\n", "line1\nline2\nline3\nline4\nline5\n"} {
		file := filepath.Join(dir, "code.go")
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cmds := [][]string{
			{"git", "add", "."},
			{"git", "commit", "-m", "commit " + string(rune('A'+i))},
		}
		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v failed: %s %v", args, out, err)
			}
		}
	}

	// Run pipeline
	repoPaths := git.DiscoverReposDepth([]string{dir}, 1)
	if len(repoPaths) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repoPaths))
	}

	repository := git.NewRepositories(repoPaths)[0]
	records, err := git.ScanRepository(t.Context(), repository, git.CollectOptions{})
	if err != nil {
		t.Fatalf("ScanRepository: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	for _, record := range records {
		if record.RepoID != repository.ID || record.RepoName != repository.Name {
			t.Errorf("record repository identity = %q/%q, want %q/%q", record.RepoID, record.RepoName, repository.ID, repository.Name)
		}
		if record.CommitID == "" || record.Subject == "" || len(record.Changes) != 1 || record.Changes[0].Path != "code.go" {
			t.Errorf("missing retained commit evidence: %+v", record)
		}
	}

	authors := stats.AggregateWithOptions(records, stats.AggregateOptions{})
	if len(authors) != 1 {
		t.Fatalf("expected 1 author, got %d", len(authors))
	}

	alice := authors[0]
	if alice.Name != "Alice" {
		t.Errorf("expected Alice, got %s", alice.Name)
	}
	if alice.Commits != 2 {
		t.Errorf("expected 2 commits, got %d", alice.Commits)
	}
	if alice.ID != "email:alice@example.com" || alice.Added != 5 || alice.Removed != 0 {
		t.Errorf("unexpected contributor identity or totals: %+v", alice)
	}
	if contribution := alice.PerRepo[repository.Name]; contribution == nil || contribution.Commits != 2 || contribution.Added != 5 {
		t.Errorf("unexpected repository contribution: %+v", contribution)
	}
}
