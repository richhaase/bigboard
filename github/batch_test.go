package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func repoSummaries(t *testing.T, repo string, rows []*rawPR, total int, more bool, cursor string) fakeStep {
	t.Helper()
	return fakeStep{repo: repo, query: summaryQuery, data: payload(t, &rawRepository{NameWithOwner: repo, PullRequests: conn(rows, total, more, cursor)}, 100)}
}

func repoFiles(t *testing.T, repo string, source *rawPR, nodes []*rawFile, total int, more bool, cursor string) fakeStep {
	t.Helper()
	raw := &rawPR{Number: source.Number, UpdatedAt: source.UpdatedAt, HeadRefOid: source.HeadRefOid, BaseRefOid: source.BaseRefOid,
		Files: conn(nodes, total, more, cursor)}
	return fakeStep{repo: repo, query: filesQuery, data: payload(t, &rawRepository{NameWithOwner: repo, PullRequest: raw}, 100)}
}

func pendingPR(number int) *rawPR {
	row := pr(number)
	row.Files = conn([]*rawFile{file("first.go", "MODIFIED")}, 2, true, "next")
	return row
}

func TestFetchAllPrioritizesInventoriesAcrossRepositories(t *testing.T) {
	rows := []*rawPR{pendingPR(1), pendingPR(2), pendingPR(3)}
	runner := &fakeRunner{t: t}
	var requests []RepositoryRequest
	for i, row := range rows {
		repo := fmt.Sprintf("acme/repo%d", i)
		runner.steps = append(runner.steps, repoSummaries(t, repo, []*rawPR{row}, 1, false, ""))
		requests = append(requests, RepositoryRequest{Repo: repo})
	}
	runner.steps = append(runner.steps, repoFiles(t, "acme/repo0", rows[0], []*rawFile{file("last.go", "ADDED")}, 2, false, ""))
	results := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 4), requests)
	if len(results) != 3 || len(runner.calls) != 4 {
		t.Fatalf("results=%d calls=%d", len(results), len(runner.calls))
	}
	for i, result := range results {
		if !result.Complete || result.InventoryErr != nil || result.InventoryRequests != 1 || len(result.PRs) != 1 {
			t.Fatalf("inventory %d was starved: %+v", i, result)
		}
		if i == 0 {
			if result.FileRequests != 1 || !result.PRs[0].FilesComplete {
				t.Fatalf("first files not completed: %+v", result)
			}
		} else if result.FileRequests != 0 || !errors.Is(result.FilesErr, ErrBudgetExceeded) || result.PRs[0].FilesComplete {
			t.Fatalf("unknown file coverage hidden: %+v", result)
		}
		if !hasPair(runner.calls[i].args, "-f", "query="+summaryQuery) {
			t.Fatal("file pagination ran before every inventory")
		}
	}
}

func TestFetchAllFilesPrioritizeNewestAcrossRepositories(t *testing.T) {
	older, newer := pendingPR(1), pendingPR(2)
	newer.UpdatedAt = "2026-10-01T12:00:00Z"
	runner := &fakeRunner{t: t, steps: []fakeStep{
		repoSummaries(t, "acme/older", []*rawPR{older}, 1, false, ""),
		repoSummaries(t, "acme/newer", []*rawPR{newer}, 1, false, ""),
		repoFiles(t, "acme/newer", newer, []*rawFile{file("newest.go", "ADDED")}, 2, false, ""),
		repoFiles(t, "acme/older", older, []*rawFile{file("older.go", "ADDED")}, 2, false, ""),
	}}
	results := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 4), []RepositoryRequest{{Repo: "acme/older"}, {Repo: "acme/newer"}})
	if !hasPair(runner.calls[2].args, "-f", "name=newer") {
		t.Fatal("repository order displaced newer PR files")
	}
	for _, result := range results {
		if !result.Complete || result.Err != nil || !result.PRs[0].FilesComplete || result.FileRequests != 1 {
			t.Fatalf("unexpected result: %+v", result)
		}
	}
}

