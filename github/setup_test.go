package github

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSetupAccountDerivesOrganizationsFromPaginatedInstallations(t *testing.T) {
	rows := make([]map[string]any, 100)
	want := make([]string, 100)
	for i := range rows {
		login := fmt.Sprintf("Example-%03d", 99-i)
		rows[i] = installationRow(i+1, login, "Organization")
		want[99-i] = strings.ToLower(login)
	}
	want = append(want, "last")
	client := &discoveryClient{t: t, steps: []discoveryStep{
		discoveryJSON(t, map[string]any{"login": "Example-User"}),
		installationPage(t, 102, rows...),
		installationPage(t, 102, installationRow(101, "Example-User", "User"), installationRow(102, "LAST", "Organization")),
	}}
	ctx := WithRequestBudget(t.Context(), 0)
	got, err := NewProvider(client).SetupAccount(ctx)
	if err != nil || got.Login != "Example-User" || !reflect.DeepEqual(got.Organizations, want) || RequestCount(ctx) != 0 {
		t.Fatalf("account=%+v err=%v", got, err)
	}
	endpoints := []string{"user", "user/installations?per_page=100&page=1", "user/installations?per_page=100&page=2"}
	for i, call := range client.calls {
		if call.Endpoint != endpoints[i] {
			t.Fatalf("unexpected account endpoint: %s", call.Endpoint)
		}
	}
}

func TestSetupAccountAllowsEmptyOrganizationsAndManagedLogin(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{discoveryJSON(t, map[string]any{"login": "Example_User"}), installationPage(t, 0)}}
	got, err := NewProvider(client).SetupAccount(t.Context())
	if err != nil || got.Login != "Example_User" || got.Organizations == nil || len(got.Organizations) != 0 {
		t.Fatalf("account=%+v err=%v", got, err)
	}
}

func TestSetupAccountRejectsMalformedIdentityAndInstallationFailure(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `[]`, `not JSON`, `{"login":123}`, `{"login":""}`, `{"login":"org/team"}`, `{"login":"@user"}`, `{"login":"-user"}`, `{"login":"user\n"}`} {
		client := &discoveryClient{t: t, steps: []discoveryStep{{response: Response{StatusCode: 200, Body: []byte(body)}}}}
		got, err := NewProvider(client).SetupAccount(t.Context())
		if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrResponse) {
			t.Fatalf("malformed account=%+v err=%v", got, err)
		}
	}
	for _, failure := range []error{ErrAuthentication, ErrUnavailable, ErrRateLimited, ErrOutputLimit, context.Canceled, context.DeadlineExceeded, ErrRequest} {
		client := &discoveryClient{t: t, steps: []discoveryStep{discoveryJSON(t, map[string]any{"login": "example"}), {err: failure}}}
		got, err := NewProvider(client).SetupAccount(t.Context())
		if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, failure) {
			t.Fatalf("failed account=%+v err=%v", got, err)
		}
	}
}

func TestSetupAccountBoundsInstallationPagination(t *testing.T) {
	calls := 0
	client := discoveryClientFunc(func(ctx context.Context, request Request) (Response, error) {
		calls++
		if request.Endpoint == "user" {
			return discoveryJSON(t, map[string]any{"login": "example"}).response, nil
		}
		page, _ := strconv.Atoi(strings.Split(request.Endpoint, "&page=")[1])
		rows := make([]map[string]any, 100)
		for i := range rows {
			n := (page-1)*100 + i
			rows[i] = installationRow(n+1, fmt.Sprintf("org-%d", n), "Organization")
		}
		return installationPage(t, maxSetupRequests*100, rows...).response, nil
	})
	got, err := NewProvider(client).SetupAccount(t.Context())
	if !reflect.DeepEqual(got, Account{}) || !errors.Is(err, ErrBudgetExceeded) || calls != maxSetupRequests {
		t.Fatalf("unbounded setup=%+v err=%v calls=%d", got, err, calls)
	}
}

func TestSetupAccountRespectsCancellationAndTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &discoveryClient{t: t}
	if got, err := NewProvider(client).SetupAccount(ctx); !reflect.DeepEqual(got, Account{}) || !errors.Is(err, context.Canceled) || len(client.calls) != 0 {
		t.Fatalf("canceled account=%+v err=%v", got, err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	waiting := discoveryClientFunc(func(ctx context.Context, _ Request) (Response, error) { <-ctx.Done(); return Response{}, ctx.Err() })
	if got, err := NewProvider(waiting).SetupAccount(ctx); !reflect.DeepEqual(got, Account{}) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out account=%+v err=%v", got, err)
	}
}
