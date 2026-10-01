package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQuotaCooldownAcrossRepositoriesAndCancellation(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, headers, body string
		err                 error
		delay               time.Duration
		success             bool
	}{
		{name: "429", headers: "HTTP/2.0 429 Too Many Requests\r\nRetry-After: 120\r\n", body: `{"message":"Too Many Requests"}`, err: ErrRequest, delay: 2 * time.Minute},
		{name: "secondary-date", headers: "HTTP/2.0 403 Forbidden\r\nRetry-After: " + now.Add(3*time.Minute).Format(time.RFC1123) + "\r\n", body: `{"errors":[{"type":"RATE_LIMITED"}]}`, err: ErrRequest, delay: 3 * time.Minute},
		{name: "secondary-rest-body", headers: "HTTP/2.0 403 Forbidden\r\nRetry-After: 120\r\n", body: `{ "message": "slow down" }`, err: context.Canceled, delay: 2 * time.Minute},
		{name: "primary", headers: fmt.Sprintf("HTTP/2.0 200 OK\r\nx-ratelimit-remaining: 0\r\nx-ratelimit-reset: %d\r\n", now.Add(time.Hour).Unix()), body: `{"errors":[{"type":"RATE_LIMITED"}]}`, err: ErrRequest, delay: time.Hour},
		{name: "missing-headers", body: `{"errors":[{"type":"RATE_LIMITED"}]}`, err: ErrRequest, delay: time.Minute},
		{name: "successful-exhaustion", headers: fmt.Sprintf("HTTP/2.0 200 OK\r\nx-ratelimit-remaining: 0\r\nx-ratelimit-reset: %d\r\n", now.Add(time.Hour).Unix()), delay: time.Hour, success: true},
		{name: "body-reset", body: fmt.Sprintf(`{"data":{"rateLimit":{"remaining":0,"resetAt":%q}},"errors":[{"type":"RATE_LIMITED"}]}`, now.Add(40*time.Minute).Format(time.RFC3339)), err: ErrRateLimited, delay: 40 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := now
			body := []byte(tc.body)
			if tc.success {
				body = summaries(t, nil, 0, false, "").data
			}
			if tc.headers != "" {
				body = append([]byte(tc.headers+"\r\n"), body...)
			}
			runner := &fakeRunner{t: t, steps: []fakeStep{{data: body, err: tc.err}, summaries(t, nil, 0, false, "")}}
			p := NewProvider(runner)
			p.now = func() time.Time { return clock }
			ctx, cancel := context.WithCancel(context.Background())
			first := p.Fetch(ctx, "acme/repo")
			cancel()
			if !first.RetryAt.Equal(now.Add(tc.delay)) {
				t.Fatalf("retry=%v want=%v", first.RetryAt, now.Add(tc.delay))
			}
			if tc.success && (!first.Complete || first.Err != nil) {
				t.Fatalf("successful last page lost: %+v", first)
			}
			if !tc.success && !errors.Is(first.Err, ErrRateLimited) {
				t.Fatalf("missing rate limit error: %v", first.Err)
			}
			if got := p.Fetch(context.Background(), "acme/other"); !errors.Is(got.Err, ErrRateLimited) || len(runner.calls) != 1 {
				t.Fatalf("cooldown ignored: %+v calls=%d", got, len(runner.calls))
			}
			clock = first.RetryAt.Add(time.Second)
			if got := p.Fetch(context.Background(), "acme/repo"); got.Err != nil || len(runner.calls) != 2 {
				t.Fatalf("did not resume: %+v", got)
			}
			if !hasPair(runner.calls[0].args, "--method", "POST") || !strings.Contains(strings.Join(runner.calls[0].args, " "), "--include") {
				t.Fatal("quota headers not requested")
			}
		})
	}
}

func TestSecondaryBackoffIsBoundedAndResetsOnSuccess(t *testing.T) {
	clock := time.Now()
	runner := &fakeRunner{t: t}
	p := NewProvider(runner)
	p.now = func() time.Time { return clock }
	for i := 0; i < 7; i++ {
		runner.steps = append(runner.steps, fakeStep{err: ErrRateLimited})
		got := p.Fetch(context.Background(), "acme/repo")
		want := time.Minute * time.Duration(1<<min(i, 4))
		if got.RetryAt.Sub(clock) != want {
			t.Fatalf("attempt %d delay %v", i, got.RetryAt.Sub(clock))
		}
		clock = got.RetryAt.Add(time.Second)
	}
	runner.steps = append(runner.steps, summaries(t, nil, 0, false, ""), fakeStep{err: ErrRateLimited})
	p.Fetch(context.Background(), "acme/repo")
	if got := p.Fetch(context.Background(), "acme/repo"); got.RetryAt.Sub(clock) != time.Minute {
		t.Fatal("backoff did not reset")
	}
}

func TestRefreshBudgetSharedAcrossRepositories(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, nil, 0, false, ""), summaries(t, nil, 0, false, "")}}
	p := NewProvider(runner)
	ctx := WithRequestBudget(context.Background(), 2)
	for i := 0; i < 4; i++ {
		got := p.Fetch(ctx, fmt.Sprintf("acme/repo%d", i))
		if i < 2 && got.Err != nil {
			t.Fatal(got.Err)
		}
		if i >= 2 && (!errors.Is(got.Err, ErrBudgetExceeded) || got.Complete) {
			t.Fatalf("false completion %+v", got)
		}
	}
	if len(runner.calls) != 2 {
		t.Fatal("global budget exceeded")
	}
}
func TestRequestBudgetConcurrentConsumers(t *testing.T) {
	ctx := WithRequestBudget(context.Background(), 17)
	var wg sync.WaitGroup
	var mu sync.Mutex
	calls := 0
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			if takeRequest(ctx) {
				mu.Lock()
				calls++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if calls != 17 {
		t.Fatal(calls)
	}
}
func TestRateLimitClassifier429(t *testing.T) {
	if !errors.Is(classifyFailure("HTTP 429: Too Many Requests secret"), ErrRateLimited) {
		t.Fatal("429 not recognized")
	}
}
