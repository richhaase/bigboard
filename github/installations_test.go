package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type discoveryStep struct {
	endpoint string
	response Response
	err      error
}
type discoveryClient struct {
	t     *testing.T
	steps []discoveryStep
	calls []Request
}

func (c *discoveryClient) Do(ctx context.Context, request Request) (Response, error) {
	c.t.Helper()
	assertDeadline(c.t, ctx)
	c.calls = append(c.calls, request)
	if len(c.steps) == 0 {
		c.t.Fatalf("unexpected request %s", request.Endpoint)
	}
	step := c.steps[0]
	c.steps = c.steps[1:]
	if request.Method != "GET" || step.endpoint != "" && request.Endpoint != step.endpoint {
		c.t.Fatalf("unexpected request %s %s; want %s", request.Method, request.Endpoint, step.endpoint)
	}
	return step.response, step.err
}

type discoveryClientFunc func(context.Context, Request) (Response, error)

func (f discoveryClientFunc) Do(ctx context.Context, request Request) (Response, error) {
	return f(ctx, request)
}

func discoveryJSON(t *testing.T, value any) discoveryStep {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return discoveryStep{response: Response{StatusCode: 200, Body: data}}
}
func installationRow(id int, login, kind string) map[string]any {
	return map[string]any{"id": id, "account": map[string]any{"login": login, "type": kind}}
}
func installationPage(t *testing.T, total int, rows ...map[string]any) discoveryStep {
	if rows == nil {
		rows = []map[string]any{}
	}
	return discoveryJSON(t, map[string]any{"total_count": total, "installations": rows})
}
func repositoryPage(t *testing.T, total int, rows ...map[string]any) discoveryStep {
	if rows == nil {
		rows = []map[string]any{}
	}
	return discoveryJSON(t, map[string]any{"total_count": total, "repositories": rows})
}
func repositoryRow(name string) map[string]any {
	return map[string]any{"full_name": name, "default_branch": "main", "archived": false}
}

func TestAccessibleDiscoveryPaginatesInstallationsAndRepositories(t *testing.T) {
	installed := make([]map[string]any, 100)
	for i := range installed {
		installed[i] = installationRow(i+1, fmt.Sprintf("organization-%03d", i), "Organization")
	}
	client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 101, installed...), installationPage(t, 101, installationRow(101, "Example", "Organization"))}}
	for range 100 {
		client.steps = append(client.steps, repositoryPage(t, 0))
	}
	rows := make([]map[string]any, 100)
	want := []DiscoveredRepository{{Name: "example/last", DefaultBranch: "trunk"}}
	for i := range rows {
		name := fmt.Sprintf("example/repo-%03d", i)
		rows[i] = repositoryRow(name)
		rows[i]["archived"] = i%10 == 0
		rows[i]["private"] = i%2 == 0
		rows[i]["fork"] = i%3 == 0
		if i%10 != 0 {
			want = append(want, DiscoveredRepository{Name: name, DefaultBranch: "main"})
		}
	}
	last := repositoryRow("EXAMPLE/Last")
	last["default_branch"] = "trunk"
	client.steps = append(client.steps, repositoryPage(t, 101, rows...), repositoryPage(t, 101, last))
	got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("repositories=%+v err=%v", got, err)
	}
	if len(client.calls) != 104 || client.calls[0].Endpoint != "user/installations?per_page=100&page=1" || client.calls[1].Endpoint != "user/installations?per_page=100&page=2" || client.calls[103].Endpoint != "user/installations/101/repositories?per_page=100&page=2" {
		t.Fatalf("incorrect pagination: calls=%d", len(client.calls))
	}
}

func TestAccessibleDiscoveryRejectsMalformedAndIncompleteCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		repos      bool
		want       error
	}{
		{"invalid JSON", "unfinished", false, ErrResponse},
		{"missing count", `{"installations":[]}`, false, ErrResponse},
		{"null installations", `{"total_count":0,"installations":null}`, false, ErrResponse},
		{"negative count", `{"total_count":-1,"installations":[]}`, false, ErrResponse},
		{"null installation", `{"total_count":1,"installations":[null]}`, false, ErrResponse},
		{"invalid ID", `{"total_count":1,"installations":[{"id":0,"account":{"login":"example","type":"User"}}]}`, false, ErrResponse},
		{"invalid account", `{"total_count":1,"installations":[{"id":1,"account":{"login":"../outside","type":"User"}}]}`, false, ErrResponse},
		{"unknown account type", `{"total_count":1,"installations":[{"id":1,"account":{"login":"example","type":"Unknown"}}]}`, false, ErrResponse},
		{"truncated installations", `{"total_count":2,"installations":[{"id":1,"account":{"login":"example","type":"User"}}]}`, false, ErrIncomplete},
		{"null repositories", `{"total_count":0,"repositories":null}`, true, ErrResponse},
		{"missing repositories", `{"total_count":0}`, true, ErrResponse},
		{"null repository", `{"total_count":1,"repositories":[null]}`, true, ErrResponse},
		{"invalid repository", `{"total_count":1,"repositories":[{"full_name":"example/../outside"}]}`, true, ErrResponse},
		{"wrong owner", `{"total_count":1,"repositories":[{"full_name":"other/repo"}]}`, true, ErrResponse},
		{"invalid archived", `{"total_count":1,"repositories":[{"full_name":"example/repo","archived":"false"}]}`, true, ErrResponse},
		{"truncated repositories", `{"total_count":2,"repositories":[{"full_name":"example/repo"}]}`, true, ErrIncomplete},
		{"duplicate repositories", `{"total_count":2,"repositories":[{"full_name":"example/repo"},{"full_name":"EXAMPLE/REPO"}]}`, true, ErrIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := []discoveryStep{}
			if tc.repos {
				steps = append(steps, installationPage(t, 1, installationRow(1, "example", "User")))
			}
			steps = append(steps, discoveryStep{response: Response{StatusCode: 200, Body: []byte(tc.body)}})
			client := &discoveryClient{t: t, steps: steps}
			got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
			if got != nil || !errors.Is(err, tc.want) {
				t.Fatalf("partial or invalid inventory accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestAccessibleDiscoveryRejectsChangingPageCounts(t *testing.T) {
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = repositoryRow(fmt.Sprintf("example/repo-%d", i))
	}
	client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 1, installationRow(1, "example", "User")), repositoryPage(t, 101, rows...), repositoryPage(t, 102, repositoryRow("example/last"), repositoryRow("example/new"))}}
	got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
	if got != nil || !errors.Is(err, ErrIncomplete) {
		t.Fatalf("changing inventory=%+v err=%v", got, err)
	}
}

