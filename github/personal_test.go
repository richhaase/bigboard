package github

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverPersonalRepositoriesPaginatesOwnedPublicPrivateAndForks(t *testing.T) {
	rows := make([]map[string]any, 100)
	want := []DiscoveredRepository{{Name: "example/last", DefaultBranch: "trunk"}}
	for i := range rows {
		number := len(rows) - 1 - i
		rows[i] = map[string]any{
			"full_name": fmt.Sprintf("Example/repo-%03d", number), "default_branch": "main",
			"archived": number%10 == 0, "private": number%2 == 0, "fork": number%3 == 0,
		}
		if i%10 != 0 {
			want = append(want, DiscoveredRepository{Name: fmt.Sprintf("example/repo-%03d", i), DefaultBranch: "main"})
		}
	}
	runner := &fakeRunner{t: t, steps: []fakeStep{
		includedStep(`{"login":"EXAMPLE"}`),
		discoveryPage(t, rows),
		discoveryPage(t, []map[string]any{
			{"full_name": "EXAMPLE/repo-001", "default_branch": "main", "archived": false},
			{"full_name": "Example/Last", "default_branch": "trunk", "archived": false, "private": true, "fork": true},
			{"full_name": "example/retired", "default_branch": "main", "archived": true},
		}),
	}}
	ctx := WithRequestBudget(t.Context(), 0)
	got, err := NewProvider(runner).DiscoverPersonalRepositories(ctx, "ExAmPlE")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("repositories=%+v err=%v; want=%+v", got, err, want)
	}
	endpoints := []string{
		"user",
		"user/repos?affiliation=owner&visibility=all&per_page=100&page=1",
		"user/repos?affiliation=owner&visibility=all&per_page=100&page=2",
	}
	if len(runner.calls) != len(endpoints) || RequestCount(ctx) != 0 {
		t.Fatalf("calls=%d refresh requests=%d", len(runner.calls), RequestCount(ctx))
	}
	for i, call := range runner.calls {
		wantArgs := []string{"api", "--hostname", "github.com", "--method", "GET", "--include", endpoints[i]}
		if call.command != "gh" || !reflect.DeepEqual(call.args, wantArgs) {
			t.Errorf("request %d=%+v", i, call)
		}
	}
}

func TestDiscoverPersonalRepositoriesRejectsDifferentSignedInAccount(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{includedStep(`{"login":"other-user"}`)}}
	got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "saved-user")
	if got != nil || err == nil || !strings.Contains(err.Error(), "other-user") || !strings.Contains(err.Error(), "saved-user") || !strings.Contains(err.Error(), "gh auth switch") || len(runner.calls) != 1 {
		t.Fatalf("account mismatch repositories=%+v err=%v calls=%d", got, err, len(runner.calls))
	}
}

func TestDiscoverPersonalRepositoriesEmptyAndInvalidAccounts(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{includedStep(`{"login":"example"}`), includedStep(`[]`)}}
	got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "example")
	if err != nil || len(got) != 0 || len(runner.calls) != 2 {
		t.Fatalf("empty inventory=%+v err=%v calls=%d", got, err, len(runner.calls))
	}
	for _, account := range []string{"", "../outside", "https://github.com/example", "--hostname", "example/other", "example\n"} {
		t.Run(account, func(t *testing.T) {
			runner := &fakeRunner{t: t}
			if got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), account); got != nil || err == nil || len(runner.calls) != 0 {
				t.Fatalf("invalid account=%q repositories=%+v err=%v", account, got, err)
			}
		})
	}
}

