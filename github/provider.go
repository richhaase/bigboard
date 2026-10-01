package github

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	prPageSize      = 50
	filePageSize    = 100
	contextPageSize = 50
)

// Provider is safe for concurrent independent repository fetches. It retains no
// credentials or response cache; the caller owns refresh scheduling and stale
// snapshots. Shared quota cooldown survives individual fetch cancellation.
type Provider struct {
	runner Runner
	limits limits
	quota  quotaState
	now    func() time.Time
}

type limits struct {
	pullRequests, files, requests, summaryPages int
}

// NewProvider uses the installed gh/git executables when runner is nil.
func NewProvider(runner Runner) *Provider {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Provider{runner: runner, now: time.Now, limits: limits{
		pullRequests: MaxPullRequests, files: MaxFilesPerPR, requests: MaxRequests,
		summaryPages: MaxPullRequests / prPageSize,
	}}
}

type pageInfo struct {
	HasNextPage bool
	EndCursor   string
}

type connection[T any] struct {
	Nodes      []*T
	TotalCount *int
	PageInfo   *pageInfo
}

type actor struct{ Login string }

type requestedReviewer struct {
	TypeName     string `json:"__typename"`
	Login        string
	Slug         string
	Organization *actor
}

type reviewRequest struct{ RequestedReviewer *requestedReviewer }

type rawReview struct {
	Author *actor
	State  *string
}

type rawFile struct {
	Path       string
	ChangeType *string
}

type rawPR struct {
	Number         int
	Title          string
	Author         *actor
	IsDraft        bool
	ReviewDecision *string
	Mergeable      *string
	UpdatedAt      string
	HeadRefOid     string
	BaseRefOid     string
	ReviewRequests *connection[reviewRequest]
	LatestReviews  *connection[rawReview]
	Files          *connection[rawFile]
	Commits        *struct {
		Nodes []*struct {
			Commit *struct {
				StatusCheckRollup *struct{ State *string }
			}
		}
	}
}

type rawRepository struct {
	NameWithOwner string
	PullRequests  *connection[rawPR]
	PullRequest   *rawPR
}

type apiResponse struct {
	Data *struct {
		Repository *rawRepository
		RateLimit  *struct {
			Remaining int
			ResetAt   string
		}
	}
	Errors []struct{ Message, Type string }
}

type fetchedPR struct {
	pr            PullRequest
	fileCursor    string
	fileTotal     int
	seenPaths     map[string]bool
	seenCursors   map[string]bool
	needsFiles    bool
	metadataValid bool
	headOID       string
	baseOID       string
}

type fetchSession struct {
	provider *Provider
	ctx      context.Context
	requests int
	blocked  error
}

