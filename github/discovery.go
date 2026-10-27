package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

type DiscoveredRepository struct {
	Name          string
	DefaultBranch string
	PushedAt      time.Time
}

func (p *Provider) DiscoverRepositories(ctx context.Context, organizations []string) ([]DiscoveredRepository, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	seen := make(map[string]bool)
	var result []DiscoveredRepository
	requests := 0
	for _, organization := range organizations {
		canonical, err := CanonicalRepo(organization + "/repository")
		if err != nil {
			return nil, fmt.Errorf("invalid organization %q", organization)
		}
		owner, _, _ := strings.Cut(canonical, "/")
		for page := 1; ; page++ {
			if requests >= MaxRefreshRequests {
				return nil, ErrBudgetExceeded
			}
			requests++
			endpoint := fmt.Sprintf("orgs/%s/repos?type=all&per_page=100&page=%d", owner, page)
			data, err := p.runner.Run(ctx, "gh", "api", "--hostname", "github.com", "--method", "GET", endpoint)
			if err != nil {
				return nil, fmt.Errorf("discovering %s: %w", owner, safeRequestError(err))
			}
			if len(data) > MaxOutputBytes {
				return nil, ErrOutputLimit
			}
			var rows []struct {
				Name          string    `json:"full_name"`
				DefaultBranch string    `json:"default_branch"`
				Archived      bool      `json:"archived"`
				PushedAt      time.Time `json:"pushed_at"`
			}
			if err := json.Unmarshal(data, &rows); err != nil || rows == nil {
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
				break
			}
		}
	}
	slices.SortFunc(result, func(a, b DiscoveredRepository) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}
