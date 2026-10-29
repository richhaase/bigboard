package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type fakeCache struct {
	mu           sync.Mutex
	repositories []git.Repository
	records      []git.CommitRecord
	scanErr      error
	inventoryErr error
	refreshErr   error
	refreshes    int
	wait         chan struct{}
	started      chan struct{}
}

func (c *fakeCache) CachedRepositories() ([]git.Repository, error) {
	return c.repositories, c.inventoryErr
}
func (c *fakeCache) Repository(id, branch string) (git.Repository, error) {
	return git.Repository{ID: id, Name: id, DefaultBranch: branch}, nil
}
func (c *fakeCache) Refresh(context.Context, git.Repository) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshes++
	return c.refreshErr
}
func (c *fakeCache) Revision(context.Context, git.Repository) (string, error) { return "abc123", nil }
func (c *fakeCache) ScanRepository(ctx context.Context, repo git.Repository, options git.CollectOptions) ([]git.CommitRecord, error) {
	if c.started != nil {
		close(c.started)
	}
	if c.wait != nil {
		select {
		case <-c.wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.records, c.scanErr
}

type fakeProvider struct {
	calls        int
	discoveryErr error
	results      []gh.Result
	started      chan struct{}
	wait         chan struct{}
}

func (p *fakeProvider) DiscoverRepositories(ctx context.Context, _ []string) ([]gh.DiscoveredRepository, error) {
	p.calls++
	if p.started != nil {
		close(p.started)
	}
	if p.wait != nil {
		select {
		case <-p.wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []gh.DiscoveredRepository{{Name: "example/engine", DefaultBranch: "main"}}, p.discoveryErr
}
func (p *fakeProvider) DiscoverPersonalRepositories(context.Context, string) ([]gh.DiscoveredRepository, error) {
	p.calls++
	return nil, p.discoveryErr
}
func (p *fakeProvider) CurrentLogin(context.Context) (string, error) { p.calls++; return "viewer", nil }
func (p *fakeProvider) FetchAll(context.Context, []gh.RepositoryRequest) []gh.Result {
	p.calls++
	return p.results
}

func testService(t *testing.T, mode string) (*Service, *fakeCache, *fakeProvider) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cache := &fakeCache{repositories: []git.Repository{{ID: "example/engine", Name: "example/engine", DefaultBranch: "main"}}, records: []git.CommitRecord{{CommitID: "abc", RepoID: "example/engine", Author: "Test Person", Email: "person@example.test", Date: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), Added: 20, Removed: 5, Changes: []git.PathChange{{Path: "src/main.go", Added: 20, Removed: 5, LinesKnown: true}}}}}
	provider := &fakeProvider{results: []gh.Result{{Repo: "example/engine", Complete: true}}}
	settings := Settings{Mode: mode, Organizations: []string{"example"}, Repositories: []string{}}
	s := &Service{ctx: ctx, cancel: cancel, cache: cache, provider: provider, cachePath: filepath.Join(t.TempDir(), "cache"), path: filepath.Join(t.TempDir(), "desktop.json"), now: func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }, analysis: analysisConfig{PRRequestBudget: 128}}
	s.snapshot = emptySnapshot(settings, s.cachePath)
	return s, cache, provider
}

func TestCacheModeIsOfflineAndSnapshotIsIndependent(t *testing.T) {
	s, cache, provider := testService(t, "cache")
	snapshot, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 || cache.refreshes != 0 {
		t.Fatal("cache mode attempted network access")
	}
	if len(snapshot.Commits) != 1 || snapshot.Commits[0].Files[0].Path != "src/main.go" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if !strings.Contains(snapshot.Status.Message, "PRs are unavailable") {
		t.Fatal("offline PR coverage was not explained")
	}
	snapshot.Commits[0].Files[0].Path = "changed"
	snapshot.Settings.Organizations[0] = "changed"
	if s.Snapshot().Commits[0].Files[0].Path != "src/main.go" || s.Snapshot().Settings.Organizations[0] != "example" {
		t.Fatal("caller can mutate service state")
	}
}

func TestFailedScanRetainsLastGoodCommits(t *testing.T) {
	s, cache, _ := testService(t, "cache")
	first, _ := s.Refresh()
	cache.scanErr = errors.New("objects unavailable")
	cache.records = nil
	second, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Commits, second.Commits) || !second.Repositories[0].Stale || second.Status.Phase != "partial" {
		t.Fatalf("lost retained evidence: %+v", second)
	}
}