// Fetch reads open PR summaries and current changed paths from github.com only.
// GraphQL fields are deliberately narrow: no descriptions, comments, patches,
// check logs, or file contents. Limits never silently turn partial data into a
// complete result, and a failed refresh can be kept separate from stale data.
func (p *Provider) Fetch(ctx context.Context, ownerRepo string) Result {
	repo, err := CanonicalRepo(ownerRepo)
	if err != nil {
		return Result{Err: err}
	}
	result := Result{Repo: repo, PRs: []PullRequest{}}
	if until := p.cooldown(); until.After(p.now()) {
		result.Err, result.RetryAt = ErrRateLimited, until
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	s := fetchSession{provider: p, ctx: ctx}
	rows := make(map[int]*fetchedPR)
	cursor := ""
	seenCursors := make(map[string]bool)
	inventoryValid := true
	total := -1
	for page := 0; ; page++ {
		if page >= p.limits.summaryPages || len(rows) >= p.limits.pullRequests {
			result.Err = joinError(result.Err, ErrBudgetExceeded)
			break
		}
		response, requestErr := s.request(summaryQuery, repo, cursor, 0)
		if response.Data == nil || response.Data.Repository == nil {
			if requestErr == nil {
				requestErr = ErrRepository
			}
			result.Err = joinError(result.Err, requestErr)
			break
		}
		data := response.Data.Repository
		canonical, canonicalErr := CanonicalRepo(data.NameWithOwner)
		if canonicalErr != nil || (page > 0 && canonical != result.Repo) {
			result.Err = joinError(result.Err, ErrResponse)
			break
		}
		result.Repo, repo = canonical, canonical
		conn := data.PullRequests
		if conn == nil || conn.PageInfo == nil || conn.TotalCount == nil || *conn.TotalCount < 0 {
			result.Err = joinError(result.Err, joinError(requestErr, ErrResponse))
			break
		}
		if total != -1 && total != *conn.TotalCount {
			inventoryValid = false
			result.Err = joinError(result.Err, ErrIncomplete)
		}
		total = *conn.TotalCount
		if len(conn.Nodes) > prPageSize {
			inventoryValid = false
			result.Err = joinError(result.Err, ErrResponse)
		}
		for i, raw := range conn.Nodes {
			if i >= prPageSize || len(rows) >= p.limits.pullRequests {
				inventoryValid = false
				result.Err = joinError(result.Err, ErrBudgetExceeded)
				break
			}
			if raw == nil || raw.Number < 1 {
				inventoryValid = false
				result.Err = joinError(result.Err, ErrResponse)
				continue
			}
			row := p.convertPR(repo, raw)
			if previous, exists := rows[raw.Number]; !exists || row.pr.UpdatedAt.After(previous.pr.UpdatedAt) {
				rows[raw.Number] = row
			}
		}
		if requestErr != nil {
			result.Err = joinError(result.Err, requestErr)
			break
		}
		if !conn.PageInfo.HasNextPage {
			result.Complete = inventoryValid && len(rows) == total
			if !result.Complete {
				result.Err = joinError(result.Err, ErrIncomplete)
			}
			break
		}
		next := conn.PageInfo.EndCursor
		if !validCursor(next) || seenCursors[next] || len(conn.Nodes) == 0 {
			result.Err = joinError(result.Err, ErrResponse)
			break
		}
		seenCursors[next], cursor = true, next
	}

	// Deterministic order also gives the most recently updated PRs priority
	// when the total request budget prevents collecting every file page.
	ordered := make([]*fetchedPR, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].pr.UpdatedAt.Equal(ordered[j].pr.UpdatedAt) {
			return ordered[i].pr.UpdatedAt.After(ordered[j].pr.UpdatedAt)
		}
		return ordered[i].pr.Number < ordered[j].pr.Number
	})
	for _, row := range ordered {
		if row.needsFiles {
			result.Err = joinError(result.Err, s.fetchFiles(result.Repo, row))
		}
		row.pr.ContextComplete = row.metadataValid && row.pr.FilesComplete && row.pr.PreviousPathsComplete &&
			row.pr.RequestedReviewersComplete && row.pr.ReviewsComplete
		result.PRs = append(result.PRs, row.pr)
	}
	result.RetryAt = p.cooldown()
	return result
}

func (p *Provider) convertPR(repo string, raw *rawPR) *fetchedPR {
	updated, timeErr := time.Parse(time.RFC3339, raw.UpdatedAt)
	row := &fetchedPR{
		pr: PullRequest{
			Repo: repo, Number: raw.Number, URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(raw.Number),
			Title: raw.Title, Draft: raw.IsDraft, UpdatedAt: updated,
			ReviewDecision: normalizeState(raw.ReviewDecision, "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED"),
			Mergeable:      normalizeState(raw.Mergeable, "MERGEABLE", "CONFLICTING"), CheckState: Unknown,
			PreviousPathsComplete: true,
		},
		seenPaths: make(map[string]bool), seenCursors: make(map[string]bool),
		metadataValid: timeErr == nil && validOID(raw.HeadRefOid) && validOID(raw.BaseRefOid),
		headOID:       raw.HeadRefOid, baseOID: raw.BaseRefOid,
	}
	if raw.Author != nil {
		row.pr.Author = raw.Author.Login
	}
	if raw.Commits != nil && len(raw.Commits.Nodes) == 1 && raw.Commits.Nodes[0] != nil {
		commit := raw.Commits.Nodes[0].Commit
		if commit != nil && commit.StatusCheckRollup != nil {
			row.pr.CheckState = normalizeState(commit.StatusCheckRollup.State, "ERROR", "EXPECTED", "FAILURE", "PENDING", "SUCCESS")
		}
	}
	row.pr.RequestedReviewers, row.pr.RequestedReviewersComplete = convertReviewers(raw.ReviewRequests)
	row.pr.Reviews, row.pr.ReviewsComplete = convertReviews(raw.LatestReviews)
	if raw.Files == nil || raw.Files.TotalCount == nil || raw.Files.PageInfo == nil || *raw.Files.TotalCount < 0 {
		row.pr.PreviousPathsComplete = false
		return row
	}
	row.fileTotal = *raw.Files.TotalCount
	row.consumeFiles(raw.Files, p.limits.files)
	return row
}

func connectionComplete[T any](conn *connection[T]) bool {
	return conn != nil && conn.TotalCount != nil && conn.PageInfo != nil && !conn.PageInfo.HasNextPage &&
		*conn.TotalCount == len(conn.Nodes) && len(conn.Nodes) <= contextPageSize
}

