package gui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type probeCache struct {
	*fakeCache
	branch, oid string
	probes      int
}

func (c *probeCache) RefreshIfChanged(_ context.Context, _ git.Repository, branch, oid string) error {
	c.probes++
	c.branch, c.oid = branch, oid
	return c.refreshErr
}

type probeProvider struct {
	*fakeProvider
	probes   []gh.RepositoryProbe
	requests [][]gh.RepositoryRequest
}

func (p *probeProvider) ProbeAll(context.Context, []string) []gh.RepositoryProbe { return p.probes }
func (p *probeProvider) FetchAll(ctx context.Context, requests []gh.RepositoryRequest) []gh.Result {
	p.requests = append(p.requests, requests)
	return p.fakeProvider.FetchAll(ctx, requests)
}

func TestLiveRefreshUsesVerifiedProbeAndClearsZeroInventory(t *testing.T) {
	s, cache, provider := testService(t, "live")
	zero := 0
	pc := &probeCache{fakeCache: cache}
	pp := &probeProvider{fakeProvider: provider, probes: []gh.RepositoryProbe{{Repo: "example/engine", DefaultBranch: "develop", HeadOID: "remote-oid", OpenPRCount: &zero, Requests: 1}}}
	s.cache, s.provider = pc, pp
	s.snapshot.PullRequests = []PR{{Repo: "example/engine", Number: 1, Complete: true}}
	snapshot, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if pc.probes != 1 || pc.branch != "develop" || pc.oid != "remote-oid" || cache.refreshes != 0 {
		t.Fatalf("probe refresh not used: %+v", pc)
	}
	if len(pp.requests) != 0 || len(snapshot.PullRequests) != 0 || !snapshot.Repositories[0].PRInventoryComplete {
		t.Fatalf("zero probe did not clear complete inventory: %+v", snapshot)
	}
	if !snapshot.Repositories[0].Fetched || snapshot.Status.ProbeRequests != 1 {
		t.Fatal("verified cache did not retain refresh eligibility/requests")
	}
}

func TestUnknownProbeFallsBackToNormalRefresh(t *testing.T) {
	s, cache, provider := testService(t, "live")
	pc := &probeCache{fakeCache: cache}
	s.cache = pc
	s.provider = &probeProvider{fakeProvider: provider, probes: []gh.RepositoryProbe{{Repo: "example/engine", Err: errors.New("unknown")}}}
	_, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if cache.refreshes != 1 || pc.probes != 0 {
		t.Fatalf("unknown probe skipped normal refresh: %d/%d", cache.refreshes, pc.probes)
	}
}

func TestPRReuseGatePrecedesZeroProbeAndPriorFilesAreReused(t *testing.T) {
	s, _, provider := testService(t, "live")
	now := s.now()
	s.now = func() time.Time { return now }
	one := 1
	pp := &probeProvider{fakeProvider: provider, probes: []gh.RepositoryProbe{{Repo: "example/engine", OpenPRCount: &one}}}
	s.provider = pp
	provider.results = []gh.Result{{Repo: "example/engine", CheckedAt: now, Complete: true, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 1, HeadOID: "head", BaseOID: "base", FileCount: 1, FilesComplete: true, Files: []gh.File{{Path: "main.go"}}}}}}
	first, _ := s.Refresh()
	if len(first.PullRequests) != 1 {
		t.Fatal("missing first PR")
	}
	zero := 0
	pp.probes[0].OpenPRCount = &zero
	now = now.Add(30 * time.Second)
	second, _ := s.Refresh()
	if len(second.PullRequests) != 1 || len(pp.requests) != 1 {
		t.Fatal("zero probe bypassed one-minute PR reuse")
	}
	now = now.Add(time.Minute)
	pp.probes[0].OpenPRCount = &one
	provider.results[0].CheckedAt = now
	_, _ = s.Refresh()
	if len(pp.requests) != 2 || len(pp.requests[1][0].Previous) != 1 || pp.requests[1][0].Previous[0].HeadOID != "head" {
		t.Fatal("raw prior file/revision evidence missing from refresh")
	}
	now = now.Add(time.Minute)
	pp.probes[0].OpenPRCount = &zero
	last, _ := s.Refresh()
	if len(last.PullRequests) != 0 || len(pp.requests) != 2 {
		t.Fatal("admitted zero probe did not clear without inventory call")
	}
}

func TestRediscoveryFailureContinuesTrackedRefresh(t *testing.T) {
	s, cache, provider := testService(t, "live")
	_, _ = s.Refresh()
	before := cache.refreshes
	provider.discoveryErr = errors.New("discovery unavailable")
	snapshot, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if cache.refreshes != before+1 || len(snapshot.Commits) != 1 || snapshot.Status.Phase != "partial" {
		t.Fatalf("discovery failure stopped refresh: %+v", snapshot)
	}
	if len(s.listed) != 1 || !s.inventoryReady {
		t.Fatal("failure discarded last successful inventory")
	}
}

func TestPRCoverageDistinguishesInventoryFilesAndReviewers(t *testing.T) {
	s, _, provider := testService(t, "live")
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 1, FilesComplete: false, RequestedReviewersComplete: true, ReviewsComplete: true, Reviews: []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: s.now()}}}}}}
	snapshot, _ := s.Refresh()
	repo := snapshot.Repositories[0]
	pr := snapshot.PullRequests[0]
	if !repo.PRInventoryComplete || repo.PRFilesComplete || !repo.PRReviewersComplete || !pr.InventoryComplete || pr.FilesComplete || len(pr.Reviews) != 1 {
		t.Fatalf("coverage conflated: %+v %+v", repo, pr)
	}
}

