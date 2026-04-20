package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

// makeTestRepo initializes a git repo in dir with the given name/email config.
func makeTestRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}
}

func writeAndCommit(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", filename},
		{"git", "commit", "-m", message},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}
}

func TestDetectDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "README.md", "hello\n", "initial commit")

	branch := git.DetectDefaultBranch(dir)
	if branch != "main" && branch != "master" {
		t.Errorf("expected main or master, got %q", branch)
	}
}

func TestCollectCommits(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)

	// First commit: file1.go with 3 lines
	writeAndCommit(t, dir, "file1.go", "line1\nline2\nline3\n", "add file1")
	// Second commit: file2.go with 5 lines
	writeAndCommit(t, dir, "file2.go", "a\nb\nc\nd\ne\n", "add file2")

	ref := git.DetectDefaultBranch(dir)
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Records come newest-first from git log
	var totalAdded, totalFiles int
	for _, r := range records {
		if r.Author == "" {
			t.Error("author should not be empty")
		}
		if r.Date.IsZero() {
			t.Error("date should not be zero")
		}
		now := time.Now()
		if r.Date.Before(now.Add(-1*time.Hour)) || r.Date.After(now.Add(1*time.Hour)) {
			t.Errorf("date %v seems unreasonable", r.Date)
		}
		totalAdded += r.Added
		totalFiles += r.Files
	}

	if totalAdded != 8 {
		t.Errorf("expected total added=8, got %d", totalAdded)
	}
	if totalFiles != 2 {
		t.Errorf("expected total files=2, got %d", totalFiles)
	}
}

func TestCollectCommitsInvalidRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	_, err := git.CollectCommits(dir, "main")
	if err == nil {
		t.Error("expected error for non-git dir, got nil")
	}
}

func TestDiscoverReposSkipsWorktrees(t *testing.T) {
	parent := t.TempDir()

	// Create a main repo
	mainRepo := filepath.Join(parent, "main-repo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatal(err)
	}
	makeTestRepo(t, mainRepo)
	writeAndCommit(t, mainRepo, "a.go", "package a\n", "init")

	// Create a worktree of that repo
	wt := filepath.Join(parent, "main-repo-wt")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature", wt, "HEAD")
	cmd.Dir = mainRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree add failed: %v\n%s", err, out)
	}

	// Discover from parent — should find 1 repo (skip worktree)
	found := git.DiscoverRepos([]string{parent})
	if len(found) != 1 {
		t.Errorf("expected 1 repo (worktree skipped), got %d: %v", len(found), found)
	}

	// Discover from direct worktree path — should find nothing
	direct := git.DiscoverRepos([]string{wt})
	if len(direct) != 0 {
		t.Errorf("expected 0 repos for worktree path, got %d: %v", len(direct), direct)
	}
}

func TestDiscoverRepos(t *testing.T) {
	parent := t.TempDir()

	// Create two git repos inside parent
	repo1 := filepath.Join(parent, "repo1")
	repo2 := filepath.Join(parent, "repo2")
	notRepo := filepath.Join(parent, "notrepo")

	for _, d := range []string{repo1, repo2, notRepo} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	makeTestRepo(t, repo1)
	writeAndCommit(t, repo1, "a.go", "package a\n", "init")
	makeTestRepo(t, repo2)
	writeAndCommit(t, repo2, "b.go", "package b\n", "init")

	// Discover from parent — should find 2 repos
	found := git.DiscoverRepos([]string{parent})
	if len(found) < 2 {
		t.Errorf("expected at least 2 repos from parent scan, got %d: %v", len(found), found)
	}

	// Discover from direct path — should find exactly that repo
	direct := git.DiscoverRepos([]string{repo1})
	if len(direct) != 1 {
		t.Errorf("expected 1 repo from direct path, got %d: %v", len(direct), direct)
	}

	// Mixed: parent + direct repo2 — deduplicated, should find at least 2
	mixed := git.DiscoverRepos([]string{parent, repo2})
	if len(mixed) < 2 {
		t.Errorf("expected at least 2 repos from mixed input, got %d: %v", len(mixed), mixed)
	}
}
