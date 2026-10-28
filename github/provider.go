package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	prPageSize           = 50
	fileBatchSize        = 50
	filePageSize         = 100
	contextPageSize      = 50
	inventoryConcurrency = 4
	inventoryBatchNodes  = 50_000
	leanInventoryBytes   = 128 * 1024
)

// Provider is safe for concurrent independent repository fetches. It retains no
// credentials or response cache; the caller owns refresh scheduling and stale
// snapshots. Shared quota cooldown survives individual fetch cancellation.
type Provider struct {
	runner      Runner
	limits      limits
	quota       quotaState
	now         func() time.Time
	conditional conditionalCache
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
	Author      *actor
	State       *string
	SubmittedAt string
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

type apiData struct {
	Repository *rawRepository
	Viewer     *actor
	RateLimit  *struct {
		Remaining int
		ResetAt   string
	}
}

type apiError struct {
	Message, Type string
	Path          []any
}

type apiResponse struct {
	probeData map[string]json.RawMessage
	Data      *apiData
	Errors    []apiError
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
	aliases    bool
	provider   *Provider
	ctx        context.Context
	requests   int
	blocked    error
	limit      int
	allowance  *repositoryAllowance
	allowances []*repositoryAllowance
	registry   *inventoryRegistry
}

// Fetch reads open PR summaries and current changed paths from github.com only.
// GraphQL fields are deliberately narrow: no descriptions, comments, patches,
// check logs, or file contents. Limits never silently turn partial data into a
// complete result, and a failed refresh can be kept separate from stale data.
func (p *Provider) Fetch(ctx context.Context, ownerRepo string) Result {
	return p.FetchAll(ctx, []RepositoryRequest{{Repo: ownerRepo}})[0]
}

type repositoryFetch struct {
	result    Result
	session   fetchSession
	rows      map[int]*fetchedPR
	canonical *repositoryFetch
	lean      bool
	fetched   bool
}

func (p *Provider) FetchAll(ctx context.Context, repositories []RepositoryRequest) []Result {
	if _, ok := ctx.Value(budgetKey{}).(*requestBudget); !ok {
		ctx = WithRequestBudget(ctx, MaxRefreshRequests)
	}
	registry := &inventoryRegistry{known: make(map[string]*repositoryFetch)}
	batches := make([]*repositoryFetch, len(repositories))
	unique := make(map[string]*repositoryFetch)
	var jobs []*repositoryFetch
	for i, request := range repositories {
		repo, err := CanonicalRepo(request.Repo)
		if existing := unique[repo]; existing != nil && err == nil {
			existing.lean = existing.lean || len(request.Previous) > 0
			batches[i] = existing
			continue
		}
		batch := &repositoryFetch{
			result: Result{Repo: repo, PRs: []PullRequest{}, Err: err, InventoryErr: err},
			session: fetchSession{provider: p, ctx: ctx, registry: registry,
				allowance: &repositoryAllowance{limit: repositoryRequestLimit(ctx, p.limits.requests)}},
			rows: make(map[int]*fetchedPR),
			lean: len(request.Previous) > 0,
		}
		batch.canonical = batch
		batches[i] = batch
		if err == nil {
			unique[repo] = batch
			jobs = append(jobs, batch)
		}
	}
	var inline, lean []*repositoryFetch
	for _, batch := range jobs {
		if batch.lean {
			lean = append(lean, batch)
		} else {
			inline = append(inline, batch)
		}
	}
	chunks := append(inventoryChunks(inline, 1), inventoryChunks(lean, inventoryBatchSize())...)
	queue := make(chan []*repositoryFetch)
	var workers sync.WaitGroup
	for range min(inventoryConcurrency, len(chunks)) {
		workers.Go(func() {
			for chunk := range queue {
				p.fetchInventories(ctx, registry, chunk)
			}
		})
	}
	for _, chunk := range chunks {
		queue <- chunk
	}
	close(queue)
	workers.Wait()
	winners := make(map[*repositoryFetch]*repositoryFetch)
	requests := make(map[*repositoryFetch]int)
	counted := make(map[*repositoryFetch]bool)
	for _, batch := range batches {
		if counted[batch] {
			continue
		}
		counted[batch] = true
		owner := batch.canonical
		requests[owner] += batch.session.requests
		if batch != owner && !batch.fetched {
			continue
		}
		if winner := winners[owner]; winner == nil || batch.result.Complete && !winner.result.Complete ||
			!winner.result.Complete && !batch.result.Complete && len(batch.rows) > len(winner.rows) {
			winners[owner] = batch
		}
	}
	for _, winner := range winners {
		winner.session.requests = requests[winner.canonical]
		winner.result.InventoryRequests = winner.session.requests
		winner.session.allowance = winner.canonical.session.allowance
	}
	for i, request := range repositories {
		batch := winners[batches[i].canonical]
		batches[i] = batch
		for _, previous := range request.Previous {
			if row := batch.rows[previous.Number]; row != nil && reusableFiles(row.pr, previous) {
				row.pr.Files = slices.Clone(previous.Files)
				row.pr.FilesComplete, row.pr.PreviousPathsComplete = previous.FilesComplete, previous.PreviousPathsComplete
				row.needsFiles = false
			}
		}
	}
	type pendingFiles struct {
		batch *repositoryFetch
		row   *fetchedPR
	}
	var pending []pendingFiles
	seen := make(map[*repositoryFetch]bool)
	for _, batch := range batches {
		if seen[batch] {
			continue
		}
		seen[batch] = true
		for _, row := range batch.rows {
			if row.needsFiles {
				pending = append(pending, pendingFiles{batch: batch, row: row})
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		a, b := pending[i].row.pr, pending[j].row.pr
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.Key() < b.Key()
	})
	for i, task := range pending {
		if !task.row.needsFiles {
			continue
		}
		if firstFilePage(task.row, p.limits.files) {
			group := []*fetchedPR{task.row}
			for _, later := range pending[i+1:] {
				if len(group) == fileBatchSize {
					break
				}
				if later.batch == task.batch && firstFilePage(later.row, p.limits.files) {
					group = append(group, later.row)
				}
			}
			err := task.batch.session.fetchFirstFiles(task.batch.result.Repo, group)
			task.batch.result.FilesErr = joinError(task.batch.result.FilesErr, err)
			if !task.row.needsFiles {
				continue
			}
		}
		err := task.batch.session.fetchFiles(task.batch.result.Repo, task.row)
		task.batch.result.FilesErr = joinError(task.batch.result.FilesErr, err)
	}
	results := make([]Result, len(batches))
	counted = make(map[*repositoryFetch]bool)
	for i, batch := range batches {
		if len(batch.result.PRs) == 0 {
			for _, row := range batch.rows {
				row.pr.ContextComplete = row.metadataValid && row.pr.FilesComplete && row.pr.PreviousPathsComplete &&
					row.pr.RequestedReviewersComplete && row.pr.ReviewsComplete
				batch.result.PRs = append(batch.result.PRs, row.pr)
			}
			sort.Slice(batch.result.PRs, func(i, j int) bool {
				a, b := batch.result.PRs[i], batch.result.PRs[j]
				if !a.UpdatedAt.Equal(b.UpdatedAt) {
					return a.UpdatedAt.After(b.UpdatedAt)
				}
				return a.Number < b.Number
			})
		}
		batch.result.FileRequests = batch.session.requests - batch.result.InventoryRequests
		batch.result.Err = joinError(batch.result.InventoryErr, batch.result.FilesErr)
		batch.result.RetryAt = p.cooldown()
		results[i] = batch.result
		if counted[batch] {
			results[i].InventoryRequests, results[i].FileRequests = 0, 0
		}
		counted[batch] = true
	}
	return results
}

func sharedFailure(err error) bool {
	return errors.Is(err, ErrAuthentication) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrRateLimited)
}

func reusableFiles(current, previous PullRequest) bool {
	return !current.FilesComplete && previous.FilesComplete && current.Key() == previous.Key() &&
		!current.UpdatedAt.IsZero() && current.UpdatedAt.Equal(previous.UpdatedAt) &&
		validOID(current.HeadOID) && current.HeadOID == previous.HeadOID &&
		validOID(current.BaseOID) && current.BaseOID == previous.BaseOID &&
		current.FileCount == previous.FileCount && len(previous.Files) == current.FileCount
}

func inventoryBatchSize() int {
	nodes := prPageSize * (2 + 2*contextPageSize)
	return max(1, min(inventoryBatchNodes/nodes, MaxOutputBytes/2/leanInventoryBytes))
}

func inventoryChunks(jobs []*repositoryFetch, size int) [][]*repositoryFetch {
	count := (len(jobs) + size - 1) / size
	chunks := make([][]*repositoryFetch, 0, count)
	for i := range count {
		chunks = append(chunks, jobs[i*len(jobs)/count:(i+1)*len(jobs)/count])
	}
	return chunks
}

type inventoryPage struct {
	page        int
	cursor      string
	seenCursors map[string]bool
	valid       bool
	total       int
	done        bool
}

func (p *Provider) fetchInventories(ctx context.Context, registry *inventoryRegistry, chunk []*repositoryFetch) {
	var batches []*repositoryFetch
	for _, batch := range chunk {
		if existing := registry.lookup(batch.result.Repo); existing != nil {
			batch.canonical = existing
			continue
		}
		batches = append(batches, batch)
	}
	pages := make([]*inventoryPage, len(batches))
	for i := range batches {
		pages[i] = &inventoryPage{seenCursors: make(map[string]bool), valid: true, total: -1}
	}
	if len(batches) > 1 {
		p.fetchFirstInventories(ctx, registry, batches, pages)
	}
	for i, batch := range batches {
		p.fetchInventory(batch, pages[i])
		batch.result.InventoryErr = batch.result.Err
	}
}

func (p *Provider) fetchFirstInventories(ctx context.Context, registry *inventoryRegistry, batches []*repositoryFetch, pages []*inventoryPage) {
	var members []int
	var repos []string
	var allowances []*repositoryAllowance
	for i, batch := range batches {
		if p.inventoryRoom(batch, pages[i]) {
			members = append(members, i)
			repos = append(repos, batch.result.Repo)
			allowances = append(allowances, batch.session.allowance)
		}
	}
	if len(members) < 2 {
		return
	}
	session := fetchSession{provider: p, ctx: ctx, registry: registry, aliases: true, allowances: allowances}
	response, requestErr := session.request(inventoryBatchQuery(repos), "", "", 0)
	batches[members[0]].session.requests += session.requests
	if errors.Is(requestErr, ErrOutputLimit) {
		return
	}
	aliases := make(map[string]int, len(members))
	for i := range members {
		aliases[fmt.Sprintf("repo%d", i)] = i
	}
	owned := make([][]apiError, len(members))
	var shared []apiError
	scoped := response.Data != nil && (requestErr == nil || probeFieldErrors(requestErr))
	for _, problem := range response.Errors {
		var alias string
		if len(problem.Path) > 0 {
			alias, _ = problem.Path[0].(string)
		}
		member, ok := aliases[alias]
		if !ok {
			scoped = false
			shared = append(shared, problem)
			continue
		}
		problem.Path = append([]any{"repository"}, problem.Path[1:]...)
		owned[member] = append(owned[member], problem)
	}
	for i, index := range members {
		memberResponse, memberErr := apiResponse{Errors: append(owned[i], shared...)}, requestErr
		if scoped {
			memberErr = nil
			for _, problem := range owned[i] {
				kind := classifyFailure(problem.Type + " " + problem.Message)
				if kind == ErrRequest {
					kind = ErrIncomplete
				}
				memberErr = joinError(memberErr, kind)
			}
		}
		if response.Data != nil {
			var repository *rawRepository
			if raw := response.probeData[fmt.Sprintf("repo%d", i)]; len(raw) > 0 && json.Unmarshal(raw, &repository) != nil {
				repository, memberErr = nil, joinError(memberErr, ErrResponse)
			}
			memberResponse.Data = &apiData{Repository: repository, RateLimit: response.Data.RateLimit}
		}
		p.acceptInventoryPage(batches[index], pages[index], memberResponse, memberErr)
	}
}

func (p *Provider) fetchInventory(batch *repositoryFetch, state *inventoryPage) {
	for p.inventoryRoom(batch, state) {
		query := summaryQuery
		if batch.lean {
			query = leanSummaryQuery
		}
		response, requestErr := batch.session.request(query, batch.result.Repo, state.cursor, 0)
		p.acceptInventoryPage(batch, state, response, requestErr)
	}
}

func (p *Provider) inventoryRoom(batch *repositoryFetch, state *inventoryPage) bool {
	if state.done {
		return false
	}
	if state.page >= p.limits.summaryPages || len(batch.rows) >= p.limits.pullRequests {
		batch.result.Err = joinError(batch.result.Err, ErrBudgetExceeded)
		state.done = true
	}
	return !state.done
}

func (p *Provider) acceptInventoryPage(batch *repositoryFetch, state *inventoryPage, response apiResponse, requestErr error) {
	result := &batch.result
	rows := batch.rows
	state.done = true
	if response.Data != nil && response.Data.Repository != nil {
		batch.fetched = true
	}
	if fileFieldErrors(response, requestErr) {
		result.FilesErr = joinError(result.FilesErr, requestErr)
		requestErr = nil
	}
	if response.Data == nil || response.Data.Repository == nil {
		if requestErr == nil {
			requestErr = ErrRepository
		}
		result.Err = joinError(result.Err, requestErr)
		return
	}
	data := response.Data.Repository
	canonical, canonicalErr := CanonicalRepo(data.NameWithOwner)
	if canonicalErr != nil || (state.page > 0 && canonical != result.Repo) {
		result.Err = joinError(result.Err, ErrResponse)
		return
	}
	result.Repo = canonical
	if state.page == 0 {
		batch.canonical = batch.session.registry.claim(canonical, batch)
	}
	conn := data.PullRequests
	if conn == nil || conn.PageInfo == nil || conn.TotalCount == nil || *conn.TotalCount < 0 {
		result.Err = joinError(result.Err, joinError(requestErr, ErrResponse))
		return
	}
	if state.total != -1 && state.total != *conn.TotalCount {
		state.valid = false
		result.Err = joinError(result.Err, ErrIncomplete)
	}
	state.total = *conn.TotalCount
	if len(conn.Nodes) > prPageSize {
		state.valid = false
		result.Err = joinError(result.Err, ErrResponse)
	}
	for i, raw := range conn.Nodes {
		if i >= prPageSize || len(rows) >= p.limits.pullRequests {
			state.valid = false
			result.Err = joinError(result.Err, ErrBudgetExceeded)
			break
		}
		if raw == nil || raw.Number < 1 {
			state.valid = false
			result.Err = joinError(result.Err, ErrResponse)
			continue
		}
		row := p.convertPR(canonical, raw, batch.lean)
		if previous, exists := rows[raw.Number]; !exists || row.pr.UpdatedAt.After(previous.pr.UpdatedAt) {
			rows[raw.Number] = row
		}
	}
	if requestErr != nil {
		result.Err = joinError(result.Err, requestErr)
		return
	}
	if !conn.PageInfo.HasNextPage {
		result.Complete = state.valid && len(rows) == state.total
		if !result.Complete {
			result.Err = joinError(result.Err, ErrIncomplete)
		}
		return
	}
	if batch.canonical != batch {
		result.Err = joinError(result.Err, ErrIncomplete)
		return
	}
	next := conn.PageInfo.EndCursor
	if !validCursor(next) || state.seenCursors[next] || len(conn.Nodes) == 0 {
		result.Err = joinError(result.Err, ErrResponse)
		return
	}
	state.seenCursors[next], state.cursor = true, next
	state.page++
	state.done = false
}

func fileFieldErrors(response apiResponse, err error) bool {
	if len(response.Errors) == 0 || !onlyIncompleteErrors(err) || response.Data == nil || response.Data.Repository == nil || response.Data.Repository.PullRequests == nil {
		return false
	}
	for _, problem := range response.Errors {
		path := problem.Path
		if len(path) < 5 || path[0] != "repository" || path[1] != "pullRequests" || path[2] != "nodes" || path[4] != "files" {
			return false
		}
		index, ok := path[3].(float64)
		if !ok || index < 0 || index >= float64(len(response.Data.Repository.PullRequests.Nodes)) || index != float64(int(index)) {
			return false
		}
	}
	return true
}

func onlyIncompleteErrors(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !onlyIncompleteErrors(child) {
				return false
			}
		}
		return true
	}
	return err == ErrRequest || err == ErrIncomplete
}

