package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/richhaase/bigboard/github"
)

type batchPRFixture struct {
	fakePRProvider
	requests []gh.RepositoryRequest
	results  []gh.Result
	batches  int
}

func (p *batchPRFixture) FetchAll(_ context.Context, requests []gh.RepositoryRequest) []gh.Result {
	p.requests = requests
	p.batches++
	return p.results
}

func TestPRRefreshUsesBatchWithPriorEvidenceAndConfiguredBudget(t *testing.T) {
	m := prFixture()
	p := &batchPRFixture{results: []gh.Result{{Repo: "org/repo", Complete: true, InventoryRequests: 2, FileRequests: 3}}}
	m.prProvider = p
	m.options.PRRequestBudget = 256
	cmd := m.startPRRefresh()
	msg := cmd().(prsLoadedMsg)
	if p.batches != 1 || p.fetches != 0 || len(p.requests) != 1 || p.requests[0].Repo != "org/repo" || len(p.requests[0].Previous) != 1 {
		t.Fatalf("batch did not receive deduplicated prior inventory: %+v", p)
	}
	if msg.inventoryRequests != 2 || msg.fileRequests != 3 || msg.requestBudget != 256 {
		t.Fatalf("refresh diagnostics lost: %+v", msg)
	}
	m.applyPRResult(msg)
	if m.prState.inventoryRequests != 2 || m.prState.fileRequests != 3 || m.prState.requestBudget != 256 {
		t.Fatal("request totals were not published")
	}
	m.prState.nextRefresh = time.Time{}
	p.results[0].InventoryRequests, p.results[0].FileRequests = 1, 0
	m.applyPRResult(m.startPRRefresh()().(prsLoadedMsg))
	if m.prState.inventoryRequests != 1 || m.prState.fileRequests != 0 {
		t.Fatal("request totals accumulated across refreshes")
	}
}

func TestPRFileFailureDoesNotMarkFreshInventoryStale(t *testing.T) {
	m := prFixture()
	incoming := m.allPRs()[0]
	incoming.Number = 8
	incoming.Files = []gh.File{{Path: "fresh.go"}}
	incoming.FilesComplete, incoming.ContextComplete = false, false
	now := time.Now()
	m.applyPRResult(prsLoadedMsg{
		checked: now,
		results: map[string]gh.Result{"org/repo": {
			Repo: "org/repo", Complete: true, PRs: []gh.PullRequest{incoming},
			Err: gh.ErrBudgetExceeded, FilesErr: gh.ErrBudgetExceeded,
		}},
	})
	snapshot := m.prState.snapshots["org/repo"]
	if snapshot.partial || snapshot.err != nil || !snapshot.lastGood.Equal(now) || !errors.Is(snapshot.filesErr, gh.ErrBudgetExceeded) {
		t.Fatalf("fresh inventory was marked stale by missing files: %+v", snapshot)
	}
	if len(snapshot.prs) != 1 || snapshot.prs[0].Number != 8 || snapshot.prs[0].FilesComplete || len(snapshot.prs[0].Files) != 1 {
		t.Fatal("closed PR or obsolete paths survived a current inventory")
	}
}

func TestPRLegacyProviderFailureStillRetainsStaleInventory(t *testing.T) {
	m := prFixture()
	m.applyPRResult(prsLoadedMsg{checked: time.Now(), results: map[string]gh.Result{
		"org/repo": {Repo: "org/repo", Err: gh.ErrAuthentication},
	}})
	snapshot := m.prState.snapshots["org/repo"]
	if !snapshot.partial || !errors.Is(snapshot.err, gh.ErrAuthentication) || len(snapshot.prs) != 1 {
		t.Fatalf("legacy provider failure lost retained inventory or its warning: %+v", snapshot)
	}
}

func TestPRInventoryRefreshDoesNotReusePathsWithoutVerifiedVersion(t *testing.T) {
	m := prFixture()
	incoming := m.allPRs()[0]
	incoming.UpdatedAt = incoming.UpdatedAt.Add(time.Minute)
	incoming.Files = []gh.File{{Path: "replacement/new.go"}}
	incoming.FilesComplete, incoming.ContextComplete = false, false
	m.applyPRResult(prsLoadedMsg{checked: time.Now(), results: map[string]gh.Result{
		"org/repo": {Repo: "org/repo", Complete: true, PRs: []gh.PullRequest{incoming}},
	}})
	got := m.allPRs()[0]
	if len(got.Files) != 1 || got.Files[0].Path != "replacement/new.go" || got.FilesComplete {
		t.Fatalf("old paths leaked into a changed PR: %+v", got)
	}
}
