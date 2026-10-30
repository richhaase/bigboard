package github

import (
	"context"
	"encoding/json"
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
	session := discoverySession{provider: p, limit: maxSetupRequests}
	data, err := session.request(ctx, "user")
	if err != nil {
		return Account{}, err
	}
	var user *actor
	if json.Unmarshal(data, &user) != nil || user == nil || !validOwner(strings.ReplaceAll(user.Login, "_", "a")) {
		return Account{}, ErrResponse
	}
	installations, err := session.installations(ctx)
	if err != nil {
		return Account{}, err
	}
	account := Account{Login: user.Login, Organizations: []string{}}
	seen := make(map[string]bool)
	for _, installed := range installations {
		login := strings.ToLower(installed.Account.Login)
		if installed.Account.Type == "Organization" && !seen[login] {
			account.Organizations = append(account.Organizations, login)
			seen[login] = true
		}
	}
	slices.Sort(account.Organizations)
	return account, nil
}
