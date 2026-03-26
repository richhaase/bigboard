package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
)

func TestFullPipeline(t *testing.T) {
	dir := t.TempDir()

	// Set up git repo
	setupCmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "alice@example.com"},
		{"git", "config", "user.name", "Alice"},
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
	repoPaths := git.DiscoverRepos([]string{dir})
	if len(repoPaths) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repoPaths))
	}

	branch := git.DetectDefaultBranch(repoPaths[0])
	records, err := git.CollectCommits(repoPaths[0], branch)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	authors := stats.Aggregate(records)
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
}
