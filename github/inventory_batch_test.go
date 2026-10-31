package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func batchInventory(t *testing.T, repositories map[string]*rawRepository, problems ...map[string]any) []byte {
	t.Helper()
	data := map[string]any{"rateLimit": map[string]int{"remaining": 100}}
	for alias, repository := range repositories {
		data[alias] = repository
	}
	body := map[string]any{"data": data}
	if len(problems) > 0 {
		body["errors"] = problems
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func repositoryRequests(count int) []RepositoryRequest {
	requests := make([]RepositoryRequest, count)
	for i := range requests {
		requests[i] = RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i), Previous: []PullRequest{snapshot(99)}}
	}
	return requests
}

func TestInventoryBatchSizesStayWellUnderNodeAndOutputLimits(t *testing.T) {
	nodes := prPageSize * (1 + 2*contextPageSize + 1)
	if inventoryBatchSize() != 8 {
		t.Fatalf("lean batch size = %d", inventoryBatchSize())
	}
	if inventoryBatchSize()*nodes > inventoryBatchNodes || inventoryBatchNodes > 500_000/10 || inventoryBatchSize()*leanInventoryBytes > MaxOutputBytes/2 {
		t.Fatal("batched inventory exceeds its node or output allowance")
	}
	jobs := make([]*repositoryFetch, 15)
	var sizes []int
	for _, chunk := range inventoryChunks(jobs, inventoryBatchSize()) {
		sizes = append(sizes, len(chunk))
	}
	if fmt.Sprint(sizes) != "[7 8]" {
		t.Fatalf("uneven chunks: %v", sizes)
	}
}

func TestInventoryBatchQueryUsesAliasedRepositoriesAndLeanFields(t *testing.T) {
	query := inventoryBatchQuery([]string{"acme/one", "acme/two.go"})
	for _, want := range []string{
		`repo0: repository(owner: "acme", name: "one") { ...BigBoardOpenPullRequests }`,
		`repo1: repository(owner: "acme", name: "two.go") { ...BigBoardOpenPullRequests }`,
		"fragment BigBoardOpenPullRequests on Repository",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("missing %q in %s", want, query)
		}
	}
	if !strings.HasSuffix(query, leanInventoryFields+"}") || !strings.HasPrefix(query, "query ") || strings.Contains(query, "mutation") {
		t.Fatal("batched inventory diverged from the lean per-repository fields")
	}
}

func TestFetchAllBatchesOnlyLeanFirstInventoryPages(t *testing.T) {
	requests := repositoryRequests(3)
	repositories := make(map[string]*rawRepository)
	for i := range requests {
		repositories[fmt.Sprintf("repo%d", i)] = &rawRepository{NameWithOwner: requests[i].Repo, PullRequests: conn([]*rawPR{leanPR(i+1, 0)}, 1, false, "")}
	}
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, data: batchInventory(t, repositories)}}}
	ctx := WithRequestBudget(context.Background(), 10)
	results := newTestProvider(runner).FetchAll(ctx, requests)
	if len(runner.calls) != 1 || RequestCount(ctx) != 1 {
		t.Fatalf("calls=%d budget=%d", len(runner.calls), RequestCount(ctx))
	}
	if repos, query, ok := batchedRepositories(runner.calls[0].args); !ok || len(repos) != 3 || query != leanSummaryQuery {
		t.Fatalf("not one batched lean inventory: %v", runner.calls[0].args)
	}
	if hasPair(runner.calls[0].args, "-f", "owner=acme") {
		t.Fatal("batched inventory used per-repository variables")
	}
	inventories := 0
	for i, result := range results {
		inventories += result.InventoryRequests
		if result.Repo != requests[i].Repo || !result.Complete || result.Err != nil || len(result.PRs) != 1 || result.PRs[0].Number != i+1 || result.FileRequests != 0 {
			t.Fatalf("result %d: %+v", i, result)
		}
	}
	if inventories != 1 {
		t.Fatalf("shared request counted %d times", inventories)
	}
}