func convertReviewers(conn *connection[reviewRequest]) ([]string, bool) {
	complete := connectionComplete(conn)
	if conn == nil {
		return nil, false
	}
	var reviewers []string
	seen := make(map[string]bool)
	for i, node := range conn.Nodes {
		if i >= contextPageSize {
			break
		}
		if node == nil || node.RequestedReviewer == nil {
			complete = false
			continue
		}
		reviewer := node.RequestedReviewer
		var name string
		switch reviewer.TypeName {
		case "User", "Mannequin":
			name = reviewer.Login
		case "Team":
			if reviewer.Organization != nil && reviewer.Organization.Login != "" && reviewer.Slug != "" {
				name = reviewer.Organization.Login + "/" + reviewer.Slug
			}
		}
		if name == "" {
			complete = false
			continue
		}
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			reviewers = append(reviewers, name)
		}
	}
	return reviewers, complete
}

func convertReviews(conn *connection[rawReview]) ([]Review, bool) {
	complete := connectionComplete(conn)
	if conn == nil {
		return nil, false
	}
	var reviews []Review
	seen := make(map[string]bool)
	for i, node := range conn.Nodes {
		if i >= contextPageSize {
			break
		}
		if node == nil || node.Author == nil || node.Author.Login == "" {
			complete = false
			continue
		}
		name := node.Author.Login
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			reviews = append(reviews, Review{Author: name,
				State: normalizeState(node.State, "PENDING", "COMMENTED", "APPROVED", "CHANGES_REQUESTED", "DISMISSED")})
		}
	}
	return reviews, complete
}

func (row *fetchedPR) consumeFiles(conn *connection[rawFile], limit int) {
	valid := len(conn.Nodes) <= filePageSize
	for i, node := range conn.Nodes {
		if i >= filePageSize || len(row.pr.Files) >= limit {
			valid = false
			break
		}
		if node == nil || node.Path == "" {
			valid = false
			continue
		}
		kind := normalizeState(node.ChangeType, "ADDED", "CHANGED", "COPIED", "DELETED", "MODIFIED", "RENAMED", "UNCHANGED")
		if kind == "RENAMED" || kind == "COPIED" || kind == Unknown {
			row.pr.PreviousPathsComplete = false
		}
		if !row.seenPaths[node.Path] {
			row.seenPaths[node.Path] = true
			row.pr.Files = append(row.pr.Files, File{Path: node.Path, ChangeType: kind})
		}
	}
	row.fileCursor = conn.PageInfo.EndCursor
	row.pr.FilesComplete = valid && !conn.PageInfo.HasNextPage && len(row.pr.Files) == row.fileTotal
	row.needsFiles = valid && conn.PageInfo.HasNextPage && validCursor(row.fileCursor) && !row.seenCursors[row.fileCursor]
	if row.needsFiles {
		row.seenCursors[row.fileCursor] = true
	}
	if !row.pr.FilesComplete {
		// Missing files may include a rename even when the known files do not.
		row.pr.PreviousPathsComplete = false
	}
}

func (s *fetchSession) fetchFiles(repo string, row *fetchedPR) error {
	if !row.metadataValid {
		return ErrIncomplete
	}
	// Preserve knowledge of known rename/copy origins independently from the
	// temporary incompleteness of a paginated current-path list.
	originsKnown := true
	for _, file := range row.pr.Files {
		if file.ChangeType == "RENAMED" || file.ChangeType == "COPIED" || file.ChangeType == Unknown {
			originsKnown = false
		}
	}
	for row.needsFiles {
		if len(row.pr.Files) >= s.provider.limits.files {
			return ErrBudgetExceeded
		}
		response, requestErr := s.request(filesQuery, repo, row.fileCursor, row.pr.Number)
		if response.Data == nil || response.Data.Repository == nil || response.Data.Repository.PullRequest == nil {
			if requestErr != nil {
				return requestErr
			}
			return ErrIncomplete
		}
		canonical, err := CanonicalRepo(response.Data.Repository.NameWithOwner)
		raw := response.Data.Repository.PullRequest
		updated, timeErr := time.Parse(time.RFC3339, raw.UpdatedAt)
		if err != nil || canonical != repo || raw.Number != row.pr.Number || timeErr != nil || !updated.Equal(row.pr.UpdatedAt) ||
			raw.HeadRefOid != row.headOID || raw.BaseRefOid != row.baseOID {
			return ErrIncomplete
		}
		conn := raw.Files
		if conn == nil || conn.PageInfo == nil || conn.TotalCount == nil || *conn.TotalCount != row.fileTotal {
			return ErrIncomplete
		}
		row.pr.PreviousPathsComplete = originsKnown
		row.consumeFiles(conn, s.provider.limits.files)
		for _, file := range row.pr.Files {
			if file.ChangeType == "RENAMED" || file.ChangeType == "COPIED" || file.ChangeType == Unknown {
				originsKnown = false
			}
		}
		if requestErr != nil {
			row.pr.FilesComplete = false
			row.pr.PreviousPathsComplete = false
			return requestErr
		}
		if len(conn.Nodes) == 0 || !row.needsFiles && !row.pr.FilesComplete {
			return ErrIncomplete
		}
	}
	return nil
}