func TestFetchAllKeepsInlineFilesEfficient(t *testing.T) {
	runner := &fakeRunner{t: t}
	for page := 0; page < 3; page++ {
		var rows []*rawPR
		for i := page * 50; i < min(140, (page+1)*50); i++ {
			rows = append(rows, pr(i+1, file("known.go", "MODIFIED")))
		}
		runner.steps = append(runner.steps, summaries(t, rows, 140, page < 2, fmt.Sprintf("page%d", page)))
	}
	result := NewProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo"}})[0]
	if !result.Complete || result.Err != nil || len(result.PRs) != 140 || len(runner.calls) != 3 || result.InventoryRequests != 3 || result.FileRequests != 0 {
		t.Fatalf("inline file evidence regressed: %+v calls=%d", result, len(runner.calls))
	}
	for _, pr := range result.PRs {
		if !pr.FilesComplete {
			t.Fatal("small PR lost inline file evidence")
		}
	}
}

func TestFetchAllReusesOnlyCurrentCompleteFileEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*rawPR, *PullRequest)
		reuse  bool
	}{
		{name: "unchanged", reuse: true},
		{name: "updated", mutate: func(raw *rawPR, _ *PullRequest) { raw.UpdatedAt = "2026-10-01T12:00:00Z" }},
		{name: "head", mutate: func(raw *rawPR, _ *PullRequest) { raw.HeadRefOid = strings.Repeat("c", 40) }},
		{name: "base", mutate: func(raw *rawPR, _ *PullRequest) { raw.BaseRefOid = strings.Repeat("c", 40) }},
		{name: "count", mutate: func(raw *rawPR, _ *PullRequest) { raw.Files.TotalCount = pointer(3) }},
		{name: "missing count", mutate: func(raw *rawPR, _ *PullRequest) { raw.Files.TotalCount = nil }},
		{name: "missing revision", mutate: func(_ *rawPR, old *PullRequest) { old.HeadOID = "" }},
		{name: "incomplete", mutate: func(_ *rawPR, old *PullRequest) { old.FilesComplete = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := pendingPR(7)
			old := NewProvider(nil).convertPR("acme/repo", pr(7, file("first.go", "MODIFIED"), file("old-only.go", "ADDED"))).pr
			if tc.mutate != nil {
				tc.mutate(raw, &old)
			}
			original := slices.Clone(old.Files)
			runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}}
			result := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 2), []RepositoryRequest{{Repo: "acme/repo", Previous: []PullRequest{old}}})[0]
			got := result.PRs[0]
			if got.FilesComplete != tc.reuse || result.InventoryRequests != 1 || result.FileRequests != 0 {
				t.Fatalf("wrong reuse: %+v", result)
			}
			if tc.reuse {
				if result.FilesErr != nil || len(got.Files) != 2 {
					t.Fatalf("unchanged evidence not reused: %+v", got)
				}
				got.Files[0].Path = "mutated.go"
			} else {
				for _, f := range got.Files {
					if f.Path == "old-only.go" {
						t.Fatal("obsolete path was retained")
					}
				}
			}
			if !slices.Equal(old.Files, original) {
				t.Fatal("reuse mutated prior snapshot")
			}
		})
	}
}

func TestFetchAllSharesRepositoryLimitAcrossPhases(t *testing.T) {
	raw := pendingPR(7)
	raw.Files.TotalCount = pointer(5)
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{raw}, 2, true, "more-prs"),
		summaries(t, []*rawPR{pr(8)}, 2, false, ""),
		filePage(t, 7, []*rawFile{file("second.go", "ADDED")}, 5, true, "second"),
		filePage(t, 7, []*rawFile{file("third.go", "ADDED")}, 5, true, "third"),
	}}
	result := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 8), []RepositoryRequest{{Repo: "acme/repo"}})[0]
	if !result.Complete || !errors.Is(result.FilesErr, ErrBudgetExceeded) || result.InventoryRequests != 2 || result.FileRequests != 2 || len(runner.calls) != 4 {
		t.Fatalf("repository allowance reset between phases: %+v", result)
	}
}