func TestFetchAllInlineInventoriesStayPerRepository(t *testing.T) {
	runner := &fakeRunner{t: t}
	var requests []RepositoryRequest
	for i := range 3 {
		repo := fmt.Sprintf("acme/repo%d", i)
		runner.steps = append(runner.steps, repoSummaries(t, repo, []*rawPR{pr(i + 1)}, 1, false, ""))
		requests = append(requests, RepositoryRequest{Repo: repo})
	}
	for _, result := range newTestProvider(runner).FetchAll(context.Background(), requests) {
		if !result.Complete || result.Err != nil || result.InventoryRequests != 1 {
			t.Fatalf("inline inventory: %+v", result)
		}
	}
	for _, call := range runner.calls {
		if _, _, batched := batchedRepositories(call.args); batched || queryOf(call) != summaryQuery {
			t.Fatal("inline-files inventory was batched")
		}
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
}

func TestFetchAllBatchOverOutputLimitFallsBackToRepositories(t *testing.T) {
	title := strings.Repeat("t", 12_000)
	respond := func(repo string) []byte {
		var rows []*rawPR
		for number := 1; number <= prPageSize; number++ {
			row := leanPR(number, 0)
			row.Title = title
			rows = append(rows, row)
		}
		return payload(t, &rawRepository{NameWithOwner: repo, PullRequests: conn(rows, len(rows), false, "")}, 100)
	}
	requests := repositoryRequests(4)
	var batches, singles int
	p := newTestProvider(batchRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if repos, _, ok := batchedRepositories(args); ok {
			batches++
			data, _ := combineInventories(t, repos, func(repo string) ([]byte, error) { return respond(repo), nil })
			if len(data) <= MaxOutputBytes {
				t.Fatalf("fixture fits combined: %d bytes", len(data))
			}
			return data, nil
		}
		singles++
		data := respond(requestRepository(args))
		if len(data) > MaxOutputBytes/2 {
			t.Fatalf("fixture too large alone: %d bytes", len(data))
		}
		return data, nil
	}))
	ctx := WithRequestBudget(context.Background(), 10)
	inventories := 0
	for i, result := range p.FetchAll(ctx, requests) {
		inventories += result.InventoryRequests
		if result.Repo != requests[i].Repo || !result.Complete || result.Err != nil || len(result.PRs) != prPageSize {
			t.Fatalf("repository %d lost its inventory: %+v", i, result)
		}
	}
	if batches != 1 || singles != 4 || inventories != 5 || RequestCount(ctx) != 5 {
		t.Fatalf("batches=%d singles=%d inventories=%d budget=%d", batches, singles, inventories, RequestCount(ctx))
	}
}

func TestFetchAllBatchedInventoryFollowsOnlyRepositoriesWithMorePages(t *testing.T) {
	second := pr(2)
	runner := &fakeRunner{t: t, steps: []fakeStep{
		{batch: true, data: batchInventory(t, map[string]*rawRepository{
			"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{pr(1)}, 2, true, "page-two")},
			"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{pr(5)}, 1, false, "")},
		})},
		{repo: "acme/repo0", query: leanSummaryQuery, data: payload(t, &rawRepository{NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{second}, 2, false, "")}, 100)},
	}}
	results := newTestProvider(runner).FetchAll(context.Background(), repositoryRequests(2))
	if len(runner.calls) != 2 || !hasPair(runner.calls[1].args, "-f", "cursor=page-two") || !hasPair(runner.calls[1].args, "-f", "name=repo0") || queryOf(runner.calls[1]) != leanSummaryQuery {
		t.Fatalf("follow-up page missing: %+v", runner.calls)
	}
	if !results[0].Complete || len(results[0].PRs) != 2 || results[0].InventoryRequests != 2 || results[0].Err != nil {
		t.Fatalf("paginated repository: %+v", results[0])
	}
	if !results[1].Complete || len(results[1].PRs) != 1 || results[1].InventoryRequests != 0 {
		t.Fatalf("single-page repository: %+v", results[1])
	}
}

