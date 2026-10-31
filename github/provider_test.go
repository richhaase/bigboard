package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type invocation struct {
	command string
	args    []string
}

type fakeStep struct {
	repo  string
	query string
	data  []byte
	err   error
	batch bool
}

type fakeRunner struct {
	mu    sync.Mutex
	t     *testing.T
	steps []fakeStep
	calls []invocation
}

func (f *fakeRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	assertDeadline(f.t, ctx)
	f.calls = append(f.calls, invocation{command: command, args: append([]string(nil), args...)})
	if repos, query, ok := batchedRepositories(args); ok {
		if len(f.steps) > 0 && f.steps[0].batch {
			step := f.steps[0]
			f.steps = f.steps[1:]
			return step.data, step.err
		}
		return combineInventories(f.t, repos, func(repo string) ([]byte, error) { return f.take(command, args, repo, query) })
	}
	owner, _ := argValue(args, "owner=")
	name, _ := argValue(args, "name=")
	query, _ := argValue(args, "query=")
	return f.take(command, args, owner+"/"+name, query)
}

func (f *fakeRunner) take(command string, args []string, repo, query string) ([]byte, error) {
	f.t.Helper()
	if len(f.steps) == 0 {
		f.t.Fatalf("unexpected call: %s %#v", command, args)
	}
	index := 0
	for i, candidate := range f.steps {
		if candidate.repo != "" && candidate.repo == repo && candidate.query == query {
			index = i
			break
		}
	}
	step := f.steps[index]
	f.steps = append(f.steps[:index], f.steps[index+1:]...)
	return step.data, step.err
}

func argValue(args []string, prefix string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-f" {
			if value, ok := strings.CutPrefix(args[i+1], prefix); ok {
				return value, true
			}
		}
	}
	return "", false
}

var batchAliasPattern = regexp.MustCompile(`repo\d+: repository\(owner: "([^"]+)", name: "([^"]+)"\)`)

func batchedRepositories(args []string) ([]string, string, bool) {
	query, _ := argValue(args, "query=")
	if !strings.HasPrefix(query, "query BigBoardOpenPullRequestBatch") {
		return nil, "", false
	}
	single := summaryQuery
	if strings.Contains(query, "files { totalCount }") {
		single = leanSummaryQuery
	}
	var repos []string
	for _, match := range batchAliasPattern.FindAllStringSubmatch(query, -1) {
		repos = append(repos, match[1]+"/"+match[2])
	}
	return repos, single, true
}

