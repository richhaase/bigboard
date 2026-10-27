package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestPathObjectTypeUsesExactRevisionAndLiteralPath(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	name := "space #?%/line\n:name.go"
	if err := os.Mkdir(filepath.Join(dir, "space #?%"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, dir, name, "one\n", "first")
	revision := gitOutput(t, dir, "rev-parse", "HEAD")
	gitOutput(t, dir, "rm", "--", name)
	gitOutput(t, dir, "commit", "-m", "delete")
	current := gitOutput(t, dir, "rev-parse", "HEAD")
	repo := git.Repository{Path: dir}
	for path, want := range map[string]string{name: "blob", "space #?%": "tree"} {
		got, err := git.PathObjectType(t.Context(), repo, revision, path)
		if err != nil || got != want {
			t.Fatalf("path %q: got %q, %v", path, got, err)
		}
		if _, err := git.PathObjectType(t.Context(), repo, current, path); err == nil {
			t.Fatalf("deleted path %q accepted at current revision", path)
		}
	}
	for _, path := range []string{"", "/absolute", "../outside", "space #?%/../outside", "./space #?%", "space #?%//file", "nul\x00path"} {
		if _, err := git.PathObjectType(t.Context(), repo, revision, path); err == nil {
			t.Fatalf("invalid path %q accepted", path)
		}
	}
	for _, revision := range []string{"HEAD", "main", strings.Repeat("g", 40), strings.Repeat("a", 39), revision + "~1"} {
		if _, err := git.PathObjectType(t.Context(), repo, revision, name); err == nil {
			t.Fatalf("invalid revision %q accepted", revision)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := git.PathObjectType(ctx, repo, revision, name); err == nil {
		t.Fatal("canceled lookup succeeded")
	}
}

func TestPathObjectTypeIgnoresAmbientRepositoryAndDeclinesPartialClone(t *testing.T) {
	dir, foreign := t.TempDir(), t.TempDir()
	makeTestRepo(t, dir)
	makeTestRepo(t, foreign)
	writeAndCommit(t, dir, "file.go", "one\n", "first")
	writeAndCommit(t, foreign, "other.go", "other\n", "foreign")
	revision := gitOutput(t, dir, "rev-parse", "HEAD")
	gitConfig(t, dir, "remote.origin.promisor", "true")
	if _, err := git.PathObjectType(t.Context(), git.Repository{Path: dir}, revision, "file.go"); err == nil {
		t.Fatal("partial repository permitted an object lookup")
	}
	gitConfig(t, dir, "remote.origin.promisor", "false")
	t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_WORK_TREE", foreign)
	got, err := git.PathObjectType(t.Context(), git.Repository{Path: dir}, revision, "file.go")
	if err != nil || got != "blob" {
		t.Fatalf("environment redirected lookup: %q, %v", got, err)
	}
}
