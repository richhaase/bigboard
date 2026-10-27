package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cacheFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false"}, args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func cacheFixtureCommit(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cacheFixtureGit(t, dir, "add", "file.txt")
	cacheFixtureGit(t, dir, "commit", "--quiet", "-m", text)
}

func cacheFixture(t *testing.T) (*RepositoryCache, Repository, string) {
	t.Helper()
	source := t.TempDir()
	cacheFixtureGit(t, source, "init", "--quiet", "--initial-branch=main", "--template=")
	cacheFixtureGit(t, source, "config", "user.name", "Example Contributor")
	cacheFixtureGit(t, source, "config", "user.email", "example@example.invalid")
	cacheFixtureCommit(t, source, "first\n")
	cacheFixtureCommit(t, source, "first\nsecond\n")
	cache := &RepositoryCache{Root: t.TempDir()}
	cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
		prefix := []string{"-c", "protocol.file.allow=always", "-c", "url.file://" + source + ".insteadOf=https://github.com/example/project.git"}
		return runCacheGit(ctx, dir, append(prefix, args...)...)
	}
	repo, err := cache.Repository("Example/Project", "main")
	if err != nil {
		t.Fatal(err)
	}
	return cache, repo, source
}

func TestRepositoryCacheRootAndIdentity(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cache, err := NewRepositoryCache()
	if err != nil || cache.Root != filepath.Join(os.Getenv("XDG_CACHE_HOME"), "bigboard", "repos") {
		t.Fatalf("cache root: %+v, %v", cache, err)
	}
	repo, err := cache.Repository("Example/Project", "")
	if err != nil || repo.ID != "example/project" || repo.Name != repo.ID || repo.Path != filepath.Join(cache.Root, "github.com", "example", "project.git") {
		t.Fatalf("repository identity: %+v, %v", repo, err)
	}
	for _, id := range []string{"../project", "example/../project", "https://github.com/example/project", "example/project/extra", "example\\project", "example/.."} {
		if _, err := cache.Repository(id, "main"); err == nil {
			t.Fatalf("accepted unsafe repository %q", id)
		}
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", t.TempDir())
	cache, err = NewRepositoryCache()
	if err != nil || cache.Root != filepath.Join(os.Getenv("HOME"), ".cache", "bigboard", "repos") {
		t.Fatalf("default cache root: %+v, %v", cache, err)
	}
	t.Setenv("XDG_CACHE_HOME", "relative")
	if _, err := NewRepositoryCache(); err == nil {
		t.Fatal("accepted a relative cache root")
	}
}

func TestRepositoryCacheFullHistoryRefreshAndDefaultBranch(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	cacheFixtureGit(t, source, "branch", "feature")
	cacheFixtureGit(t, source, "tag", "release")
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if got := cacheFixtureGit(t, repo.Path, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Fatalf("not bare: %s", got)
	}
	if got := cacheFixtureGit(t, repo.Path, "rev-parse", "--is-shallow-repository"); got != "false" {
		t.Fatalf("not full: %s", got)
	}
	if got := cacheFixtureGit(t, repo.Path, "for-each-ref", "--format=%(refname)"); got != "refs/heads/main\nrefs/remotes/origin/HEAD\nrefs/remotes/origin/main" {
		t.Fatalf("unexpected cloned refs: %s", got)
	}
	commits, err := ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(commits) != 2 || commits[0].RepoID != repo.ID {
		t.Fatalf("full bare scan: %d, %v", len(commits), err)
	}
	cacheFixtureCommit(t, source, "first\nsecond\nthird\n")
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	commits, err = ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(commits) != 3 {
		t.Fatalf("refreshed bare scan: %d, %v", len(commits), err)
	}
	cacheFixtureGit(t, source, "checkout", "--quiet", "-b", "release/next")
	cacheFixtureCommit(t, source, "next branch\n")
	repo.DefaultBranch = "release/next"
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if got := cacheFixtureGit(t, repo.Path, "for-each-ref", "--format=%(refname)"); got != "refs/heads/release/next\nrefs/remotes/origin/HEAD\nrefs/remotes/origin/release/next" {
		t.Fatalf("old default branch retained: %s", got)
	}
	commits, err = ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(commits) != 4 {
		t.Fatalf("changed default branch scan: %d, %v", len(commits), err)
	}
	cached, err := cache.CachedRepositories()
	if err != nil || len(cached) != 1 || cached[0].DefaultBranch != "release/next" {
		t.Fatalf("offline inventory: %+v, %v", cached, err)
	}
	info, err := cache.Info(nil)
	if err != nil || info.Bytes == 0 || len(info.Orphans) != 1 || info.Orphans[0].ID != repo.ID {
		t.Fatalf("orphan inventory: %+v, %v", info, err)
	}
	info, err = cache.Info([]Repository{repo})
	if err != nil || len(info.Orphans) != 0 {
		t.Fatalf("listed repository marked orphan: %+v, %v", info, err)
	}
	if err := cache.Remove(repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache not removed: %v", err)
	}
	if got := cacheFixtureGit(t, source, "rev-list", "--count", "HEAD"); got != "4" {
		t.Fatal("cache removal changed source repository")
	}
}

func TestRepositoryCacheRefreshIfChanged(t *testing.T) {
	for _, kind := range []string{"unchanged", "changed", "renamed", "head-mismatch", "origin-head-mismatch", "missing-branch", "missing-oid", "extra-branch", "extra-remote", "extra-tag", "missing-tip"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			branch := "main"
			oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
			switch kind {
			case "changed":
				cacheFixtureCommit(t, source, "third\n")
				oid = cacheFixtureGit(t, source, "rev-parse", "HEAD")
			case "renamed":
				branch = "release/next"
				cacheFixtureGit(t, source, "branch", "-m", branch)
			case "head-mismatch":
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/heads/other", oid)
				cacheFixtureGit(t, repo.Path, "symbolic-ref", "HEAD", "refs/heads/other")
			case "origin-head-mismatch":
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/remotes/origin/other", oid)
				cacheFixtureGit(t, repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/other")
			case "missing-branch":
				branch = ""
			case "missing-oid":
				oid = ""
			case "extra-branch":
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/heads/production", oid)
			case "extra-remote":
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/remotes/origin/production", oid)
			case "extra-tag":
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/tags/v1", oid)
			case "missing-tip":
				if err := os.Remove(filepath.Join(repo.Path, "objects", oid[:2], oid[2:])); err != nil {
					t.Fatal(err)
				}
			}
			originalRun := cache.run
			fetches, checks := 0, 0
			cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
				if args[0] == "fetch" {
					fetches++
				}
				if args[0] == "fsck" {
					checks++
				}
				return originalRun(ctx, dir, args...)
			}
			if err := cache.RefreshIfChanged(context.Background(), repo, branch, oid); err != nil {
				t.Fatal(err)
			}
			wantFetches := 1
			if kind == "unchanged" {
				wantFetches = 0
			}
			if fetches != wantFetches || checks != 0 {
				t.Fatalf("fetches=%d, want %d; fsck runs=%d", fetches, wantFetches, checks)
			}
			if branch == "" {
				branch = repo.DefaultBranch
			}
			if got := cacheFixtureGit(t, repo.Path, "symbolic-ref", "HEAD"); got != "refs/heads/"+branch {
				t.Fatalf("default branch not refreshed: %s", got)
			}
			if got, want := cacheFixtureGit(t, repo.Path, "rev-parse", "HEAD"), cacheFixtureGit(t, source, "rev-parse", "HEAD"); got != want {
				t.Fatalf("revision=%s, want %s", got, want)
			}
			refs := cacheFixtureGit(t, repo.Path, "for-each-ref", "--format=%(refname)")
			if want := "refs/heads/" + branch + "\nrefs/remotes/origin/HEAD\nrefs/remotes/origin/" + branch; refs != want {
				t.Fatalf("refs=%q, want %q", refs, want)
			}
		})
	}
}

