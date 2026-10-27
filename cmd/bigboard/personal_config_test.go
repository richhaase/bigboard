package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/tui"
)

func personalTestRepositories() []git.Repository {
	return []git.Repository{
		{ID: "personal/app", Name: "personal/app"},
		{ID: "personal/archive", Name: "personal/archive"},
		{ID: "example/api", Name: "example/api"},
		{ID: "other/unrelated", Name: "other/unrelated"},
	}
}

func TestPersonalConfigurationValidatesOwnerAndGroupScope(t *testing.T) {
	cfg := &Config{PersonalAccount: " Personal ", Groups: map[string][]string{"work": {"PERSONAL/APP"}}}
	if err := validateRepositoryConfig(cfg); err != nil || cfg.PersonalAccount != "personal" || cfg.Groups["work"][0] != "personal/app" {
		t.Fatalf("personal config = %+v, %v", cfg, err)
	}
	selected, err := selectRepositories(cfg, "work", personalTestRepositories())
	if err != nil || len(selected) != 1 || selected[0].ID != "personal/app" {
		t.Fatalf("personal group selection = %+v, %v", selected, err)
	}
	cfg.Organizations = []string{"EXAMPLE"}
	cfg.Groups["work"] = append(cfg.Groups["work"], "example/api")
	if err := validateRepositoryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	selected, err = selectRepositories(cfg, "work", personalTestRepositories())
	if err != nil || len(selected) != 2 || selected[1].ID != "example/api" {
		t.Fatalf("mixed group selection = %+v, %v", selected, err)
	}
	for _, personal := range []string{" ", "../personal", "personal/app", "https://github.com/personal"} {
		if err := validateRepositoryConfig(&Config{PersonalAccount: personal}); err == nil || !strings.Contains(err.Error(), "invalid personal account") {
			t.Fatalf("invalid personal account %q = %v", personal, err)
		}
	}
	cfg.Groups["work"] = append(cfg.Groups["work"], "other/unrelated")
	if err := validateRepositoryConfig(cfg); err == nil || !strings.Contains(err.Error(), "outside configured") {
		t.Fatalf("out-of-scope group = %v", err)
	}
}

func TestPersonalSettingsSaveAndRestart(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "personal only", true: "mixed"}[mixed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			cfg, err := loadConfig(path, false)
			if err != nil {
				t.Fatal(err)
			}
			store := newConfigStore(path, cfg)
			settings := newSettingsOptions(path, cfg, "", nil, nil, store, &repositoryInventory{})
			draft := tui.SettingsConfig{PersonalAccount: " Personal ", Exclude: []string{"personal/archive"}}
			want := []git.Repository{personalTestRepositories()[0]}
			if mixed {
				draft.Organizations = []string{"Example"}
				want = append(want, personalTestRepositories()[2])
			}
			selected, err := settings.Save(t.Context(), draft, personalTestRepositories())
			if err != nil || !reflect.DeepEqual(selected, want) {
				t.Fatalf("saved selection = %+v, %v", selected, err)
			}
			if err := store.saveIdentities(testIdentityMapping()); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadConfig(path, true)
			if err != nil || loaded.PersonalAccount != "personal" || !reflect.DeepEqual(loaded.Identities, testIdentityMapping()) {
				t.Fatalf("restarted config = %+v, %v", loaded, err)
			}
			if err := validateRepositoryConfig(loaded); err != nil {
				t.Fatal(err)
			}
			selected, _, err = selectIncludedRepositories(loaded, "", personalTestRepositories())
			if err != nil || !reflect.DeepEqual(selected, want) {
				t.Fatalf("restarted selection = %+v, %v", selected, err)
			}
			initial := newSettingsOptions(path, loaded, "", nil, nil, newConfigStore(path, loaded), &repositoryInventory{}).Initial
			if initial.PersonalAccount != "personal" || !reflect.DeepEqual(initial.Organizations, loaded.Organizations) {
				t.Fatalf("reopened settings = %+v", initial)
			}
		})
	}
}

func TestSettingsCanEnableAndDisablePersonalAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"organizations":["example"],"groups":{"work":["example/api"]},"theme":"light"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	settings := newSettingsOptions(path, cfg, "", nil, nil, newConfigStore(path, cfg), &repositoryInventory{})
	for _, personal := range []string{"personal", ""} {
		draft := tui.SettingsConfig{PersonalAccount: personal, Organizations: []string{"example"}}
		if _, err := settings.Save(t.Context(), draft, personalTestRepositories()); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadConfig(path, true)
		if err != nil || loaded.PersonalAccount != personal || loaded.Theme != "light" || !reflect.DeepEqual(loaded.Groups, cfg.Groups) {
			t.Fatalf("saved personal toggle = %+v, %v", loaded, err)
		}
	}
}

