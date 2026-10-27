package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

const repositoryConfigExample = `{"organizations":["example-org"],"groups":{"backend":["example-org/api"]}}`

func validateRepositoryConfig(cfg *Config) error {
	if len(cfg.Organizations) == 0 && cfg.PersonalAccount == "" {
		return fmt.Errorf("organizations or personal_account is required; configure a GitHub account or organizations, for example: %s", repositoryConfigExample)
	}
	if cfg.PersonalAccount != "" {
		canonical, err := gh.CanonicalRepo(strings.TrimSpace(cfg.PersonalAccount) + "/repository")
		if err != nil {
			return fmt.Errorf("invalid personal account %q", cfg.PersonalAccount)
		}
		cfg.PersonalAccount, _, _ = strings.Cut(canonical, "/")
	}
	for i, org := range cfg.Organizations {
		canonical, err := gh.CanonicalRepo(strings.TrimSpace(org) + "/repository")
		if err != nil {
			return fmt.Errorf("invalid organization %q", org)
		}
		cfg.Organizations[i], _, _ = strings.Cut(canonical, "/")
	}
	for group, names := range cfg.Groups {
		for i, name := range names {
			canonical, err := gh.CanonicalRepo(name)
			if err != nil {
				return fmt.Errorf("groups.%s must contain owner/name repository identities, not paths; new schema: %s", group, repositoryConfigExample)
			}
			owner, _, _ := strings.Cut(canonical, "/")
			if owner != cfg.PersonalAccount && !slices.Contains(cfg.Organizations, owner) {
				return fmt.Errorf("groups.%s contains %s outside configured organizations or personal account", group, canonical)
			}
			cfg.Groups[group][i] = canonical
		}
	}
	return nil
}

func selectRepositories(cfg *Config, group string, repos []git.Repository) ([]git.Repository, error) {
	var names []string
	if group != "" {
		var ok bool
		names, ok = cfg.Groups[group]
		if !ok {
			return nil, fmt.Errorf("unknown group %q", group)
		}
	}
	var selected []git.Repository
	seen := make(map[string]bool)
	for _, repo := range repos {
		owner, _, _ := strings.Cut(repo.ID, "/")
		if owner != cfg.PersonalAccount && !slices.Contains(cfg.Organizations, owner) || group != "" && !slices.Contains(names, repo.ID) {
			continue
		}
		selected = append(selected, repo)
		seen[repo.ID] = true
	}
	for _, name := range names {
		if !seen[name] {
			return nil, fmt.Errorf("group repository %s is unavailable or archived; check organization access and the cache", name)
		}
	}
	return selected, nil
}

func discoverCachedRepositories(ctx context.Context, cfg *Config, cache *git.RepositoryCache, provider settingsProvider, offline bool) ([]git.Repository, error) {
	if offline {
		return cache.CachedRepositories()
	}
	return discoverRepositories(ctx, provider, cfg, cache)
}

func discoverRepositories(ctx context.Context, provider settingsProvider, cfg *Config, cache *git.RepositoryCache) ([]git.Repository, error) {
	var remote []gh.DiscoveredRepository
	if len(cfg.Organizations) > 0 {
		organizations, err := provider.DiscoverRepositories(ctx, cfg.Organizations)
		if err != nil {
			return nil, err
		}
		remote = append(remote, organizations...)
	}
	if cfg.PersonalAccount != "" {
		personal, err := provider.DiscoverPersonalRepositories(ctx, cfg.PersonalAccount)
		if err != nil {
			return nil, err
		}
		remote = append(remote, personal...)
	}
	repos := make([]git.Repository, 0, len(remote))
	seen := make(map[string]bool)
	for _, item := range remote {
		repo, err := cache.Repository(item.Name, item.DefaultBranch)
		if err != nil {
			return nil, err
		}
		if !seen[repo.ID] {
			repo.PushedAt = item.PushedAt
			repos = append(repos, repo)
			seen[repo.ID] = true
		}
	}
	slices.SortFunc(repos, func(a, b git.Repository) int { return strings.Compare(a.ID, b.ID) })
	return repos, nil
}
