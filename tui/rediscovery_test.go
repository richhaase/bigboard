package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type rediscoveryFixture struct {
	mu        sync.Mutex
	listed    []git.Repository
	cached    []git.Repository
	err       error
	calls     int
	prepared  []string
	discovery func() ([]git.Repository, error)
}

func managedRepository(id string) git.Repository {
	return git.Repository{ID: id, Name: id, Path: "/cache/" + id + ".git", DefaultBranch: "main"}
}

func (f *rediscoveryFixture) model(t *testing.T, repos ...git.Repository) Model {
	t.Helper()
	f.listed, f.cached = slices.Clone(repos), slices.Clone(repos)
	m := NewModelWithOptions(repos, stats.SortByTotal, nil, "", DefaultTimeIndex, Options{
		PRProvider: &fakePRProvider{},
		PrepareRepository: func(_ context.Context, repo git.Repository) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.prepared = append(f.prepared, repo.ID)
			if !slices.ContainsFunc(f.cached, func(cached git.Repository) bool { return cached.ID == repo.ID }) {
				f.cached = append(f.cached, repo)
			}
			return nil
		},
		ScanRepository: func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) {
			return nil, nil
		},
		CacheInfo: func() (git.CacheInfo, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			info := git.CacheInfo{Root: "/cache"}
			for _, repo := range f.cached {
				if !slices.ContainsFunc(f.listed, func(listed git.Repository) bool { return listed.ID == repo.ID }) {
					info.Orphans = append(info.Orphans, repo)
				}
			}
			return info, nil
		},
		RemoveCachedRepository: func(string) error { return nil },
		Rediscover: func(context.Context) ([]git.Repository, map[string]bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls++
			if f.err != nil {
				return nil, nil, f.err
			}
			repos, err := f.discovery()
			if err == nil {
				f.listed = slices.Clone(repos)
			}
			return repos, nil, err
		},
	})
	m.width, m.height = 120, 40
	t.Cleanup(m.Close)
	m = runRefreshCommands(t, m, m.Init())
	f.prepared = nil
	return m
}

func repositoryIDs(repos []git.Repository) []string {
	ids := make([]string, 0, len(repos))
	for _, repo := range repos {
		ids = append(ids, repo.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestManualRefreshTracksRepositoriesAddedAndRemovedUpstream(t *testing.T) {
	fixture := &rediscoveryFixture{}
	a, b, c := managedRepository("org/a"), managedRepository("org/b"), managedRepository("org/c")
	fixture.discovery = func() ([]git.Repository, error) { return []git.Repository{a, c}, nil }
	m := fixture.model(t, a, b)
	if fixture.calls != 0 || !slices.Equal(repositoryIDs(m.local.repositories), []string{"org/a", "org/b"}) {
		t.Fatalf("startup rediscovered or lost repositories: calls=%d", fixture.calls)
	}
	m.excludedRepos["org/a"] = true
	m, cmd := applyLocal(t, m, key("r"))
	if cmd == nil || !m.scan.running {
		t.Fatal("manual refresh did not start rediscovery")
	}
	if again, _ := applyLocal(t, m, key("r")); again.scan.generation != m.scan.generation {
		t.Fatal("repeated refresh restarted active rediscovery")
	}
	m = runRefreshCommands(t, m, cmd)
	if fixture.calls != 1 || !slices.Equal(repositoryIDs(m.repositories), []string{"org/a", "org/c"}) || !slices.Equal(repositoryIDs(m.local.repositories), []string{"org/a", "org/c"}) {
		t.Fatalf("tracked=%v scanned=%v", repositoryIDs(m.repositories), repositoryIDs(m.local.repositories))
	}
	if !m.excludedRepos["org/a"] || !slices.Equal(fixture.prepared, []string{"org/c"}) {
		t.Fatalf("session exclusion lost or excluded repository fetched: prepared=%v", fixture.prepared)
	}
	if !m.orphanedRepository("org/b") || m.orphanedRepository("org/c") || m.discoveryErr != "" {
		t.Fatalf("removed repository is not an unlisted cache entry: %+v", m.cacheInfo.Orphans)
	}
	m.openRepositoryControls()
	if !strings.Contains(ansi.Strip(m.View()), "org/b") {
		t.Fatal("Repository controls hid the unlisted cache entry")
	}
}

func TestManualRefreshKeepsBoardWhenRediscoveryFails(t *testing.T) {
	fixture := &rediscoveryFixture{}
	a, b := managedRepository("org/a"), managedRepository("org/b")
	m := fixture.model(t, a, b)
	fixture.err = errors.New("discovering org: GitHub authentication is required")
	m, cmd := applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, cmd)
	if m.quitting || m.err != nil || m.loading || !slices.Equal(repositoryIDs(m.local.repositories), []string{"org/a", "org/b"}) {
		t.Fatalf("failed rediscovery replaced the board: tracked=%v err=%v", repositoryIDs(m.local.repositories), m.err)
	}
	if !slices.Equal(fixture.prepared, []string{"org/a", "org/b"}) {
		t.Fatalf("failed rediscovery skipped the fetch pass: %v", fixture.prepared)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "discovery failed") {
		t.Fatalf("status hid discovery failure: %s", view)
	}
	m.showScanErrors = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "authentication is required") {
		t.Fatalf("Data status hid discovery failure: %s", view)
	}
	fixture.err = nil
	fixture.discovery = func() ([]git.Repository, error) { return []git.Repository{a, b}, nil }
	m.showScanErrors = false
	m, cmd = applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, cmd)
	if m.discoveryErr != "" || strings.Contains(ansi.Strip(m.View()), "discovery failed") {
		t.Fatal("successful rediscovery retained the failure status")
	}
}

func TestManualRefreshDiscoversRepositoriesWhenNoneAreTracked(t *testing.T) {
	fixture := &rediscoveryFixture{}
	fixture.discovery = func() ([]git.Repository, error) { return nil, nil }
	m := fixture.model(t)
	m, cmd := applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, cmd)
	if m.scan.running || fixture.calls != 1 || len(m.repositories) != 0 {
		t.Fatal("empty rediscovery left refresh running")
	}
	created := managedRepository("org/new")
	fixture.discovery = func() ([]git.Repository, error) { return []git.Repository{created}, nil }
	m, cmd = applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, cmd)
	if !slices.Equal(repositoryIDs(m.local.repositories), []string{"org/new"}) || !slices.Equal(fixture.prepared, []string{"org/new"}) {
		t.Fatalf("new repository did not appear: %v", repositoryIDs(m.local.repositories))
	}
}
