package tui

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func TestSharedRepositoryFilterScopesBoardsWithoutChangingTracking(t *testing.T) {
	m := boardLayoutFixture()
	m.sharedRepos = filterValues{"/sample/gateway"}
	m.recomputeAuthors()
	if len(m.filteredRecords()) != 24 || len(m.activityRepositories()) != 1 || len(m.visiblePRs()) != 3 || len(m.allPRs()) != 48 {
		t.Fatal("shared repository selection did not consistently scope board data")
	}
	commits := 0
	for _, person := range m.authors {
		commits += person.Commits
	}
	if commits != 24 || len(m.excludedRepos) != 0 || len(m.repositories) != 16 {
		t.Fatal("shared selection changed contributor counts or tracking")
	}
	m.resetPending()
	if m.scan.remaining != 16 {
		t.Fatal("view selection stopped background repository scans")
	}
	p := &fakePRProvider{result: gh.Result{Complete: true}}
	m.prProvider = p
	cmd := m.startPRRefresh()
	if cmd == nil {
		t.Fatal("shared selection prevented PR refresh")
	}
	cmd()
	if p.resolves != 16 {
		t.Fatal("shared selection changed which tracked repositories refresh")
	}
	m.excludedRepos["/sample/gateway"] = true
	m.recomputeAuthors()
	if len(m.filteredRecords()) != 0 || len(m.activityRepositories()) != 0 || len(m.visiblePRs()) != 0 || m.repoInView("/sample/gateway") {
		t.Fatal("view selection restored an excluded tracked repository")
	}
}

func TestActivityPersonAndBotFiltersAreIndependentOfContributors(t *testing.T) {
	m := awarenessFixture()
	bot := m.local.records[0]
	bot.CommitID, bot.Author, bot.Email = "bot", "automation[bot]", "automation[bot]@example.test"
	m.local.records = append(m.local.records, bot)
	m.sharedRepos = filterValues{"/api"}
	m.hideBots = true
	m.recomputeAuthors()
	if len(m.activityRecords()) != 3 || len(m.authors) != 2 {
		t.Fatal("Contributors bot visibility affected Activity")
	}
	m.activityPeople = filterValues{"email:grace@x", stats.IdentityID(bot)}
	if len(m.activityRecords()) != 2 {
		t.Fatal("Activity people did not use OR semantics")
	}
	m.activityHideBots = true
	if records := m.activityRecords(); len(records) != 1 || records[0].Email != "grace@x" {
		t.Fatal("Activity bot and person filters did not compose")
	}
	m.hideBots = false
	m.recomputeAuthors()
	if len(m.authors) != 3 || len(m.activityRecords()) != 1 {
		t.Fatal("Activity filters changed Contributors or Contributors changed Activity")
	}
	m.personID = "email:grace@x"
	m.areaRepoID = "/api"
	m.sharedRepos = filterValues{"/web"}
	m.recomputeAuthors()
	if len(m.activityRecords()) != 0 || m.personID != "email:grace@x" || len(m.activityPeople) != 2 {
		t.Fatal("missing selected people silently broadened Activity")
	}
	options := m.activityPeopleOptions()
	for _, want := range []string{"email:ada@x", "email:grace@x", stats.IdentityID(bot)} {
		if !slices.ContainsFunc(options, func(option prFilterOption) bool { return option.value == want }) {
			t.Fatalf("person option %s disappeared after repository filtering", want)
		}
	}
	m.activityPeople = nil
	if len(m.glanceScopeRecords()) != 0 {
		t.Fatal("a filtered-out directory inherited records from another repository")
	}
}

func TestActivityFilteringKeepsCanonicalIdentityAndRepositoryAssociation(t *testing.T) {
	m := awarenessFixture()
	m.local.records = []git.CommitRecord{
		{CommitID: "shared", RepoID: "/api", RepoName: "api", Author: "Canonical", Email: "canonical@example.test", Date: time.Now()},
		{CommitID: "shared", RepoID: "/web", RepoName: "web", Author: "Alias", Email: "alias@example.test", Date: time.Now()},
	}
	m.activityPeople = filterValues{"email:canonical@example.test"}
	m.recomputeAuthors()
	rows := m.activityRepositories()
	for _, row := range rows {
		if row.scopeID != "/empty" && (row.commits != 1 || len(row.people) != 1 || row.people[0].ID != "email:canonical@example.test") {
			t.Fatal("canonical person filtering lost an associated repository")
		}
	}
}

