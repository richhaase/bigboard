package tui

import (
	"strings"
	"testing"
	"time"

	gh "github.com/richhaase/bigboard/github"
)

func prOnlySelectionFixture() Model {
	m := prFixture()
	m.width, m.height = 120, 36
	m.allRecords = nil
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = []gh.PullRequest{
		{Repo: "org/repo", Number: 260, Title: "Bump sys", Files: []gh.File{{Path: "go.mod"}}, FilesComplete: true, ContextComplete: true},
		{Repo: "org/repo", Number: 261, Title: "Bump term", Files: []gh.File{{Path: "go.mod"}}, FilesComplete: true, ContextComplete: true},
		{Repo: "org/repo", Number: 262, Title: "Update docs", Files: []gh.File{{Path: "docs/guide.md"}}, FilesComplete: true, ContextComplete: true},
	}
	now := time.Now()
	for i := range snapshot.prs {
		snapshot.prs[i].UpdatedAt = now
	}
	m.prState.snapshots["org/repo"] = snapshot
	return m
}

func TestPRBoardIgnoresWorklistSearchPersonAndBots(t *testing.T) {
	for _, query := range []string{"", "docs", "no-match"} {
		m := prOnlySelectionFixture()
		m.areaRepoID, m.selectedAreaID = "/api", "root"
		m.glance.overviewQueries[1] = query
		m.personID = "email:absent@example.test"
		m.hideBots, m.timeIdx = true, 0
		m = keyboardContractPress(t, m, "p")
		if !m.showPRs || m.prDetail || len(m.visiblePRs()) != 3 {
			t.Fatalf("query %q limited the PR board: %+v", query, m.visiblePRs())
		}
		if !strings.Contains(m.View(), "all included repositories") {
			t.Fatal("PR board does not identify its scope")
		}
	}
}

func TestPRBoardRespectsIncludedRepositories(t *testing.T) {
	m := prOnlySelectionFixture()
	m.excludedRepos["/api"], m.excludedRepos["/web"] = true, true
	m = keyboardContractPress(t, m, "p")
	if len(m.visiblePRs()) != 0 || !strings.Contains(m.View(), "No PRs to display") {
		t.Fatal("excluded repositories leaked into the PR board")
	}
}

func TestPRBoardRetainsSelectionAcrossBoardChanges(t *testing.T) {
	m := keyboardContractPress(t, prOnlySelectionFixture(), "p", "down")
	selected := m.visiblePRs()[m.prRow].Key()
	m = keyboardContractPress(t, m, "l", "p")
	if m.prDetail || m.visiblePRs()[m.prRow].Key() != selected {
		t.Fatal("returning to the PR board lost selection")
	}
	m = keyboardContractPress(t, m, "enter", "down", "w", "p")
	if m.prDetail || m.prOffset != 0 || m.visiblePRs()[m.prRow].Key() != selected {
		t.Fatal("p did not return from detail to the selected PR in the board")
	}
}

func TestPRBoardEmptySnapshotDoesNotFetch(t *testing.T) {
	m := prOnlySelectionFixture()
	provider := &fakePRProvider{}
	m.prProvider = provider
	m.prState.snapshots = nil
	m = keyboardContractPress(t, m, "p", "l", "w", "p")
	if provider.resolves != 0 || provider.fetches != 0 || len(m.visiblePRs()) != 0 || !strings.Contains(m.View(), "No PRs to display") {
		t.Fatal("opening the PR board fetched or manufactured data")
	}
}

func TestPRBoardEscapeStaysAtBoardRoot(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		m := prOnlySelectionFixture()
		m.viewMode = view
		m = keyboardContractPress(t, m, "p", "enter", "esc")
		if !m.showPRs || m.prDetail {
			t.Fatal("Esc did not return to PR list")
		}
		m = keyboardContractPress(t, m, "esc")
		if !m.showPRs || m.viewMode != view {
			t.Fatal("Esc left the PR board root")
		}
	}
}
