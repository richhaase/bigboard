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
	rows := append([]gh.DiscoveredRepository(nil), p.rows...)
	for i := range rows {
		if rows[i].OwnerType == "" {
			rows[i].OwnerType = "Organization"
		}
	}
	return rows, p.err
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
	status := s.SnapshotStatus()
	if status.Commits != 1 || status.Repositories != 1 || status.PullRequests != 0 {
		t.Fatal("lightweight status exposed unauthorized counts")
	}
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

type deniedAccessProvider struct {
	*accessProvider
	probeErr error
}

func (p *deniedAccessProvider) ProbeAll(context.Context, []string) []gh.RepositoryProbe {
	return []gh.RepositoryProbe{{Err: p.probeErr}}
}

func TestAuthoritativeDenialDuringRefreshRemovesPersistedAccess(t *testing.T) {
	for _, phase := range []string{"probe", "inventory", "git", "not-found"} {
		t.Run(phase, func(t *testing.T) {
			s, cache, base := testService(t, "live")
			s.accountID = "41"
			s.snapshot.Settings.Repositories = []string{"example/engine"}
			provider := &deniedAccessProvider{accessProvider: &accessProvider{fakeProvider: base, rows: []gh.DiscoveredRepository{{Name: "example/engine", DefaultBranch: "main"}}}}
			switch phase {
			case "probe":
				provider.probeErr = gh.ErrPermission
			case "not-found":
				provider.probeErr = gh.ErrRepository
			case "git":
				cache.refreshErr = gh.ErrAuthentication
				cache.started = make(chan struct{})
				s.snapshot.GeneratedAt = "2026-10-28T00:00:00Z"
			default:
				base.results = []gh.Result{{Repo: "example/engine", Err: gh.ErrPermission}}
			}
			s.provider = provider
			result, err := s.Refresh()
			if err != nil || len(result.Commits) != 0 || len(result.Repositories) != 0 || len(result.PullRequests) != 0 || result.Status.Phase != "partial" {
				t.Fatal("authoritative denial retained evidence")
			}
			if phase == "probe" && cache.refreshes != 0 {
				t.Fatal("denied repository reached Git refresh")
			}
			if phase == "git" {
				select {
				case <-cache.started:
					t.Fatal("denied Git repository was scanned")
				default:
				}
			}
			s.authorized = nil
			if err := s.restoreAccess(); err != nil || len(s.authorized) != 0 {
				t.Fatal("denied repository access returned on restart")
			}
		})
	}
}

func TestTransientTokenRefreshFailuresRetainCachedEvidenceAndGrants(t *testing.T) {
	for _, failure := range []error{gh.ErrUnavailable, gh.ErrRateLimited} {
		for _, phase := range []string{"access", "git"} {
			t.Run(phase+failure.Error(), func(t *testing.T) {
				s, cache, base := testService(t, "cache")
				s.accountID = "41"
				s.snapshot.Settings.Repositories = []string{"example/engine"}
				if err := s.replaceAccess(context.Background(), map[string]git.Repository{"example/engine": {ID: "example/engine", DefaultBranch: "main"}}); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Refresh(); err != nil {
					t.Fatal(err)
				}
				s.snapshot.Mode, s.snapshot.Settings.Mode = "live", "live"
				provider := &accessProvider{fakeProvider: base, rows: []gh.DiscoveredRepository{{Name: "example/engine", DefaultBranch: "main"}}}
				if phase == "access" {
					provider.err = failure
				} else {
					cache.refreshErr = failure
				}
				s.provider = provider
				result, err := s.Refresh()
				if err != nil || len(result.Commits) != 1 || !result.Repositories[0].Stale || result.Status.Phase != "partial" {
					t.Fatal("transient token refresh failure discarded cached evidence")
				}
				s.authorized = nil
				if err := s.restoreAccess(); err != nil || len(s.authorized) != 1 {
					t.Fatal("transient token refresh failure removed saved grants")
				}
			})
		}
	}
}

func TestFailedRevocationWriteCannotRestorePreviousGrant(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.accountID = "41"
	allowed := map[string]git.Repository{"example/engine": {ID: "example/engine"}}
	if err := s.replaceAccess(t.Context(), allowed); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.cachePath, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.cachePath, 0700) })
	if err := s.replaceAccess(t.Context(), map[string]git.Repository{}); err == nil {
		t.Fatal("expected atomic replacement to fail in read-only directory")
	}
	if err := s.restoreAccess(); err != nil {
		t.Fatal(err)
	}
	if len(s.authorized) != 0 {
		t.Fatal("old grant returned after failed revocation save")
	}
}

func TestInstallationDenialRetainsUnrelatedGrants(t *testing.T) {
	s, _, base := testService(t, "cache")
	s.accountID = "41"
	if err := s.replaceAccess(t.Context(), map[string]git.Repository{"one/private": {ID: "one/private"}, "two/public": {ID: "two/public"}}); err != nil {
		t.Fatal(err)
	}
	s.provider = &accessProvider{fakeProvider: base, err: &gh.InstallationAccessError{Owner: "one", Err: gh.ErrRepository}}
	if err := s.checkAccess(t.Context()); !errors.Is(err, gh.ErrRepository) {
		t.Fatal(err)
	}
	if err := s.restoreAccess(); err != nil {
		t.Fatal(err)
	}
	if len(s.authorized) != 1 || s.authorized["two/public"].ID == "" {
		t.Fatal("scoped denial cleared unrelated access or kept revoked access")
	}
}

func TestFilteredDiscoveryReusesVerifiedInventory(t *testing.T) {
	s, _, base := testService(t, "live")
	s.accountID = "41"
	s.provider = &accessProvider{fakeProvider: base, rows: []gh.DiscoveredRepository{{OwnerType: "Organization", Name: "example/engine"}, {OwnerType: "User", Name: "viewer/personal"}}}
	rows, err := s.Discover(Settings{Mode: "live", Organizations: []string{"example"}})
	if err != nil || len(rows) != 1 || base.calls != 0 {
		t.Fatalf("organization discovery repeated requests: %v %v calls=%d", rows, err, base.calls)
	}
	rows, err = s.Discover(Settings{Mode: "live", PersonalAccount: "viewer"})
	if err != nil || len(rows) != 1 || base.calls != 1 {
		t.Fatalf("personal discovery did more than verify login: %v %v calls=%d", rows, err, base.calls)
	}
	if _, err = s.Discover(Settings{Mode: "live", PersonalAccount: "example"}); err == nil {
		t.Fatal("accepted wrong personal account")
	}
}

func TestAuthorizedSnapshotOwnsWorkAreaRules(t *testing.T) {
	s, _, _ := testService(t, "cache")
	s.accountID = "41"
	s.authorized = map[string]git.Repository{"example/engine": {ID: "example/engine"}}
	s.analysis.WorkAreas = map[string][]stats.WorkAreaRule{"example/engine": {{Name: "core", Paths: []string{"src"}}}}
	snapshot := s.Snapshot()
	snapshot.WorkAreas["example/engine"][0].Name = "changed"
	snapshot.WorkAreas["example/engine"][0].Paths[0] = "elsewhere"
	if got := s.Snapshot().WorkAreas["example/engine"][0]; got.Name != "core" || got.Paths[0] != "src" {
		t.Fatalf("snapshot exposed shared work areas: %+v", got)
	}
}
