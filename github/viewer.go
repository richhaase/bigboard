package github

import (
	"context"
	"strings"
)

func (p *Provider) CurrentLogin(ctx context.Context) (string, error) {
	session := fetchSession{provider: p, ctx: ctx, limit: 1}
	response, err := session.request(viewerQuery, "", "", 0)
	if err != nil {
		return "", err
	}
	if response.Data == nil || response.Data.Viewer == nil || !validOwner(strings.ReplaceAll(response.Data.Viewer.Login, "_", "a")) {
		return "", ErrResponse
	}
	return response.Data.Viewer.Login, nil
}

const viewerQuery = `query BigBoardCurrentAccount {
  viewer { login }
  rateLimit { remaining resetAt }
}`
