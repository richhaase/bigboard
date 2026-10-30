package github

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverPersonalRepositoriesFiltersInstalledOwnedRepositories(t *testing.T) {
	private := repositoryRow("EXAMPLE/private")
	private["private"] = true
	private["fork"] = true
	archived := repositoryRow("example/archived")
	archived["archived"] = true
	client := &discoveryClient{t: t, steps: []discoveryStep{
		discoveryJSON(t, map[string]any{"login": "EXAMPLE"}),
		installationPage(t, 2, installationRow(1, "example", "User"), installationRow(2, "other", "Organization")),
		repositoryPage(t, 3, private, archived, repositoryRow("example/public")),
	}}
	ctx := WithRequestBudget(t.Context(), 0)
	got, err := NewProvider(client).DiscoverPersonalRepositories(ctx, "ExAmPlE")
	want := []DiscoveredRepository{{Name: "example/private", DefaultBranch: "main"}, {Name: "example/public", DefaultBranch: "main"}}
	if err != nil || !reflect.DeepEqual(got, want) || RequestCount(ctx) != 0 || len(client.calls) != 3 {
		t.Fatalf("personal repositories=%+v err=%v budget=%d", got, err, RequestCount(ctx))
	}
	if client.calls[0].Endpoint != "user" {
		t.Fatal("personal discovery did not verify current user")
	}
}

func TestDiscoverPersonalRepositoriesRejectsDifferentAccount(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{discoveryJSON(t, map[string]any{"login": "other-user"})}}
	got, err := NewProvider(client).DiscoverPersonalRepositories(t.Context(), "saved-user")
	if got != nil || err == nil || !strings.Contains(err.Error(), "other-user") || !strings.Contains(err.Error(), "saved-user") || len(client.calls) != 1 {
		t.Fatalf("account mismatch=%+v err=%v", got, err)
	}
}

func TestDiscoverPersonalRepositoriesRejectsInvalidAccountsAndMalformedIdentity(t *testing.T) {
	for _, account := range []string{"", "../outside", "https://github.com/example", "--hostname", "example/other", "example\n"} {
		client := &discoveryClient{t: t}
		if got, err := NewProvider(client).DiscoverPersonalRepositories(t.Context(), account); got != nil || err == nil || len(client.calls) != 0 {
			t.Fatalf("invalid account=%q repositories=%+v err=%v", account, got, err)
		}
	}
	for _, body := range []string{`{}`, `null`, `[]`, `not JSON`, `{"login":123}`, `{"login":""}`, `{"login":"other\n"}`} {
		client := &discoveryClient{t: t, steps: []discoveryStep{{response: Response{StatusCode: 200, Body: []byte(body)}}}}
		if got, err := NewProvider(client).DiscoverPersonalRepositories(t.Context(), "example"); got != nil || !errors.Is(err, ErrResponse) {
			t.Fatalf("malformed identity=%+v err=%v", got, err)
		}
	}
}

func TestDiscoverPersonalRepositoriesDoesNotHideUnavailableInstallations(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{discoveryJSON(t, map[string]any{"login": "example"}), {err: ErrAuthentication}}}
	if got, err := NewProvider(client).DiscoverPersonalRepositories(t.Context(), "example"); got != nil || !errors.Is(err, ErrAuthentication) {
		t.Fatalf("unavailable installations=%+v err=%v", got, err)
	}
	client = &discoveryClient{t: t, steps: []discoveryStep{discoveryJSON(t, map[string]any{"login": "example"}), installationPage(t, 0)}}
	if got, err := NewProvider(client).DiscoverPersonalRepositories(t.Context(), "example"); got != nil || !errors.Is(err, ErrPermission) {
		t.Fatalf("uninstalled account became empty inventory: %+v, %v", got, err)
	}
}
