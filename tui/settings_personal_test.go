package tui

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func TestSettingsPersonalSourcesSaveAndReopen(t *testing.T) {
	for _, tc := range []struct {
		name          string
		organizations []string
	}{
		{name: "personal only"},
		{name: "personal and organization", organizations: []string{"acme"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := settingsTestOptions()
			options.Initial = SettingsConfig{Organizations: tc.organizations, Exclude: []string{"viewer/web"}}
			options.Account = func(context.Context) (SettingsAccount, error) {
				return SettingsAccount{Login: "Viewer", Organizations: []string{"acme"}}, nil
			}
			repositories := []git.Repository{{ID: "viewer/api", Name: "viewer/api"}, {ID: "viewer/web", Name: "viewer/web"}}
			if len(tc.organizations) > 0 {
				repositories = append([]git.Repository{{ID: "acme/service", Name: "acme/service"}}, repositories...)
			}
			discoveries, saves := 0, 0
			options.Discover = func(_ context.Context, cfg SettingsConfig) ([]git.Repository, error) {
				discoveries++
				if cfg.PersonalAccount != "viewer" || !reflect.DeepEqual(cfg.Organizations, tc.organizations) {
					t.Fatalf("discovery lost selected sources: %+v", cfg)
				}
				return repositories, nil
			}
			want := SettingsConfig{PersonalAccount: "viewer", Organizations: tc.organizations, Exclude: []string{"viewer/web"}}
			selectRepositories := options.Save
			options.Save = func(ctx context.Context, cfg SettingsConfig, repos []git.Repository) ([]git.Repository, error) {
				saves++
				if !reflect.DeepEqual(cfg, want) || !reflect.DeepEqual(repos, repositories) {
					t.Fatalf("save lost the source or repository choices: cfg=%+v repos=%v", cfg, repos)
				}
				return selectRepositories(ctx, cfg, repos)
			}
			m := settingsTestOpen(t, options)
			view := ansi.Strip(m.View())
			if m.settings.personalAccount != "" || !strings.Contains(view, "Sources") || !strings.Contains(view, "@viewer · Personal repositories") {
				t.Fatalf("personal source was preselected or omitted from Sources: %s", view)
			}
			m = settingsTestToggleChoice(t, m, "@viewer")
			m, discover := applyLocal(t, m, keyboardContractKey("enter"))
			m = settingsTestApplyCommand(t, m, discover)
			if m.settings.step != 1 || !m.settings.selectedRepositories["viewer/api"] || m.settings.selectedRepositories["viewer/web"] {
				t.Fatal("personal repository discovery did not honor saved exclusions")
			}
			m = keyboardContractPress(t, m, "enter")
			if m.settings.step != 2 || saves != 0 || options.Initial.PersonalAccount != "" || !strings.Contains(ansi.Strip(m.View()), "@viewer") {
				t.Fatal("review omitted the personal source or saved before confirmation")
			}
			m, save := applyLocal(t, m, keyboardContractKey("enter"))
			if save == nil || saves != 0 {
				t.Fatal("review did not schedule the confirmed save")
			}
			m, reload := applyLocal(t, m, save())
			defer m.Close()
			if saves != 1 || reload == nil || m.settings.open || !reflect.DeepEqual(m.options.Settings.Initial, want) || len(m.repositories) != len(repositories)-1 {
				t.Fatalf("save did not retain personal sources and selected repositories: config=%+v repos=%v", m.options.Settings.Initial, m.repositories)
			}
			for _, repo := range m.repositories {
				if repo.ID == "viewer/web" {
					t.Fatal("save loaded an excluded personal repository")
				}
			}
			m, account := applyLocal(t, m, keyboardContractKey(","))
			m = settingsTestApplyCommand(t, m, account)
			if m.settings.personalAccount != "viewer" || !reflect.DeepEqual(m.settings.selectedOrgs(), tc.organizations) {
				t.Fatal("reopened settings lost saved personal or organization sources")
			}
			m, discover = applyLocal(t, m, keyboardContractKey("enter"))
			m = settingsTestApplyCommand(t, m, discover)
			if discoveries != 2 || m.settings.selectedRepositories["viewer/web"] || !m.settings.selectedRepositories["viewer/api"] {
				t.Fatal("reopened settings lost the personal repository selection")
			}
		})
	}
}

