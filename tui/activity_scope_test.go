package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestActivityRowsKeepOwningRepositoryWithCanonicalCloneEvidence(t *testing.T) {
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	m := awarenessFixture()
	m.width, m.height = 120, 36
	m.repositories = []git.Repository{{ID: "/source", Name: "source"}, {ID: "/clone", Name: "clone"}}
	m.local.repositories = m.repositories
	m.selectedRepoID = "/source"
	m.local.scannedAt = map[string]time.Time{"/source": at.Add(3 * time.Hour), "/clone": at.Add(time.Hour)}
	m.local.staleRepos = map[string]bool{"/source": true}
	m.local.records = []git.CommitRecord{
		{CommitID: "shared", RepoID: "/source", RepoName: "source", Author: "Alias", Email: "alias@example.test", Date: at, LinesUnknown: true, PathsUnknown: true},
		{CommitID: "shared", RepoID: "/clone", RepoName: "clone", Author: "Contributor", Email: "canonical@example.test", Date: at, Added: 10, Removed: 2, Changes: []git.PathChange{{Path: "src/main.go", Added: 7, Removed: 2, LinesKnown: true}, {Path: "docs/guide.md", Added: 3, LinesKnown: true}}},
	}
	m.prState = prState{
		repositories: map[string]string{"/source": "example/source", "/clone": "example/clone"},
		snapshots: map[string]prSnapshot{
			"example/source": {prs: []gh.PullRequest{{Repo: "example/source", Number: 11, Title: "Source change", UpdatedAt: at, CheckState: "FAILURE", FilesComplete: true, Files: []gh.File{{Path: "src/main.go"}}}}},
			"example/clone":  {prs: []gh.PullRequest{{Repo: "example/clone", Number: 22, Title: "Clone change", UpdatedAt: at, CheckState: "SUCCESS", FilesComplete: true, Files: []gh.File{{Path: "src/main.go"}}}}},
		},
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	for _, directory := range []bool{false, true} {
		name := "repository"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			current := m
			rows := current.glanceRepositories()
			row := rows[current.selectedRepository(rows)]
			wantScope, wantAdded := "/source", 10
			if directory {
				current.openDirectoryRepository("/source")
				row = directoryRow(t, current, "dir:src")
				current.openDirectoryDetails(row.scopeID, "src", glanceActivity)
				wantScope, wantAdded = "dir:src", 7
			}
			if row.scopeID != wantScope || row.repositoryID != "/source" {
				t.Fatalf("row lost source ownership: %#v", row)
			}
			added, removed, unknown := repositoryLineTotals(row)
			if row.commits != 1 || len(row.people) != 1 || row.people[0].ID != "email:canonical@example.test" || added != wantAdded || removed != 2 || unknown != 0 {
				t.Fatalf("row lost canonical attribution or scoped line totals: %#v", row)
			}
			rendered := ansi.Strip(strings.Join(current.worklistRepositoryRow(row, true, 116, 1, 12), "\n"))
			if !strings.Contains(rendered, "3h") || strings.Contains(rendered, "1h") || strings.Contains(rendered, "[STALE]") == directory {
				t.Fatalf("row used the clone's age or changed stale labeling: %s", rendered)
			}
			evidence := ansi.Strip(strings.Join(current.worklistEvidence(row, 116, 30, directory), "\n"))
			if !strings.Contains(evidence, "Repository snapshot: STALE") || !strings.Contains(evidence, m.local.scannedAt["/source"].Local().Format("Jan 02 15:04 MST")) || !strings.Contains(evidence, "#11 Source change") || strings.Contains(evidence, "#22 Clone change") {
				t.Fatalf("evidence lost the owning repository's snapshot or PRs: %s", evidence)
			}
			opened := keyboardContractPress(t, current, "v")
			prs := opened.visiblePRs()
			if !opened.prContext.active || opened.prRepoID != "/source" || len(prs) != 1 || prs[0].Repo != "example/source" || prs[0].Number != 11 {
				t.Fatalf("contextual PR navigation used the canonical clone: %#v", prs)
			}
		})
	}
}