func TestFailedPRInventoryRetainsStalePRsAndCompleteEmptyClears(t *testing.T) {
	s, _, provider := testService(t, "live")
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 7, Title: "A change", ContextComplete: true}}}}
	first, err := s.Refresh()
	if err != nil || len(first.PullRequests) != 1 {
		t.Fatalf("first refresh: %+v %v", first, err)
	}
	provider.results = []gh.Result{{Repo: "example/engine", Err: errors.New("rate limited")}}
	second, err := s.Refresh()
	if err != nil || len(second.PullRequests) != 1 || second.PullRequests[0].Complete || second.Status.Phase != "partial" {
		t.Fatalf("failed retention: %+v %v", second, err)
	}
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true}}
	third, err := s.Refresh()
	if err != nil || len(third.PullRequests) != 0 || third.Status.Phase != "ready" {
		t.Fatalf("complete empty did not clear: %+v %v", third, err)
	}
}

func TestFailedDiscoveryRetainsEvidenceAndReportsPartialScope(t *testing.T) {
	s, _, provider := testService(t, "live")
	first, _ := s.Refresh()
	provider.discoveryErr = errors.New("unavailable")
	second, err := s.Refresh()
	if err != nil || len(second.Commits) != len(first.Commits) || second.Repositories[0].Stale || second.Status.Phase != "partial" {
		t.Fatalf("unexpected result: %+v %v", second, err)
	}
}

