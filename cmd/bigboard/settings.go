package main

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/tui"
)

type settingsProvider interface {
	SetupAccount(context.Context) (gh.Account, error)
	DiscoverRepositories(context.Context, []string) ([]gh.DiscoveredRepository, error)
	DiscoverPersonalRepositories(context.Context, string) ([]gh.DiscoveredRepository, error)
}

type repositoryInventory struct {
	mu      sync.RWMutex
	listed  []git.Repository
	config  Config
	version uint64
}

func (i *repositoryInventory) track(cfg Config, repos []git.Repository) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.config, i.listed = cfg, slices.Clone(repos)
	i.version++
}

func (i *repositoryInventory) sources() (Config, uint64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.config, i.version
}

func (i *repositoryInventory) replaceTracked(version uint64, repos []git.Repository) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.version != version {
		return false
	}
	i.listed = slices.Clone(repos)
	return true
}

func (i *repositoryInventory) snapshot() []git.Repository {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return slices.Clone(i.listed)
}

func newSettingsOptions(path string, cfg *Config, group string, provider settingsProvider, cache *git.RepositoryCache, store *configStore, inventory *repositoryInventory) *tui.SettingsOptions {
	return &tui.SettingsOptions{
		ConfigPath: path,
		Initial:    tui.SettingsConfig{PersonalAccount: cfg.PersonalAccount, Organizations: slices.Clone(cfg.Organizations), Exclude: slices.Clone(cfg.Exclude)},
		Group:      group,
		Account: func(ctx context.Context) (tui.SettingsAccount, error) {
			account, err := provider.SetupAccount(ctx)
			return tui.SettingsAccount{Login: account.Login, Organizations: slices.Clone(account.Organizations)}, err
		},
		Discover: func(ctx context.Context, settings tui.SettingsConfig) ([]git.Repository, error) {
			draft := &Config{PersonalAccount: settings.PersonalAccount, Organizations: slices.Clone(settings.Organizations)}
			if err := validateRepositoryConfig(draft); err != nil {
				return nil, err
			}
			return discoverRepositories(ctx, provider, draft, cache)
		},
		Save: func(ctx context.Context, settings tui.SettingsConfig, discovered []git.Repository) ([]git.Repository, error) {
			draft := *cfg
			draft.PersonalAccount = settings.PersonalAccount
			draft.Organizations = slices.Clone(settings.Organizations)
			draft.Exclude = slices.Clone(settings.Exclude)
			draft.Groups = make(map[string][]string, len(cfg.Groups))
			for name, members := range cfg.Groups {
				draft.Groups[name] = slices.Clone(members)
			}
			if err := validateRepositoryConfig(&draft); err != nil {
				return nil, err
			}
			selected, _, err := selectIncludedRepositories(&draft, group, discovered)
			if err != nil {
				return nil, err
			}
			if err := store.update(ctx, map[string]any{"personal_account": draft.PersonalAccount, "organizations": draft.Organizations, "exclude": draft.Exclude}); err != nil {
				return nil, err
			}
			inventory.track(draft, discovered)
			return selected, nil
		},
	}
}

func newRediscovery(provider settingsProvider, cache *git.RepositoryCache, group string, inventory *repositoryInventory) func(context.Context) ([]git.Repository, map[string]bool, error) {
	return func(ctx context.Context) ([]git.Repository, map[string]bool, error) {
		cfg, version := inventory.sources()
		listed, err := discoverRepositories(ctx, provider, &cfg, cache)
		if err != nil {
			return nil, nil, err
		}
		selected, excluded, err := selectIncludedRepositories(&cfg, group, listed)
		if err != nil {
			return nil, nil, err
		}
		if !inventory.replaceTracked(version, listed) {
			return nil, nil, fmt.Errorf("repository settings changed during discovery")
		}
		return selected, excluded, nil
	}
}

func selectIncludedRepositories(cfg *Config, group string, listed []git.Repository) ([]git.Repository, map[string]bool, error) {
	repositories, err := selectRepositories(cfg, group, listed)
	if err != nil {
		return nil, nil, err
	}
	excluded, err := buildExcludeSet(repositories, cfg.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid exclusion: %w", err)
	}
	selected := repositories[:0]
	for _, repo := range repositories {
		if !excluded[repo.ID] {
			selected = append(selected, repo)
		}
	}
	return selected, excluded, nil
}
