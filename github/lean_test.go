package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func leanPR(number, files int) *rawPR {
	row := pr(number)
	row.Files = &connection[rawFile]{TotalCount: pointer(files)}
	return row
}

func leanSummaries(t *testing.T, rows []*rawPR, total int, more bool, cursor string) fakeStep {
	t.Helper()
	return fakeStep{repo: "acme/repo", query: leanSummaryQuery, data: payload(t, &rawRepository{NameWithOwner: "Acme/Repo", PullRequests: conn(rows, total, more, cursor)}, 100)}
}

func batchFiles(t *testing.T, rows []*rawPR, problems ...map[string]any) fakeStep {
	t.Helper()
	repository := map[string]any{"nameWithOwner": "Acme/Repo"}
	numbers := make([]int, len(rows))
	for i, row := range rows {
		numbers[i] = row.Number
		repository[fmt.Sprintf("pr%d", i)] = row
	}
	body := map[string]any{"data": map[string]any{"repository": repository, "rateLimit": map[string]int{"remaining": 100}}}
	var err error
	if len(problems) > 0 {
		body["errors"] = problems
		err = ErrRequest
	}
	data, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return fakeStep{repo: "acme/repo", query: fileBatchQuery(numbers), data: data, err: err}
}

func withFiles(source *rawPR, total int, more bool, cursor string, nodes ...*rawFile) *rawPR {
	return &rawPR{Number: source.Number, UpdatedAt: source.UpdatedAt, HeadRefOid: source.HeadRefOid, BaseRefOid: source.BaseRefOid,
		Files: conn(nodes, total, more, cursor)}
}

func snapshot(number int, files ...*rawFile) PullRequest {
	return newTestProvider(nil).convertPR("acme/repo", pr(number, files...), false).pr
}

func queryOf(call invocation) string {
	for i := 0; i < len(call.args)-1; i++ {
		if call.args[i] == "-f" && strings.HasPrefix(call.args[i+1], "query=") {
			return strings.TrimPrefix(call.args[i+1], "query=")
		}
	}
	return ""
}

func TestFetchAllFirstLaunchUsesInlineFiles(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{repoSummaries(t, "acme/repo", []*rawPR{pr(1, file("a.go", "MODIFIED"))}, 1, false, "")}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo"}})[0]
	if len(runner.calls) != 1 || queryOf(runner.calls[0]) != summaryQuery || !result.PRs[0].FilesComplete || result.FileRequests != 0 {
		t.Fatalf("first launch lost inline files: %+v", result)
	}
}

func TestFetchAllUnchangedSnapshotUsesLeanInventoryWithoutFileRequests(t *testing.T) {
	previous := []PullRequest{snapshot(1, file("a.go", "MODIFIED")), snapshot(2, file("b.go", "ADDED"), file("c.go", "DELETED"))}
	runner := &fakeRunner{t: t, steps: []fakeStep{leanSummaries(t, []*rawPR{leanPR(1, 1), leanPR(2, 2)}, 2, false, "")}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: previous}})[0]
	if len(runner.calls) != 1 || queryOf(runner.calls[0]) != leanSummaryQuery || result.FileRequests != 0 || result.Err != nil || !result.Complete {
		t.Fatalf("unchanged snapshot fetched files: calls=%d %+v", len(runner.calls), result)
	}
	for _, got := range result.PRs {
		if !got.FilesComplete || !got.PreviousPathsComplete || !got.ContextComplete {
			t.Fatalf("reused evidence incomplete: %+v", got)
		}
	}
	if strings.Contains(leanSummaryQuery, "path") || !strings.Contains(leanSummaryQuery, "files { totalCount }") {
		t.Fatal("lean inventory still requests file paths")
	}
}

func TestFetchAllLeanInventoryBatchesChangedAndNewFiles(t *testing.T) {
	changed, added, kept, empty := leanPR(1, 1), leanPR(2, 1), leanPR(3, 1), leanPR(4, 0)
	changed.HeadRefOid = strings.Repeat("c", 40)
	changed.UpdatedAt = "2026-10-01T12:00:00Z"
	previous := []PullRequest{snapshot(1, file("old.go", "MODIFIED")), snapshot(3, file("kept.go", "MODIFIED"))}
	runner := &fakeRunner{t: t, steps: []fakeStep{
		leanSummaries(t, []*rawPR{changed, added, kept, empty}, 4, false, ""),
		batchFiles(t, []*rawPR{withFiles(changed, 1, false, "", file("new.go", "ADDED")), withFiles(added, 1, false, "", file("added.go", "ADDED"))}),
	}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: previous}})[0]
	if len(runner.calls) != 2 || queryOf(runner.calls[1]) != fileBatchQuery([]int{1, 2}) || result.InventoryRequests != 1 || result.FileRequests != 1 || result.Err != nil {
		t.Fatalf("changed files not batched: calls=%d %+v", len(runner.calls), result)
	}
	want := map[int]string{1: "new.go", 2: "added.go", 3: "kept.go"}
	for _, got := range result.PRs {
		if !got.FilesComplete || !got.ContextComplete {
			t.Fatalf("PR %d incomplete: %+v", got.Number, got)
		}
		if path, ok := want[got.Number]; ok && (len(got.Files) != 1 || got.Files[0].Path != path) {
			t.Fatalf("PR %d files = %+v", got.Number, got.Files)
		}
	}
}

