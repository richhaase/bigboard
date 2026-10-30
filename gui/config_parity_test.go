package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestSavedWorkspacePreservesPatternExclusionsAndExplicitOverrides(t *testing.T) {
	s, _, provider := testService(t, "live")
	configuration := []byte(`{"organizations":["example"],"exclude":["vendor-*","archived","example/retired"]}`)
	configPath := filepath.Join(filepath.Dir(s.path), "config.json")
	if err := os.WriteFile(configPath, configuration, 0600); err != nil {
		t.Fatal(err)
	}
	_, analysis, err := loadSettings(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.analysis = analysis
	p := &changingDiscovery{fakeProvider: provider, repositories: []gh.DiscoveredRepository{
		{Name: "example/engine", DefaultBranch: "main"},
		{Name: "example/vendor-kept", DefaultBranch: "main"},
		{Name: "example/unselected", DefaultBranch: "main"},
	}}
	s.provider = p
	settings := Settings{Mode: "live", Organizations: []string{"example"}, Repositories: []string{"example/engine", "example/vendor-kept"}, ExcludedRepositories: []string{"example/unselected"}}
	if _, err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings, analysis, err = loadSettings(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.analysis = analysis
	s.snapshot.Settings = settings
	for _, name := range []string{"vendor-new", "archived", "retired", "new"} {
		p.repositories = append(p.repositories, gh.DiscoveredRepository{Name: "example/" + name, DefaultBranch: "main"})
	}
	choices, err := s.Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example/engine", "example/new", "example/vendor-kept"}
	for _, repo := range choices {
		if repo.Included != slices.Contains(want, repo.ID) {
			t.Fatalf("discovery inclusion differs for %s: %v", repo.ID, repo.Included)
		}
	}
	result, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, repo := range result.Repositories {
		got = append(got, repo.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("included repositories = %v, want %v", got, want)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved analysisConfig
	if err := json.Unmarshal(data, &saved); err != nil || !slices.Equal(saved.Exclude, analysis.Exclude) {
		t.Fatal("workspace settings changed exclusion patterns")
	}
}

func TestSavedExclusionsWinAndIncludesDoNotRestrictNewRepositories(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.analysis.Exclude = []string{"vendor-*"}
	settings := Settings{Mode: "cache", Repositories: []string{"example/vendor-kept"}, ExcludedRepositories: []string{"example/vendor-kept"}}
	if s.configuredIncluded(settings, "example/vendor-kept") {
		t.Fatal("explicit exclusion did not win over conflicting inclusion")
	}
	settings.ExcludedRepositories = nil
	if !s.configuredIncluded(settings, "example/vendor-kept") || !s.configuredIncluded(settings, "example/new") {
		t.Fatal("explicit includes restricted newly discovered repositories")
	}
}

func TestGroupsAllowInactiveSourcesAndCanonicalizeMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"organizations":["active"],"groups":{"core":["INACTIVE/Engine"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, analysis, err := loadSettings(path)
	if err != nil || !slices.Equal(analysis.Groups["core"], []string{"inactive/engine"}) {
		t.Fatalf("inactive group was rejected or not canonicalized: %+v, err=%v", analysis.Groups, err)
	}
}

func TestSourceChangesPreserveGroupsAndShowOnlyActiveMembers(t *testing.T) {
	s, cache, _ := testService(t, "cache")
	groups := map[string][]string{"core": {"example/engine", "other/engine"}, "inactive": {"outside/engine"}}
	configuration := []byte(`{"organizations":["example"],"groups":{"core":["example/engine","other/engine"],"inactive":["outside/engine"]}}`)
	configPath := filepath.Join(filepath.Dir(s.path), "config.json")
	if err := os.WriteFile(configPath, configuration, 0600); err != nil {
		t.Fatal(err)
	}
	s.analysis.Groups = groups
	s.analysis.source = configuration
	cache.repositories = append(cache.repositories, git.Repository{ID: "other/engine", Name: "other/engine", DefaultBranch: "main"})
	result, err := s.SelectGroup("core")
	if err != nil || len(result.Repositories) != 1 || result.Repositories[0].ID != "example/engine" {
		t.Fatalf("initial active group members = %+v, err=%v", result.Repositories, err)
	}
	result, err = s.SaveSettings(Settings{Mode: "cache", Organizations: []string{"other"}, Repositories: []string{"other/engine"}})
	if err != nil || len(result.Repositories) != 1 || result.Repositories[0].ID != "other/engine" || !reflect.DeepEqual(result.Groups, groups) {
		t.Fatalf("source change did not preserve group definitions and filter members: %+v, err=%v", result, err)
	}
	settings, analysis, err := loadSettings(s.path)
	if err != nil || !slices.Equal(settings.Organizations, []string{"other"}) || !reflect.DeepEqual(analysis.Groups, groups) {
		t.Fatalf("saved source change and retained groups did not reload: %+v %+v, err=%v", settings, analysis.Groups, err)
	}
	result, err = s.SelectGroup("inactive")
	if err != nil || len(result.Repositories) != 0 || !reflect.DeepEqual(result.Groups, groups) {
		t.Fatalf("inactive group was rejected, removed, or showed unavailable members: %+v, err=%v", result, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved analysisConfig
	if err := json.Unmarshal(data, &saved); err != nil || !reflect.DeepEqual(saved.Groups, groups) {
		t.Fatal("source changes rewrote group definitions")
	}
}

func TestLegacyRepositoryPathsAndDepthHaveMigrationGuidance(t *testing.T) {
	for _, key := range []string{"paths", "depth"} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "config.json"), []byte(fmt.Sprintf(`{"%s":null}`, key)), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadSettings(path)
			if err == nil || !strings.Contains(err.Error(), "legacy "+key) || !strings.Contains(err.Error(), "cache") || !strings.Contains(err.Error(), "personal_account or organizations") {
				t.Fatalf("missing migration guidance: %v", err)
			}
		})
	}
}

type blockingScanCache struct {
	*fakeCache
	entered chan struct{}
	release chan struct{}
}

func (c *blockingScanCache) ScanRepository(ctx context.Context, _ git.Repository, _ git.CollectOptions) ([]git.CommitRecord, error) {
	c.entered <- struct{}{}
	select {
	case <-c.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRefreshScansEightRepositoriesConcurrently(t *testing.T) {
	s, base, _ := testService(t, "cache")
	base.repositories = nil
	for i := range 12 {
		base.repositories = append(base.repositories, git.Repository{ID: fmt.Sprintf("example/repo-%02d", i), DefaultBranch: "main"})
	}
	cache := &blockingScanCache{fakeCache: base, entered: make(chan struct{}, 12), release: make(chan struct{})}
	s.cache = cache
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(); done <- err }()
	t.Cleanup(func() {
		close(cache.release)
		if err := <-done; err != nil {
			t.Errorf("refresh failed: %v", err)
		}
	})
	for range 8 {
		select {
		case <-cache.entered:
		case <-time.After(time.Second):
			t.Fatal("refresh did not start eight repository scans")
		}
	}
	select {
	case <-cache.entered:
		t.Fatal("refresh exceeded eight concurrent repository scans")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestCommitSnapshotPreservesUnknownPathsIndependently(t *testing.T) {
	s, _, _ := testService(t, "cache")
	commit := s.commit(git.CommitRecord{CommitID: "unknown-path", RepoID: "example/engine", PathsUnknown: true, Added: 10, Removed: 3})
	if !commit.PathsUnknown || commit.LinesUnknown || commit.Added != 10 || len(commit.Files) != 0 {
		t.Fatalf("unknown path evidence changed during conversion: %+v", commit)
	}
	data, err := json.Marshal(commit)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || fields["pathsUnknown"] != true || fields["linesUnknown"] != false {
		t.Fatalf("unknown path JSON contract missing: %s %v", data, err)
	}
}
