package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (p *Provider) DiscoverPersonalRepositories(ctx context.Context, account string) ([]DiscoveredRepository, error) {
	canonical, err := CanonicalRepo(account + "/repository")
	if err != nil {
		return nil, fmt.Errorf("invalid personal GitHub account %q", account)
	}
	owner, _, _ := strings.Cut(canonical, "/")
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	session := discoverySession{provider: p, limit: MaxRefreshRequests}
	data, err := session.request(ctx, "user")
	if err != nil {
		return nil, err
	}
	var user *actor
	if json.Unmarshal(data, &user) != nil || user == nil || !validOwner(strings.ReplaceAll(user.Login, "_", "a")) {
		return nil, ErrResponse
	}
	if !strings.EqualFold(user.Login, owner) {
		return nil, fmt.Errorf("GitHub is signed in as %q, but personal repositories are configured for %q; sign in with the correct GitHub account or update Settings", user.Login, owner)
	}
	return session.repositories(ctx, map[string]string{owner: "User"})
}
