package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

type accessProvider struct {
	*fakeProvider
	rows []gh.DiscoveredRepository
	err  error
}

func (p *accessProvider) DiscoverAccessibleRepositories(context.Context) ([]gh.DiscoveredRepository, error) {
	return p.rows, p.err
}

func TestAccountCacheAndBookmarkIsolation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, id := range []string{"41", "42"} {
		s, err := newService(id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		want := filepath.Join(cacheRoot, "bigboard", "accounts", id, "repos")
		if s.cachePath != want || s.bookmarkKey() != "account:"+id+":all" {
			t.Fatalf("account namespace: %q %q", s.cachePath, s.bookmarkKey())
		}
		s.group = "work"
		if s.bookmarkKey() != "account:"+id+":group:work" {
			t.Fatal("group bookmark crossed accounts")
		}
	}
	for _, id := range []string{"", "0", "../other", "01", "-1"} {
		if _, err := NewServiceForAccount(context.Background(), id, func(context.Context) (string, error) { t.Fatal("invalid account reached token source"); return "", nil }); err == nil {
			t.Fatalf("accepted invalid account %q", id)
		}
	}
}

func TestAccountAccessFiltersSnapshotsSettingsAndIdentities(t *testing.T) {
	s, _, p := testService(t, "cache")
	s.accountID = "41"
	s.authorized = map[string]git.Repository{"example/engine": {ID: "example/engine"}}
	s.analysis.Groups = map[string][]string{"visible": {"example/engine", "other/private"}, "hidden": {"other/private"}}
	s.analysis.WorkAreas = map[string][]stats.WorkAreaRule{"other/private": {{Name: "hidden"}}}
	s.analysis.Identities = map[string]git.Identity{"hidden@example.test": {Name: "Hidden"}}
	s.snapshot.Settings = Settings{Mode: "cache", PersonalAccount: "other", Organizations: []string{"example", "other"}, Repositories: []string{"example/engine", "other/private"}, ExcludedRepositories: []string{"other/private"}}
	s.snapshot.Repositories = []Repository{{ID: "example/engine"}, {ID: "other/private"}}
	s.snapshot.Commits = []Commit{{Repo: "example/engine", Email: "visible@example.test"}, {Repo: "other/private", Email: "hidden@example.test"}}
	s.snapshot.PullRequests = []PR{{Repo: "other/private"}}
	s.snapshot.Status.Errors = []string{"other/private: unavailable"}
	snapshot := s.Snapshot()
	if len(snapshot.Repositories) != 1 || len(snapshot.Commits) != 1 || len(snapshot.PullRequests) != 0 || len(snapshot.Status.Errors) != 0 {
		t.Fatal("unauthorized evidence remained visible")
	}
	if snapshot.Settings.PersonalAccount != "" || len(snapshot.Settings.Organizations) != 1 || len(snapshot.Settings.Repositories) != 1 || len(snapshot.Settings.ExcludedRepositories) != 0 {
		t.Fatal("unauthorized settings remained visible")
	}
	if len(snapshot.Groups) != 1 || len(snapshot.Groups["visible"]) != 1 || len(snapshot.WorkAreas) != 0 {
		t.Fatal("unauthorized groups or paths remained visible")
	}
	s.snapshot = snapshot
	if options := s.IdentityOptions(); len(options.Identities) != 1 || options.Identities[0].Email != "visible@example.test" {
		t.Fatal("unloaded account identity was exposed")
	}
	if p.calls != 0 {
		t.Fatal("reading the snapshot made a network request")
	}
}