func TestPRMultiValueFiltersUseORWithinAndANDAcrossCategories(t *testing.T) {
	f := prFilters{
		author: filterValues{"one", "two"},
		review: filterValues{"Approved", "Needs review"},
		checks: filterValues{"Passing", "Pending"},
		state:  filterValues{"draft", "open"},
	}
	for _, pr := range []gh.PullRequest{
		{Author: "One", ReviewDecision: "APPROVED", CheckState: "SUCCESS"},
		{Author: "two", ReviewDecision: "REVIEW_REQUIRED", CheckState: "PENDING", Draft: true},
	} {
		if !f.matches(pr, "") {
			t.Fatal("an alternate selected value failed to match")
		}
	}
	base := gh.PullRequest{Author: "one", ReviewDecision: "APPROVED", CheckState: "SUCCESS"}
	for _, pr := range []gh.PullRequest{
		{Author: "three", ReviewDecision: base.ReviewDecision, CheckState: base.CheckState},
		{Author: base.Author, ReviewDecision: "CHANGES_REQUESTED", CheckState: base.CheckState},
		{Author: base.Author, ReviewDecision: base.ReviewDecision, CheckState: "FAILURE"},
	} {
		if f.matches(pr, "") {
			t.Fatal("different filter categories were combined with OR")
		}
	}
	unknown := prFilters{author: filterValues{"?"}, review: filterValues{"Unknown"}, checks: filterValues{"Unknown"}, state: filterValues{"draft"}}
	if !unknown.matches(gh.PullRequest{Draft: true}, "") {
		t.Fatal("unknown values no longer remain explicitly selectable")
	}
	clone := f.clone()
	clone.author[0] = "changed"
	clone.checks = clone.checks.toggle("Passing")
	if f.author[0] != "one" || !reflect.DeepEqual(f.checks, filterValues{"Passing", "Pending"}) {
		t.Fatal("pending selections mutated applied filters")
	}
	if values := (filterValues{"one"}).toggle(""); len(values) != 0 || !values.includes("anything") {
		t.Fatal("All did not clear a category restriction")
	}
}

func TestPRScopeEvidenceUsesSharedRangeAndLiteralPaths(t *testing.T) {
	m := prFixture()
	m.sharedRepos = filterValues{"/api"}
	m.timeIdx = 0
	now := time.Now()
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = []gh.PullRequest{
		{Repo: "org/repo", Number: 1, Author: "one", UpdatedAt: now, FilesComplete: true, Files: []gh.File{{Path: "src/feature/a.go"}}},
		{Repo: "org/repo", Number: 2, Author: "two", UpdatedAt: now.Add(-48 * time.Hour), FilesComplete: true, Files: []gh.File{{Path: "src/feature/old.go"}}},
		{Repo: "org/repo", Number: 3, Author: "unknown-date", FilesComplete: true, Files: []gh.File{{Path: "src/feature/unknown.go"}}},
		{Repo: "org/repo", Number: 4, Author: "four", UpdatedAt: now, FilesComplete: true, Files: []gh.File{{Path: "src/features/neighbor.go"}}},
		{Repo: "org/repo", Number: 5, UpdatedAt: now, FilesComplete: false},
	}
	m.prState.snapshots["org/repo"] = snapshot
	m.prFilters.author = filterValues{"nobody"}
	m.prQuery = "does not exist"
	if len(m.visiblePRs()) != 0 || len(m.scopePRs("/api")) != 3 || len(m.allPRs()) != 5 {
		t.Fatal("PR board filters leaked into shared evidence or raw snapshot counts")
	}
	m.areaRepoID = "/api"
	if prs := m.scopePRs("dir:src/feature"); len(prs) != 1 || prs[0].Number != 1 {
		t.Fatal("literal directory evidence ignored path boundaries or shared dates")
	}
	if prs := m.scopePRs("file:src/feature/a.go"); len(prs) != 1 || prs[0].Number != 1 {
		t.Fatal("literal file evidence did not use exact paths")
	}
	if prs := m.scopePRs("unknown"); len(prs) != 1 || prs[0].Number != 5 {
		t.Fatal("incomplete PR path evidence was treated as a complete empty scope")
	}
	m.timeIdx = len(TimePresets) - 1
	if len(m.scopePRs("dir:src/feature")) != 3 {
		t.Fatal("All time did not restore unknown and older PR dates")
	}
	m.sharedRepos = filterValues{"/empty"}
	if len(m.scopePRs("/api")) != 0 {
		t.Fatal("PR evidence ignored shared repository selection")
	}
}

func TestFilterAuthorOptionsUseSharedScopeAndRetainMissingSelections(t *testing.T) {
	m := prRangeFixture()
	m.prFilterKind = "author"
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[0].Author = "recent"
	snapshot.prs[1].Author = "older"
	snapshot.prs[2].Author = "undated"
	snapshot.prs[3].Author = "future"
	m.prState.snapshots["org/repo"] = snapshot
	m.prFilters.author = filterValues{"missing"}
	options := m.prFilterOptions()
	values := make([]string, len(options))
	for i, option := range options {
		values[i] = option.value
	}
	if !slices.Contains(values, "recent") || !slices.Contains(values, "future") || !slices.Contains(values, "missing") || slices.Contains(values, "older") || slices.Contains(values, "undated") {
		t.Fatal("author choices did not reflect shared date scope and retained intent")
	}
}
