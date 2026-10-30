package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func TestStartupPreferencesAndStrictConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		valid       bool
		since, sort string
	}{
		{"default", `{}`, true, "14d", "total"},
		{"configured", `{"since":"30d","sort":"commits"}`, true, "30d", "commits"},
		{"alias", `{"since":"ALL","sort":"impact"}`, true, "all", "total"},
		{"range", `{"since":"15d"}`, false, "", ""},
		{"sort", `{"sort":"surprise"}`, false, "", ""},
		{"legacy", `{"paths":["/tmp"]}`, false, "", ""},
		{"unknown", `{"typo":true}`, false, "", ""},
		{"budget", `{"pr_request_budget":0}`, false, "", ""},
		{"group", `{"organizations":["example"],"groups":{"other":["outside/too/many"]}}`, false, "", ""},
		{"areas", `{"work_areas":{"example/engine":[{"name":"Core","paths":["../escape"]}]}}`, false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "config.json"), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, analysis, err := loadSettings(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
			if tc.valid && (analysis.Since != tc.since || analysis.Sort != tc.sort) {
				t.Fatalf("preferences: %+v", analysis)
			}
		})
	}
}

func TestSnapshotMetadataCannotMutateGroupsOrWorkAreas(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.analysis.Groups = map[string][]string{"core": {"example/engine"}}
	s.analysis.WorkAreas = map[string][]stats.WorkAreaRule{"example/engine": {{Name: "Core", Paths: []string{"src"}}}}
	result := s.Snapshot()
	if result.Preferences.Since != "14d" || result.Preferences.Sort != "total" {
		t.Fatalf("wrong defaults: %+v", result.Preferences)
	}
	result.Groups["core"][0] = "changed"
	result.WorkAreas["example/engine"][0].Paths[0] = "changed"
	again := s.Snapshot()
	if again.Groups["core"][0] != "example/engine" || again.WorkAreas["example/engine"][0].Paths[0] != "src" {
		t.Fatal("metadata mutation leaked")
	}
}

type mappingCache struct {
	*fakeCache
	options git.CollectOptions
}

func (c *mappingCache) ScanRepository(ctx context.Context, repo git.Repository, options git.CollectOptions) ([]git.CommitRecord, error) {
	c.options = options
	records, err := c.fakeCache.ScanRepository(ctx, repo, options)
	records = slices.Clone(records)
	for i := range records {
		for primary, identity := range options.Identities {
			if records[i].Email == primary || slices.Contains(identity.Aliases, records[i].Email) {
				records[i].Email = primary
				if identity.Name != "" {
					records[i].Author = identity.Name
				}
			}
		}
	}
	return records, err
}