func (p *Provider) convertPR(repo string, raw *rawPR, lean bool) *fetchedPR {
	updated, timeErr := time.Parse(time.RFC3339, raw.UpdatedAt)
	row := &fetchedPR{
		pr: PullRequest{
			Repo: repo, Number: raw.Number, URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(raw.Number),
			Title: raw.Title, Draft: raw.IsDraft, UpdatedAt: updated,
			HeadOID: raw.HeadRefOid, BaseOID: raw.BaseRefOid, FileCount: -1,
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
	if lean && raw.Files != nil && raw.Files.TotalCount != nil && *raw.Files.TotalCount >= 0 {
		row.fileTotal = *raw.Files.TotalCount
		row.pr.FileCount = row.fileTotal
		row.pr.FilesComplete = row.fileTotal == 0
		row.pr.PreviousPathsComplete = row.pr.FilesComplete
		row.needsFiles = !row.pr.FilesComplete
		return row
	}
	if raw.Files == nil || raw.Files.TotalCount == nil || raw.Files.PageInfo == nil || *raw.Files.TotalCount < 0 {
		row.pr.PreviousPathsComplete = false
		return row
	}
	row.fileTotal = *raw.Files.TotalCount
	row.pr.FileCount = row.fileTotal
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
			submitted, _ := time.Parse(time.RFC3339, node.SubmittedAt)
			reviews = append(reviews, Review{Author: name, SubmittedAt: submitted,
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
		if err != nil || canonical != repo || !row.samePage(raw) {
			return ErrIncomplete
		}
		conn := raw.Files
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

func (row *fetchedPR) samePage(raw *rawPR) bool {
	if raw == nil {
		return false
	}
	updated, timeErr := time.Parse(time.RFC3339, raw.UpdatedAt)
	conn := raw.Files
	return raw.Number == row.pr.Number && timeErr == nil && updated.Equal(row.pr.UpdatedAt) &&
		raw.HeadRefOid == row.headOID && raw.BaseRefOid == row.baseOID &&
		conn != nil && conn.PageInfo != nil && conn.TotalCount != nil && *conn.TotalCount == row.fileTotal
}

func firstFilePage(row *fetchedPR, limit int) bool {
	return row.needsFiles && row.metadataValid && row.fileCursor == "" && len(row.pr.Files) == 0 && limit > 0
}

func (s *fetchSession) fetchFirstFiles(repo string, rows []*fetchedPR) error {
	numbers := make([]int, len(rows))
	for i, row := range rows {
		row.needsFiles = false
		numbers[i] = row.pr.Number
	}
	s.aliases = true
	response, requestErr := s.request(fileBatchQuery(numbers), repo, "", 0)
	s.aliases = false
	failed := make(map[string]bool)
	for _, problem := range response.Errors {
		if requestErr != nil && !onlyIncompleteErrors(requestErr) || len(problem.Path) < 2 || problem.Path[0] != "repository" {
			return requestErr
		}
		alias, _ := problem.Path[1].(string)
		failed[alias] = true
	}
	if response.Data == nil || response.Data.Repository == nil {
		if requestErr != nil {
			return requestErr
		}
		return ErrIncomplete
	}
	if requestErr != nil && len(response.Errors) == 0 {
		return requestErr
	}
	canonical, err := CanonicalRepo(response.Data.Repository.NameWithOwner)
	var fields map[string]json.RawMessage
	if err != nil || canonical != repo || json.Unmarshal(response.probeData["repository"], &fields) != nil {
		return ErrIncomplete
	}
	var result error
	for i, row := range rows {
		alias := fmt.Sprintf("pr%d", i)
		var raw *rawPR
		if failed[alias] || json.Unmarshal(fields[alias], &raw) != nil || !row.samePage(raw) {
			result = joinError(result, ErrIncomplete)
			continue
		}
		row.pr.PreviousPathsComplete = true
		row.consumeFiles(raw.Files, s.provider.limits.files)
		if len(raw.Files.Nodes) == 0 || !row.needsFiles && !row.pr.FilesComplete {
			row.needsFiles, row.pr.FilesComplete, row.pr.PreviousPathsComplete = false, false, false
			result = joinError(result, ErrIncomplete)
		}
	}
	if len(failed) > 0 {
		result = joinError(result, requestErr)
	}
	return result
}

func fileBatchQuery(numbers []int) string {
	var query strings.Builder
	query.WriteString("query BigBoardPullRequestFileBatch($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {\n    nameWithOwner\n")
	for i, number := range numbers {
		fmt.Fprintf(&query, "    pr%d: pullRequest(number: %d) {%s}\n", i, number, firstFilesFields)
	}
	query.WriteString("  }\n  rateLimit { remaining resetAt }\n}")
	return query.String()
}

func (s *fetchSession) request(query, repo, cursor string, number int) (apiResponse, error) {
	var response apiResponse
	if s.ctx.Err() != nil {
		return response, s.ctx.Err()
	}
	if s.blocked != nil {
		return response, s.blocked
	}
	if s.registry != nil {
		if err := s.registry.failure(); err != nil {
			return response, err
		}
	}
	if s.provider.cooldown().After(s.provider.now()) {
		return response, ErrRateLimited
	}
	timeout := RequestTimeout
	if s.allowance != nil {
		var err error
		timeout, err = s.allowance.take(s.ctx)
		if err != nil {
			return response, err
		}
	} else if len(s.allowances) > 0 {
		var err error
		timeout, err = reserveShared(s.ctx, s.allowances)
		if err != nil {
			return response, err
		}
	} else if s.requests >= s.limit || !takeRequest(s.ctx) {
		return response, ErrBudgetExceeded
	}
	args := []string{"api", "graphql", "--hostname", "github.com", "--method", "POST", "--include",
		"-f", "query=" + query}
	if repo != "" {
		owner, name, _ := strings.Cut(repo, "/")
		args = append(args, "-f", "owner="+owner, "-f", "name="+name)
	}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	if number != 0 {
		args = append(args, "-F", "number="+strconv.Itoa(number))
	}
	s.requests++
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	started := time.Now()
	data, err := s.provider.runner.Run(ctx, "gh", args...)
	elapsed := time.Since(started)
	if s.allowance != nil {
		s.allowance.charge(elapsed)
	}
	for _, allowance := range s.allowances {
		allowance.charge(elapsed)
	}
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
		if s.registry != nil {
			s.registry.fail(s.blocked)
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
	if s.aliases {
		var envelope struct{ Data map[string]json.RawMessage }
		if json.Unmarshal(data, &envelope) != nil {
			return apiResponse{}, ErrResponse
		}
		response.probeData = envelope.Data
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

const inventoryFieldsPrefix = `    nameWithOwner
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
          nodes { author { login } state submittedAt }
        }
        commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
`

const inventoryFieldsSuffix = `      }
    }
`

const inlineInventoryFields = inventoryFieldsPrefix + `        files(first: 100) {
          totalCount pageInfo { hasNextPage endCursor }
          nodes { path changeType }
        }
` + inventoryFieldsSuffix

const leanInventoryFields = inventoryFieldsPrefix + `        files { totalCount }
` + inventoryFieldsSuffix

const summaryQueryPrefix = `query BigBoardOpenPullRequests($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
`

const summaryQuerySuffix = `  }
  rateLimit { remaining resetAt }
}`

const summaryQuery = summaryQueryPrefix + inlineInventoryFields + summaryQuerySuffix

const leanSummaryQuery = summaryQueryPrefix + leanInventoryFields + summaryQuerySuffix

func inventoryBatchQuery(repositories []string) string {
	var query strings.Builder
	query.WriteString("query BigBoardOpenPullRequestBatch($cursor: String) {\n")
	for i, repo := range repositories {
		owner, name, _ := strings.Cut(repo, "/")
		fmt.Fprintf(&query, "  repo%d: repository(owner: %q, name: %q) { ...BigBoardOpenPullRequests }\n", i, owner, name)
	}
	query.WriteString("  rateLimit { remaining resetAt }\n}\nfragment BigBoardOpenPullRequests on Repository {\n")
	query.WriteString(leanInventoryFields)
	query.WriteString("}")
	return query.String()
}

const firstFilesFields = ` number updatedAt headRefOid baseRefOid files(first: 100) { totalCount pageInfo { hasNextPage endCursor } nodes { path changeType } } `

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