func TestFetchAllSharedCooldownStopsLaterInventories(t *testing.T) {
	var calls atomic.Int32
	p := NewProvider(batchRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		calls.Add(1)
		return nil, ErrRateLimited
	}))
	var requests []RepositoryRequest
	for i := range 12 {
		requests = append(requests, RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i)})
	}
	results := p.FetchAll(context.Background(), requests)
	for _, result := range results {
		if !errors.Is(result.InventoryErr, ErrRateLimited) || result.Complete || !result.RetryAt.After(time.Now()) {
			t.Fatalf("cooldown missing: %+v", result)
		}
	}
	if got := calls.Load(); got < 1 || got > inventoryConcurrency {
		t.Fatalf("quota failure did not stop queued repositories: %d calls", got)
	}
}

func TestFetchAllDeduplicatesCanonicalAliases(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(7)}, 1, false, ""), summaries(t, []*rawPR{pr(7)}, 1, false, "")}}
	results := NewProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/old"}, {Repo: "acme/repo"}, {Repo: "ACME/OLD"}})
	if len(runner.calls) < 1 || len(runner.calls) > 2 || len(results) != 3 {
		t.Fatalf("canonical aliases fetched repeatedly: calls=%d results=%d", len(runner.calls), len(results))
	}
	requests := 0
	for _, result := range results {
		requests += result.InventoryRequests
		if result.Repo != "acme/repo" || !result.Complete || len(result.PRs) != 1 {
			t.Fatalf("alias result missing: %+v", result)
		}
	}
	if requests != len(runner.calls) {
		t.Fatal("canonical aliases counted a request more than once")
	}
}

type batchRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f batchRunnerFunc) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	return f(ctx, command, args...)
}

func TestFetchAllCancellationPreservesCollectedInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	p := NewProvider(batchRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls.Add(1)
		cancel()
		return repoSummaries(t, requestRepository(args), []*rawPR{pendingPR(7)}, 1, false, "").data, nil
	}))
	var requests []RepositoryRequest
	for i := range 12 {
		requests = append(requests, RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i)})
	}
	results := p.FetchAll(ctx, requests)
	retained := 0
	for _, result := range results {
		if result.Complete || !errors.Is(result.InventoryErr, context.Canceled) {
			t.Fatalf("cancellation ignored: %+v", result)
		}
		if len(result.PRs) != 0 {
			retained++
			if result.PRs[0].FilesComplete || !errors.Is(result.FilesErr, context.Canceled) {
				t.Fatalf("canceled file phase looked complete: %+v", result)
			}
		}
	}
	if got := calls.Load(); got < 1 || got > inventoryConcurrency || retained != int(got) {
		t.Fatalf("cancellation lost returned evidence: calls=%d retained=%d", got, retained)
	}
}

func TestFetchAllFileAuthenticationFailureStopsOtherRepositories(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{
		repoSummaries(t, "acme/one", []*rawPR{pendingPR(1)}, 1, false, ""),
		repoSummaries(t, "acme/two", []*rawPR{pendingPR(2)}, 1, false, ""),
		{err: ErrAuthentication},
	}}
	results := NewProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/one"}, {Repo: "acme/two"}})
	for _, result := range results {
		if !result.Complete || result.InventoryErr != nil || !errors.Is(result.FilesErr, ErrAuthentication) {
			t.Fatalf("file failure corrupted inventory: %+v", result)
		}
	}
	if len(runner.calls) != 3 {
		t.Fatal("authentication failure did not stop the remaining file work")
	}
}

func TestFetchAllCountsRequestsForNewlyDiscoveredAliases(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{pr(7)}, 1, false, ""),
		summaries(t, []*rawPR{pr(7)}, 1, false, ""),
		summaries(t, []*rawPR{pr(7)}, 1, false, ""),
	}}
	results := NewProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/old-one"}, {Repo: "acme/old-two"}, {Repo: "acme/repo"}})
	requests := 0
	for _, result := range results {
		requests += result.InventoryRequests
	}
	if len(runner.calls) < 2 || len(runner.calls) > 3 || requests != len(runner.calls) {
		t.Fatalf("actual redirect lookups were not counted: calls=%d requests=%d", len(runner.calls), requests)
	}
}

