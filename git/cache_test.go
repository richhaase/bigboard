package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	for _, kind := range []string{"unchanged", "changed", "renamed", "head-mismatch", "origin-head-mismatch", "missing-branch", "missing-oid"} {
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
			if fetches != wantFetches || checks == 0 {
				t.Fatalf("fetches=%d, want %d; full integrity checks=%d", fetches, wantFetches, checks)
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
		})
	}
}

func TestRepositoryCacheRefreshIfChangedRepairsUnusableCache(t *testing.T) {
	for _, kind := range []string{"missing", "unowned", "incomplete", "corrupt", "shallow"} {
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
			case "incomplete", "corrupt":
				object := "HEAD:file.txt"
				if kind == "corrupt" {
					object = "HEAD^{tree}"
				}
				objectID := cacheFixtureGit(t, source, "rev-parse", object)
				path := filepath.Join(repo.Path, "objects", objectID[:2], objectID[2:])
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "corrupt" {
					if err := os.WriteFile(path, []byte("invalid object"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if current, err := cache.Revision(context.Background(), repo); err != nil || current != oid {
					t.Fatalf("fixture should still resolve HEAD: %s, %v", current, err)
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
			if err := cache.verify(context.Background(), repo, true); err != nil {
				t.Fatalf("replacement unusable: %v", err)
			}
		})
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

func TestRepositoryCacheReplacementRetainsInvalidData(t *testing.T) {
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
			backups, err := filepath.Glob(filepath.Join(filepath.Dir(repo.Path), ".bigboard-replaced-*", "project.git", "retained-data"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("invalid cache was not retained: %v, %v", backups, err)
			}
			if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replacement reused invalid cache contents")
			}
		})
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
