package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestRediscoveryAppliesCurrentSourcesGroupsAndExcludes(t *testing.T) {
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	provider := &settingsProviderStub{repositories: []gh.DiscoveredRepository{
		{Name: "example/api", DefaultBranch: "main"},
		{Name: "example/new", DefaultBranch: "trunk"},
		{Name: "example/legacy", DefaultBranch: "main"},
	}}
	cfg := Config{Organizations: []string{"example"}, Exclude: []string{"legacy"}}
	inventory := &repositoryInventory{}
	listed, err := discoverRepositories(t.Context(), provider, &Config{Organizations: []string{"example"}}, cache)
	if err != nil {
		t.Fatal(err)
	}
	inventory.track(cfg, append(listed, git.Repository{ID: "example/removed", Name: "example/removed"}))
	rediscover := newRediscovery(provider, cache, "", inventory)
	selected, excluded, err := rediscover(t.Context())
	if err != nil || !reflect.DeepEqual(repositoryIDsOf(selected), []string{"example/api", "example/new"}) || !excluded["example/legacy"] {
		t.Fatalf("selected=%v excluded=%v err=%v", repositoryIDsOf(selected), excluded, err)
	}
	if got := repositoryIDsOf(inventory.snapshot()); !reflect.DeepEqual(got, []string{"example/api", "example/legacy", "example/new"}) {
		t.Fatalf("listed inventory = %v; removed repositories must become unlisted", got)
	}
	if selected[1].DefaultBranch != "trunk" || selected[1].Path == "" {
		t.Fatalf("new repository = %+v", selected[1])
	}

	settings := Config{PersonalAccount: "personal", Groups: map[string][]string{"backend": {"personal/app"}}}
	provider.personalRepositories = []gh.DiscoveredRepository{{Name: "personal/app", DefaultBranch: "main"}, {Name: "personal/other", DefaultBranch: "main"}}
	inventory.track(settings, nil)
	selected, _, err = newRediscovery(provider, cache, "backend", inventory)(t.Context())
	if err != nil || !reflect.DeepEqual(repositoryIDsOf(selected), []string{"personal/app"}) || provider.personalAccount != "personal" {
		t.Fatalf("Settings sources ignored: selected=%v err=%v", repositoryIDsOf(selected), err)
	}
}

func TestRediscoveryFailureRetainsInventory(t *testing.T) {
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	before := []git.Repository{{ID: "example/api", Name: "example/api"}}
	for name, provider := range map[string]*settingsProviderStub{
		"discovery":     {discoveryErr: gh.ErrAuthentication},
		"missing group": {repositories: []gh.DiscoveredRepository{{Name: "example/other", DefaultBranch: "main"}}},
	} {
		t.Run(name, func(t *testing.T) {
			inventory := &repositoryInventory{}
			inventory.track(Config{Organizations: []string{"example"}, Groups: map[string][]string{"backend": {"example/api"}}}, before)
			selected, _, err := newRediscovery(provider, cache, "backend", inventory)(t.Context())
			if selected != nil || err == nil || !reflect.DeepEqual(inventory.snapshot(), before) {
				t.Fatalf("selected=%v err=%v inventory=%v", selected, err, inventory.snapshot())
			}
		})
	}
	inventory := &repositoryInventory{}
	inventory.track(Config{Organizations: []string{"example"}}, before)
	_, version := inventory.sources()
	inventory.track(Config{Organizations: []string{"other"}}, nil)
	if inventory.replaceTracked(version, before) || len(inventory.snapshot()) != 0 {
		t.Fatal("stale rediscovery replaced inventory saved by Settings")
	}
}

func repositoryIDsOf(repos []git.Repository) []string {
	ids := make([]string, 0, len(repos))
	for _, repo := range repos {
		ids = append(ids, repo.ID)
	}
	return ids
}
