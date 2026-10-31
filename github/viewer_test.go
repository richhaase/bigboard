package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCurrentLoginUsesReadOnlyProviderRequest(t *testing.T) {
	runner := &responseClient{t: t, steps: []fakeStep{{data: []byte(`{"data":{"viewer":{"login":"Example-User"},"rateLimit":{"remaining":100}}}`)}}}
	ctx := WithRequestBudget(context.Background(), 3)
	login, err := NewProvider(runner).CurrentLogin(ctx)
	if err != nil || login != "Example-User" || len(runner.calls) != 1 || RequestCount(ctx) != 1 {
		t.Fatalf("login=%q err=%v calls=%d requests=%d", login, err, len(runner.calls), RequestCount(ctx))
	}
	call := runner.calls[0]
	var payload struct {
		Query     string
		Variables map[string]any
	}
	if call.Method != "POST" || call.Endpoint != "graphql" || json.Unmarshal(call.Body, &payload) != nil || payload.Query != viewerQuery || len(payload.Variables) != 0 {
		t.Fatalf("unexpected account request: %+v", call)
	}

}

func TestCurrentLoginRejectsMissingOrMalformedAccount(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"data":{}}`,
		`{"data":{"viewer":null}}`,
		`{"data":{"viewer":{"login":""}}}`,
		`{"data":{"viewer":{"login":"org/team"}}}`,
		`{"data":{"viewer":{"login":"@user"}}}`,
		`{"data":{"viewer":{"login":"-user"}}}`,
		`{"data":{"viewer":{"login":"user\n"}}}`,
		`not JSON`,
	} {
		t.Run(body, func(t *testing.T) {
			runner := &responseClient{t: t, steps: []fakeStep{{data: []byte(body)}}}
			login, err := NewProvider(runner).CurrentLogin(context.Background())
			if login != "" || !errors.Is(err, ErrResponse) {
				t.Fatalf("login=%q err=%v", login, err)
			}
		})
	}
}

func TestCurrentLoginSupportsManagedAccounts(t *testing.T) {
	runner := &responseClient{t: t, steps: []fakeStep{{data: []byte(`{"data":{"viewer":{"login":"example_user"}}}`)}}}
	login, err := NewProvider(runner).CurrentLogin(context.Background())
	if err != nil || login != "example_user" {
		t.Fatalf("managed account login=%q err=%v", login, err)
	}
}

func TestCurrentLoginPreservesSafeFailures(t *testing.T) {
	for _, want := range []error{ErrAuthentication, ErrUnavailable, ErrRateLimited, ErrOutputLimit} {
		t.Run(want.Error(), func(t *testing.T) {
			runner := &responseClient{t: t, steps: []fakeStep{{err: want}}}
			ctx := WithRequestBudget(context.Background(), 2)
			login, err := NewProvider(runner).CurrentLogin(ctx)
			if login != "" || !errors.Is(err, want) || RequestCount(ctx) != 1 {
				t.Fatalf("login=%q err=%v requests=%d", login, err, RequestCount(ctx))
			}
		})
	}
}

func TestCurrentLoginSharesInventoryBudget(t *testing.T) {
	runner := &responseClient{t: t, steps: []fakeStep{{data: []byte(`{"data":{"viewer":{"login":"viewer"}}}`)}, summaries(t, nil, 0, false, "")}}
	p := NewProvider(runner)
	ctx := WithRequestBudget(context.Background(), 2)
	if _, err := p.CurrentLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if result := p.Fetch(ctx, "acme/repo"); result.Err != nil || !result.Complete {
		t.Fatalf("inventory=%+v", result)
	}
	if login, err := p.CurrentLogin(ctx); login != "" || !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("exhausted account lookup login=%q err=%v", login, err)
	}
	if RequestCount(ctx) != 2 || len(runner.calls) != 2 {
		t.Fatalf("requests=%d calls=%d", RequestCount(ctx), len(runner.calls))
	}
}

func TestCurrentLoginRespectsCancellationAndZeroBudget(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"canceled", WithRequestBudget(canceled, 2), context.Canceled},
		{"no allowance", WithRequestBudget(context.Background(), 0), ErrBudgetExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &responseClient{t: t}
			login, err := NewProvider(runner).CurrentLogin(tc.ctx)
			if login != "" || !errors.Is(err, tc.err) || RequestCount(tc.ctx) != 0 || len(runner.calls) != 0 {
				t.Fatalf("login=%q err=%v requests=%d calls=%d", login, err, RequestCount(tc.ctx), len(runner.calls))
			}
		})
	}
}

func TestCurrentLoginSharesProviderCooldown(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	for _, exhaustedSuccess := range []bool{false, true} {
		t.Run(fmt.Sprint(exhaustedSuccess), func(t *testing.T) {
			body := fmt.Sprintf(`{"data":{"rateLimit":{"remaining":0,"resetAt":%q}},"errors":[{"type":"RATE_LIMITED"}]}`, reset.Format(time.RFC3339))
			if exhaustedSuccess {
				body = fmt.Sprintf(`{"data":{"viewer":{"login":"viewer"},"rateLimit":{"remaining":0,"resetAt":%q}}}`, reset.Format(time.RFC3339))
			}
			runner := &responseClient{t: t, steps: []fakeStep{{data: []byte(body)}}}
			p := NewProvider(runner)
			p.now = func() time.Time { return now }
			ctx := WithRequestBudget(context.Background(), 3)
			login, err := p.CurrentLogin(ctx)
			if exhaustedSuccess && (err != nil || login != "viewer") {
				t.Fatalf("successful account lookup login=%q err=%v", login, err)
			}
			if !exhaustedSuccess && (login != "" || !errors.Is(err, ErrRateLimited)) {
				t.Fatalf("limited account lookup login=%q err=%v", login, err)
			}
			if result := p.Fetch(ctx, "acme/repo"); !errors.Is(result.Err, ErrRateLimited) || !result.RetryAt.Equal(reset) {
				t.Fatalf("inventory cooldown=%+v", result)
			}
			if _, err := p.CurrentLogin(ctx); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("account cooldown err=%v", err)
			}
			if len(runner.calls) != 1 || RequestCount(ctx) != 1 {
				t.Fatalf("calls=%d requests=%d", len(runner.calls), RequestCount(ctx))
			}
		})
	}
}
