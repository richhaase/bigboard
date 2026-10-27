package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryCacheRevisionMissingAndCanceled(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	cache.run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("missing cache started git")
		return "", nil
	}
	if revision, err := cache.Revision(context.Background(), repo); err != nil || revision != "" {
		t.Fatalf("missing cache revision: %q, %v", revision, err)
	}
	if _, err := os.Stat(repo.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing cache was created: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Revision(ctx, repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestRepositoryCacheRevisionMatchesScannedHistoryAcrossDefaultChange(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	initial := cacheFixtureGit(t, source, "rev-parse", "HEAD")
	cacheFixtureGit(t, source, "checkout", "--quiet", "-b", "release/next")
	cacheFixtureCommit(t, source, "new default branch\n")
	repo.DefaultBranch = "release/next"
	refreshRunner := cache.run
	cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] != "config" && args[0] != "rev-parse" {
			t.Fatalf("revision used non-read command: %v", args)
		}
		return refreshRunner(ctx, dir, args...)
	}
	revision, err := cache.Revision(context.Background(), repo)
	if err != nil || revision != initial {
		t.Fatalf("cached revision with renamed default: %q, %v; want %q", revision, err, initial)
	}
	records, err := cache.ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(records) != 2 || records[0].CommitID != revision {
		t.Fatalf("revision differs from scanned history: %+v, %v", records, err)
	}
	cache.run = refreshRunner
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	want := cacheFixtureGit(t, source, "rev-parse", "HEAD")
	revision, err = cache.Revision(context.Background(), repo)
	if err != nil || revision != want || revision == initial {
		t.Fatalf("refreshed revision: %q, %v; want %q", revision, err, want)
	}
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	unchanged, err := cache.Revision(context.Background(), repo)
	if err != nil || unchanged != revision {
		t.Fatalf("unchanged revision: %q, %v; want %q", unchanged, err, revision)
	}
}

func TestRepositoryCacheRevisionRejectsUntrustedCache(t *testing.T) {
	for _, kind := range []string{"foreign-path", "symlink", "unowned", "wrong-origin", "partial", "alternates", "broken-ref"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign-path":
				repo.Path = source
			case "symlink":
				if err := os.RemoveAll(repo.Path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, repo.Path); err != nil {
					t.Fatal(err)
				}
			case "unowned":
				if err := os.Remove(filepath.Join(repo.Path, "bigboard-cache")); err != nil {
					t.Fatal(err)
				}
			case "wrong-origin":
				cacheFixtureGit(t, repo.Path, "config", "remote.origin.url", "https://github.com/example/other.git")
			case "partial":
				cacheFixtureGit(t, repo.Path, "config", "remote.origin.promisor", "true")
			case "alternates":
				if err := os.WriteFile(filepath.Join(repo.Path, "objects", "info", "alternates"), []byte(source+"/.git/objects\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "broken-ref":
				cacheFixtureGit(t, repo.Path, "update-ref", "-d", "refs/remotes/origin/main")
				cacheFixtureGit(t, repo.Path, "update-ref", "-d", "refs/heads/main")
			}
			if revision, err := cache.Revision(context.Background(), repo); err == nil || revision != "" {
				t.Fatalf("accepted %s cache: %q, %v", kind, revision, err)
			}
		})
	}
}