func TestAccessibleDiscoveryNeverReturnsPartialInventoryOnFailure(t *testing.T) {
	for _, failure := range []error{ErrAuthentication, ErrPermission, ErrUnavailable, ErrRateLimited, context.Canceled, context.DeadlineExceeded, errors.New("untrusted diagnostic")} {
		client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 2, installationRow(1, "example", "User"), installationRow(2, "other", "Organization")), repositoryPage(t, 1, repositoryRow("example/private")), {err: failure}}}
		got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
		if got != nil || err == nil || strings.Contains(err.Error(), "untrusted diagnostic") {
			t.Fatalf("partial inventory or unsafe error: %+v, %v", got, err)
		}
		if want := safeRequestError(failure); !errors.Is(err, want) {
			t.Fatalf("error=%v want=%v", err, want)
		}
	}
}

func TestAccessibleDiscoveryBoundsAllInstallationAndRepositoryRequests(t *testing.T) {
	calls := 0
	client := discoveryClientFunc(func(ctx context.Context, request Request) (Response, error) {
		calls++
		if strings.HasPrefix(request.Endpoint, "user/installations?") {
			page, _ := strconv.Atoi(strings.Split(request.Endpoint, "&page=")[1])
			start := (page - 1) * 100
			rows := []map[string]any{}
			for i := start; i < min(start+100, MaxRefreshRequests); i++ {
				rows = append(rows, installationRow(i+1, fmt.Sprintf("organization-%03d", i), "Organization"))
			}
			return installationPage(t, MaxRefreshRequests, rows...).response, nil
		}
		return repositoryPage(t, 0).response, nil
	})
	got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
	if got != nil || !errors.Is(err, ErrBudgetExceeded) || calls != MaxRefreshRequests {
		t.Fatalf("request cap: count=%d repositories=%+v error=%v", calls, got, err)
	}
}

func TestAccessibleDiscoveryHandlesEmptyAccessAndCancellation(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 0)}}
	got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty access=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client = &discoveryClient{t: t}
	if got, err := NewProvider(client).DiscoverAccessibleRepositories(ctx); got != nil || !errors.Is(err, context.Canceled) || len(client.calls) != 0 {
		t.Fatalf("canceled discovery=%+v err=%v", got, err)
	}
}

func TestAccessibleDiscoveryPreservesPushTimeWithoutMetadataFallback(t *testing.T) {
	pushed := time.Date(2025, 4, 8, 9, 10, 11, 0, time.UTC)
	known := repositoryRow("example/known")
	known["pushed_at"] = pushed.Format(time.RFC3339)
	missing := repositoryRow("example/missing")
	missing["updated_at"] = "2026-10-27T12:00:00Z"
	null := repositoryRow("example/null")
	null["pushed_at"] = nil
	null["updated_at"] = "2026-10-27T12:00:00Z"
	client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 1, installationRow(1, "example", "User")), repositoryPage(t, 3, null, known, missing)}}
	got, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context())
	want := []DiscoveredRepository{{Name: "example/known", DefaultBranch: "main", PushedAt: pushed}, {Name: "example/missing", DefaultBranch: "main"}, {Name: "example/null", DefaultBranch: "main"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("push times=%+v err=%v", got, err)
	}
}

func TestAccessibleDiscoveryPreservesSafeStatusFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{{401, ErrAuthentication}, {403, ErrPermission}, {404, ErrRepository}, {429, ErrRateLimited}} {
		client := &discoveryClient{t: t, steps: []discoveryStep{{response: Response{StatusCode: tc.status, Header: http.Header{}, Body: []byte(`{"message":"private diagnostic"}`)}}}}
		if _, err := NewProvider(client).DiscoverAccessibleRepositories(t.Context()); !errors.Is(err, tc.want) {
			t.Fatalf("status %d error=%v", tc.status, err)
		}
	}
}