func TestRepositoryCacheRefreshIfChangedRepairsUnusableCache(t *testing.T) {
	for _, kind := range []string{"missing", "unowned", "shallow"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
			if kind != "missing" {
				if err := cache.Refresh(context.Background(), repo); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "unowned":
				if err := os.Remove(filepath.Join(repo.Path, "bigboard-cache")); err != nil {
					t.Fatal(err)
				}
			case "shallow":
				if err := os.WriteFile(filepath.Join(repo.Path, "shallow"), []byte(oid+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			originalRun := cache.run
			fetches, replacements := 0, 0
			cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
				if args[0] == "fetch" {
					fetches++
				}
				if args[0] == "init" {
					replacements++
				}
				return originalRun(ctx, dir, args...)
			}
			if err := cache.RefreshIfChanged(context.Background(), repo, "main", oid); err != nil {
				t.Fatal(err)
			}
			if fetches != 1 || replacements != 1 {
				t.Fatalf("unusable cache not replaced: fetches=%d, replacements=%d", fetches, replacements)
			}
			if err := cache.verify(context.Background(), repo); err != nil {
				t.Fatalf("replacement unusable: %v", err)
			}
		})
	}
}

func removeCacheObject(t *testing.T, repo Repository, objectID string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repo.Path, "objects", objectID[:2], objectID[2:])); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCacheScanDamageRebuildsOnRefresh(t *testing.T) {
	for _, object := range []string{"HEAD:file.txt", "HEAD~1", "HEAD^{tree}"} {
		t.Run(object, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{}); err != nil {
				t.Fatal(err)
			}
			oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
			removeCacheObject(t, repo, cacheFixtureGit(t, source, "rev-parse", object))
			if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{}); !errors.Is(err, ErrCacheDamaged) {
				t.Fatalf("scan error=%v, want ErrCacheDamaged", err)
			}
			if _, err := os.Stat(filepath.Join(repo.Path, damagedCacheFile)); err != nil {
				t.Fatalf("damage marker missing: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo.Path, scanCacheFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("parsed scan cache retained: %v", err)
			}
			if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{}); !errors.Is(err, ErrCacheDamaged) {
				t.Fatalf("marked cache scanned: %v", err)
			}
			if repos, err := cache.CachedRepositories(); err != nil || len(repos) != 1 {
				t.Fatalf("damaged cache missing from inventory: %v, %v", repos, err)
			}
			originalRun := cache.run
			fetches, replacements, checks := 0, 0, 0
			cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
				switch args[0] {
				case "fetch":
					fetches++
				case "init":
					replacements++
				case "fsck":
					checks++
				}
				return originalRun(ctx, dir, args...)
			}
			if err := cache.RefreshIfChanged(context.Background(), repo, "main", oid); err != nil {
				t.Fatal(err)
			}
			if fetches != 1 || replacements != 1 || checks != 0 {
				t.Fatalf("fetches=%d replacements=%d fsck=%d, want 1, 1, 0", fetches, replacements, checks)
			}
			if _, err := os.Stat(filepath.Join(repo.Path, damagedCacheFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("damage marker carried over: %v", err)
			}
			assertNoTemporaryCaches(t, cache, repo)
			records, err := cache.ScanRepository(context.Background(), repo, CollectOptions{})
			if err != nil || len(records) != 2 {
				t.Fatalf("rebuilt scan: %d records, %v", len(records), err)
			}
		})
	}
}

