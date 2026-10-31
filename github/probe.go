package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const probeBatchSize = 100

type RepositoryProbe struct {
	Repo          string
	DefaultBranch string
	HeadOID       string
	OpenPRCount   *int
	Requests      int
	RetryAt       time.Time
	Err           error
}

func (p *Provider) ProbeAll(ctx context.Context, repositories []string) []RepositoryProbe {
	if _, ok := ctx.Value(budgetKey{}).(*requestBudget); !ok {
		ctx = WithRequestBudget(ctx, MaxRefreshRequests)
	}
	results := make([]RepositoryProbe, len(repositories))
	indexes := make(map[string][]int)
	var unique []string
	for i, id := range repositories {
		repo, err := CanonicalRepo(id)
		results[i] = RepositoryProbe{Repo: repo, Err: err}
		if err != nil {
			continue
		}
		if len(indexes[repo]) == 0 {
			unique = append(unique, repo)
		}
		indexes[repo] = append(indexes[repo], i)
	}
	session := fetchSession{provider: p, ctx: ctx, limit: MaxRefreshRequests, aliases: true}
	for start := 0; start < len(unique); start += probeBatchSize {
		batch := unique[start:min(start+probeBatchSize, len(unique))]
		before := session.requests
		response, err := session.request(repositoryProbeQuery(batch), "", "", 0)
		for i, repo := range batch {
			result := parseRepositoryProbe(response, fmt.Sprintf("repo%d", i), repo, err)
			result.RetryAt = p.cooldown()
			if i == 0 {
				result.Requests = session.requests - before
			}
			for j, index := range indexes[repo] {
				results[index] = result
				if j > 0 {
					results[index].Requests = 0
				}
			}
		}
	}
	return results
}

func repositoryProbeQuery(repositories []string) string {
	var query strings.Builder
	query.WriteString("query BigBoardRepositoryProbe {\n")
	for i, repo := range repositories {
		owner, name, _ := strings.Cut(repo, "/")
		fmt.Fprintf(&query, "repo%d: repository(owner: %q, name: %q) { nameWithOwner defaultBranchRef { name target { oid } } pullRequests(first: 1, states: OPEN) { totalCount } }\n", i, owner, name)
	}
	query.WriteString("rateLimit { remaining resetAt }\n}")
	return query.String()
}

func parseRepositoryProbe(response apiResponse, alias, repo string, requestErr error) RepositoryProbe {
	result := RepositoryProbe{Repo: repo}
	if requestErr != nil && (len(response.Errors) == 0 || !probeFieldErrors(requestErr)) {
		result.Err = requestErr
		return result
	}
	for _, problem := range response.Errors {
		if len(problem.Path) == 0 || problem.Path[0] == alias {
			kind := classifyFailure(problem.Type + " " + problem.Message)
			if kind == ErrRequest {
				kind = ErrIncomplete
			}
			result.Err = joinError(result.Err, kind)
			return result
		}
	}
	var data *struct {
		NameWithOwner    string
		DefaultBranchRef *struct {
			Name   string
			Target *struct{ OID string }
		}
		PullRequests *struct{ TotalCount *int }
	}
	raw := response.probeData[alias]
	if len(raw) == 0 || json.Unmarshal(raw, &data) != nil || data == nil {
		result.Err = ErrResponse
		return result
	}
	canonical, err := CanonicalRepo(data.NameWithOwner)
	if err != nil || data.PullRequests == nil || data.PullRequests.TotalCount == nil || *data.PullRequests.TotalCount < 0 {
		result.Err = ErrResponse
		return result
	}
	result.Repo, result.OpenPRCount = canonical, data.PullRequests.TotalCount
	branch := data.DefaultBranchRef
	if branch != nil && branch.Name != "" && branch.Target != nil && validOID(branch.Target.OID) {
		result.DefaultBranch, result.HeadOID = branch.Name, branch.Target.OID
	}
	return result
}

func probeFieldErrors(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !probeFieldErrors(child) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, ErrRequest) || errors.Is(err, ErrIncomplete) || errors.Is(err, ErrRepository) || errors.Is(err, ErrPermission)
}
