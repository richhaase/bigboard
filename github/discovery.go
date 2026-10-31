package github

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type DiscoveredRepository struct {
	OwnerType     string
	Name          string
	DefaultBranch string
	PushedAt      time.Time
}

func (p *Provider) DiscoverRepositories(ctx context.Context, organizations []string) ([]DiscoveredRepository, error) {
	owners := make(map[string]string, len(organizations))
	for _, organization := range organizations {
		canonical, err := CanonicalRepo(organization + "/repository")
		if err != nil {
			return nil, fmt.Errorf("invalid organization %q", organization)
		}
		owner, _, _ := strings.Cut(canonical, "/")
		owners[owner] = "Organization"
	}
	if len(owners) == 0 {
		return []DiscoveredRepository{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	session := discoverySession{provider: p, limit: MaxRefreshRequests}
	return session.repositories(ctx, owners)
}