func combineInventories(t *testing.T, repos []string, respond func(repo string) ([]byte, error)) ([]byte, error) {
	t.Helper()
	data := map[string]any{"rateLimit": map[string]int{"remaining": 100}}
	var problems []map[string]any
	var failure error
	for i, repo := range repos {
		alias := fmt.Sprintf("repo%d", i)
		body, err := respond(repo)
		if failure == nil {
			failure = err
		}
		data[alias] = nil
		if len(body) == 0 {
			continue
		}
		var decoded struct {
			Data   map[string]json.RawMessage
			Errors []map[string]any
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		data[alias] = decoded.Data["repository"]
		for _, problem := range decoded.Errors {
			if path, ok := problem["path"].([]any); ok && len(path) > 0 && path[0] == "repository" {
				problem["path"] = append([]any{alias}, path[1:]...)
			}
			problems = append(problems, problem)
		}
	}
	body := map[string]any{"data": data}
	if len(problems) > 0 {
		body["errors"] = problems
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, failure
}

func pointer[T any](v T) *T { return &v }

func conn[T any](nodes []*T, total int, next bool, cursor string) *connection[T] {
	return &connection[T]{Nodes: nodes, TotalCount: pointer(total), PageInfo: &pageInfo{HasNextPage: next, EndCursor: cursor}}
}

func file(path, kind string) *rawFile { return &rawFile{Path: path, ChangeType: pointer(kind)} }

func pr(number int, files ...*rawFile) *rawPR {
	return &rawPR{Number: number, Title: fmt.Sprintf("Change %d", number), Author: &actor{Login: "author"},
		UpdatedAt: "2026-09-30T12:00:00Z", HeadRefOid: strings.Repeat("a", 40), BaseRefOid: strings.Repeat("b", 40), ReviewDecision: pointer("APPROVED"), Mergeable: pointer("MERGEABLE"),
		ReviewRequests: conn[reviewRequest](nil, 0, false, ""), LatestReviews: conn[rawReview](nil, 0, false, ""),
		Files: conn(files, len(files), false, "")}
}

func payload(t *testing.T, repository *rawRepository, remaining int, problems ...string) []byte {
	t.Helper()
	data := map[string]any{"data": map[string]any{"repository": repository, "rateLimit": map[string]int{"remaining": remaining}}}
	if len(problems) > 0 {
		var errs []map[string]string
		for _, message := range problems {
			errs = append(errs, map[string]string{"message": message})
		}
		data["errors"] = errs
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func summaries(t *testing.T, rows []*rawPR, total int, more bool, cursor string) fakeStep {
	return fakeStep{data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequests: conn(rows, total, more, cursor)}, 100)}
}

func filePage(t *testing.T, number int, files []*rawFile, total int, more bool, cursor string) fakeStep {
	raw := &rawPR{Number: number, UpdatedAt: "2026-09-30T12:00:00Z", HeadRefOid: strings.Repeat("a", 40), BaseRefOid: strings.Repeat("b", 40), Files: conn(files, total, more, cursor)}
	return fakeStep{data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequest: raw}, 100)}
}

func hasPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestFetchEmptyCompleteInventory(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, nil, 0, false, "")}}
	result := newTestProvider(runner).Fetch(context.Background(), "ACME/Repo")
	if result.Err != nil || !result.Complete || result.Repo != "acme/repo" || result.PRs == nil || len(result.PRs) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
}

func TestFetchSummariesPaginationAndCanonicalIdentity(t *testing.T) {
	first := pr(1, file("api/server.go", "MODIFIED"))
	second := pr(2, file("web/index.ts", "ADDED"))
	second.IsDraft = true
	second.UpdatedAt = "2026-09-30T13:00:00Z"
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{first}, 2, true, "cursor-one"), summaries(t, []*rawPR{second}, 2, false, ""),
	}}
	result := newTestProvider(runner).Fetch(context.Background(), "ACME/REPO")
	if result.Err != nil || !result.Complete || len(result.PRs) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.PRs[0].Number != 2 || !result.PRs[0].Draft || result.PRs[0].URL != "https://github.com/acme/repo/pull/2" {
		t.Fatalf("first row = %+v", result.PRs[0])
	}
	for _, row := range result.PRs {
		if row.Repo != "acme/repo" || !row.FilesComplete || !row.ContextComplete || !row.PreviousPathsComplete {
			t.Fatalf("row = %+v", row)
		}
	}
	for _, call := range runner.calls {
		if call.command != "gh" || !hasPair(call.args, "--hostname", "github.com") || !hasPair(call.args, "-f", "owner=acme") || !hasPair(call.args, "-f", "name=repo") {
			t.Fatalf("unsafe/missing request constraints: %+v", call)
		}
		if call.args[0] != "api" || call.args[1] != "graphql" || !hasPair(call.args, "--method", "POST") {
			t.Fatalf("unexpected endpoint: %+v", call)
		}
	}
	if !hasPair(runner.calls[1].args, "-f", "cursor=cursor-one") {
		t.Fatalf("missing raw cursor variable: %+v", runner.calls[1])
	}
}

