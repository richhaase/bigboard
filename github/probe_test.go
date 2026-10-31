package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func probePayload(t *testing.T, repositories map[string]any, problems ...map[string]any) []byte {
	t.Helper()
	repositories["rateLimit"] = map[string]any{"remaining": 100}
	data, err := json.Marshal(map[string]any{"data": repositories, "errors": problems})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func probeRepository(repo string, count int) map[string]any {
	return map[string]any{"nameWithOwner": repo, "defaultBranchRef": map[string]any{"name": "main", "target": map[string]any{"oid": strings.Repeat("a", 40)}}, "pullRequests": map[string]any{"totalCount": count}}
}

func TestRepositoryProbeBatchesDeduplicateAndShareBudget(t *testing.T) {
	runner := &fakeRunner{t: t}
	var repositories []string
	for start := 0; start < 201; start += 100 {
		data := map[string]any{}
		for i := start; i < min(start+100, 201); i++ {
			repo := fmt.Sprintf("org/repo%d", i)
			repositories = append(repositories, repo)
			data[fmt.Sprintf("repo%d", i-start)] = probeRepository(repo, 0)
		}
		runner.steps = append(runner.steps, fakeStep{data: probePayload(t, data)})
	}
	repositories = append(repositories, "ORG/REPO0")
	provider := newTestProvider(runner)
	ctx := WithRequestBudget(context.Background(), 3)
	results := provider.ProbeAll(ctx, repositories)
	calls := 0
	for _, result := range results {
		calls += result.Requests
		if result.Err != nil || result.OpenPRCount == nil || *result.OpenPRCount != 0 || result.DefaultBranch != "main" || !validOID(result.HeadOID) {
			t.Fatalf("invalid probe: %+v", result)
		}
	}
	if calls != 3 || RequestCount(ctx) != 3 || len(runner.calls) != 3 {
		t.Fatalf("requests=%d budget=%d calls=%d", calls, RequestCount(ctx), len(runner.calls))
	}
	for i, call := range runner.calls {
		query := strings.Join(call.args, " ")
		if count := strings.Count(query, ": repository("); count != min(100, 201-i*100) || strings.Contains(query, "updatedAt") || !strings.Contains(query, "first: 1") {
			t.Fatalf("wrong probe query: %s", query)
		}
	}
	if _, err := provider.CurrentLogin(ctx); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("probe did not share account allowance: %v", err)
	}
	if result := provider.FetchAll(ctx, []RepositoryRequest{{Repo: "org/repo0"}})[0]; !errors.Is(result.Err, ErrBudgetExceeded) || result.Complete {
		t.Fatalf("inventory escaped shared budget: %+v", result)
	}
}

func TestRepositoryProbeUnknownAndAliasErrorsCannotProveEmpty(t *testing.T) {
	malformed := probeRepository("org/malformed", 0)
	malformed["pullRequests"] = map[string]any{}
	runner := &fakeRunner{t: t, steps: []fakeStep{{data: probePayload(t, map[string]any{
		"repo0": probeRepository("ORG/RENAMED", 0), "repo1": nil, "repo3": malformed, "repo4": probeRepository("org/error", 0), "repo5": probeRepository("org/positive", 2),
	}, map[string]any{"message": "Could not resolve to a Repository", "path": []any{"repo4"}})}}}
	results := newTestProvider(runner).ProbeAll(context.Background(), []string{"org/old", "org/null", "org/missing", "org/malformed", "org/error", "org/positive"})
	if results[0].Err != nil || results[0].Repo != "org/renamed" || *results[0].OpenPRCount != 0 || results[5].Err != nil || *results[5].OpenPRCount != 2 {
		t.Fatalf("valid aliases lost: %+v", results)
	}
	for _, result := range results[1:5] {
		if result.Err == nil || result.OpenPRCount != nil || result.HeadOID != "" {
			t.Fatalf("unknown alias became proof: %+v", result)
		}
	}
}