func TestIdentitySaveRescansOfflinePreservesPRsAndConfigFields(t *testing.T) {
	s, base, provider := testService(t, "live")
	cache := &mappingCache{fakeCache: base}
	s.cache = cache
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 3, Title: "Change", ContextComplete: true}}}}
	first, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(s.path), "config.json")
	original := []byte(`{"organizations":["example"],"since":"14d","customFutureSetting":{"keep":true}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s.analysis.source = original
	base.repositories = append(base.repositories, git.Repository{ID: "example/new", Name: "example/new", DefaultBranch: "main"})
	beforeCalls, beforeFetch := provider.calls, cache.refreshes
	result, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Emails: []string{"person@example.test"}, Name: "Renamed Person"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != beforeCalls || cache.refreshes != beforeFetch {
		t.Fatal("identity save performed remote work")
	}
	if len(result.Repositories) != len(first.Repositories) || result.Repositories[0].ID != first.Repositories[0].ID {
		t.Fatal("identity rescan widened the tracked repository scope")
	}
	if result.Commits[0].Author != "Renamed Person" || cache.options.Identities["person@example.test"].Name != "Renamed Person" {
		t.Fatal("identity mapping did not reach rescan")
	}
	if !reflect.DeepEqual(result.PullRequests, first.PullRequests) {
		t.Fatal("identity rescan discarded PR evidence")
	}
	data, _ := os.ReadFile(path)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["customFutureSetting"] == nil || fields["organizations"] == nil || fields["identities"] == nil {
		t.Fatal("identity save lost unrelated fields")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Name: "Again"}); err == nil {
		t.Fatal("external edit overwritten")
	}
}

func TestIdentityMergeUsesCanonicalEmailAndPreservesExistingAliases(t *testing.T) {
	s, base, _ := testService(t, "cache")
	s.cache = &mappingCache{fakeCache: base}
	s.analysis.Identities = map[string]git.Identity{"other@example.test": {Aliases: []string{"alias@example.test"}}}
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Emails: []string{"person@example.test", "other@example.test"}, Name: "Merged Person"}); err != nil {
		t.Fatal(err)
	}
	identity := s.analysis.Identities["person@example.test"]
	if !slices.Contains(identity.Aliases, "alias@example.test") || !slices.Contains(identity.Aliases, "other@example.test") {
		t.Fatalf("aliases lost: %+v", identity)
	}
	if _, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "not-an-email", Name: "Invalid"}); err == nil {
		t.Fatal("invalid identity accepted")
	}
}

func TestSessionRepositoryControlsDoNotPersistAndCanReinclude(t *testing.T) {
	s, cache, provider := testService(t, "cache")
	cache.repositories = append(cache.repositories, git.Repository{ID: "outside/other", Name: "outside/other", DefaultBranch: "main"})
	checkControls := func(included bool) {
		t.Helper()
		controls, err := s.RepositoryControls()
		if err != nil || len(controls.Repositories) != 2 {
			t.Fatalf("cached controls: %+v %v", controls, err)
		}
		for _, repo := range controls.Repositories {
			if repo.Listed || repo.CanRemove {
				t.Fatalf("offline controls claimed live discovery: %+v", repo)
			}
			if repo.Selectable != (repo.ID == "example/engine") || repo.Included != (included && repo.ID == "example/engine") {
				t.Fatalf("cached tracking availability: %+v", repo)
			}
		}
	}
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	checkControls(true)
	if _, err := s.RemoveCachedRepository("example/engine"); err == nil {
		t.Fatal("offline selection made a cache removable")
	}
	result, err := s.ApplyRepositoryControls(RepositorySelection{Included: []string{}})
	if err != nil || len(result.Repositories) != 0 {
		t.Fatalf("exclusion failed: %+v %v", result, err)
	}
	checkControls(false)
	result, err = s.ApplyRepositoryControls(RepositorySelection{Included: []string{"example/engine"}})
	if err != nil || len(result.Repositories) != 1 {
		t.Fatalf("reinclusion failed: %+v %v", result, err)
	}
	checkControls(true)
	if provider.calls != 0 || cache.refreshes != 0 {
		t.Fatal("cached tracking attempted network access")
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("session selection persisted settings")
	}
}

type managedCache struct {
	*fakeCache
	removed []string
}

func (c *managedCache) Info([]git.Repository) (git.CacheInfo, error) {
	return git.CacheInfo{Bytes: 1234}, nil
}
func (c *managedCache) Remove(id string) error { c.removed = append(c.removed, id); return nil }

func TestCacheDeletionRequiresUnlistedOwnedEntryAfterDiscovery(t *testing.T) {
	s, base, _ := testService(t, "cache")
	cache := &managedCache{fakeCache: base}
	s.cache = cache
	if _, err := s.RemoveCachedRepository("example/engine"); err == nil {
		t.Fatal("cache deleted without discovery")
	}
	s.inventoryReady = true
	s.listed = slices.Clone(base.repositories)
	s.sessionExcluded = map[string]bool{"example/engine": true}
	if _, err := s.RemoveCachedRepository("example/engine"); err == nil {
		t.Fatal("excluded listed cache was removable")
	}
	s.listed = nil
	controls, err := s.RepositoryControls()
	if err != nil || controls.CacheBytes != 1234 || !controls.Repositories[0].CanRemove {
		t.Fatalf("unlisted cache not exposed: %+v %v", controls, err)
	}
	if _, err := s.RemoveCachedRepository("example/engine"); err != nil || len(cache.removed) != 1 {
		t.Fatalf("unlisted removal failed: %v", err)
	}
}

func TestBookmarksAreImmutablePerGroupAndAdvanceOnlyAfterFetchedHistory(t *testing.T) {
	s, _, _ := testService(t, "cache")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s.loadBookmarks()
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBookmark(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.bookmarkPath); !os.IsNotExist(err) {
		t.Fatal("offline-only session advanced bookmark")
	}
	seen := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := saveBookmark(s.bookmarkPath, "all", seen); err != nil {
		t.Fatal(err)
	}
	s.loadBookmarks()
	s.snapshot.Settings.Mode = "live"
	s.snapshot.Mode = "live"
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Bookmark.SeenAt != timestamp(seen) || s.Snapshot().Bookmark.EligibleAt == "" {
		t.Fatal("bookmark baseline or eligibility wrong")
	}
	if err := s.SaveBookmark(); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Bookmark.SeenAt != timestamp(seen) {
		t.Fatal("saved bookmark changed session baseline")
	}
	stored, err := readBookmarks(s.bookmarkPath)
	if err != nil || !stored["all"].After(seen) {
		t.Fatalf("bookmark not advanced: %v %v", stored, err)
	}
	s.analysis.Groups = map[string][]string{"core": {"example/engine"}}
	if _, err := s.SelectGroup("core"); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().ActiveGroup != "core" || s.Snapshot().Bookmark.SeenAt != "" {
		t.Fatal("group reused all-repository baseline")
	}
	if _, err := s.SelectGroup("missing"); err == nil {
		t.Fatal("unknown group accepted")
	}
}

func TestBookmarkRefusesMalformedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-seen.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveBookmark(path, "all", time.Now()); err == nil {
		t.Fatal("malformed bookmark overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("bookmark changed")
	}
}

func TestOpenPathRejectsMovingOrUnknownRevision(t *testing.T) {
	s, _, _ := testService(t, "cache")
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenPath(PathRequest{Repo: "example/engine", Path: "src/main.go", Revision: "main"}); err == nil {
		t.Fatal("moving branch accepted")
	}
	if _, err := s.OpenPath(PathRequest{Repo: "other/engine"}); err == nil {
		t.Fatal("unknown repository accepted")
	}
	address, err := s.OpenPath(PathRequest{Repo: "example/engine"})
	if err != nil || address != "https://github.com/example/engine" {
		t.Fatalf("repository URL: %s %v", address, err)
	}
}

func TestIdentityRescanRetainsStaleGitState(t *testing.T) {
	s, cache, _ := testService(t, "cache")
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	cache.scanErr = errors.New("lost object")
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	cache.scanErr = nil
	result, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Name: "Name"})
	if err != nil || !result.Repositories[0].Stale || result.Status.Phase != "partial" {
		t.Fatalf("offline rescan cleared stale evidence: %+v %v", result, err)
	}
}
