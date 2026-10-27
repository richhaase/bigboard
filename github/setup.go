package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const maxSetupRequests = 16

type Account struct {
	Login         string
	Organizations []string
}

func (p *Provider) SetupAccount(ctx context.Context) (Account, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	data, err := p.setupRequest(ctx, "user")
	if err != nil {
		return Account{}, err
	}
	var user *actor
	if err := json.Unmarshal(data, &user); err != nil || user == nil || !validOwner(strings.ReplaceAll(user.Login, "_", "a")) {
		return Account{}, ErrResponse
	}
	account := Account{Login: user.Login, Organizations: []string{}}
	seen := make(map[string]bool)
	for page := 1; page < maxSetupRequests; page++ {
		endpoint := fmt.Sprintf("user/orgs?per_page=100&page=%d", page)
		data, err := p.setupRequest(ctx, endpoint)
		if err != nil {
			return Account{}, err
		}
		var organizations []*actor
		if err := json.Unmarshal(data, &organizations); err != nil || organizations == nil || len(organizations) > 100 {
			return Account{}, ErrResponse
		}
		for _, organization := range organizations {
			if organization == nil {
				return Account{}, ErrResponse
			}
			canonical, err := CanonicalRepo(organization.Login + "/repository")
			if err != nil {
				return Account{}, ErrResponse
			}
			login, _, _ := strings.Cut(canonical, "/")
			if !seen[login] {
				seen[login] = true
				account.Organizations = append(account.Organizations, login)
			}
		}
		if len(organizations) < 100 {
			slices.Sort(account.Organizations)
			return account, nil
		}
	}
	return Account{}, ErrBudgetExceeded
}

func (p *Provider) setupRequest(ctx context.Context, endpoint string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	data, err := p.runner.Run(ctx, "gh", "api", "--hostname", "github.com", "--method", "GET", endpoint)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, safeRequestError(err)
	}
	if len(data) > MaxOutputBytes {
		return nil, ErrOutputLimit
	}
	return data, nil
}