func TestFetchAllBatchedAliasErrorIsolatesRepository(t *testing.T) {
	for _, tc := range []struct {
		name    string
		problem map[string]any
		target  error
	}{
		{name: "missing repository", problem: map[string]any{"type": "NOT_FOUND", "message": "Could not resolve to a Repository", "path": []any{"repo1"}}, target: ErrRepository},
		{name: "inventory field", problem: map[string]any{"message": "field unavailable", "path": []any{"repo1", "pullRequests", "totalCount"}}, target: ErrIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var broken *rawRepository
			if tc.target == ErrIncomplete {
				broken = &rawRepository{NameWithOwner: "acme/repo1"}
			}
			runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, err: ErrRepository, data: batchInventory(t, map[string]*rawRepository{
				"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{pr(1)}, 1, false, "")},
				"repo1": broken,
				"repo2": {NameWithOwner: "acme/repo2", PullRequests: conn([]*rawPR{pr(3)}, 1, false, "")},
			}, tc.problem)}}}
			results := newTestProvider(runner).FetchAll(context.Background(), repositoryRequests(3))
			if len(runner.calls) != 1 {
				t.Fatalf("calls=%d", len(runner.calls))
			}
			for i, result := range results {
				if i == 1 {
					if result.Complete || !errors.Is(result.InventoryErr, tc.target) {
						t.Fatalf("failed alias looked complete: %+v", result)
					}
				} else if !result.Complete || result.Err != nil || len(result.PRs) != 1 {
					t.Fatalf("alias error contaminated repository %d: %+v", i, result)
				}
			}
		})
	}
}

func TestFetchAllBatchedFileFieldErrorKeepsInventoryComplete(t *testing.T) {
	raw := pr(1)
	raw.Files = nil
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, err: ErrRequest, data: batchInventory(t, map[string]*rawRepository{
		"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{raw}, 1, false, "")},
		"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{leanPR(2, 0)}, 1, false, "")},
	}, map[string]any{"message": "field unavailable", "path": []any{"repo0", "pullRequests", "nodes", 0, "files"}})}}}
	results := newTestProvider(runner).FetchAll(context.Background(), repositoryRequests(2))
	if !results[0].Complete || results[0].InventoryErr != nil || results[0].FilesErr == nil || results[0].PRs[0].FilesComplete {
		t.Fatalf("file field error changed inventory coverage: %+v", results[0])
	}
	if !results[1].Complete || results[1].Err != nil || !results[1].PRs[0].FilesComplete {
		t.Fatalf("file field error leaked to another repository: %+v", results[1])
	}
}

func TestFetchAllBatchedUnscopedErrorAppliesToEveryRepository(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, err: ErrRequest, data: batchInventory(t, map[string]*rawRepository{
		"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{pr(1)}, 1, false, "")},
		"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{pr(2)}, 1, false, "")},
	}, map[string]any{"message": "something failed"})}}}
	for _, result := range newTestProvider(runner).FetchAll(context.Background(), repositoryRequests(2)) {
		if result.Complete || result.InventoryErr == nil || len(result.PRs) != 1 {
			t.Fatalf("unscoped error was ignored: %+v", result)
		}
	}
}

func TestFetchAllBatchedInventoryRespectsBudgetAndCooldown(t *testing.T) {
	t.Run("budget", func(t *testing.T) {
		runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, data: batchInventory(t, map[string]*rawRepository{
			"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{pr(1)}, 2, true, "next")},
			"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{pr(2)}, 1, false, "")},
		})}}}
		ctx := WithRequestBudget(context.Background(), 1)
		results := newTestProvider(runner).FetchAll(ctx, repositoryRequests(2))
		if len(runner.calls) != 1 || RequestCount(ctx) != 1 {
			t.Fatalf("calls=%d budget=%d", len(runner.calls), RequestCount(ctx))
		}
		if results[0].Complete || !errors.Is(results[0].InventoryErr, ErrBudgetExceeded) || len(results[0].PRs) != 1 || !results[1].Complete {
			t.Fatalf("budget not enforced after batched page: %+v", results)
		}
	})
	t.Run("zero budget", func(t *testing.T) {
		runner := &fakeRunner{t: t}
		for _, result := range newTestProvider(runner).FetchAll(WithRequestBudget(context.Background(), 0), repositoryRequests(3)) {
			if result.Complete || !errors.Is(result.InventoryErr, ErrBudgetExceeded) || result.InventoryRequests != 0 {
				t.Fatalf("batched inventory ignored the refresh budget: %+v", result)
			}
		}
		if len(runner.calls) != 0 {
			t.Fatal("request ran without budget")
		}
	})
	t.Run("repository allowance", func(t *testing.T) {
		p := newTestProvider(&fakeRunner{t: t, steps: []fakeStep{{batch: true, data: batchInventory(t, map[string]*rawRepository{
			"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{pendingPR(1)}, 1, false, "")},
			"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{pendingPR(2)}, 1, false, "")},
		})}}})
		p.limits.requests = 1
		for _, result := range p.FetchAll(context.Background(), repositoryRequests(2)) {
			if !result.Complete || result.FileRequests != 0 || !errors.Is(result.FilesErr, ErrBudgetExceeded) {
				t.Fatalf("shared request was not charged to each repository: %+v", result)
			}
		}
	})
	t.Run("cooldown", func(t *testing.T) {
		var calls atomic.Int32
		p := newTestProvider(batchRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			calls.Add(1)
			return nil, ErrRateLimited
		}))
		results := p.FetchAll(context.Background(), repositoryRequests((inventoryConcurrency+1)*inventoryBatchSize()))
		for _, result := range results {
			if result.Complete || !errors.Is(result.InventoryErr, ErrRateLimited) || !result.RetryAt.After(time.Now()) {
				t.Fatalf("cooldown missing: %+v", result)
			}
		}
		if got := calls.Load(); got < 1 || got > inventoryConcurrency {
			t.Fatalf("rate-limited batch did not stop later batches: %d calls", got)
		}
	})
}