func TestSettingsDiscoversPersonalAndOrganizationRepositories(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "personal only", true: "mixed"}[mixed], func(t *testing.T) {
			provider := &settingsProviderStub{
				personalRepositories: []gh.DiscoveredRepository{{Name: "personal/z", DefaultBranch: "main"}, {Name: "personal/a", DefaultBranch: "trunk"}, {Name: "PERSONAL/A", DefaultBranch: "trunk"}},
				repositories:         []gh.DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}},
			}
			cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
			settings := newSettingsOptions("", &Config{}, "", provider, cache, nil, nil)
			draft := tui.SettingsConfig{PersonalAccount: " PERSONAL "}
			want := []string{"personal/a", "personal/z"}
			if mixed {
				draft.Organizations = []string{" Example "}
				want = append([]string{"example/api"}, want...)
			}
			repos, err := settings.Discover(t.Context(), draft)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, repo := range repos {
				ids = append(ids, repo.ID)
			}
			if !reflect.DeepEqual(ids, want) || provider.personalAccount != "personal" || (len(provider.organizations) > 0) != mixed {
				t.Fatalf("discovery = %+v, personal=%q orgs=%v", ids, provider.personalAccount, provider.organizations)
			}
			if _, err := os.Stat(cache.Root); !os.IsNotExist(err) {
				t.Fatal("discovery created cache content")
			}
		})
	}
}

func TestMixedRepositoryDiscoveryDoesNotReturnPartialInventory(t *testing.T) {
	failure := errors.New("discovery failed")
	for _, failPersonal := range []bool{false, true} {
		provider := &settingsProviderStub{repositories: []gh.DiscoveredRepository{{Name: "example/api"}}, personalRepositories: []gh.DiscoveredRepository{{Name: "personal/app"}}}
		if failPersonal {
			provider.personalErr = failure
		} else {
			provider.discoveryErr = failure
		}
		cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
		repos, err := discoverRepositories(t.Context(), provider, &Config{PersonalAccount: "personal", Organizations: []string{"example"}}, cache)
		if !errors.Is(err, failure) || repos != nil {
			t.Fatalf("partial discovery = %+v, %v", repos, err)
		}
	}
}

func TestPersonalExportDiscoveryRemainsOffline(t *testing.T) {
	for _, cfg := range []*Config{{PersonalAccount: "personal"}, {PersonalAccount: "personal", Organizations: []string{"example"}}} {
		provider := &settingsProviderStub{}
		cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
		if _, err := discoverCachedRepositories(t.Context(), cfg, cache, provider, true); err != nil {
			t.Fatal(err)
		}
		if provider.calls != 0 {
			t.Fatal("export invoked personal or organization discovery")
		}
	}
}

func TestDiscoveryPreservesLastPushMetadataForAllSources(t *testing.T) {
	personalPush := time.Date(2026, 10, 27, 12, 30, 0, 0, time.UTC)
	organizationPush := personalPush.Add(-24 * time.Hour)
	provider := &settingsProviderStub{
		personalRepositories: []gh.DiscoveredRepository{
			{Name: "personal/app", DefaultBranch: "main", PushedAt: personalPush},
			{Name: "personal/unknown", DefaultBranch: "main"},
		},
		repositories: []gh.DiscoveredRepository{{Name: "example/api", DefaultBranch: "main", PushedAt: organizationPush}},
	}
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	repos, err := discoverRepositories(t.Context(), provider, &Config{PersonalAccount: "personal", Organizations: []string{"example"}}, cache)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Time{"personal/app": personalPush, "personal/unknown": {}, "example/api": organizationPush}
	if len(repos) != len(want) {
		t.Fatalf("discovered %d repositories, want %d", len(repos), len(want))
	}
	for _, repo := range repos {
		pushedAt, exists := want[repo.ID]
		if !exists || !repo.PushedAt.Equal(pushedAt) {
			t.Fatalf("last push for %s = %v, want %v", repo.ID, repo.PushedAt, pushedAt)
		}
	}
}