func TestFetchAllLeanInventoryPaginatesBatchedFirstPages(t *testing.T) {
	big := leanPR(1, 2)
	runner := &fakeRunner{t: t, steps: []fakeStep{
		leanSummaries(t, []*rawPR{big}, 1, false, ""),
		batchFiles(t, []*rawPR{withFiles(big, 2, true, "next", file("a.go", "ADDED"))}),
		filePage(t, 1, []*rawFile{file("b.go", "ADDED")}, 2, false, ""),
	}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: []PullRequest{snapshot(9)}}})[0]
	got := result.PRs[0]
	if len(runner.calls) != 3 || queryOf(runner.calls[2]) != filesQuery || !hasPair(runner.calls[2].args, "-f", "cursor=next") ||
		result.FileRequests != 2 || !got.FilesComplete || !got.PreviousPathsComplete || len(got.Files) != 2 {
		t.Fatalf("batched first page did not continue: calls=%d %+v", len(runner.calls), result)
	}
}

func TestFetchAllLeanBatchFieldErrorMarksOnlyThatPR(t *testing.T) {
	one, two := leanPR(1, 1), leanPR(2, 1)
	problem := map[string]any{"message": "unavailable", "path": []any{"repository", "pr1", "files"}}
	runner := &fakeRunner{t: t, steps: []fakeStep{
		leanSummaries(t, []*rawPR{one, two}, 2, false, ""),
		batchFiles(t, []*rawPR{withFiles(one, 1, false, "", file("a.go", "ADDED")), {Number: 2}}, problem),
	}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: []PullRequest{snapshot(9)}}})[0]
	if len(runner.calls) != 2 || !result.Complete || result.InventoryErr != nil || result.FilesErr == nil {
		t.Fatalf("field error handling wrong: %+v", result)
	}
	for _, got := range result.PRs {
		if got.FilesComplete != (got.Number == 1) || got.ContextComplete != (got.Number == 1) {
			t.Fatalf("PR %d coverage wrong: %+v", got.Number, got)
		}
	}
}

func TestFetchAllLeanFileBudgetKeepsAreasUnknown(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{leanSummaries(t, []*rawPR{leanPR(1, 1), leanPR(2, 3)}, 2, false, "")}}
	result := newTestProvider(runner).FetchAll(WithRequestBudget(context.Background(), 2), []RepositoryRequest{{Repo: "acme/repo", Previous: []PullRequest{snapshot(9)}}})[0]
	if len(runner.calls) != 1 || !result.Complete || result.InventoryErr != nil || !errors.Is(result.FilesErr, ErrBudgetExceeded) || result.FileRequests != 0 {
		t.Fatalf("budget cut hid file coverage: %+v", result)
	}
	for _, got := range result.PRs {
		if got.FilesComplete || got.PreviousPathsComplete || got.ContextComplete || len(got.Files) != 0 || got.FileCount < 1 {
			t.Fatalf("PR %d claimed areas: %+v", got.Number, got)
		}
	}
}

func TestFetchAllLeanBatchRevisionChangeStaysPartial(t *testing.T) {
	row := leanPR(1, 1)
	moved := withFiles(row, 1, false, "", file("a.go", "ADDED"))
	moved.HeadRefOid = strings.Repeat("d", 40)
	runner := &fakeRunner{t: t, steps: []fakeStep{leanSummaries(t, []*rawPR{row}, 1, false, ""), batchFiles(t, []*rawPR{moved})}}
	result := newTestProvider(runner).FetchAll(context.Background(), []RepositoryRequest{{Repo: "acme/repo", Previous: []PullRequest{snapshot(9)}}})[0]
	if len(runner.calls) != 2 || result.PRs[0].FilesComplete || len(result.PRs[0].Files) != 0 || !errors.Is(result.FilesErr, ErrIncomplete) {
		t.Fatalf("moved revision accepted: %+v", result)
	}
}