func TestFetchDeduplicatesCanonicalPRAndKeepsNewest(t *testing.T) {
	old, newest := pr(4), pr(4)
	newest.Title, newest.UpdatedAt = "Newest", "2026-09-30T13:00:00Z"
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{old}, 1, true, "next"), summaries(t, []*rawPR{newest}, 1, false, "")}}
	got := newTestProvider(runner).Fetch(context.Background(), "Acme/Repo")
	if got.Err != nil || !got.Complete || len(got.PRs) != 1 || got.PRs[0].Title != "Newest" || got.PRs[0].Key() != "acme/repo#4" {
		t.Fatalf("result = %+v", got)
	}
}

func TestNullAggregatesRemainUnknown(t *testing.T) {
	raw := pr(1)
	raw.Author, raw.ReviewDecision, raw.Mergeable = nil, nil, nil
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	row := got.PRs[0]
	if got.Err != nil || row.ReviewDecision != Unknown || row.Mergeable != Unknown || row.CheckState != Unknown || row.Author != "" {
		t.Fatalf("unknown metadata inferred as known: %+v", row)
	}
	if !row.ContextComplete {
		t.Fatalf("null aggregate is not a pagination failure: %+v", row)
	}
}

func TestUnfamiliarAggregateStatesAreUnknown(t *testing.T) {
	raw := pr(1)
	raw.ReviewDecision, raw.Mergeable = pointer("SOMETHING_NEW"), pointer("READY")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.PRs[0].ReviewDecision != Unknown || got.PRs[0].Mergeable != Unknown {
		t.Fatalf("row = %+v", got.PRs[0])
	}
	for _, state := range []string{"ERROR", "EXPECTED", "FAILURE", "PENDING", "SUCCESS"} {
		t.Run(state, func(t *testing.T) {
			body := fmt.Sprintf(`{"data":{"repository":{"nameWithOwner":"acme/repo","pullRequests":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"number":1,"updatedAt":"2026-09-30T12:00:00Z","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":%q}}}]}}]}}}}`, state)
			r := &fakeRunner{t: t, steps: []fakeStep{{data: []byte(body)}}}
			result := newTestProvider(r).Fetch(context.Background(), "acme/repo")
			if result.PRs[0].CheckState != state {
				t.Fatalf("checks = %q", result.PRs[0].CheckState)
			}
		})
	}
}

func TestFilePaginationAndRenameHonesty(t *testing.T) {
	raw := pr(7)
	raw.Files = conn([]*rawFile{file("api/a.go", "MODIFIED")}, 3, true, "files-one")
	runner := &fakeRunner{t: t, steps: []fakeStep{
		summaries(t, []*rawPR{raw}, 1, false, ""),
		filePage(t, 7, []*rawFile{file("web/b.ts", "RENAMED")}, 3, true, "files-two"),
		filePage(t, 7, []*rawFile{file("old/c.go", "DELETED")}, 3, false, ""),
	}}
	result := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if result.Err != nil || !result.Complete || len(result.PRs) != 1 {
		t.Fatalf("result = %+v", result)
	}
	row := result.PRs[0]
	if !row.FilesComplete || row.PreviousPathsComplete || row.ContextComplete || len(row.Files) != 3 || row.Files[1].PreviousPath != "" || row.Files[2].Path != "old/c.go" {
		t.Fatalf("rename/file context not explicit: %+v", row)
	}
	if !hasPair(runner.calls[1].args, "-F", "number=7") || !hasPair(runner.calls[1].args, "-f", "cursor=files-one") || !hasPair(runner.calls[2].args, "-f", "cursor=files-two") {
		t.Fatalf("file pagination calls = %+v", runner.calls)
	}
}

func TestFilePaginationRecoversOriginsCompletenessWhenNoRenames(t *testing.T) {
	raw := pr(1)
	raw.Files = conn([]*rawFile{file("a.go", "MODIFIED")}, 2, true, "more")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), filePage(t, 1, []*rawFile{file("a.go", "MODIFIED"), file("b.go", "ADDED")}, 2, false, "")}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Err != nil || !got.PRs[0].FilesComplete || !got.PRs[0].PreviousPathsComplete || !got.PRs[0].ContextComplete || len(got.PRs[0].Files) != 2 {
		t.Fatalf("result = %+v", got)
	}
}

