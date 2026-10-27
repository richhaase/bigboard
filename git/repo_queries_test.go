package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func countGitProcesses(t *testing.T) func() []string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	program := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nexec %s \"$@\"\n", quote(logPath), quote(gitPath))
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		contents, err := os.ReadFile(logPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if err := os.WriteFile(logPath, nil, 0600); err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(bytes.TrimSpace(contents))), "\n")
	}
}

func noOpRefresh(t *testing.T, cache *RepositoryCache, repo Repository, oid string) {
	t.Helper()
	ctx := context.Background()
	if err := cache.RefreshIfChanged(ctx, repo, "main", oid); err != nil {
		t.Fatal(err)
	}
	revision, err := cache.Revision(ctx, repo)
	if err != nil || revision != oid {
		t.Fatalf("revision: %q, %v; want %q", revision, err, oid)
	}
	records, err := cache.ScanRepository(ctx, repo, CollectOptions{})
	if err != nil || len(records) != 2 {
		t.Fatalf("scan: %d, %v", len(records), err)
	}
	if again, err := cache.Revision(ctx, repo); err != nil || again != oid {
		t.Fatalf("revision after scan: %q, %v", again, err)
	}
	if info, err := cache.Info([]Repository{repo}); err != nil || len(info.Problems) != 0 || len(info.Orphans) != 0 {
		t.Fatalf("inventory: %+v, %v", info, err)
	}
	if repos, err := cache.CachedRepositories(); err != nil || len(repos) != 1 || repos[0].DefaultBranch != "main" {
		t.Fatalf("cached repositories: %+v, %v", repos, err)
	}
}

func TestNoOpRefreshGitProcessBound(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
	processes := countGitProcesses(t)
	noOpRefresh(t, cache, repo, oid)
	if first := processes(); len(first) > 20 {
		t.Fatalf("first refresh spawned %d git processes:\n%s", len(first), strings.Join(first, "\n"))
	}
	noOpRefresh(t, cache, repo, oid)
	if again := processes(); len(again) > 1 {
		t.Fatalf("no-op refresh spawned %d git processes:\n%s", len(again), strings.Join(again, "\n"))
	}
}

func TestMemoizedQueriesDetectRepositoryChanges(t *testing.T) {
	for _, kind := range []string{"origin", "unsupported-config", "marker", "damaged", "symlink", "ref", "object"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
			noOpRefresh(t, cache, repo, oid)
			noOpRefresh(t, cache, repo, oid)
			ctx := context.Background()
			switch kind {
			case "origin":
				cacheFixtureGit(t, repo.Path, "config", "remote.origin.url", "https://github.com/example/projecu.git")
			case "unsupported-config":
				cacheFixtureGit(t, repo.Path, "config", "http.proxy", "http://127.0.0.1:9")
			case "marker":
				if err := os.Remove(filepath.Join(repo.Path, "bigboard-cache")); err != nil {
					t.Fatal(err)
				}
			case "damaged":
				if err := os.WriteFile(filepath.Join(repo.Path, damagedCacheFile), []byte(repo.ID+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(source, filepath.Join(repo.Path, "refs", "link")); err != nil {
					t.Fatal(err)
				}
			case "ref":
				parent := cacheFixtureGit(t, source, "rev-parse", "HEAD^")
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/remotes/origin/main", parent)
				revision, err := cache.Revision(ctx, repo)
				if err != nil || revision != parent {
					t.Fatalf("moved ref not observed: %q, %v; want %q", revision, err, parent)
				}
				records, err := cache.ScanRepository(ctx, repo, CollectOptions{})
				if err != nil || len(records) != 1 || records[0].CommitID != parent {
					t.Fatalf("moved ref scan: %+v, %v", records, err)
				}
				fetches := 0
				run := cache.run
				cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
					if args[0] == "fetch" {
						fetches++
					}
					return run(ctx, dir, args...)
				}
				if err := cache.RefreshIfChanged(ctx, repo, "main", oid); err != nil || fetches != 1 {
					t.Fatalf("stale ref skipped fetch: fetches=%d, %v", fetches, err)
				}
				noOpRefresh(t, cache, repo, oid)
				return
			case "object":
				removeCacheObject(t, repo, oid)
			}
			if revision, err := cache.Revision(ctx, repo); err == nil || revision != "" {
				t.Fatalf("revision accepted %s change: %q", kind, revision)
			}
			if _, err := cache.ScanRepository(ctx, repo, CollectOptions{}); err == nil {
				t.Fatalf("scan accepted %s change", kind)
			}
		})
	}
}

func TestFetchSkipsUnchangedCacheWrites(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	run := cache.run
	var writes []string
	cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "update-ref" || args[0] == "init" || (args[0] == "config" && args[1] != "--local") || (args[0] == "symbolic-ref" && len(args) == 3 && args[1] != "--quiet") {
			writes = append(writes, strings.Join(args, " "))
		}
		return run(ctx, dir, args...)
	}
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 {
		t.Fatalf("unchanged fetch rewrote cache state: %v", writes)
	}
	cacheFixtureCommit(t, source, "first\nsecond\nthird\n")
	cacheFixtureGit(t, repo.Path, "config", "--unset", "remote.origin.tagOpt")
	cacheFixtureGit(t, repo.Path, "update-ref", "refs/remotes/origin/stale", cacheFixtureGit(t, source, "rev-parse", "HEAD^"))
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	want := cacheFixtureGit(t, source, "rev-parse", "HEAD")
	if got := strings.Join(writes, "\n"); got != "config remote.origin.tagOpt --no-tags\nupdate-ref refs/heads/main "+want+"\nupdate-ref --no-deref -d refs/remotes/origin/stale" {
		t.Fatalf("changed fetch writes:\n%s", got)
	}
	if got := cacheFixtureGit(t, repo.Path, "for-each-ref", "--format=%(refname) %(symref) %(objectname)"); got != "refs/heads/main  "+want+"\nrefs/remotes/origin/HEAD refs/remotes/origin/main "+want+"\nrefs/remotes/origin/main  "+want {
		t.Fatalf("refs after fetch: %s", got)
	}
}

func TestRepositoryStampDoesNotFollowSwappedSymlinks(t *testing.T) {
	for _, kind := range []string{"file", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			repo := t.TempDir()
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Join(repo, "refs", "heads"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(outside, "heads"), 0700); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join("refs", "heads", "main")
			if err := os.WriteFile(filepath.Join(repo, name), []byte("inside\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "heads", "main"), []byte("outside\n"), 0600); err != nil {
				t.Fatal(err)
			}
			checked, err := os.Lstat(filepath.Join(repo, name))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				if err := os.Remove(filepath.Join(repo, name)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "heads", "main"), filepath.Join(repo, name)); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				if err := os.RemoveAll(filepath.Join(repo, "refs")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(repo, "refs")); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(repo)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			if contents, err := readStampFile(root, name, checked); err == nil || strings.Contains(string(contents), "outside") {
				t.Fatalf("followed swapped %s symlink: %q, %v", kind, contents, err)
			}
			if _, err := repositoryStamp(repo); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("stamp accepted %s symlink: %v", kind, err)
			}
		})
	}
}
