package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type fakeCache struct {
	repositories []git.Repository
	records      []git.CommitRecord
	scanErr      error
	refreshErr   error
	refreshes    int
	wait         chan struct{}
	started      chan struct{}
}

func (c *fakeCache) CachedRepositories() ([]git.Repository, error) { return c.repositories, nil }
func (c *fakeCache) Repository(id, branch string) (git.Repository, error) {
	return git.Repository{ID: id, Name: id, DefaultBranch: branch}, nil
}
func (c *fakeCache) Refresh(context.Context, git.Repository) error {
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
}

func (p *fakeProvider) DiscoverRepositories(context.Context, []string) ([]gh.DiscoveredRepository, error) {
	p.calls++
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

func TestFailedDiscoveryRetainsEvidenceAndMarksItStale(t *testing.T) {
	s, _, provider := testService(t, "live")
	first, _ := s.Refresh()
	provider.discoveryErr = errors.New("unavailable")
	second, err := s.Refresh()
	if err != nil || len(second.Commits) != len(first.Commits) || !second.Repositories[0].Stale || second.Status.Phase != "partial" {
		t.Fatalf("unexpected result: %+v %v", second, err)
	}
}

func TestSettingsSaveValidatesAndPersistsWithoutTouchingCLIConfig(t *testing.T) {
	s, _, _ := testService(t, "demo")
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
	if _, err := s.SaveSettings(Settings{Mode: "demo"}); err == nil {
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

func TestDemoIsDeterministicRichAndSynthetic(t *testing.T) {
	s, cache, provider := testService(t, "demo")
	first, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.Refresh()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("demo is nondeterministic for a fixed clock")
	}
	if len(first.Repositories) < 5 || len(first.Commits) < 100 || len(first.PullRequests) < 8 {
		t.Fatal("demo does not exercise all boards")
	}
	if !strings.Contains(first.Status.Message, "Synthetic") || provider.calls != 0 || cache.refreshes != 0 {
		t.Fatal("demo is not isolated and labeled")
	}
	for _, commit := range first.Commits {
		added, removed := 0, 0
		for _, file := range commit.Files {
			added += file.Added
			removed += file.Removed
		}
		if added != commit.Added || removed != commit.Removed {
			t.Fatal("demo path totals disagree with commit totals")
		}
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

func TestDemoSelectionFiltersEvidenceButDiscoveryListsAll(t *testing.T) {
	s, _, _ := testService(t, "demo")
	result, err := s.SaveSettings(Settings{Mode: "demo", Repositories: []string{"northstar/atlas"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repositories) != 1 || len(result.Commits) == 0 || len(result.PullRequests) == 0 {
		t.Fatalf("invalid selection: %+v", result)
	}
	for _, commit := range result.Commits {
		if commit.Repo != "northstar/atlas" {
			t.Fatal("out-of-scope commit retained")
		}
	}
	for _, pr := range result.PullRequests {
		if pr.Repo != "northstar/atlas" {
			t.Fatal("out-of-scope PR retained")
		}
	}
	choices, err := s.Discover(result.Settings)
	if err != nil || len(choices) != 6 {
		t.Fatalf("discovery hid unselected repositories: %d, %v", len(choices), err)
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

func TestNewServiceStartsInDemoAndImportsSources(t *testing.T) {
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
	if result.Mode != "demo" || result.Settings.PersonalAccount != "example" || len(result.Commits) == 0 {
		t.Fatalf("bad startup: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "bigboard", "desktop.json")); !os.IsNotExist(err) {
		t.Fatal("startup wrote settings")
	}
}