func TestFetchAllBatchedRenamedRepositoriesShareCanonicalResult(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, data: batchInventory(t, map[string]*rawRepository{
		"repo0": {NameWithOwner: "Acme/Repo", PullRequests: conn([]*rawPR{pr(1), pr(2)}, 2, false, "")},
		"repo1": {NameWithOwner: "Acme/Repo", PullRequests: conn([]*rawPR{pr(1)}, 2, true, "next")},
		"repo2": {NameWithOwner: "acme/other", PullRequests: conn([]*rawPR{pr(3)}, 1, false, "")},
	})}}}
	previous := []PullRequest{snapshot(99)}
	results := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: previous}, {Repo: "acme/old-name"}, {Repo: "acme/other", Previous: previous}, {Repo: "ACME/OLD-NAME", Previous: previous}})
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
	requests := 0
	for i, result := range results {
		requests += result.InventoryRequests
		want := "acme/repo"
		if i == 2 {
			want = "acme/other"
		}
		if result.Repo != want || !result.Complete || result.Err != nil {
			t.Fatalf("result %d: %+v", i, result)
		}
	}
	if requests != 1 || len(results[1].PRs) != 2 {
		t.Fatalf("renamed repository evidence: requests=%d %+v", requests, results[1])
	}
}

func TestFetchAllBatchedCompleteAliasEvidenceWinsWithoutDoubleCounting(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, data: batchInventory(t, map[string]*rawRepository{
		"repo0": {NameWithOwner: "Acme/Repo", PullRequests: conn([]*rawPR{pr(1)}, 2, false, "")},
		"repo1": {NameWithOwner: "Acme/Repo", PullRequests: conn([]*rawPR{pr(1), pr(2)}, 2, false, "")},
	})}}}
	previous := []PullRequest{snapshot(99)}
	results := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/old-name", Previous: previous}, {Repo: "acme/repo", Previous: previous}})
	requests := 0
	for _, result := range results {
		requests += result.InventoryRequests
		if result.Repo != "acme/repo" || !result.Complete || result.Err != nil || len(result.PRs) != 2 {
			t.Fatalf("incomplete alias evidence replaced complete evidence: %+v", results)
		}
	}
	if requests != 1 || len(runner.calls) != 1 {
		t.Fatalf("shared request counted %d times over %d calls", requests, len(runner.calls))
	}
}

func TestFetchAllBatchedUnscopedErrorSurvivesMemberFileFieldError(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{batch: true, err: ErrRequest, data: batchInventory(t, map[string]*rawRepository{
		"repo0": {NameWithOwner: "acme/repo0", PullRequests: conn([]*rawPR{leanPR(1, 0)}, 1, false, "")},
		"repo1": {NameWithOwner: "acme/repo1", PullRequests: conn([]*rawPR{leanPR(2, 0)}, 1, false, "")},
	}, map[string]any{"message": "query failed"}, map[string]any{"message": "field unavailable", "path": []any{"repo0", "pullRequests", "nodes", 0, "files"}})}}}
	for i, result := range newTestProvider(runner).FetchAll(context.Background(), repositoryRequests(2)) {
		if result.Complete || result.InventoryErr == nil {
			t.Fatalf("batch-wide failure cleared for repository %d: %+v", i, result)
		}
	}
}