func TestSettingsPersonalSourceChangesCancelWithoutSaving(t *testing.T) {
	options := settingsTestOptions()
	original := SettingsConfig{PersonalAccount: "previous", Organizations: []string{"acme"}, Exclude: []string{"previous/web"}}
	options.Initial = original
	options.Discover = func(_ context.Context, cfg SettingsConfig) ([]git.Repository, error) {
		if cfg.PersonalAccount != "viewer" || len(cfg.Organizations) != 0 {
			t.Fatalf("discovery did not receive the staged personal-only source: %+v", cfg)
		}
		return []git.Repository{{ID: "viewer/api", Name: "viewer/api"}, {ID: "viewer/web", Name: "viewer/web"}}, nil
	}
	saves := 0
	options.Save = func(context.Context, SettingsConfig, []git.Repository) ([]git.Repository, error) {
		saves++
		return nil, nil
	}
	m := settingsTestOpen(t, options)
	m = settingsTestToggleChoice(t, m, "@viewer")
	m = settingsTestToggleChoice(t, m, "acme")
	m, discover := applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, discover)
	m = settingsTestToggleChoice(t, m, "viewer/web")
	m = keyboardContractPress(t, m, "enter")
	if m.settings.step != 2 {
		t.Fatal("personal source draft did not reach review")
	}
	m = keyboardContractPress(t, m, "esc", "esc", "esc")
	if m.settings.open || saves != 0 || !reflect.DeepEqual(m.options.Settings.Initial, original) || !reflect.DeepEqual(options.Initial, original) || len(m.repositories) != 0 {
		t.Fatal("cancel applied staged personal sources or repository exclusions")
	}
	m, account := applyLocal(t, m, keyboardContractKey(","))
	m = settingsTestApplyCommand(t, m, account)
	if m.settings.personalAccount != "previous" || !m.settings.selectedOrganizations["acme"] {
		t.Fatal("canceled source changes survived reopening settings")
	}
}

func TestSettingsChangedAccountOffersSavedOwnerAndSelectsOnlyOnePersonalSource(t *testing.T) {
	options := settingsTestOptions()
	options.Initial.PersonalAccount = "previous"
	options.Account = func(context.Context) (SettingsAccount, error) {
		return SettingsAccount{Login: "NewViewer", Organizations: []string{"acme"}}, nil
	}
	m := settingsTestOpen(t, options)
	choices := m.settings.choices()
	if m.settings.personalAccount != "previous" || !slices.Contains(choices, "@previous") || !slices.Contains(choices, "@newviewer") || len(m.settings.personalAccounts) != 2 {
		t.Fatalf("account change lost the configured owner or current account: selected=%q choices=%v", m.settings.personalAccount, choices)
	}
	if slices.Index(choices, "@previous") > slices.Index(choices, "acme") || slices.Index(choices, "@newviewer") > slices.Index(choices, "acme") {
		t.Fatalf("personal sources did not precede organization sources: %v", choices)
	}
	before := m
	m = settingsTestToggleChoice(t, m, "@newviewer")
	if m.settings.personalAccount != "newviewer" || before.settings.personalAccount != "previous" || !m.settings.selectedOrganizations["acme"] || options.Initial.PersonalAccount != "previous" {
		t.Fatal("selecting the new account changed another model, an organization, or saved settings")
	}
	m = settingsTestToggleChoice(t, m, "@newviewer")
	if m.settings.personalAccount != "" || !m.settings.selectedOrganizations["acme"] {
		t.Fatal("deselecting the new account restored the old account or changed organizations")
	}
	m = settingsTestToggleChoice(t, m, "@previous")
	m = settingsTestToggleChoice(t, m, "acme")
	if m.settings.personalAccount != "previous" || m.settings.selectedOrganizations["acme"] {
		t.Fatal("organization selection was not independent of personal selection")
	}
	m = settingsTestToggleChoice(t, m, "@previous")
	m, discover := applyLocal(t, m, keyboardContractKey("enter"))
	if discover != nil || m.settings.step != 0 || m.settings.error == "" {
		t.Fatal("an empty personal and organization selection started discovery")
	}
}