func TestReviewsAndRequestedReviewersBoundedIndependentlyOfDecision(t *testing.T) {
	raw := pr(1)
	raw.ReviewRequests = conn([]*reviewRequest{
		{RequestedReviewer: &requestedReviewer{TypeName: "User", Login: "alice"}},
		{RequestedReviewer: &requestedReviewer{TypeName: "Team", Slug: "backend", Organization: &actor{Login: "Acme"}}},
	}, 3, true, "more-reviewers")
	submitted := time.Date(2026, time.September, 30, 11, 0, 0, 0, time.UTC)
	raw.LatestReviews = conn([]*rawReview{{Author: &actor{Login: "bob"}, State: pointer("CHANGES_REQUESTED"), SubmittedAt: submitted.Format(time.RFC3339)}}, 1, false, "")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}}
	result := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	row := result.PRs[0]
	if !reflect.DeepEqual(row.RequestedReviewers, []string{"alice", "Acme/backend"}) || row.RequestedReviewersComplete || !row.ReviewsComplete || row.ContextComplete || row.ReviewDecision != "APPROVED" {
		t.Fatalf("review context = %+v", row)
	}
	if !reflect.DeepEqual(row.Reviews, []Review{{Author: "bob", State: "CHANGES_REQUESTED", SubmittedAt: submitted}}) {
		t.Fatalf("reviews = %+v", row.Reviews)
	}
	if len(runner.calls) != 1 {
		t.Fatal("review context must not cause unbounded pagination")
	}
}

