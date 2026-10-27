package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type probePRProvider struct {
	fakePRProvider
	probes    int
	requested []string
	results   []gh.RepositoryProbe
}

func (p *probePRProvider) ProbeAll(_ context.Context, repositories []string) []gh.RepositoryProbe {
	p.probes++
	p.requested = append([]string(nil), repositories...)
	return p.results
}

func probeFixture(t *testing.T, count *int) (Model, *startupFixture, *probePRProvider, *int) {
	t.Helper()
	m, f := newStartupFixture(t, "org/repo")
	f.revisions["org/repo"] = strings.Repeat("a", 40)
	p := &probePRProvider{results: []gh.RepositoryProbe{{Repo: "org/repo", DefaultBranch: "main", HeadOID: f.revisions["org/repo"], OpenPRCount: count}}}
	p.result = gh.Result{Repo: "org/repo", Complete: true, PRs: []gh.PullRequest{{Repo: "org/repo", Number: 7, CheckState: "SUCCESS"}}}
	m.prProvider = p
	prepares := new(int)
	m.options.PrepareIfChanged = func(_ context.Context, repo git.Repository, branch, oid string) error {
		*prepares++
		if repo.DefaultBranch != branch || oid != f.revisions[repo.ID] {
			t.Fatal("probe metadata did not reach owned cache")
		}
		return nil
	}
	return m, f, p, prepares
}

func TestRemoteProbePublishesOfflineFirstAndClearsOnlyProvenEmpty(t *testing.T) {
	zero := 0
	m, f, p, prepares := probeFixture(t, &zero)
	m.prState.snapshots = map[string]prSnapshot{"org/repo": {prs: []gh.PullRequest{{Repo: "org/repo", Number: 7}}}}
	m, cmd := finishStartupPhase(t, m, m.Init())
	if p.probes != 0 || f.fetches["org/repo"] != 0 || len(m.local.records) != 1 {
		t.Fatal("probe preceded offline publication")
	}
	m = runRefreshCommands(t, m, cmd)
	if p.probes != 1 || p.fetches != 0 || *prepares != 1 || f.scans["org/repo"] != 1 || len(m.allPRs()) != 0 || m.prState.snapshots["org/repo"].lastGood.IsZero() {
		t.Fatalf("zero proof not applied: probes=%d fetches=%d prepares=%d scans=%v state=%+v", p.probes, p.fetches, *prepares, f.scans, m.prState)
	}
	checked := m.prState.snapshots["org/repo"].checked
	if !checked.Equal(m.remoteProbe.checked) {
		t.Fatal("zero inventory lost its probe timestamp")
	}
	m.prState.nextRefresh = time.Time{}
	p.results[0].OpenPRCount = nil
	p.results[0].Err = gh.ErrResponse
	p.result = gh.Result{Repo: "org/repo", Err: gh.ErrRateLimited}
	m.prState.snapshots["org/repo"] = prSnapshot{prs: []gh.PullRequest{{Repo: "org/repo", Number: 7}}, lastGood: checked}
	cmd = m.startLocalRefresh()
	m = runRefreshCommands(t, m, cmd)
	if p.fetches != 1 || f.fetches["org/repo"] != 1 || f.scans["org/repo"] != 2 || len(m.allPRs()) != 1 || !m.prState.snapshots["org/repo"].lastGood.Equal(checked) {
		t.Fatal("unknown probe skipped fallback, scanning, or stale retention")
	}
}

func TestRemoteProbePositiveRefreshesChecksAndManualScan(t *testing.T) {
	one := 1
	m, f, p, prepares := probeFixture(t, &one)
	m = runRefreshCommands(t, m, m.Init())
	m.prState.nextRefresh = time.Time{}
	p.result.PRs[0].CheckState = "FAILURE"
	p.results[0].DefaultBranch = "trunk"
	cmd := m.startLocalRefresh()
	m = runRefreshCommands(t, m, cmd)
	if p.probes != 2 || p.fetches != 2 || *prepares != 2 || f.scans["org/repo"] != 2 || f.fetches["org/repo"] != 0 || m.allPRs()[0].CheckState != "FAILURE" || m.local.repositories[0].DefaultBranch != "trunk" {
		t.Fatalf("positive inventory or scan bypassed: probes=%d fetches=%d prepares=%d scans=%v", p.probes, p.fetches, *prepares, f.scans)
	}
}

func TestRemoteProbeObsoleteGenerationIsIgnored(t *testing.T) {
	zero := 0
	m, _, _, _ := probeFixture(t, &zero)
	m, cmd := finishStartupPhase(t, m, m.Init())
	msg := cmd().(repositoriesProbedMsg)
	m.resetPending()
	m, followup := applyLocal(t, m, msg)
	if followup != nil || m.remoteProbe != nil || len(m.prState.snapshots) != 0 {
		t.Fatal("obsolete probe published data")
	}
}