func TestDiscoverPersonalRepositoriesRejectsMalformedAccountAndInventory(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `[]`, `not JSON`, `{"login":123}`, `{"login":""}`, `{"login":"other\n"}`} {
		t.Run("user "+body, func(t *testing.T) {
			runner := &fakeRunner{t: t, steps: []fakeStep{includedStep(body)}}
			got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "example")
			if got != nil || !errors.Is(err, ErrResponse) || len(runner.calls) != 1 {
				t.Fatalf("repositories=%+v err=%v calls=%d", got, err, len(runner.calls))
			}
		})
	}
	for _, body := range []string{
		`{}`, `null`, `not JSON`, `[null]`, `[{}]`,
		`[{"full_name":"example/../outside"}]`, `[{"full_name":"other/repo"}]`,
		`[{"full_name":"example-other/repo"}]`, `[{"full_name":"example/repo","archived":"false"}]`,
		`[{"full_name":"other/repo","archived":true}]`,
	} {
		t.Run("repositories "+body, func(t *testing.T) {
			runner := &fakeRunner{t: t, steps: []fakeStep{includedStep(`{"login":"example"}`), includedStep(body)}}
			got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "example")
			if got != nil || !errors.Is(err, ErrResponse) {
				t.Fatalf("repositories=%+v err=%v", got, err)
			}
		})
	}
}

func TestDiscoverPersonalRepositoriesPreservesSafeFailuresAndOutputLimit(t *testing.T) {
	for _, stage := range []string{"user", "repositories"} {
		for _, tc := range []struct {
			name string
			step fakeStep
			want error
		}{
			{"authentication", fakeStep{err: ErrAuthentication}, ErrAuthentication},
			{"unavailable", fakeStep{err: ErrUnavailable}, ErrUnavailable},
			{"rate-limit", fakeStep{err: ErrRateLimited}, ErrRateLimited},
			{"timeout", fakeStep{err: context.DeadlineExceeded}, context.DeadlineExceeded},
			{"canceled", fakeStep{err: context.Canceled}, context.Canceled},
			{"raw-error", fakeStep{err: errors.New("untrusted diagnostic")}, ErrRequest},
			{"oversized", fakeStep{data: make([]byte, MaxOutputBytes+1)}, ErrOutputLimit},
		} {
			t.Run(stage+" "+tc.name, func(t *testing.T) {
				steps := []fakeStep{tc.step}
				if stage == "repositories" {
					steps = append([]fakeStep{includedStep(`{"login":"example"}`)}, steps...)
				}
				runner := &fakeRunner{t: t, steps: steps}
				got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "example")
				if got != nil || !errors.Is(err, tc.want) || strings.Contains(err.Error(), "untrusted diagnostic") {
					t.Fatalf("repositories=%+v err=%v", got, err)
				}
			})
		}
	}
}

func TestDiscoverPersonalRepositoriesBoundsRequestsAndReturnsNoPartialInventory(t *testing.T) {
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = map[string]any{"full_name": fmt.Sprintf("example/repo-%03d", i), "default_branch": "main", "archived": false}
	}
	page := discoveryPage(t, rows)
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			steps := []fakeStep{includedStep(`{"login":"example"}`), page}
			want, calls := ErrAuthentication, 3
			if limited {
				want, calls = ErrBudgetExceeded, MaxRefreshRequests
				for len(steps) < MaxRefreshRequests {
					steps = append(steps, page)
				}
			} else {
				steps = append(steps, fakeStep{err: ErrAuthentication})
			}
			runner := &fakeRunner{t: t, steps: steps}
			got, err := NewProvider(runner).DiscoverPersonalRepositories(t.Context(), "example")
			if got != nil || !errors.Is(err, want) || len(runner.calls) != calls {
				t.Fatalf("repositories=%+v err=%v calls=%d", got, err, len(runner.calls))
			}
		})
	}
}

func TestDiscoverPersonalRepositoriesStopsBeforeRequestWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner := &fakeRunner{t: t}
	got, err := NewProvider(runner).DiscoverPersonalRepositories(ctx, "example")
	if got != nil || !errors.Is(err, context.Canceled) || len(runner.calls) != 0 {
		t.Fatalf("repositories=%+v err=%v calls=%d", got, err, len(runner.calls))
	}
}
