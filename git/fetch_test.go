package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

func TestFetchUpdatesRemoteHistoryWithoutChangingLocalWork(t *testing.T) {
	source := t.TempDir()
	makeTestRepo(t, source)
	writeAndCommit(t, source, "a.go", "one\n", "first")
	gitOutput(t, source, "branch", "retired")
	clone := filepath.Join(t.TempDir(), "clone")
	gitOutput(t, source, "clone", "file://"+source, clone)
	gitOutput(t, clone, "config", "user.name", "Test User")
	gitOutput(t, clone, "config", "user.email", "test@example.com")
	gitOutput(t, clone, "config", "commit.gpgSign", "false")
	gitOutput(t, clone, "config", "core.hooksPath", t.TempDir())
	writeAndCommit(t, clone, "local.go", "local\n", "local work")
	gitOutput(t, clone, "tag", "local-tag")
	gitOutput(t, clone, "config", "remote.origin.fetch", "+refs/heads/*:refs/heads/*")
	gitOutput(t, clone, "config", "remote.origin.pruneTags", "true")
	if err := os.WriteFile(filepath.Join(clone, "a.go"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	head := gitOutput(t, clone, "rev-parse", "HEAD")
	status := gitOutput(t, clone, "status", "--porcelain")
	writeAndCommit(t, source, "a.go", "one\ntwo\n", "remote update")
	gitOutput(t, source, "branch", "-D", "retired")
	gitOutput(t, source, "tag", "remote-tag")
	if got := scanFixture(t, clone); len(got) != 1 {
		t.Fatalf("offline scan fetched remote history: %+v", got)
	}
	repo := git.NewRepositories([]string{clone})[0]
	if err := git.FetchRepository(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if got := scanFixture(t, clone); len(got) != 2 {
		t.Fatalf("fetch did not update scanned history: %+v", got)
	}
	if gitOutput(t, clone, "rev-parse", "HEAD") != head || gitOutput(t, clone, "status", "--porcelain") != status {
		t.Fatal("fetch changed local branch or worktree")
	}
	if got := gitOutput(t, clone, "tag"); got != "local-tag" {
		t.Fatalf("fetch changed tags: %q", got)
	}
	if got := gitOutput(t, clone, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/retired"); got != "" {
		t.Fatalf("deleted remote branch not pruned: %q", got)
	}
	gitOutput(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing"))
	if err := git.FetchRepository(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "fetch failed") {
		t.Fatalf("missing fetch diagnostic: %v", err)
	}
	if got := scanFixture(t, clone); len(got) != 2 {
		t.Fatal("failed fetch prevented cached scan")
	}
}

func TestFetchNoOriginAndCancellation(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	repo := git.NewRepositories([]string{dir})[0]
	if err := git.FetchRepository(context.Background(), repo); err != nil {
		t.Fatalf("local-only repository failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := git.FetchRepository(ctx, repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestFetchKeepsShallowBoundary(t *testing.T) {
	source := t.TempDir()
	makeTestRepo(t, source)
	writeAndCommit(t, source, "a.go", "one\n", "first")
	writeAndCommit(t, source, "a.go", "one\ntwo\n", "second")
	clone := filepath.Join(t.TempDir(), "clone")
	gitOutput(t, source, "clone", "--depth=1", "file://"+source, clone)
	writeAndCommit(t, source, "a.go", "one\ntwo\nthree\n", "third")
	if err := git.FetchRepository(context.Background(), git.NewRepositories([]string{clone})[0]); err != nil {
		t.Fatal(err)
	}
	if gitOutput(t, clone, "rev-parse", "--is-shallow-repository") != "true" || len(scanFixture(t, clone)) != 2 {
		t.Fatal("fetch deepened clone or failed to update history")
	}
}

func TestFetchUsesNoninteractiveSSHAndHonorsDeadline(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	gitOutput(t, dir, "remote", "add", "origin", "ssh://example.invalid/repo")
	ssh := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$GIT_TERMINAL_PROMPT\" \"$@\" >&2\nexit 1\n"
	if err := os.WriteFile(ssh, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", "'"+ssh+"'")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	repo := git.NewRepositories([]string{dir})[0]
	err := git.FetchRepository(context.Background(), repo)
	if err == nil || !strings.Contains(err.Error(), "0\n-o\nBatchMode=yes") {
		t.Fatalf("SSH could prompt: %v", err)
	}
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := git.FetchRepository(ctx, repo); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("fetch waited for SSH after deadline")
	}
}