func TestAccessFailureRetainsAuthorizationAndRevocationRemovesHistory(t *testing.T) {
	s, cache, p := testService(t, "live")
	s.accountID = "41"
	s.authorized = map[string]git.Repository{"example/engine": {ID: "example/engine"}}
	s.snapshot.Settings.Repositories = []string{"example/engine"}
	s.snapshot.Repositories = []Repository{{ID: "example/engine"}}
	s.snapshot.Commits = []Commit{{Repo: "example/engine", Subject: "retained"}}
	access := &accessProvider{fakeProvider: p, err: errors.New("incomplete access inventory")}
	s.provider = access
	before, err := s.Refresh()
	if err != nil || before.Status.Phase != "partial" || len(before.Commits) != 1 || s.inventoryReady || cache.refreshes != 0 {
		t.Fatalf("failed access treated as successful discovery: %+v %v", before.Status, err)
	}
	access.err = nil
	access.rows = []gh.DiscoveredRepository{{Name: "other/available", DefaultBranch: "main"}}
	after, err := s.Refresh()
	if err != nil || after.Status.Phase != "setup" || len(after.Commits) != 0 || len(after.Repositories) != 0 || cache.refreshes != 0 {
		t.Fatal("revoked selection loaded unrelated repositories")
	}
	if len(after.Settings.Repositories) != 0 {
		t.Fatal("revoked selection was retained")
	}
}

func TestSignedInSelectionCanSpanAccessibleAccounts(t *testing.T) {
	settings := Settings{Mode: "live", Repositories: []string{"one/repo", "two/repo"}}
	if _, err := normalizeSettings(settings); err != nil {
		t.Fatal(err)
	}
	s, _, _ := testService(t, "live")
	s.accountID = "41"
	s.authorized = map[string]git.Repository{"one/repo": {ID: "one/repo"}}
	if _, err := s.SaveSettings(settings); err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatal("saving an unauthorized repository was accepted")
	}
}

func TestAuthenticatedDiscoverAllowsEmptySourceFilters(t *testing.T) {
	s, _, base := testService(t, "live")
	s.accountID = "41"
	s.provider = &accessProvider{fakeProvider: base, rows: []gh.DiscoveredRepository{{Name: "one/repo", DefaultBranch: "main"}, {Name: "two/repo", DefaultBranch: "main"}}}
	rows, err := s.Discover(Settings{Mode: "live"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("first-run discovery failed: %v %v", rows, err)
	}
	if base.calls != 0 {
		t.Fatal("empty filters made source-specific requests")
	}
}

func TestStoredAccessSupportsOfflineCacheAndPersistsAuthoritativeRevocation(t *testing.T) {
	s, cache, base := testService(t, "cache")
	s.accountID = "41"
	s.snapshot.Settings.Repositories = []string{"example/engine"}
	allowed := map[string]git.Repository{"example/engine": {ID: "example/engine", DefaultBranch: "main"}}
	if err := s.replaceAccess(context.Background(), allowed); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(s.cachePath, "access.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cached access is not private")
	}
	s.authorized = nil
	if err := s.restoreAccess(); err != nil {
		t.Fatal(err)
	}
	result, err := s.Refresh()
	if err != nil || len(result.Commits) != 1 || base.calls != 0 || cache.refreshes != 0 {
		t.Fatal("stored account could not read cached history offline")
	}
	access := &accessProvider{fakeProvider: base, err: gh.ErrUnavailable}
	s.provider = access
	if err := s.checkAccess(context.Background()); !errors.Is(err, gh.ErrUnavailable) {
		t.Fatal(err)
	}
	s.authorized = nil
	if err := s.restoreAccess(); err != nil || len(s.authorized) != 1 {
		t.Fatal("transient failure removed verified access")
	}
	access.err = gh.ErrPermission
	if err := s.checkAccess(context.Background()); !errors.Is(err, gh.ErrPermission) {
		t.Fatal(err)
	}
	s.authorized = nil
	if err := s.restoreAccess(); err != nil || len(s.authorized) != 0 {
		t.Fatal("revoked access returned after restart")
	}
	if len(s.Snapshot().Commits) != 0 {
		t.Fatal("authoritative denial retained visible history")
	}
}

func TestCachedAccessRejectsOtherAccountsAndSymlinks(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.accountID = "41"
	if err := os.MkdirAll(s.cachePath, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.cachePath, "access.json")
	data, _ := json.Marshal(cachedAccess{AccountID: "42", Repositories: []string{"example/engine"}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreAccess(); err == nil {
		t.Fatal("another account's access was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreAccess(); err == nil {
		t.Fatal("symlinked access inventory was read")
	}
	if err := s.saveAccess(context.Background(), map[string]git.Repository{}); err == nil {
		t.Fatal("symlinked access inventory was replaced")
	}
}
