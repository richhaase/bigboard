package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

func (p *Provider) DiscoverPersonalRepositories(ctx context.Context, account string) ([]DiscoveredRepository, error) {
	canonical, err := CanonicalRepo(account + "/repository")
	if err != nil {
		return nil, fmt.Errorf("invalid personal GitHub account %q", account)
	}
	owner, _, _ := strings.Cut(canonical, "/")
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	data, err := p.setupRequest(ctx, "user")
	if err != nil {
		return nil, err
	}
	var user *actor
	if err := json.Unmarshal(data, &user); err != nil || user == nil || !validOwner(strings.ReplaceAll(user.Login, "_", "a")) {
		return nil, ErrResponse
	}
	if !strings.EqualFold(user.Login, owner) {
		return nil, fmt.Errorf("GitHub is signed in as %q, but personal repositories are configured for %q; use gh auth switch or update Settings", user.Login, owner)
	}
	seen := make(map[string]bool)
	var result []DiscoveredRepository
	for page := 1; page < MaxRefreshRequests; page++ {
		endpoint := fmt.Sprintf("user/repos?affiliation=owner&visibility=all&per_page=100&page=%d", page)
		data, err := p.setupRequest(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("discovering personal repositories for %s: %w", owner, err)
		}
		var rows []struct {
			Name          string    `json:"full_name"`
			DefaultBranch string    `json:"default_branch"`
			Archived      bool      `json:"archived"`
			PushedAt      time.Time `json:"pushed_at"`
		}
		if err := json.Unmarshal(data, &rows); err != nil || rows == nil || len(rows) > 100 {
			return nil, ErrResponse
		}
		for _, row := range rows {
			name, err := CanonicalRepo(row.Name)
			if err != nil || !strings.HasPrefix(name, owner+"/") {
				return nil, ErrResponse
			}
			if row.Archived || seen[name] {
				continue
			}
			seen[name] = true
			result = append(result, DiscoveredRepository{Name: name, DefaultBranch: row.DefaultBranch, PushedAt: row.PushedAt})
		}
		if len(rows) < 100 {
			slices.SortFunc(result, func(a, b DiscoveredRepository) int { return strings.Compare(a.Name, b.Name) })
			return result, nil
		}
	}
	return nil, ErrBudgetExceeded
}
