package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func requestRepository(args []string) string {
	var owner, name string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "owner="); ok {
			owner = value
		}
		if value, ok := strings.CutPrefix(arg, "name="); ok {
			name = value
		}
	}
	return owner + "/" + name
}

func TestInventoryWorkersOverlapWithinBoundAndJoinBeforeFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan string, 8)
	first, second := make(chan struct{}), make(chan struct{})
	var active, maximum, calls, finished atomic.Int32
	runner := batchRunnerFunc(func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		repo := requestRepository(args)
		if hasPair(args, "-f", "query="+filesQuery) {
			if finished.Load() != 8 {
				t.Error("files requested before all inventories completed")
			}
			return repoFiles(t, repo, pendingPR(1), []*rawFile{file("last.go", "ADDED")}, 2, false, "").data, nil
		}
		now := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); now > old && !maximum.CompareAndSwap(old, now); old = maximum.Load() {
		}
		gate := first
		if calls.Add(1) > inventoryConcurrency {
			gate = second
		}
		started <- repo
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		finished.Add(1)
		return repoSummaries(t, repo, []*rawPR{pendingPR(1)}, 1, false, "").data, nil
	})
	var requests []RepositoryRequest
	for i := range 8 {
		requests = append(requests, RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i)})
	}
	done := make(chan []Result, 1)
	go func() { done <- NewProvider(runner).FetchAll(ctx, requests) }()
	for range inventoryConcurrency {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("inventory requests did not overlap")
		}
	}
	if active.Load() != inventoryConcurrency {
		t.Fatalf("active=%d", active.Load())
	}
	close(first)
	for range inventoryConcurrency {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("queued inventory requests did not start")
		}
	}
	close(second)
	select {
	case results := <-done:
		for i, result := range results {
			if result.Repo != requests[i].Repo || !result.Complete || result.Err != nil || result.InventoryRequests != 1 || result.FileRequests != 1 || !result.PRs[0].FilesComplete {
				t.Fatalf("result order or coverage changed: %+v", results)
			}
		}
	case <-ctx.Done():
		t.Fatal("workers did not join")
	}
	if maximum.Load() != inventoryConcurrency || active.Load() != 0 {
		t.Fatalf("maximum=%d active=%d", maximum.Load(), active.Load())
	}
}

func TestConcurrentAliasCompleteEvidenceWinsAndRequestsCountOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx = WithRequestBudget(ctx, 2)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	p := NewProvider(batchRunnerFunc(func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if requestRepository(args) == "acme/old-name" {
			return summaries(t, []*rawPR{pr(1)}, 2, true, "next").data, nil
		}
		return summaries(t, []*rawPR{pr(1), pr(2)}, 2, false, "").data, nil
	}))
	done := make(chan []Result, 1)
	go func() {
		done <- p.FetchAll(ctx, []RepositoryRequest{{Repo: "acme/old-name"}, {Repo: "acme/repo"}, {Repo: "ACME/REPO"}})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("aliases did not overlap")
		}
	}
	close(release)
	select {
	case results := <-done:
		requests := 0
		for _, result := range results {
			requests += result.InventoryRequests
			if result.Repo != "acme/repo" || !result.Complete || result.Err != nil || len(result.PRs) != 2 {
				t.Fatalf("incomplete alias replaced complete evidence: %+v", results)
			}
		}
		if requests != 2 || RequestCount(ctx) != 2 {
			t.Fatalf("request counts: results=%d budget=%d", requests, RequestCount(ctx))
		}
	case <-ctx.Done():
		t.Fatal("alias workers did not join")
	}
}

func TestInventorySharedFailuresStopQueuedRequests(t *testing.T) {
	for _, failure := range []error{ErrAuthentication, ErrUnavailable} {
		t.Run(failure.Error(), func(t *testing.T) {
			var calls atomic.Int32
			p := NewProvider(batchRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
				calls.Add(1)
				return nil, failure
			}))
			var requests []RepositoryRequest
			for i := range 20 {
				requests = append(requests, RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i)})
			}
			for _, result := range p.FetchAll(t.Context(), requests) {
				if result.Complete || !errors.Is(result.InventoryErr, failure) {
					t.Fatalf("shared failure lost: %+v", result)
				}
			}
			if calls.Load() > inventoryConcurrency {
				t.Fatalf("queued requests started after shared failure: %d", calls.Load())
			}
		})
	}
}

func TestConcurrentInventoryCannotExceedRefreshBudget(t *testing.T) {
	ctx := WithRequestBudget(t.Context(), 7)
	var calls atomic.Int32
	p := NewProvider(batchRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls.Add(1)
		return repoSummaries(t, requestRepository(args), nil, 0, false, "").data, nil
	}))
	var requests []RepositoryRequest
	for i := range 30 {
		requests = append(requests, RepositoryRequest{Repo: fmt.Sprintf("acme/repo%d", i)})
	}
	complete, count := 0, 0
	for _, result := range p.FetchAll(ctx, requests) {
		count += result.InventoryRequests
		if result.Complete {
			complete++
		} else if !errors.Is(result.InventoryErr, ErrBudgetExceeded) {
			t.Fatalf("missing limit explanation: %+v", result)
		}
	}
	if complete != 7 || count != 7 || calls.Load() != 7 || RequestCount(ctx) != 7 {
		t.Fatalf("budget exceeded: complete=%d count=%d calls=%d", complete, count, calls.Load())
	}
}

func TestRepositoryActiveTimeoutAndAliasAllowanceAreShared(t *testing.T) {
	ctx := WithRequestBudget(t.Context(), 20)
	primary := &repositoryAllowance{limit: 4, requests: 1, elapsed: FetchTimeout - 3*time.Second}
	alias := &repositoryAllowance{limit: 4, requests: 1, elapsed: 2 * time.Second}
	primary.include(alias)
	if timeout, err := primary.take(ctx); err != nil || timeout != time.Second {
		t.Fatalf("active timeout reset after alias: timeout=%v err=%v", timeout, err)
	}
	primary.charge(time.Second)
	if _, err := primary.take(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active timeout ignored: %v", err)
	}
	primary.elapsed = 0
	if _, err := primary.take(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.take(ctx); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("alias requests not counted: %v", err)
	}
	if RequestCount(ctx) != 2 {
		t.Fatalf("rejected request consumed global budget: %d", RequestCount(ctx))
	}
}