func TestRemoteProbeDoesNotCancelActivePRRefresh(t *testing.T) {
	one := 1
	m, _, _, _ := probeFixture(t, &one)
	m = runRefreshCommands(t, m, m.Init())
	m.prState.nextRefresh = time.Time{}
	command := m.startPRRefreshWithProbe(m.remoteProbe)
	generation := m.prGeneration
	m.resetPending()
	if m.remoteProbe != nil {
		t.Fatal("probe survived local generation")
	}
	msg := command().(prsLoadedMsg)
	if msg.loginErr != nil || len(msg.results) != 1 || msg.generation != generation {
		t.Fatalf("local reset canceled independent PRs: %+v", msg)
	}
}

func TestRemoteProbeCanonicalConflictDoesNotClearPositiveEvidence(t *testing.T) {
	zero, one := 0, 1
	m, _, p, prepares := probeFixture(t, &zero)
	m = runRefreshCommands(t, m, m.Init())
	m.prState.nextRefresh = time.Time{}
	state := &remoteProbeState{context: context.Background(), checked: time.Now(), results: map[string]gh.RepositoryProbe{
		"org/repo":  {Repo: "org/new", OpenPRCount: &zero, DefaultBranch: "main", HeadOID: strings.Repeat("a", 40)},
		"org/alias": {Repo: "org/new", OpenPRCount: &one},
	}}
	before := *prepares
	m.remoteProbe = state
	options := m.probedOptions(m.repositories[0])
	if err := options.PrepareRepository(context.Background(), m.repositories[0]); err != nil {
		t.Fatal(err)
	}
	if *prepares != before {
		t.Fatal("canonical rename skipped owned cache validation")
	}
	p.result = gh.Result{Repo: "org/new", Complete: true, PRs: []gh.PullRequest{{Repo: "org/new", Number: 9}}}
	m.applyPRResult(m.startPRRefreshWithProbe(state)().(prsLoadedMsg))
	if p.fetches != 1 || len(m.allPRs()) != 1 || m.prState.repositories["org/repo"] != "org/new" {
		t.Fatal("conflicting zero erased positive canonical inventory")
	}
}

type probeBudgetRunner struct {
	calls   int
	limited bool
}

func (r *probeBudgetRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls++
	query := strings.Join(args, " ")
	if strings.Contains(query, "BigBoardRepositoryProbe") {
		if r.limited {
			return []byte("HTTP/2 429\r\nRetry-After: 600\r\n\r\n"), gh.ErrRateLimited
		}
		return []byte(`{"data":{"repo0":{"nameWithOwner":"org/repo","defaultBranchRef":null,"pullRequests":{"totalCount":1}},"rateLimit":{"remaining":100}}}`), nil
	}
	if strings.Contains(query, "BigBoardCurrentAccount") {
		return []byte(`{"data":{"viewer":{"login":"viewer"}}}`), nil
	}
	return nil, errors.New("unexpected inventory")
}

func TestRemoteProbeUsesSharedBudgetAndCooldown(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{true: "cooldown", false: "budget"}[limited], func(t *testing.T) {
			one := 1
			m, f, _, _ := probeFixture(t, &one)
			runner := &probeBudgetRunner{limited: limited}
			m.prProvider = gh.NewProvider(runner)
			m.options.PRRequestBudget = 2
			old := time.Now().Add(-time.Hour)
			m.prState.snapshots = map[string]prSnapshot{"org/repo": {prs: []gh.PullRequest{{Repo: "org/repo", Number: 7}}, lastGood: old}}
			m = runRefreshCommands(t, m, m.Init())
			expected := 2
			if limited {
				expected = 1
			}
			if runner.calls != expected || m.prState.probeRequests != 1 || len(m.allPRs()) != 1 || !m.prState.snapshots["org/repo"].lastGood.Equal(old) || f.fetches["org/repo"] != 1 {
				t.Fatalf("budget/cooldown escaped or stale evidence lost: calls=%d state=%+v", runner.calls, m.prState)
			}
		})
	}
}

func TestRemoteProbeCanonicalRenameFailureRetainsExistingPRs(t *testing.T) {
	one := 1
	m, _, p, _ := probeFixture(t, &one)
	m = runRefreshCommands(t, m, m.Init())
	m.prState.nextRefresh = time.Time{}
	checked := m.prState.snapshots["org/repo"].lastGood
	p.results[0].Repo = "org/renamed"
	p.result = gh.Result{Repo: "org/repo", Err: gh.ErrRateLimited}
	command := m.startLocalRefresh()
	m = runRefreshCommands(t, m, command)
	if len(m.allPRs()) != 1 || m.prState.repositories["org/repo"] != "org/repo" || !m.prState.snapshots["org/repo"].lastGood.Equal(checked) {
		t.Fatal("probe rename hid retained PRs after inventory failure")
	}
}

func TestRemoteProbeEmptyQuotaRetainsResetTime(t *testing.T) {
	zero := 0
	m, _, p, _ := probeFixture(t, &zero)
	retry := time.Now().Add(time.Hour)
	p.results[0].RetryAt = retry
	p.loginErr = gh.ErrRateLimited
	m = runRefreshCommands(t, m, m.Init())
	if !m.prState.nextRefresh.Equal(retry) || p.fetches != 0 || m.prState.snapshots["org/repo"].partial {
		t.Fatalf("empty quota reset lost: %+v", m.prState)
	}
}
