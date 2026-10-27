package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type discoveryRunner struct {
	t     *testing.T
	steps []fakeStep
	calls []invocation
}

func (r *discoveryRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	r.t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > FetchTimeout {
		r.t.Fatal("discovery request has no bounded deadline")
	}
	r.calls = append(r.calls, invocation{command: command, args: append([]string(nil), args...)})
	if len(r.steps) == 0 {
		r.t.Fatalf("unexpected request: %s %v", command, args)
	}
	step := r.steps[0]
	r.steps = r.steps[1:]
	return step.data, step.err
}

func discoveryPage(t *testing.T, rows []map[string]any) fakeStep {
	t.Helper()
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return fakeStep{data: data}
}

func TestDiscoverRepositoriesPaginatesAndExcludesArchived(t *testing.T) {
	rows := make([]map[string]any, 100)
	want := []DiscoveredRepository{{Name: "example/last", DefaultBranch: "trunk"}}
	for i := range rows {
		name := fmt.Sprintf("example/repo-%03d", i)
		archived := i%10 == 0
		rows[i] = map[string]any{"full_name": name, "default_branch": "main", "archived": archived}
		if !archived {
			want = append(want, DiscoveredRepository{Name: name, DefaultBranch: "main"})
		}
	}
	want = append(want, DiscoveredRepository{Name: "other/api", DefaultBranch: "release/stable"})
	runner := &discoveryRunner{t: t, steps: []fakeStep{
		discoveryPage(t, rows),
		discoveryPage(t, []map[string]any{
			{"full_name": "EXAMPLE/repo-001", "default_branch": "main", "archived": false},
			{"full_name": "Example/Last", "default_branch": "trunk", "archived": false},
			{"full_name": "example/retired", "default_branch": "main", "archived": true},
		}),
		discoveryPage(t, []map[string]any{{"full_name": "Other/API", "default_branch": "release/stable", "archived": false}}),
	}}
	got, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{"EXAMPLE", "Other"})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("repositories = %+v, %v; want %+v", got, err, want)
	}
	endpoints := []string{
		"orgs/example/repos?type=all&per_page=100&page=1",
		"orgs/example/repos?type=all&per_page=100&page=2",
		"orgs/other/repos?type=all&per_page=100&page=1",
	}
	if len(runner.calls) != len(endpoints) {
		t.Fatalf("requests = %d, want %d", len(runner.calls), len(endpoints))
	}
	for i, call := range runner.calls {
		wantArgs := []string{"api", "--hostname", "github.com", "--method", "GET", endpoints[i]}
		if call.command != "gh" || !reflect.DeepEqual(call.args, wantArgs) {
			t.Errorf("request %d = %+v", i, call)
		}
	}
}

func TestDiscoverRepositoriesRejectsFailedAndMalformedResponses(t *testing.T) {
	cases := []struct {
		name string
		step fakeStep
		want error
	}{
		{"invalid-json", fakeStep{data: []byte("unfinished")}, ErrResponse},
		{"null", fakeStep{data: []byte(`null`)}, ErrResponse},
		{"object", fakeStep{data: []byte(`{"message":"not a repository list"}`)}, ErrResponse},
		{"null-row", fakeStep{data: []byte(`[null]`)}, ErrResponse},
		{"missing-name", fakeStep{data: []byte(`[{"archived":false}]`)}, ErrResponse},
		{"invalid-name", fakeStep{data: []byte(`[{"full_name":"example/../escape"}]`)}, ErrResponse},
		{"wrong-owner", fakeStep{data: []byte(`[{"full_name":"example-other/api"}]`)}, ErrResponse},
		{"wrong-archived-type", fakeStep{data: []byte(`[{"full_name":"example/api","archived":"false"}]`)}, ErrResponse},
		{"authentication", fakeStep{err: ErrAuthentication}, ErrAuthentication},
		{"unavailable", fakeStep{err: ErrUnavailable}, ErrUnavailable},
		{"rate-limit", fakeStep{err: ErrRateLimited}, ErrRateLimited},
		{"timeout", fakeStep{err: context.DeadlineExceeded}, context.DeadlineExceeded},
		{"canceled", fakeStep{err: context.Canceled}, context.Canceled},
		{"raw-error", fakeStep{err: errors.New("untrusted diagnostic")}, ErrRequest},
		{"oversized", fakeStep{data: make([]byte, MaxOutputBytes+1)}, ErrOutputLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &discoveryRunner{t: t, steps: []fakeStep{tc.step}}
			got, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{"example"})
			if got != nil || !errors.Is(err, tc.want) {
				t.Fatalf("repositories = %+v, %v; want %v", got, err, tc.want)
			}
			if strings.Contains(err.Error(), "untrusted diagnostic") {
				t.Fatal("discovery exposed an untrusted diagnostic")
			}
		})
	}
}