func TestSettingsSaveValidatesAndPersistsWithoutTouchingCLIConfig(t *testing.T) {
	s, _, _ := testService(t, "cache")
	cliPath := filepath.Join(filepath.Dir(s.path), "config.json")
	cli := []byte(`{"organizations":["example"],"sort":"commits"}`)
	if err := os.WriteFile(cliPath, cli, 0600); err != nil {
		t.Fatal(err)
	}
	initial := s.Snapshot()
	if _, err := s.SaveSettings(Settings{Mode: "live", Repositories: []string{"invalid"}}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	if !reflect.DeepEqual(initial, s.Snapshot()) {
		t.Fatal("validation failure modified current data")
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("invalid settings persisted")
	}
	settings := Settings{Mode: "cache", Organizations: []string{" Example ", "example"}, Repositories: []string{"EXAMPLE/engine"}}
	if _, err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Organizations, []string{"example"}) || saved.Repositories[0] != "example/engine" {
		t.Fatalf("not normalized: %+v", saved)
	}
	data, _ = os.ReadFile(cliPath)
	if string(data) != string(cli) {
		t.Fatal("CLI configuration changed")
	}
	if err := os.WriteFile(s.path, []byte(`{"mode":"demo"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSettings(settings); err == nil || !strings.Contains(err.Error(), "outside this window") {
		t.Fatal("external edit overwritten")
	}
}

func TestBusyRefreshAndShutdown(t *testing.T) {
	s, cache, _ := testService(t, "cache")
	cache.wait = make(chan struct{})
	cache.started = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(); done <- err }()
	<-cache.started
	if s.Snapshot().Status.Phase != "refreshing" {
		t.Fatal("refresh progress unavailable")
	}
	if _, err := s.Refresh(); err == nil {
		t.Fatal("overlapping refresh accepted")
	}
	if _, err := s.SaveSettings(Settings{Mode: "cache"}); err == nil {
		t.Fatal("settings changed during refresh")
	}
	s.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel scan")
	}
	if _, err := s.Refresh(); err == nil {
		t.Fatal("closed service refreshed")
	}
}

func TestImportedExclusionsAndExplicitSelection(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.analysis.Exclude = []string{"engine"}
	result, _ := s.Refresh()
	if len(result.Repositories) != 0 {
		t.Fatal("imported exclusion ignored")
	}
	result, err := s.SaveSettings(Settings{Mode: "cache", Repositories: []string{"example/engine"}})
	if err != nil || len(result.Repositories) != 1 {
		t.Fatalf("explicit selection failed: %v", err)
	}
}

func TestChangedSettingsCannotRetainOutOfScopeEvidenceOnFailure(t *testing.T) {
	s, _, provider := testService(t, "live")
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	provider.discoveryErr = errors.New("unavailable")
	result, err := s.SaveSettings(Settings{Mode: "live", Organizations: []string{"different"}})
	if err != nil || len(result.Commits) != 0 || len(result.Repositories) != 0 || result.Status.Phase != "partial" {
		t.Fatalf("out-of-scope evidence survived source change: %+v, %v", result, err)
	}
}

func TestNewServiceStartsWithRealImportedSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "bigboard"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bigboard", "config.json"), []byte(`{"personal_account":"example","organizations":["sample"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := s.Snapshot()
	if result.Mode != "live" || result.Settings.PersonalAccount != "example" || len(result.Commits) != 0 || result.Status.Phase != "initial" {
		t.Fatalf("bad startup: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "bigboard", "desktop.json")); !os.IsNotExist(err) {
		t.Fatal("startup wrote settings")
	}
}

func TestObsoleteSavedModeMigratesToRealSourcesWithoutWriting(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", root)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			configDir := filepath.Join(root, "bigboard")
			if err := os.MkdirAll(configDir, 0700); err != nil {
				t.Fatal(err)
			}
			if configured {
				if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"organizations":["example"]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			obsolete := []byte(`{"mode":"demo","organizations":["synthetic"],"repositories":["synthetic/project"]}`)
			path := filepath.Join(configDir, "desktop.json")
			if err := os.WriteFile(path, obsolete, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := NewService()
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			settings := s.Snapshot().Settings
			expectedMode := "cache"
			if configured {
				expectedMode = "live"
			}
			if settings.Mode != expectedMode || len(settings.Repositories) != 0 || (configured && !reflect.DeepEqual(settings.Organizations, []string{"example"})) || (!configured && len(settings.Organizations) != 0) {
				t.Fatalf("obsolete settings were not replaced by real sources: %+v", settings)
			}
			data, _ := os.ReadFile(path)
			if string(data) != string(obsolete) {
				t.Fatal("startup edited the settings file")
			}
		})
	}
}

func TestExistingRealSettingsArePreserved(t *testing.T) {
	for _, mode := range []string{"cache", "live"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "desktop.json")
			settings := Settings{Mode: mode, PersonalAccount: "example", Organizations: []string{"sample"}, Repositories: []string{"sample/engine"}}
			data, _ := json.Marshal(settings)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, _, _, err := loadSettings(path)
			if err != nil || !reflect.DeepEqual(loaded, settings) {
				t.Fatalf("saved settings changed: %+v %v", loaded, err)
			}
		})
	}
}

func TestSyntheticModeIsRejected(t *testing.T) {
	s, _, provider := testService(t, "cache")
	settings := Settings{Mode: "demo"}
	if _, err := s.SaveSettings(settings); err == nil {
		t.Fatal("synthetic mode accepted for saving")
	}
	if _, err := s.Discover(settings); err == nil {
		t.Fatal("synthetic mode accepted for discovery")
	}
	if provider.calls != 0 || len(s.Snapshot().Commits) != 0 {
		t.Fatal("rejected mode loaded data")
	}
}

func TestStartupPublishesRealCacheBeforeNetworkAndRetainsOnFailure(t *testing.T) {
	s, cache, provider := testService(t, "live")
	cache.refreshErr = errors.New("network unavailable")
	provider.started = make(chan struct{})
	provider.wait = make(chan struct{})
	provider.discoveryErr = errors.New("network unavailable")
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(); done <- err }()
	<-provider.started
	cached := s.Snapshot()
	if cached.Mode != "live" || cached.Settings.Mode != "live" || cached.Status.Phase != "refreshing" || len(cached.Commits) != 1 || cached.Commits[0].ID != "abc" || cached.GeneratedAt == "" {
		t.Fatalf("cached evidence was not published before remote access: %+v", cached)
	}
	close(provider.wait)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	final := s.Snapshot()
	if final.Status.Phase != "partial" || !final.Repositories[0].Stale || !reflect.DeepEqual(final.Commits, cached.Commits) {
		t.Fatalf("cached evidence lost after network failure: %+v", final)
	}
}

func TestFreshUnconfiguredServiceUsesRealCacheAndRequestsSetupWhenEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot().Mode != "cache" {
		t.Fatal("fresh settings did not select real cache")
	}
	result, err := s.Refresh()
	if err != nil || result.Status.Phase != "setup" || len(result.Repositories) != 0 || len(result.Commits) != 0 {
		t.Fatalf("empty cache did not request setup: %+v, %v", result, err)
	}
}

func TestPartialCacheInventoryKeepsUsableRepositories(t *testing.T) {
	s, cache, provider := testService(t, "cache")
	cache.inventoryErr = errors.New("another cache entry is unavailable")
	result, err := s.Refresh()
	if err != nil || len(result.Commits) != 1 || len(result.Repositories) != 1 || result.Status.Phase != "partial" || provider.calls != 0 {
		t.Fatalf("partial cache inventory discarded usable history: %+v, %v", result, err)
	}
}
