package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/tui"
)

type settingsProviderStub struct {
	account              gh.Account
	repositories         []gh.DiscoveredRepository
	personalRepositories []gh.DiscoveredRepository
	personalAccount      string
	personalErr          error
	discoveryErr         error
	calls                int
	organizations        []string
}

func (p *settingsProviderStub) SetupAccount(ctx context.Context) (gh.Account, error) {
	p.calls++
	return p.account, ctx.Err()
}

func (p *settingsProviderStub) DiscoverRepositories(ctx context.Context, organizations []string) ([]gh.DiscoveredRepository, error) {
	p.calls++
	p.organizations = slices.Clone(organizations)
	if p.discoveryErr != nil {
		return nil, p.discoveryErr
	}
	return p.repositories, ctx.Err()
}

func (p *settingsProviderStub) DiscoverPersonalRepositories(ctx context.Context, login string) ([]gh.DiscoveredRepository, error) {
	p.calls++
	p.personalAccount = login
	if p.personalErr != nil {
		return nil, p.personalErr
	}
	return p.personalRepositories, ctx.Err()
}

func settingsRepositories() []git.Repository {
	return []git.Repository{
		{ID: "example/api", Name: "example/api"},
		{ID: "example/worker", Name: "example/worker"},
		{ID: "example/other", Name: "example/other"},
	}
}

func TestSettingsSavePreservesConfigAndSharesIdentityStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	source := []byte(`{"organizations":["example"],"exclude":["archive-*"],"sort":"commits","since":"90d","theme":"light","groups":{"backend":["example/api","example/worker"]},"github":{"enabled":false},"fuzzy":true,"ai_identities":["agent@example.invalid"],"bot_identities":["bot@example.invalid"],"work_areas":{},"pr_request_budget":23}`)
	if err := os.WriteFile(path, source, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	store := newConfigStore(path, cfg)
	inventory := &repositoryInventory{}
	settings := newSettingsOptions(path, cfg, "backend", nil, nil, store, inventory)
	repos := settingsRepositories()
	for _, excluded := range []string{"example/worker", "example/api"} {
		selected, err := settings.Save(t.Context(), tui.SettingsConfig{Organizations: []string{" Example "}, Exclude: []string{excluded}}, repos)
		if err != nil {
			t.Fatal(err)
		}
		if len(selected) != 1 || selected[0].ID == excluded || selected[0].ID == "example/other" {
			t.Fatalf("saved group selection = %+v", selected)
		}
		if !reflect.DeepEqual(inventory.snapshot(), repos) {
			t.Fatal("excluded or out-of-group repositories lost from listed inventory")
		}
		if err := store.saveIdentities(testIdentityMapping()); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal(source, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"personal_account", "organizations", "exclude", "identities"} {
		delete(before, key)
		delete(after, key)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unrelated settings changed: %+v", after)
	}
	loaded, err := loadConfig(path, true)
	if err != nil || !reflect.DeepEqual(loaded.Organizations, []string{"example"}) || !reflect.DeepEqual(loaded.Exclude, []string{"example/api"}) || !reflect.DeepEqual(loaded.Identities, testIdentityMapping()) {
		t.Fatalf("saved configuration = %+v, %v", loaded, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("existing config permissions changed")
	}
}

func TestSetupOnlyCreatesConfigWhenSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatal(err)
	}
	provider := &settingsProviderStub{
		account:      gh.Account{Login: "example-user", Organizations: []string{"example"}},
		repositories: []gh.DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}},
	}
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	inventory := &repositoryInventory{}
	settings := newSettingsOptions(path, cfg, "", provider, cache, newConfigStore(path, cfg), inventory)
	account, err := settings.Account(t.Context())
	if err != nil || account.Login != "example-user" || !reflect.DeepEqual(account.Organizations, []string{"example"}) {
		t.Fatalf("setup account = %+v, %v", account, err)
	}
	repos, err := settings.Discover(t.Context(), tui.SettingsConfig{Organizations: []string{" Example "}})
	if err != nil || len(repos) != 1 || repos[0].ID != "example/api" || repos[0].DefaultBranch != "main" || !strings.HasPrefix(repos[0].Path, cache.Root) {
		t.Fatalf("setup discovery = %+v, %v", repos, err)
	}
	if !reflect.DeepEqual(provider.organizations, []string{"example"}) {
		t.Fatal("discovery did not normalize organization")
	}
	if _, err := os.Stat(cache.Root); !os.IsNotExist(err) {
		t.Fatal("setup discovery created a cache")
	}
	draft := tui.SettingsConfig{Organizations: []string{"example"}, Exclude: []string{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := settings.Save(ctx, draft, repos); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("setup created configuration before save")
	}
	if len(inventory.snapshot()) != 0 {
		t.Fatal("canceled save changed inventory")
	}
	if _, err := settings.Save(t.Context(), draft, repos); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("new configuration is not private")
	}
	if loaded, err := loadConfig(path, true); err != nil || !reflect.DeepEqual(loaded.Organizations, draft.Organizations) {
		t.Fatalf("created configuration = %+v, %v", loaded, err)
	}
}