func (s *fetchSession) request(query, repo, cursor string, number int) (apiResponse, error) {
	var response apiResponse
	if s.ctx.Err() != nil {
		return response, s.ctx.Err()
	}
	if s.blocked != nil {
		return response, s.blocked
	}
	if s.provider.cooldown().After(s.provider.now()) {
		return response, ErrRateLimited
	}
	if s.requests >= s.provider.limits.requests || !takeRequest(s.ctx) {
		return response, ErrBudgetExceeded
	}
	owner, name, _ := strings.Cut(repo, "/")
	args := []string{"api", "graphql", "--hostname", "github.com", "--method", "POST", "--include",
		"-f", "query=" + query, "-f", "owner=" + owner, "-f", "name=" + name}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	if number != 0 {
		args = append(args, "-F", "number="+strconv.Itoa(number))
	}
	s.requests++
	ctx, cancel := context.WithTimeout(s.ctx, RequestTimeout)
	defer cancel()
	data, err := s.provider.runner.Run(ctx, "gh", args...)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	err = safeRequestError(err)
	if len(data) > MaxOutputBytes {
		return response, ErrOutputLimit
	}
	data, retryAt, limited, throttled := responseBody(data, s.provider.now())
	if throttled {
		err = ErrRateLimited
	}
	// Inspect all response forms, including errors and successful last pages.
	defer func() {
		if response.Data != nil && response.Data.RateLimit != nil {
			if response.Data.RateLimit.Remaining <= 0 {
				limited = true
			}
			if reset, parseErr := time.Parse(time.RFC3339, response.Data.RateLimit.ResetAt); parseErr == nil && reset.After(retryAt) && limited {
				retryAt = reset
			}
		}
		if limited || errors.Is(err, ErrRateLimited) {
			s.provider.limitUntil(retryAt)
			s.blocked = ErrRateLimited
		} else if errors.Is(err, ErrAuthentication) || errors.Is(err, ErrUnavailable) {
			s.blocked = err
		} else if err == nil {
			s.provider.quotaSuccess()
		}
	}()
	if limited && len(data) == 0 {
		err = ErrRateLimited
	}
	if len(data) == 0 && err != nil {
		return response, err
	}
	if json.Unmarshal(data, &response) != nil {
		err = joinError(err, ErrResponse)
		return apiResponse{}, err
	}
	for _, problem := range response.Errors {
		kind := classifyFailure(problem.Type + " " + problem.Message)
		if kind == ErrRequest {
			kind = ErrIncomplete
		}
		err = joinError(err, kind)
	}
	return response, err
}

func normalizeState(value *string, known ...string) string {
	if value != nil {
		for _, state := range known {
			if *value == state {
				return state
			}
		}
	}
	return Unknown
}

func validOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validCursor(cursor string) bool {
	return cursor != "" && len(cursor) <= 2048 && !strings.ContainsRune(cursor, '\x00')
}

// Avoid multiplying the same human-readable error for every partial PR.
func joinError(current, next error) error {
	if next == nil || errors.Is(current, next) {
		return current
	}
	if current == nil {
		return next
	}
	return errors.Join(current, next)
}

const summaryQuery = `query BigBoardOpenPullRequests($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    pullRequests(states: OPEN, first: 50, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC}) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        number title isDraft updatedAt headRefOid baseRefOid author { login } reviewDecision mergeable
        reviewRequests(first: 50) {
          totalCount pageInfo { hasNextPage endCursor }
          nodes { requestedReviewer {
            __typename
            ... on User { login }
            ... on Mannequin { login }
            ... on Team { slug organization { login } }
          } }
        }
        latestReviews(first: 50) {
          totalCount pageInfo { hasNextPage endCursor }
          nodes { author { login } state }
        }
        commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
        files(first: 100) {
          totalCount pageInfo { hasNextPage endCursor }
          nodes { path changeType }
        }
      }
    }
  }
  rateLimit { remaining resetAt }
}`

const filesQuery = `query BigBoardPullRequestFiles($owner: String!, $name: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    pullRequest(number: $number) {
      number updatedAt headRefOid baseRefOid
      files(first: 100, after: $cursor) {
        totalCount pageInfo { hasNextPage endCursor }
        nodes { path changeType }
      }
    }
  }
  rateLimit { remaining resetAt }
}`