func TestRepositoryProbeCooldownStopsInventoryAndLaterProbes(t *testing.T) {
	for _, successful := range []bool{false, true} {
		t.Run(fmt.Sprint(successful), func(t *testing.T) {
			data := probePayload(t, map[string]any{"repo0": probeRepository("org/repo", 1)})
			header := "HTTP/2 429\r\nRetry-After: 600\r\n\r\n"
			if successful {
				header = "HTTP/2 200\r\nX-RateLimit-Remaining: 0\r\nRetry-After: 600\r\n\r\n"
			}
			runner := &fakeRunner{t: t, steps: []fakeStep{{data: append([]byte(header), data...)}}}
			provider := newTestProvider(runner)
			ctx := WithRequestBudget(context.Background(), 10)
			result := provider.ProbeAll(ctx, []string{"org/repo"})[0]
			if !result.RetryAt.After(time.Now()) || (!successful && (result.Err == nil || result.OpenPRCount != nil)) {
				t.Fatalf("invalid cooldown proof: %+v", result)
			}
			inventory := provider.FetchAll(ctx, []RepositoryRequest{{Repo: "org/repo"}})[0]
			next := provider.ProbeAll(ctx, []string{"org/repo"})[0]
			if !errors.Is(inventory.Err, ErrRateLimited) || inventory.Complete || !errors.Is(next.Err, ErrRateLimited) || RequestCount(ctx) != 1 || len(runner.calls) != 1 {
				t.Fatalf("cooldown retried: inventory=%+v probe=%+v", inventory, next)
			}
		})
	}
}

func TestRepositoryProbeCanceledContextMakesNoRequests(t *testing.T) {
	runner := &fakeRunner{t: t}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := newTestProvider(runner).ProbeAll(ctx, []string{"org/repo"})[0]
	if !errors.Is(result.Err, context.Canceled) || result.OpenPRCount != nil || len(runner.calls) != 0 {
		t.Fatalf("canceled probe: %+v", result)
	}
}

func TestRepositoryProbePartialGraphQLErrorWithCLIExitIsIsolated(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{err: ErrRequest, data: probePayload(t, map[string]any{
		"repo0": probeRepository("org/good", 0), "repo1": probeRepository("org/bad", 0),
	}, map[string]any{"message": "field unavailable", "path": []any{"repo1", "pullRequests"}})}}}
	results := newTestProvider(runner).ProbeAll(context.Background(), []string{"org/good", "org/bad"})
	if results[0].Err != nil || results[0].OpenPRCount == nil || *results[0].OpenPRCount != 0 || results[1].Err == nil || results[1].OpenPRCount != nil {
		t.Fatalf("partial CLI exit was not isolated: %+v", results)
	}
}

func TestScopedGraphQLDenialsPreserveOtherAliases(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want error
	}{{"FORBIDDEN", ErrPermission}, {"NOT_FOUND", ErrRepository}} {
		t.Run(tc.kind, func(t *testing.T) {
			body := probePayload(t, map[string]any{"repo0": nil, "repo1": probeRepository("other/allowed", 0)}, map[string]any{"type": tc.kind, "message": "access unavailable", "path": []any{"repo0"}})
			p := NewProvider(discoveryClientFunc(func(context.Context, Request) (Response, error) { return Response{StatusCode: 200, Body: body}, nil }))
			rows := p.ProbeAll(t.Context(), []string{"one/private", "other/allowed"})
			if !errors.Is(rows[0].Err, tc.want) || rows[1].Err != nil || rows[1].OpenPRCount == nil {
				t.Fatalf("denial scope lost: %+v", rows)
			}
			inventoryBody := []byte(fmt.Sprintf(`{"data":{"repository":null},"errors":[{"type":%q,"path":["repository"]}]}`, tc.kind))
			p = NewProvider(discoveryClientFunc(func(context.Context, Request) (Response, error) {
				return Response{StatusCode: 200, Body: inventoryBody}, nil
			}))
			if result := p.Fetch(t.Context(), "one/private"); !errors.Is(result.Err, tc.want) {
				t.Fatalf("inventory denial lost: %v", result.Err)
			}
		})
	}
}