func TestSettingsRejectsInvalidDraftWithoutChangingConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group string
		draft tui.SettingsConfig
		want  string
	}{
		{"missing organizations", "", tui.SettingsConfig{}, "organizations or personal_account is required"},
		{"invalid organizations", "", tui.SettingsConfig{Organizations: []string{"../example"}}, "invalid organization"},
		{"group outside organizations", "", tui.SettingsConfig{Organizations: []string{"other"}}, "outside configured organizations"},
		{"unknown group", "missing", tui.SettingsConfig{Organizations: []string{"example"}}, "unknown group"},
		{"unavailable group member", "unavailable", tui.SettingsConfig{Organizations: []string{"example"}}, "unavailable or archived"},
		{"invalid exclusion", "", tui.SettingsConfig{Organizations: []string{"example"}, Exclude: []string{"["}}, "pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			source := []byte(`{"organizations":["example"],"groups":{"unavailable":["EXAMPLE/MISSING"]}}`)
			if err := os.WriteFile(path, source, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadConfig(path, true)
			if err != nil {
				t.Fatal(err)
			}
			inventory := &repositoryInventory{}
			settings := newSettingsOptions(path, cfg, tc.group, nil, nil, newConfigStore(path, cfg), inventory)
			if _, err := settings.Save(t.Context(), tc.draft, settingsRepositories()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid draft save = %v, want %q", err, tc.want)
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
				t.Fatal("invalid draft changed config")
			}
			if cfg.Groups["unavailable"][0] != "EXAMPLE/MISSING" || len(inventory.snapshot()) != 0 {
				t.Fatal("invalid draft mutated active configuration")
			}
		})
	}
}

func TestSettingsRejectsExternalConfigChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatal(err)
	}
	inventory := &repositoryInventory{}
	settings := newSettingsOptions(path, cfg, "", nil, nil, newConfigStore(path, cfg), inventory)
	external := []byte(`{"organizations":["other"]}`)
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Save(t.Context(), tui.SettingsConfig{Organizations: []string{"example"}}, settingsRepositories()); err == nil || !strings.Contains(err.Error(), "changed outside") {
		t.Fatalf("external configuration save = %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, external) || len(inventory.snapshot()) != 0 {
		t.Fatal("failed save changed configuration or active inventory")
	}
}

func TestExportDiscoveryRemainsOffline(t *testing.T) {
	provider := &settingsProviderStub{}
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	if _, err := discoverCachedRepositories(t.Context(), &Config{Organizations: []string{"example"}}, cache, provider, true); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatal("export invoked GitHub discovery")
	}
}

func TestRepositoryInventoryConcurrentSnapshots(t *testing.T) {
	inventory := &repositoryInventory{}
	repos := settingsRepositories()
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				inventory.track(Config{}, repos)
				current := inventory.snapshot()
				current[0].ID = "local/edit"
			}
		})
	}
	workers.Wait()
	if !reflect.DeepEqual(inventory.snapshot(), repos) {
		t.Fatal("snapshot mutations leaked into inventory")
	}
}
