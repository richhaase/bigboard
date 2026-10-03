package tui

import (
	"strings"
	"testing"

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
	m.prState.snapshots["org/repo"] = snapshot
	return m
}

func TestPRScopeUsesVisibleOverviewSelection(t *testing.T) {
	for _, tc := range []struct {
		name, selected, query string
		want                  []int
	}{
		{"default PR-only root", "", "", []int{260, 261}},
		{"removed selection", "removed-area", "", []int{260, 261}},
		{"filtered selection", "root", "docs", []int{262}},
		{"no matching rows", "root", "does-not-exist", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := prOnlySelectionFixture()
			m.areaRepoID, m.selectedAreaID = "/api", tc.selected
			m.glance.overviewQueries[1] = tc.query
			_ = m.View() // Rendering must not be relied on to mutate model selection.
			next, cmd := m.handleAwarenessKey(key("p"))
			m = next.(Model)
			if cmd != nil {
				t.Fatal("opening PRs started an external command")
			}
			got := m.visiblePRs()
			if len(got) != len(tc.want) {
				t.Fatalf("scope %q: got %+v, want %v", m.selectedAreaID, got, tc.want)
			}
			for i, n := range tc.want {
				if got[i].Number != n {
					t.Fatalf("got %d, want %d", got[i].Number, n)
				}
			}
			if len(tc.want) == 0 && !strings.Contains(m.View(), "No PRs to display") {
				t.Fatal("empty filter displays unrelated PRs")
			}
		})
	}
}

func TestPRScopeImmediatelyAfterRepositoryEntry(t *testing.T) {
	m := prOnlySelectionFixture()
	m.selectedRepoID = "/api"
	m = pressAwareness(m, "enter")
	if m.areaRepoID != "/api" || m.selectedAreaID != "" {
		t.Fatal("fixture must enter an unresolved area overview")
	}
	if !strings.Contains(m.View(), "2 open") {
		t.Fatal("root preview lacks PR evidence")
	}
	m = pressAwareness(m, "p")
	if len(m.visiblePRs()) != 2 {
		t.Fatalf("immediate p lost root PRs: %s", m.View())
	}
}

func TestPRScopeRepositoryFallbackAndAllInventory(t *testing.T) {
	m := prOnlySelectionFixture()
	m.selectedRepoID = "removed-repo"
	// Repository query determines the visible row, just as the Worklist does.
	m.glance.overviewQueries[0] = "api"
	m = pressAwareness(m, "p")
	if m.selectedRepoID != "/api" || len(m.visiblePRs()) != 3 {
		t.Fatal("repository p did not match visible selection")
	}
	m = pressAwareness(m, "esc")
	m.glance.overviewQueries[0] = "no match"
	m = pressAwareness(m, "p")
	if len(m.visiblePRs()) != 0 {
		t.Fatal("empty repository filter leaked old selection")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "P")
	if len(m.visiblePRs()) != 3 {
		t.Fatal("all-repository inventory was filtered by overview query")
	}
}
