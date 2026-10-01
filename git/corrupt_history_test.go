package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestScanRepositoryRejectsCorruptHistory(t *testing.T) {
	for _, tc := range []struct{ name, path, content string }{
		{"malformed branch", "refs/heads/main", "corrupted\n"},
		{"missing branch object", "refs/heads/main", "1111111111111111111111111111111111111111\n"},
		{"malformed HEAD", "HEAD", "corrupted\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			makeTestRepo(t, dir)
			writeAndCommit(t, dir, "README.md", "hello\n", "initial commit")
			repository := git.NewRepositories([]string{dir})[0]
			records, err := git.ScanRepository(context.Background(), repository, git.CollectOptions{})
			if err != nil || len(records) != 1 {
				t.Fatalf("setup: records=%d err=%v", len(records), err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".git", tc.path), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			records, err = git.ScanRepository(context.Background(), repository, git.CollectOptions{})
			if err == nil {
				t.Fatalf("corrupt repository returned success with %d records", len(records))
			}
		})
	}
}

func TestScanRepositoryAcceptsUnbornHEAD(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	repository := git.NewRepositories([]string{dir})[0]
	records, err := git.ScanRepository(context.Background(), repository, git.CollectOptions{})
	if err != nil || len(records) != 0 {
		t.Fatalf("empty repository: records=%d err=%v", len(records), err)
	}
}
