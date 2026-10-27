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

func setupOrganizations(t *testing.T, logins []string) fakeStep {
	t.Helper()
	organizations := make([]actor, len(logins))
	for i, login := range logins {
		organizations[i] = actor{Login: login}
	}
	data, err := json.Marshal(organizations)
	if err != nil {
		t.Fatal(err)
	}
	return fakeStep{data: data}
}

func TestSetupAccountPaginatesCanonicalOrganizations(t *testing.T) {
	logins := make([]string, 100)
	for i := range logins {
		logins[i] = fmt.Sprintf("Example-%03d", len(logins)-1-i)
	}
	want := make([]string, len(logins)+1)
	for i := range logins {
		want[i] = fmt.Sprintf("example-%03d", i)
	}
	want[len(logins)] = "last"
	runner := &fakeRunner{t: t, steps: []fakeStep{
		{data: []byte(`{"login":"Example-User"}`)},
		setupOrganizations(t, logins),
		setupOrganizations(t, []string{"LAST", "example-001", "Last"}),
	}}
	ctx := WithRequestBudget(t.Context(), 0)
	got, err := NewProvider(runner).SetupAccount(ctx)
	if err != nil || got.Login != "Example-User" || !reflect.DeepEqual(got.Organizations, want) {
		t.Fatalf("account=%+v err=%v", got, err)
	}
	endpoints := []string{"user", "user/orgs?per_page=100&page=1", "user/orgs?per_page=100&page=2"}
	if len(runner.calls) != len(endpoints) || RequestCount(ctx) != 0 {
		t.Fatalf("calls=%d refresh requests=%d", len(runner.calls), RequestCount(ctx))
	}
	for i, call := range runner.calls {
		wantArgs := []string{"api", "--hostname", "github.com", "--method", "GET", endpoints[i]}
		if call.command != "gh" || !reflect.DeepEqual(call.args, wantArgs) {
			t.Errorf("call %d=%+v", i, call)
		}
	}
}

func TestSetupAccountAllowsEmptyOrganizationsAndManagedLogin(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{
		{data: []byte(`{"login":"Example_User"}`)},
		{data: []byte(`[]`)},
	}}
	got, err := NewProvider(runner).SetupAccount(t.Context())
	if err != nil || got.Login != "Example_User" || got.Organizations == nil || len(got.Organizations) != 0 {
		t.Fatalf("account=%+v err=%v", got, err)
	}
}

func TestSetupAccountRejectsMalformedResponses(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, `not JSON`, `{"login":123}`, `{"login":""}`,
		`{"login":"org/team"}`, `{"login":"@user"}`, `{"login":"-user"}`,
		`{"login":"user\n"}`, `{"login":"user\u001b[0m"}`,
	} {
		t.Run("user "+body, func(t *testing.T) {
			runner := &fakeRunner{t: t, steps: []fakeStep{{data: []byte(body)}}}
			got, err := NewProvider(runner).SetupAccount(t.Context())
			if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrResponse) || len(runner.calls) != 1 {
				t.Fatalf("account=%+v err=%v calls=%d", got, err, len(runner.calls))
			}
		})
	}
	for _, body := range []string{
		`{}`, `null`, `not JSON`, `[null]`, `[{}]`, `[{"login":123}]`,
		`[{"login":""}]`, `[{"login":"org/repo"}]`, `[{"login":"../org"}]`,
		`[{"login":"--hostname"}]`, `[{"login":"org\n"}]`, `[{"login":"org\u001b[0m"}]`,
	} {
		t.Run("organizations "+body, func(t *testing.T) {
			runner := &fakeRunner{t: t, steps: []fakeStep{
				{data: []byte(`{"login":"viewer"}`)},
				{data: []byte(body)},
			}}
			got, err := NewProvider(runner).SetupAccount(t.Context())
			if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrResponse) {
				t.Fatalf("account=%+v err=%v", got, err)
			}
		})
	}
}

func TestSetupAccountPreservesSafeFailures(t *testing.T) {
	for _, stage := range []string{"user", "organizations"} {
		for _, want := range []error{ErrAuthentication, ErrUnavailable, ErrRateLimited, ErrOutputLimit, context.Canceled, context.DeadlineExceeded, ErrRequest} {
			t.Run(stage+" "+want.Error(), func(t *testing.T) {
				failure := want
				if want == ErrRequest {
					failure = errors.New("untrusted diagnostic")
				}
				steps := []fakeStep{{err: failure}}
				if stage == "organizations" {
					steps = append([]fakeStep{{data: []byte(`{"login":"viewer"}`)}}, steps...)
				}
				runner := &fakeRunner{t: t, steps: steps}
				got, err := NewProvider(runner).SetupAccount(t.Context())
				if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, want) || strings.Contains(err.Error(), "untrusted diagnostic") {
					t.Fatalf("account=%+v err=%v", got, err)
				}
			})
		}
	}
}

func TestSetupAccountRejectsOversizedOutput(t *testing.T) {
	for _, stage := range []string{"user", "organizations"} {
		t.Run(stage, func(t *testing.T) {
			steps := []fakeStep{{data: make([]byte, MaxOutputBytes+1)}}
			if stage == "organizations" {
				steps = append([]fakeStep{{data: []byte(`{"login":"viewer"}`)}}, steps...)
			}
			runner := &fakeRunner{t: t, steps: steps}
			got, err := NewProvider(runner).SetupAccount(t.Context())
			if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("account=%+v err=%v", got, err)
			}
		})
	}
}

func TestSetupAccountBoundsRequestsWithoutPartialInventory(t *testing.T) {
	logins := make([]string, 100)
	for i := range logins {
		logins[i] = fmt.Sprintf("example-%d", i)
	}
	runner := &fakeRunner{t: t, steps: []fakeStep{{data: []byte(`{"login":"viewer"}`)}}}
	for i := 1; i < maxSetupRequests; i++ {
		runner.steps = append(runner.steps, setupOrganizations(t, logins))
	}
	got, err := NewProvider(runner).SetupAccount(t.Context())
	if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrBudgetExceeded) || len(runner.calls) != maxSetupRequests {
		t.Fatalf("account=%+v err=%v calls=%d", got, err, len(runner.calls))
	}
}

type setupTimeoutRunner struct {
	t *testing.T
}

func (r setupTimeoutRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	assertDeadline(r.t, ctx)
	<-ctx.Done()
	return []byte(`{"login":"viewer"}`), nil
}

func TestSetupAccountRespectsCancellationAndTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner := &fakeRunner{t: t}
	got, err := NewProvider(runner).SetupAccount(ctx)
	if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, context.Canceled) || len(runner.calls) != 0 {
		t.Fatalf("canceled account=%+v err=%v calls=%d", got, err, len(runner.calls))
	}
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	got, err = NewProvider(setupTimeoutRunner{t: t}).SetupAccount(ctx)
	if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out account=%+v err=%v", got, err)
	}
}