func TestDiscoverRepositoriesEmptyAndInvalidOrganizations(t *testing.T) {
	t.Run("empty inventory", func(t *testing.T) {
		runner := &discoveryRunner{t: t, steps: []fakeStep{{data: []byte(`[]`)}}}
		got, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{"example"})
		if err != nil || len(got) != 0 || len(runner.calls) != 1 {
			t.Fatalf("empty discovery = %+v, %v; requests = %d", got, err, len(runner.calls))
		}
	})
	for _, org := range []string{"", "../outside", "https://github.com/example", "--hostname", "example/other"} {
		t.Run(org, func(t *testing.T) {
			runner := &discoveryRunner{t: t}
			if _, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{org}); err == nil || len(runner.calls) != 0 {
				t.Fatalf("invalid organization accepted: %q, %v", org, err)
			}
		})
	}
}

func TestDiscoverRepositoriesNeverReturnsPartialInventory(t *testing.T) {
	runner := &discoveryRunner{t: t, steps: []fakeStep{
		{data: []byte(`[{"full_name":"example/api","default_branch":"main","archived":false}]`)},
		{err: ErrAuthentication},
	}}
	got, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{"example", "other"})
	if got != nil || !errors.Is(err, ErrAuthentication) || len(runner.calls) != 2 {
		t.Fatalf("partial inventory = %+v, %v; requests = %d", got, err, len(runner.calls))
	}
}

func TestDiscoverRepositoriesEnforcesRequestBudget(t *testing.T) {
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = map[string]any{"full_name": fmt.Sprintf("example/repo-%d", i), "default_branch": "main", "archived": false}
	}
	page := discoveryPage(t, rows)
	runner := &discoveryRunner{t: t, steps: make([]fakeStep, MaxRefreshRequests)}
	for i := range runner.steps {
		runner.steps[i] = page
	}
	got, err := NewProvider(runner).DiscoverRepositories(t.Context(), []string{"example"})
	if got != nil || !errors.Is(err, ErrBudgetExceeded) || len(runner.calls) != MaxRefreshRequests {
		t.Fatalf("unbounded inventory = %+v, %v; requests = %d", got, err, len(runner.calls))
	}
}

func TestRepositoryDiscoveryPreservesPushTimeWithoutMetadataFallback(t *testing.T) {
	pushed := time.Date(2025, 4, 8, 9, 10, 11, 0, time.UTC)
	page := discoveryPage(t, []map[string]any{
		{"full_name": "example/null", "default_branch": "main", "archived": false, "pushed_at": nil, "updated_at": "2026-10-27T12:00:00Z"},
		{"full_name": "example/known", "default_branch": "trunk", "archived": false, "pushed_at": pushed.Format(time.RFC3339), "updated_at": "2026-10-27T12:00:00Z"},
		{"full_name": "example/missing", "default_branch": "main", "archived": false, "updated_at": "2026-10-27T12:00:00Z"},
	})
	want := []DiscoveredRepository{
		{Name: "example/known", DefaultBranch: "trunk", PushedAt: pushed},
		{Name: "example/missing", DefaultBranch: "main"},
		{Name: "example/null", DefaultBranch: "main"},
	}
	for _, personal := range []bool{false, true} {
		t.Run(fmt.Sprintf("personal=%v", personal), func(t *testing.T) {
			steps := []fakeStep{page}
			if personal {
				steps = append([]fakeStep{{data: []byte(`{"login":"example"}`)}}, steps...)
			}
			runner := &discoveryRunner{t: t, steps: steps}
			provider := NewProvider(runner)
			var got []DiscoveredRepository
			var err error
			if personal {
				got, err = provider.DiscoverPersonalRepositories(t.Context(), "example")
			} else {
				got, err = provider.DiscoverRepositories(t.Context(), []string{"example"})
			}
			if err != nil || !reflect.DeepEqual(got, want) || len(runner.calls) != len(steps) {
				t.Fatalf("repositories=%+v err=%v calls=%d; want=%+v calls=%d", got, err, len(runner.calls), want, len(steps))
			}
		})
	}
}