func TestFetchAllIncompleteAliasCannotReplaceCompleteCanonicalInventory(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{pr(1), pr(2)}, 2, false, ""),
		summaries(t, []*rawPR{pr(1)}, 2, true, "next"),
	}}
	results := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 2), []RepositoryRequest{{Repo: "acme/repo"}, {Repo: "acme/old-name"}})
	requests := 0
	for _, result := range results {
		requests += result.InventoryRequests
		if !result.Complete || len(result.PRs) != 2 || result.Err != nil {
			t.Fatalf("redundant alias degraded a complete canonical inventory: %+v", result)
		}
	}
	if requests != 2 || len(runner.calls) != 2 {
		t.Fatal("canonical alias lookup was not charged exactly once")
	}
}

func TestFetchAllAliasLookupSharesCanonicalAllowance(t *testing.T) {
	row := pendingPR(1)
	row.Files.TotalCount = pointer(4)
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{row}, 1, false, ""),
		summaries(t, []*rawPR{row}, 1, false, ""),
		filePage(t, 1, []*rawFile{file("second.go", "ADDED")}, 4, true, "second"),
	}}
	results := NewProvider(runner).FetchAll(WithRequestBudget(context.Background(), 6), []RepositoryRequest{{Repo: "acme/repo"}, {Repo: "acme/old-name"}})
	requests := 0
	for _, result := range results {
		requests += result.InventoryRequests + result.FileRequests
		if !result.Complete || !errors.Is(result.FilesErr, ErrBudgetExceeded) || result.PRs[0].FilesComplete {
			t.Fatalf("alias reset canonical request allowance: %+v", result)
		}
	}
	if requests != 3 || len(runner.calls) != 3 {
		t.Fatalf("per-repository allowance was exceeded: calls=%d requests=%d", len(runner.calls), requests)
	}
}

func TestInlineFileGraphQLErrorKeepsInventoryCoverageSeparate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		paths    [][]any
		kind     string
		complete bool
	}{
		{name: "file field only", paths: [][]any{{"repository", "pullRequests", "nodes", 0, "files"}}, complete: true},
		{name: "nested file field", paths: [][]any{{"repository", "pullRequests", "nodes", 0, "files", "nodes", 0, "path"}}, complete: true},
		{name: "inventory field", paths: [][]any{{"repository", "pullRequests", "totalCount"}}},
		{name: "other metadata", paths: [][]any{{"repository", "pullRequests", "nodes", 0, "number"}}},
		{name: "mixed fields", paths: [][]any{{"repository", "pullRequests", "nodes", 0, "files"}, {"repository", "pullRequests", "nodes", 0, "number"}}},
		{name: "no path", paths: [][]any{nil}},
		{name: "bad index", paths: [][]any{{"repository", "pullRequests", "nodes", 9, "files"}}},
		{name: "quota", kind: "RATE_LIMITED", paths: [][]any{{"repository", "pullRequests", "nodes", 0, "files"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := pr(1)
			raw.Files = nil
			var body map[string]any
			if err := json.Unmarshal(summaries(t, []*rawPR{raw}, 1, false, "").data, &body); err != nil {
				t.Fatal(err)
			}
			var problems []map[string]any
			for _, path := range tc.paths {
				problems = append(problems, map[string]any{"message": "field unavailable", "type": tc.kind, "path": path})
			}
			body["errors"] = problems
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{t: t, steps: []fakeStep{{data: data, err: ErrRequest}}}
			result := NewProvider(runner).Fetch(context.Background(), "acme/repo")
			if result.Complete != tc.complete || len(result.PRs) != 1 || result.PRs[0].FilesComplete {
				t.Fatalf("wrong field coverage: %+v", result)
			}
			if tc.complete {
				if result.InventoryErr != nil || result.FilesErr == nil {
					t.Fatalf("file-only error contaminated inventory: %+v", result)
				}
			} else if result.InventoryErr == nil {
				t.Fatalf("unrelated error was suppressed: %+v", result)
			}
		})
	}
}