func TestRepositoryCacheScanFailuresWithoutDamageLeaveNoMarker(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{Identities: map[string]Identity{"not an email": {}}}); err == nil || errors.Is(err, ErrCacheDamaged) {
		t.Fatalf("invalid options error=%v", err)
	}
	removeCacheObject(t, repo, cacheFixtureGit(t, source, "rev-parse", "HEAD:file.txt"))
	ctx, cancel := context.WithCancel(context.Background())
	originalRun := cache.run
	cache.run = func(runCtx context.Context, dir string, args ...string) (string, error) {
		output, err := originalRun(runCtx, dir, args...)
		if args[0] == "rev-parse" && args[len(args)-1] == "--is-bare-repository" {
			cancel()
		}
		return output, err
	}
	if _, err := cache.ScanRepository(ctx, repo, CollectOptions{}); err == nil || errors.Is(err, ErrCacheDamaged) {
		t.Fatalf("canceled scan error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, damagedCacheFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker written without classified damage: %v", err)
	}
}

func TestIsObjectDamage(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for text, want := range map[string]bool{
		"git log failed: unexpected EOF: fatal: unable to read " + oid:                          true,
		"error: Could not read " + oid + "\nfatal: Failed to traverse parents of commit " + oid: true,
		"error: missing blob " + oid:                                                         true,
		"fatal: bad object " + oid:                                                           true,
		"error: inflate: data stream error (incorrect header check)":                         true,
		"fatal: unable to read tree (" + oid + ")":                                           true,
		"error: could not parse commit " + oid:                                               true,
		"error: Could not parse object '" + oid + "'":                                        true,
		"error: missing tree " + oid + " for commit " + oid:                                  true,
		"error: bad tree object " + oid:                                                      true,
		"fatal: cannot read blob " + oid + " for path file.txt":                              true,
		"fatal: unable to read tree (HEAD)":                                                  false,
		"error: could not parse commit message":                                              false,
		"fatal: bad tree object":                                                             false,
		"error: could not parse object":                                                      false,
		"error: loose object " + oid + " (stored in objects/aa/x) is corrupt":                true,
		"error: packfile objects/pack/p.pack cannot be accessed":                             true,
		"fatal: not a git repository":                                                        false,
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled": false,
		"cache is not a complete independent clone":                                          false,
		"cache has unsupported configuration: remote.origin.promisor":                        false,
		"fatal: bad object HEAD":                                                             false,
	} {
		if got := isObjectDamage(errors.New(text)); got != want {
			t.Errorf("isObjectDamage(%q)=%v, want %v", text, got, want)
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("unable to read %s: %w", oid, context.DeadlineExceeded)} {
		if isObjectDamage(err) {
			t.Errorf("classified %v as damage", err)
		}
	}
}

func TestRepositoryCacheRefreshIfChangedRejectsForeignPathAndCancellation(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	oid := cacheFixtureGit(t, source, "rev-parse", "HEAD")
	foreign := repo
	foreign.Path = source
	if err := cache.RefreshIfChanged(context.Background(), foreign, "main", oid); err == nil {
		t.Fatal("accepted a developer path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.RefreshIfChanged(ctx, repo, "main", oid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func assertNoTemporaryCaches(t *testing.T, cache *RepositoryCache, repo Repository) {
	t.Helper()
	for _, prefix := range []string{replacedCachePrefix, cloneCachePrefix} {
		leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(repo.Path), prefix+"*"))
		if err != nil || len(leftovers) != 0 {
			t.Fatalf("temporary cache directories remain: %v, %v", leftovers, err)
		}
	}
	repos, err := cache.CachedRepositories()
	if err != nil || len(repos) != 1 || repos[0].ID != repo.ID {
		t.Fatalf("inventory after replacement: %v, %v", repos, err)
	}
}

func TestRepositoryCacheReplacementRetainsInvalidDataUntilPublished(t *testing.T) {
	for _, kind := range []string{"wrong-origin", "shallow", "partial", "unowned", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, _ := cacheFixture(t)
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(repo.Path, "retained-data")
			if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "wrong-origin":
				cacheFixtureGit(t, repo.Path, "config", "remote.origin.url", "https://github.com/other/project.git")
			case "shallow":
				if err := os.WriteFile(filepath.Join(repo.Path, "shallow"), []byte(cacheFixtureGit(t, repo.Path, "rev-parse", "HEAD")+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "partial":
				cacheFixtureGit(t, repo.Path, "config", "remote.origin.promisor", "true")
			case "unowned":
				if err := os.Remove(filepath.Join(repo.Path, "bigboard-cache")); err != nil {
					t.Fatal(err)
				}
			case "redirect":
				if err := os.WriteFile(filepath.Join(repo.Path, "commondir"), []byte(t.TempDir()+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := cache.Remove(repo.ID); err == nil {
				t.Fatal("removed an unverified cache")
			}
			originalRun := cache.run
			cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
				if args[0] == "fetch" {
					return "", errors.New("offline fixture")
				}
				return originalRun(ctx, dir, args...)
			}
			if err := cache.Refresh(context.Background(), repo); err == nil {
				t.Fatal("replacement should fail offline")
			}
			if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "preserve" {
				t.Fatalf("failed replacement discarded existing data: %q, %v", contents, err)
			}
			cache.run = originalRun
			if err := cache.Refresh(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			assertNoTemporaryCaches(t, cache, repo)
			if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replacement reused invalid cache contents")
			}
		})
	}
}

func makeTemporaryCache(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "project.git", "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "project.git", "objects", "pack"), []byte("old clone"), 0600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCacheInventoryCleansStaleTemporaryDirectories(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Dir(repo.Path)
	staleReplaced := filepath.Join(owner, replacedCachePrefix+"stale")
	staleClone := filepath.Join(owner, cloneCachePrefix+"stale")
	active := filepath.Join(owner, replacedCachePrefix+"active")
	makeTemporaryCache(t, staleReplaced, 2*staleTemporaryAge)
	makeTemporaryCache(t, staleClone, 2*staleTemporaryAge)
	makeTemporaryCache(t, active, time.Second)
	info, err := cache.Info([]Repository{repo})
	if err != nil || len(info.Problems) != 0 {
		t.Fatalf("info: %v, %v", info.Problems, err)
	}
	for _, path := range []string{staleReplaced, staleClone} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale temporary directory retained: %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(active, "project.git", "objects", "pack")); err != nil {
		t.Fatalf("active temporary directory removed: %v", err)
	}
	var size int64
	if err := filepath.WalkDir(cache.Root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			stat, statErr := entry.Info()
			if statErr != nil {
				return statErr
			}
			size += stat.Size()
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if info.Bytes != size {
		t.Fatalf("cache size=%d, want %d after cleanup", info.Bytes, size)
	}
	if repos, err := cache.CachedRepositories(); err != nil || len(repos) != 1 {
		t.Fatalf("inventory with active temporary directory: %v, %v", repos, err)
	}
}

func TestRepositoryCacheTemporaryCleanupNeverFollowsLinks(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	makeTemporaryCache(t, outside, 2*staleTemporaryAge)
	link := filepath.Join(filepath.Dir(repo.Path), replacedCachePrefix+"link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.CachedRepositories(); err == nil {
		t.Fatal("symlinked temporary entry was not reported")
	}
	if err := cache.removeTemporary(link); err == nil {
		t.Fatal("removed a symlinked temporary entry")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink removed: %v", err)
	}
	misplaced := []string{
		filepath.Join(cache.Root, replacedCachePrefix+"root"),
		filepath.Join(cache.Root, "github.com", replacedCachePrefix+"host"),
		filepath.Join(outside, replacedCachePrefix+"outside"),
	}
	for _, path := range misplaced {
		makeTemporaryCache(t, path, 2*staleTemporaryAge)
		if err := cache.removeTemporary(path); err == nil {
			t.Fatalf("removed misplaced entry %s", path)
		}
	}
	if _, err := cache.CachedRepositories(); err == nil {
		t.Fatal("misplaced temporary entries were not reported")
	}
	for _, path := range append(misplaced, filepath.Join(outside, "project.git", "objects", "pack")) {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("misplaced or linked data removed: %s: %v", path, err)
		}
	}
}

func TestRepositoryCacheRejectsSymlinksAndForeignPaths(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := os.MkdirAll(filepath.Dir(repo.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, repo.Path); err != nil {
		t.Fatal(err)
	}
	if err := cache.Refresh(context.Background(), repo); err == nil {
		t.Fatal("followed a cache symlink")
	}
	if err := cache.Remove(repo.ID); err == nil {
		t.Fatal("removed a cache symlink")
	}
	if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{}); err == nil {
		t.Fatal("scanned a cache symlink")
	}
	if err := os.Remove(repo.Path); err != nil {
		t.Fatal(err)
	}
	repo.Path = source
	if err := cache.Refresh(context.Background(), repo); err == nil {
		t.Fatal("refreshed a developer path")
	}
	if _, err := cache.ScanRepository(context.Background(), repo, CollectOptions{}); err == nil {
		t.Fatal("scanned a developer path")
	}
	if got := cacheFixtureGit(t, source, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatal("source repository changed")
	}
}

func TestRepositoryCacheOfflineAndCanceledRefresh(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.Refresh(ctx, repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	cache.run = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "fetch" {
			return "", errors.New("offline fixture")
		}
		return runCacheGit(ctx, dir, args...)
	}
	if err := cache.Refresh(context.Background(), repo); err == nil {
		t.Fatal("offline refresh should report failure")
	}
	commits, err := ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(commits) != 2 {
		t.Fatalf("offline scan lost retained history: %d, %v", len(commits), err)
	}
	if repos, err := cache.CachedRepositories(); err != nil || len(repos) != 1 {
		t.Fatalf("offline inventory: %v, %v", repos, err)
	}
}

func TestCacheGitIsolatesEnvironmentAndDisablesPrompts(t *testing.T) {
	dir := t.TempDir()
	program := "#!/bin/sh\nprintf '%s\\n' \"$@\" \"dir=${GIT_DIR-unset}\" \"count=${GIT_CONFIG_COUNT-unset}\" \"prompt=$GIT_TERMINAL_PROMPT\" \"lazy=$GIT_NO_LAZY_FETCH\" \"ssh=$GIT_SSH_COMMAND\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_DIR", "untrusted")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_SSH_COMMAND", "untrusted")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	out, err := runCacheGit(context.Background(), dir, "--version")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"core.fsmonitor=false", "core.hooksPath=" + os.DevNull, "credential.helper=!gh auth git-credential", "dir=unset", "count=unset", "prompt=0", "lazy=1", "ssh=ssh -o BatchMode=yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing safety setting %q in %q", want, out)
		}
	}
}

func TestRepositoryCacheBareMailmap(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	if err := os.WriteFile(filepath.Join(source, ".mailmap"), []byte("Canonical Contributor <canonical@example.invalid> <example@example.invalid>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cacheFixtureGit(t, source, "add", ".mailmap")
	cacheFixtureGit(t, source, "commit", "--quiet", "-m", "Map contributor identity")
	if err := cache.Refresh(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	commits, err := ScanRepository(context.Background(), repo, CollectOptions{})
	if err != nil || len(commits) != 3 {
		t.Fatalf("bare mailmap scan: %d, %v", len(commits), err)
	}
	for _, commit := range commits {
		if commit.Author != "Canonical Contributor" || commit.Email != "canonical@example.invalid" {
			t.Fatalf("bare cache lost mailmap identity: %+v", commit)
		}
	}
}
