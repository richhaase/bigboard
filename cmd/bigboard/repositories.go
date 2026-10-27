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
	if len(cfg.Organizations) == 0 {
		return fmt.Errorf("organizations is required; configure GitHub organizations, for example: %s", repositoryConfigExample)
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
			if !slices.Contains(cfg.Organizations, owner) {
				return fmt.Errorf("groups.%s contains %s outside configured organizations", group, canonical)
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
		if !slices.Contains(cfg.Organizations, owner) || group != "" && !slices.Contains(names, repo.ID) {
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

func discoverCachedRepositories(ctx context.Context, cfg *Config, cache *git.RepositoryCache, offline bool) ([]git.Repository, error) {
	if offline {
		return cache.CachedRepositories()
	}
	remote, err := gh.NewProvider(nil).DiscoverRepositories(ctx, cfg.Organizations)
	if err != nil {
		return nil, err
	}
	repos := make([]git.Repository, 0, len(remote))
	for _, item := range remote {
		repo, err := cache.Repository(item.Name, item.DefaultBranch)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
}
