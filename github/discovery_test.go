package github

import (
	"errors"
	"reflect"
	"testing"
)

func TestDiscoverRepositoriesFiltersInstalledAccessByConfiguredOrganizations(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{
		installationPage(t, 3, installationRow(1, "example", "Organization"), installationRow(2, "other", "Organization"), installationRow(3, "person", "User")),
		repositoryPage(t, 1, repositoryRow("example/allowed")),
	}}
	got, err := NewProvider(client).DiscoverRepositories(t.Context(), []string{"EXAMPLE", "example"})
	want := []DiscoveredRepository{{OwnerType: "Organization", Name: "example/allowed", DefaultBranch: "main"}}
	if err != nil || !reflect.DeepEqual(got, want) || len(client.calls) != 2 {
		t.Fatalf("filtered repositories=%+v err=%v", got, err)
	}
}

func TestDiscoverRepositoriesEmptyAndInvalidOrganizations(t *testing.T) {
	client := &discoveryClient{t: t}
	if got, err := NewProvider(client).DiscoverRepositories(t.Context(), nil); err != nil || len(got) != 0 || len(client.calls) != 0 {
		t.Fatalf("empty selection=%+v err=%v", got, err)
	}
	for _, org := range []string{"", "../outside", "https://github.com/example", "--hostname", "example/other"} {
		client := &discoveryClient{t: t}
		if _, err := NewProvider(client).DiscoverRepositories(t.Context(), []string{org}); err == nil || len(client.calls) != 0 {
			t.Fatalf("invalid organization accepted: %q, %v", org, err)
		}
	}
	client = &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 0)}}
	if got, err := NewProvider(client).DiscoverRepositories(t.Context(), []string{"example"}); !errors.Is(err, ErrPermission) || got != nil {
		t.Fatalf("uninstalled organization=%+v err=%v", got, err)
	}
	client = &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 1, installationRow(1, "example", "Organization")), repositoryPage(t, 0)}}
	if got, err := NewProvider(client).DiscoverRepositories(t.Context(), []string{"example"}); err != nil || len(got) != 0 {
		t.Fatalf("installed empty organization=%+v err=%v", got, err)
	}
}

func TestDiscoverRepositoriesDoesNotHideAccessFailure(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{installationPage(t, 1, installationRow(1, "example", "Organization")), {err: ErrPermission}}}
	got, err := NewProvider(client).DiscoverRepositories(t.Context(), []string{"example"})
	if got != nil || !errors.Is(err, ErrPermission) {
		t.Fatalf("access failure became empty inventory: %+v, %v", got, err)
	}
}
