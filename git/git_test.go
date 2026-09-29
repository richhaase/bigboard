package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDetectDefaultBranchPrefersLocalRemoteCounterpart(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "README.md", "hello\n", "initial commit")

	for _, args := range [][]string{
		{"git", "update-ref", "refs/remotes/origin/main", "HEAD"},
		{"git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}
	writeAndCommit(t, dir, "local.go", "package local\n", "unpushed local commit")

	ref := git.DetectDefaultBranch(dir)
	if ref != "main" {
		t.Fatalf("DetectDefaultBranch = %q, want main", ref)
	}
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("CollectCommits(%q): %v", ref, err)
	}
	if len(records) != 2 {
		t.Fatalf("CollectCommits(%q) returned %d commits, want 2", ref, len(records))
	}
}

func TestDetectDefaultBranchKeepsRemoteRef(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "README.md", "hello\n", "initial commit")

	for _, args := range [][]string{
		{"git", "update-ref", "refs/remotes/origin/main", "HEAD"},
		{"git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"},
		{"git", "checkout", "--detach", "HEAD"},
		{"git", "branch", "-D", "main"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}

	ref := git.DetectDefaultBranch(dir)
	if ref != "origin/main" {
		t.Fatalf("DetectDefaultBranch = %q, want origin/main", ref)
	}
	if _, err := git.CollectCommits(dir, ref); err != nil {
		t.Fatalf("CollectCommits(%q): %v", ref, err)
	}
}

func TestNewRepositoriesUsesUniqueNamesAndStableIDs(t *testing.T) {
	repos := git.NewRepositories([]string{
		"/workspace/org-a/api",
		"/workspace/org-b/api",
		"/workspace/web",
		"/workspace/org-a/api",
	})
	if len(repos) != 3 {
		t.Fatalf("NewRepositories returned %d entries, want 3", len(repos))
	}
	if repos[0].Name != "org-a/api" || repos[1].Name != "org-b/api" || repos[2].Name != "web" {
		t.Fatalf("repository names = %q, %q, %q", repos[0].Name, repos[1].Name, repos[2].Name)
	}
	for _, repo := range repos {
		if !filepath.IsAbs(repo.ID) || repo.ID != repo.Path {
			t.Errorf("repository identity is not an absolute path: %+v", repo)
		}
	}
}

func TestScanRepositoryHonorsCancellation(t *testing.T) {
	repository := git.NewRepositories([]string{t.TempDir()})[0]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := git.ScanRepository(ctx, repository, git.CollectOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanRepository error = %v, want context.Canceled", err)
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
	var totalAdded int
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
	}

	if totalAdded != 8 {
		t.Errorf("expected total added=8, got %d", totalAdded)
	}
}

func writeAndCommitWithMessage(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cmd := exec.Command("git", "add", filename)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}
}

func TestCollectCommitsAIAttribution(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)

	writeAndCommit(t, dir, "a.go", "package a\n", "plain commit")

	writeAndCommitWithMessage(t, dir, "b.go", "package b\n",
		"ai commit\n\nCo-Authored-By: Claude <noreply@anthropic.com>")

	writeAndCommitWithMessage(t, dir, "c.go", "package c\n",
		"copilot commit\n\nCo-Authored-By: GitHub Copilot <copilot@github.com>")

	writeAndCommitWithMessage(t, dir, "e.go", "package e\n",
		"codex commit\n\nCo-authored-by: Codex <noreply@openai.com>")

	writeAndCommit(t, dir, "d.go", "package d\n", "another plain commit")

	ref := git.DetectDefaultBranch(dir)
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}

	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}

	aiCount := 0
	for _, r := range records {
		if r.AIAssisted {
			aiCount++
		}
	}
	if aiCount != 3 {
		t.Errorf("expected 3 AI-assisted commits, got %d", aiCount)
	}
}

func TestCollectCommitsInvalidRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	_, err := git.CollectCommits(dir, "main")
	if err == nil {
		t.Error("expected error for non-git dir, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
		t.Errorf("error omitted git stderr: %v", err)
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
	found := git.DiscoverReposDepth([]string{parent}, 1)
	if len(found) != 1 {
		t.Errorf("expected 1 repo (worktree skipped), got %d: %v", len(found), found)
	}

	// Discover from direct worktree path — should find nothing
	direct := git.DiscoverReposDepth([]string{wt}, 1)
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
	found := git.DiscoverReposDepth([]string{parent}, 1)
	if len(found) < 2 {
		t.Errorf("expected at least 2 repos from parent scan, got %d: %v", len(found), found)
	}

	// Discover from direct path — should find exactly that repo
	direct := git.DiscoverReposDepth([]string{repo1}, 1)
	if len(direct) != 1 {
		t.Errorf("expected 1 repo from direct path, got %d: %v", len(direct), direct)
	}

	// Mixed: parent + direct repo2 — deduplicated, should find at least 2
	mixed := git.DiscoverReposDepth([]string{parent, repo2}, 1)
	if len(mixed) < 2 {
		t.Errorf("expected at least 2 repos from mixed input, got %d: %v", len(mixed), mixed)
	}
}

func gitConfig(t *testing.T, dir, key, value string) {
	t.Helper()
	cmd := exec.Command("git", "config", key, value)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config %s failed: %v\n%s", key, err, out)
	}
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestCollectCommitsHonorsMailmap(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "a.go", "package a\n", "first")

	gitConfig(t, dir, "user.name", "T. User")
	gitConfig(t, dir, "user.email", "stray@example.com")
	writeAndCommit(t, dir, "b.go", "package b\n", "second")

	mailmap := "Test User <test@example.com> <stray@example.com>\n"
	if err := os.WriteFile(filepath.Join(dir, ".mailmap"), []byte(mailmap), 0644); err != nil {
		t.Fatal(err)
	}

	ref := git.DetectDefaultBranch(dir)
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	for _, r := range records {
		if r.Author != "Test User" || r.Email != "test@example.com" {
			t.Errorf("mailmap not honored: got %q <%s>", r.Author, r.Email)
		}
	}
}

func TestCollectCommitsIgnoresNonASCIIVendoredPath(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "main.go", "package main\n", "real code")

	vendorDir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "naïve.lock"), []byte("a\nb\nc\nd\ne\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitInDir(t, dir, "add", "-A")
	runGitInDir(t, dir, "commit", "-m", "add vendored dep")

	ref := git.DetectDefaultBranch(dir)
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}
	total := 0
	for _, r := range records {
		total += r.Added
	}
	if total != 1 {
		t.Errorf("non-ASCII vendored path counted: total Added = %d, want 1", total)
	}
}

func TestCollectCommitsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	ref := git.DetectDefaultBranch(dir)
	records, err := git.CollectCommits(dir, ref)
	if err != nil {
		t.Fatalf("empty repo should be a silent no-op, got error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records from empty repo, got %d", len(records))
	}
}