func TestReviewSubmissionTimeParsingPreservesReviewEvidence(t *testing.T) {
	cases := []struct {
		name      string
		state     string
		timestamp string
		want      time.Time
	}{
		{"submitted", "APPROVED", `,"submittedAt":"2026-09-30T12:30:00-06:00"`, time.Date(2026, time.September, 30, 18, 30, 0, 0, time.UTC)},
		{"missing", "COMMENTED", "", time.Time{}},
		{"null", "CHANGES_REQUESTED", `,"submittedAt":null`, time.Time{}},
		{"empty", "APPROVED", `,"submittedAt":""`, time.Time{}},
		{"malformed", "DISMISSED", `,"submittedAt":"not-a-date"`, time.Time{}},
		{"pending", "PENDING", `,"submittedAt":null`, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"author":{"login":"alice"},"state":%q%s}]}`, tc.state, tc.timestamp)
			var raw connection[rawReview]
			if err := json.Unmarshal([]byte(body), &raw); err != nil {
				t.Fatal(err)
			}
			reviews, complete := convertReviews(&raw)
			if !complete || len(reviews) != 1 || reviews[0].Author != "alice" || reviews[0].State != tc.state || !reviews[0].SubmittedAt.Equal(tc.want) {
				t.Fatalf("reviews = %+v, complete = %v", reviews, complete)
			}
		})
	}
}

func TestQueryIncludesSubmittedAtInBoundedLatestReviews(t *testing.T) {
	query := strings.Join(strings.Fields(summaryQuery), " ")
	if !strings.Contains(query, "latestReviews(first: 50) { totalCount pageInfo { hasNextPage endCursor } nodes { author { login } state submittedAt } }") {
		t.Fatal("latest review submission time is missing from the bounded summary query")
	}
}

func TestQueryIncludesOnlyReadOnlyLightweightContext(t *testing.T) {
	for _, query := range []string{summaryQuery, leanSummaryQuery, filesQuery, fileBatchQuery([]int{1, 2}), inventoryBatchQuery([]string{"acme/one", "acme/two"})} {
		for _, forbidden := range []string{"mutation", "body", "bodyText", "comments", "patch", "diff", "log", "contents", "accessToken"} {
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(forbidden) + `\b`).MatchString(query) {
				t.Errorf("query requests forbidden field %q", forbidden)
			}
		}
		if !strings.HasPrefix(query, "query ") {
			t.Fatal("not a read-only query")
		}
	}
}

func TestErrorsCannotBecomeSuccessfulEmptyResults(t *testing.T) {
	cases := []struct {
		name string
		step fakeStep
		want error
	}{
		{"authentication", fakeStep{err: ErrAuthentication}, ErrAuthentication},
		{"missing-gh", fakeStep{err: ErrUnavailable}, ErrUnavailable},
		{"timeout", fakeStep{err: context.DeadlineExceeded}, context.DeadlineExceeded},
		{"raw-diagnostic", fakeStep{err: errors.New("private-token secret error")}, ErrRequest},
		{"malformed-json", fakeStep{data: []byte("not JSON")}, ErrResponse},
		{"oversized-output", fakeStep{data: make([]byte, MaxOutputBytes+1)}, ErrOutputLimit},
		{"repository-null", fakeStep{data: []byte(`{"data":{"repository":null}}`)}, ErrRepository},
		{"connection-null", fakeStep{data: []byte(`{"data":{"repository":{"nameWithOwner":"acme/repo","pullRequests":null}}}`)}, ErrResponse},
		{"rate-limit", fakeStep{data: []byte(`{"errors":[{"type":"RATE_LIMITED","message":"rate limit exceeded"}]}`)}, ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{t: t, steps: []fakeStep{tc.step}}
			result := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
			if result.Complete || len(result.PRs) != 0 || !errors.Is(result.Err, tc.want) || strings.Contains(result.Err.Error(), "secret") {
				t.Fatalf("result = %+v, want %v", result, tc.want)
			}
		})
	}
}

func TestGraphQLPartialErrorPreservesRows(t *testing.T) {
	step := summaries(t, []*rawPR{pr(1)}, 1, false, "")
	step.data = payload(t, &rawRepository{NameWithOwner: "acme/repo", PullRequests: conn([]*rawPR{pr(1)}, 1, false, "")}, 100, "field unavailable")
	step.err = ErrRequest // gh exits nonzero even when GraphQL includes partial data.
	runner := &fakeRunner{t: t, steps: []fakeStep{step}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Complete || len(got.PRs) != 1 || !errors.Is(got.Err, ErrIncomplete) {
		t.Fatalf("partial data lost: %+v", got)
	}
}

func TestLaterSummaryFailurePreservesEarlierRows(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(1)}, 2, true, "more"), {err: ErrAuthentication}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Complete || len(got.PRs) != 1 || !errors.Is(got.Err, ErrAuthentication) {
		t.Fatalf("result = %+v", got)
	}
}

func TestFileFailureDoesNotInvalidateSummaryInventory(t *testing.T) {
	raw := pr(1)
	raw.Files = conn([]*rawFile{file("a.go", "ADDED")}, 2, true, "more")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), {err: ErrRateLimited}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if !got.Complete || len(got.PRs) != 1 || !errors.Is(got.Err, ErrRateLimited) || got.PRs[0].FilesComplete || got.PRs[0].ContextComplete || len(got.PRs[0].Files) != 1 {
		t.Fatalf("result = %+v", got)
	}
}

func TestRateLimitStopsFurtherRequests(t *testing.T) {
	first := payload(t, &rawRepository{NameWithOwner: "acme/repo", PullRequests: conn([]*rawPR{pr(1)}, 2, true, "more")}, 0)
	runner := &fakeRunner{t: t, steps: []fakeStep{{data: first}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Complete || !errors.Is(got.Err, ErrRateLimited) || len(got.PRs) != 1 || len(runner.calls) != 1 {
		t.Fatalf("result = %+v; calls = %d", got, len(runner.calls))
	}
}

func TestPRAndRequestBudgets(t *testing.T) {
	t.Run("PR count", func(t *testing.T) {
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(1), pr(2)}, 3, true, "more")}}
		p := newTestProvider(runner)
		p.limits.pullRequests = 2
		got := p.Fetch(context.Background(), "acme/repo")
		if got.Complete || len(got.PRs) != 2 || !errors.Is(got.Err, ErrBudgetExceeded) || len(runner.calls) != 1 {
			t.Fatalf("result = %+v", got)
		}
	})
	t.Run("request count", func(t *testing.T) {
		raw := pr(1)
		raw.Files = conn([]*rawFile{file("a", "ADDED")}, 2, true, "more")
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}}
		p := newTestProvider(runner)
		p.limits.requests = 1
		got := p.Fetch(context.Background(), "acme/repo")
		if !got.Complete || !errors.Is(got.Err, ErrBudgetExceeded) || got.PRs[0].FilesComplete || len(runner.calls) != 1 {
			t.Fatalf("result = %+v", got)
		}
	})
}

func TestFilesHardBudget(t *testing.T) {
	files := func(offset int) []*rawFile {
		var nodes []*rawFile
		for i := range filePageSize {
			nodes = append(nodes, file(fmt.Sprintf("file-%d.go", offset+i), "ADDED"))
		}
		return nodes
	}
	raw := pr(1)
	raw.Files = conn(files(0), MaxFilesPerPR+1, true, "cursor-1")
	steps := []fakeStep{summaries(t, []*rawPR{raw}, 1, false, "")}
	for page := 1; page < MaxFilesPerPR/filePageSize; page++ {
		steps = append(steps, filePage(t, 1, files(page*filePageSize), MaxFilesPerPR+1, true, fmt.Sprintf("cursor-%d", page+1)))
	}
	runner := &fakeRunner{t: t, steps: steps}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if !got.Complete || !errors.Is(got.Err, ErrBudgetExceeded) || got.PRs[0].FilesComplete || len(got.PRs[0].Files) != MaxFilesPerPR || len(runner.calls) != MaxFilesPerPR/filePageSize {
		t.Fatalf("result: complete=%v err=%v fileCount=%d calls=%d", got.Complete, got.Err, len(got.PRs[0].Files), len(runner.calls))
	}
}

func TestBrokenPaginationCannotClaimCompleteness(t *testing.T) {
	t.Run("repeated summary cursor", func(t *testing.T) {
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(1)}, 3, true, "same"), summaries(t, []*rawPR{pr(2)}, 3, true, "same")}}
		got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
		if got.Complete || !errors.Is(got.Err, ErrResponse) || len(runner.calls) != 2 {
			t.Fatalf("result = %+v", got)
		}
	})
	t.Run("missing summary cursor", func(t *testing.T) {
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(1)}, 2, true, "")}}
		got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
		if got.Complete || !errors.Is(got.Err, ErrResponse) {
			t.Fatalf("result = %+v", got)
		}
	})
	t.Run("repeated file cursor", func(t *testing.T) {
		raw := pr(1)
		raw.Files = conn([]*rawFile{file("a", "ADDED")}, 3, true, "same")
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), filePage(t, 1, []*rawFile{file("b", "ADDED")}, 3, true, "same")}}
		got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
		if !got.Complete || got.PRs[0].FilesComplete || !errors.Is(got.Err, ErrIncomplete) {
			t.Fatalf("result = %+v", got)
		}
	})
	t.Run("total mismatch", func(t *testing.T) {
		runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{pr(1)}, 2, false, "")}}
		got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
		if got.Complete || !errors.Is(got.Err, ErrIncomplete) {
			t.Fatalf("result = %+v", got)
		}
	})
}

func TestChangedPRDuringFilePaginationStaysPartial(t *testing.T) {
	raw := pr(1)
	raw.Files = conn([]*rawFile{file("before.go", "ADDED")}, 2, true, "next")
	next := pr(1)
	next.UpdatedAt = "2026-09-30T14:00:00Z"
	next.Files = conn([]*rawFile{file("after.go", "ADDED")}, 2, false, "")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), {data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequest: next}, 100)}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if !errors.Is(got.Err, ErrIncomplete) || got.PRs[0].FilesComplete || len(got.PRs[0].Files) != 1 {
		t.Fatalf("mixed two snapshots: %+v", got)
	}
}

func TestNoRequestsForInvalidRepoOrCanceledContext(t *testing.T) {
	runner := &fakeRunner{t: t}
	if got := newTestProvider(runner).Fetch(context.Background(), "https://evil.test/a/b"); !errors.Is(got.Err, ErrInvalidRepository) || got.Complete {
		t.Fatalf("result = %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := newTestProvider(runner).Fetch(ctx, "a/b"); !errors.Is(got.Err, context.Canceled) || got.Complete {
		t.Fatalf("result = %+v", got)
	}
	if len(runner.calls) != 0 {
		t.Fatal("invalid input executed a command")
	}
}

func TestSchemaFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/open_prs.json")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{t: t, steps: []fakeStep{{data: data}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Err != nil || !got.Complete || len(got.PRs) != 3 {
		t.Fatalf("fixture invalid: %+v", got)
	}
	if got.PRs[0].UpdatedAt.IsZero() || got.PRs[0].UpdatedAt.After(time.Now().AddDate(10, 0, 0)) {
		t.Fatal("fixture timestamp missing")
	}
}

func TestChangedRevisionDuringPaginationStaysPartial(t *testing.T) {
	for _, change := range []string{"head", "base", "missing-head", "missing-base"} {
		t.Run(change, func(t *testing.T) {
			raw := pr(1)
			raw.Files = conn([]*rawFile{file("before.go", "ADDED")}, 2, true, "next")
			next := pr(1)
			next.Files = conn([]*rawFile{file("after.go", "ADDED")}, 2, false, "")
			switch change {
			case "head":
				next.HeadRefOid = strings.Repeat("c", 40)
			case "base":
				next.BaseRefOid = strings.Repeat("c", 40)
			case "missing-head":
				next.HeadRefOid = ""
			case "missing-base":
				next.BaseRefOid = ""
			}
			runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), {data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequest: next}, 100)}}}
			got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
			if !errors.Is(got.Err, ErrIncomplete) || !got.Complete || got.PRs[0].FilesComplete || got.PRs[0].ContextComplete || len(got.PRs[0].Files) != 1 {
				t.Fatalf("mixed different revisions: %+v", got)
			}
		})
	}
}

func TestFractionalTimestampPaginationRemainsComplete(t *testing.T) {
	raw := pr(1)
	raw.UpdatedAt = "2026-09-30T12:00:00.123Z"
	raw.Files = conn([]*rawFile{file("before.go", "ADDED")}, 2, true, "next")
	next := pr(1)
	next.UpdatedAt = raw.UpdatedAt
	next.Files = conn([]*rawFile{file("after.go", "ADDED")}, 2, false, "")
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{raw}, 1, false, ""), {data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequest: next}, 100)}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if got.Err != nil || !got.PRs[0].FilesComplete || !got.PRs[0].ContextComplete {
		t.Fatalf("timestamp precision lost: %+v", got)
	}
}

func TestAuthenticationFailureStopsRemainingFileCalls(t *testing.T) {
	first, second := pr(1), pr(2)
	for _, row := range []*rawPR{first, second} {
		row.Files = conn([]*rawFile{file("a", "MODIFIED")}, 2, true, "next")
	}
	runner := &fakeRunner{t: t, steps: []fakeStep{summaries(t, []*rawPR{first, second}, 2, false, ""), {err: ErrAuthentication}}}
	got := newTestProvider(runner).Fetch(context.Background(), "acme/repo")
	if !got.Complete || !errors.Is(got.Err, ErrAuthentication) || len(runner.calls) != 2 {
		t.Fatalf("result = %+v; calls = %d", got, len(runner.calls))
	}
}