func TestIncompleteInventoryRetainsRawPRsAcrossGroupSwitch(t *testing.T) {
	s, _, provider := testService(t, "live")
	now := s.now()
	s.now = func() time.Time { return now }
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, CheckedAt: now, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 1, Title: "Retained"}}}}
	if _, err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	s.analysis.Groups = map[string][]string{"core": {"example/engine"}}
	now = now.Add(2 * time.Minute)
	provider.results = []gh.Result{{Repo: "example/engine", CheckedAt: now, Err: gh.ErrIncomplete, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 2, Title: "New partial"}}}}
	snapshot, err := s.SelectGroup("core")
	if err != nil || len(snapshot.PullRequests) != 2 {
		t.Fatalf("group switch lost raw retained evidence: %+v %v", snapshot.PullRequests, err)
	}
	for _, pr := range snapshot.PullRequests {
		if pr.InventoryComplete || pr.Complete {
			t.Fatal("retained partial evidence claims completeness")
		}
	}
	now = now.Add(20 * time.Second)
	snapshot, err = s.Refresh()
	if err != nil || len(snapshot.PullRequests) != 2 {
		t.Fatal("reuse lost retained partial evidence")
	}
	now = now.Add(time.Minute)
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, CheckedAt: now}}
	snapshot, err = s.Refresh()
	if err != nil || len(snapshot.PullRequests) != 0 {
		t.Fatal("complete empty inventory did not clear retained PRs")
	}
}

func TestErroredZeroProbeDoesNotClearPRs(t *testing.T) {
	s, _, provider := testService(t, "live")
	zero := 0
	pp := &probeProvider{fakeProvider: provider, probes: []gh.RepositoryProbe{{Repo: "example/engine", OpenPRCount: &zero, Err: gh.ErrIncomplete}}}
	s.provider = pp
	provider.results = []gh.Result{{Repo: "example/engine", Complete: true, PRs: []gh.PullRequest{{Repo: "example/engine", Number: 1}}}}
	result, err := s.Refresh()
	if err != nil || len(result.PullRequests) != 1 || len(pp.requests) != 1 {
		t.Fatal("errored probe was treated as a verified empty inventory")
	}
}

type changingDiscovery struct {
	*fakeProvider
	repositories []gh.DiscoveredRepository
}

func (p *changingDiscovery) DiscoverRepositories(context.Context, []string) ([]gh.DiscoveredRepository, error) {
	return p.repositories, nil
}

func TestSavedExclusionsAllowNewRepositoriesOnRefresh(t *testing.T) {
	s, _, provider := testService(t, "live")
	p := &changingDiscovery{fakeProvider: provider, repositories: []gh.DiscoveredRepository{{Name: "example/engine", DefaultBranch: "main"}, {Name: "example/skipped", DefaultBranch: "main"}}}
	s.provider = p
	settings := Settings{Mode: "live", Organizations: []string{"example"}, Repositories: []string{"example/engine"}, ExcludedRepositories: []string{"example/skipped"}}
	result, err := s.SaveSettings(settings)
	if err != nil || len(result.Repositories) != 1 {
		t.Fatalf("initial selection: %+v %v", result.Repositories, err)
	}
	p.repositories = append(p.repositories, gh.DiscoveredRepository{Name: "example/new", DefaultBranch: "main"})
	result, err = s.Refresh()
	if err != nil || len(result.Repositories) != 2 {
		t.Fatalf("new repository suppressed by saved settings: %+v %v", result.Repositories, err)
	}
	for _, repo := range result.Repositories {
		if repo.ID == "example/skipped" {
			t.Fatal("excluded repository reappeared")
		}
	}
	choices, err := s.Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range choices {
		if repo.Included != (repo.ID != "example/skipped") {
			t.Fatalf("wrong discovery selection: %+v", repo)
		}
	}
	if _, err := s.SaveSettings(Settings{Mode: "live", Organizations: []string{"example"}, Repositories: []string{}, ExcludedRepositories: []string{"example/engine"}}); err == nil {
		t.Fatal("all-unchecked settings accepted")
	}
}

func TestLegacySettingsDiscoveryIgnoresGroupAndSessionScope(t *testing.T) {
	s, _, provider := testService(t, "live")
	s.provider = &changingDiscovery{fakeProvider: provider, repositories: []gh.DiscoveredRepository{
		{Name: "example/engine", DefaultBranch: "main"},
		{Name: "example/hidden", DefaultBranch: "main"},
		{Name: "example/new", DefaultBranch: "main"},
		{Name: "example/excluded", DefaultBranch: "main"},
	}}
	s.analysis.Groups = map[string][]string{"core": {"example/engine"}}
	s.analysis.Exclude = []string{"excluded"}
	s.group = "core"
	s.sessionExcluded = map[string]bool{"example/hidden": true}
	settings := s.Snapshot().Settings
	choices, err := s.Discover(settings)
	if err != nil || len(choices) != 4 {
		t.Fatalf("legacy discovery: %+v %v", choices, err)
	}
	settings.ExcludedRepositories = []string{}
	for _, repo := range choices {
		if repo.Included != (repo.ID != "example/excluded") {
			t.Fatalf("temporary scope replaced saved selection: %+v", repo)
		}
		if repo.Included {
			settings.Repositories = append(settings.Repositories, repo.ID)
		} else {
			settings.ExcludedRepositories = append(settings.ExcludedRepositories, repo.ID)
		}
	}
	if _, err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	result, err := s.SelectGroup("")
	if err != nil || len(result.Repositories) != 3 {
		t.Fatalf("saved workspace lost repositories outside the old scope: %+v %v", result.Repositories, err)
	}
	for _, repo := range result.Repositories {
		if repo.ID == "example/excluded" {
			t.Fatal("legacy exclusion was lost")
		}
	}
}
