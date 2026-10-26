package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func directoryFixture() Model {
	m := monorepoFixture()
	m.width, m.height = 120, 30
	m.areaRepoID = ""
	m.allRecords[0].Changes[0].Added, m.allRecords[0].Changes[0].Removed = 60, 40
	m.allRecords[0].Changes[1].Added = 200
	m.allRecords[0].Added, m.allRecords[0].Removed = 260, 40
	m.allRecords[1].Changes[0].Added, m.allRecords[1].Changes[0].Removed = 300, 50
	m.allRecords[1].Added, m.allRecords[1].Removed = 300, 50
	m.allRecords[2].Changes[0].Added = 10
	m.allRecords[2].Added = 10
	for i := range m.allRecords {
		for j := range m.allRecords[i].Changes {
			m.allRecords[i].Changes[j].LinesKnown = true
		}
	}
	m.recomputeAuthors()
	return m
}

func directoryRow(t *testing.T, m Model, id string) repositoryActivity {
	t.Helper()
	for _, row := range m.directoryRows() {
		if row.repo.ID == id {
			return row
		}
	}
	t.Fatalf("missing directory entry %s", id)
	return repositoryActivity{}
}

func TestDirectoryDrilldownUsesLiteralPathsAndExactLines(t *testing.T) {
	m := directoryFixture()
	m.options.WorkAreas = map[string][]stats.WorkAreaRule{"mono": {{Name: "Cross-cutting", Paths: []string{"services/auth", "apps/web"}}}}
	m.rebuildAreaDefinitions()
	m = keyboardContractPress(t, m, "enter")
	if !m.directory.active || m.glance.detailOpen || m.directoryRows()[0].repo.ID != "dir:services" {
		t.Fatal("repository did not open literal directory ranking")
	}
	service := directoryRow(t, m, "dir:services")
	added, removed, unknown := repositoryLineTotals(service)
	if service.commits != 2 || added != 360 || removed != 90 || unknown != 0 {
		t.Fatal("directory counted whole-commit lines from outside its path")
	}
	apps := directoryRow(t, m, "dir:apps")
	added, removed, _ = repositoryLineTotals(apps)
	if added != 200 || removed != 0 || apps.commits != 1 {
		t.Fatal("cross-directory commit was not attributed per file")
	}
	m = keyboardContractPress(t, m, "enter")
	if m.directory.frame.path != "services" {
		t.Fatal("Enter skipped a literal directory level")
	}
	m = keyboardContractPress(t, m, "/", "auth", "enter", "enter")
	if m.directory.frame.path != "services/auth" || !strings.Contains(ansi.Strip(m.View()), "session.go") {
		t.Fatal("directory child ranking is missing")
	}
	m = keyboardContractPress(t, m, "enter")
	if !m.glance.frame.literal || m.glance.frame.areaID != "file:services/auth/session.go" || len(m.glanceEvidence()) != 1 {
		t.Fatal("file did not open its commit history")
	}
	record := m.glanceEvidence()[0]
	if record.Added != 60 || record.Removed != 40 || len(record.Changes) != 1 {
		t.Fatal("file history retained unrelated line counts or paths")
	}
	m = keyboardContractPress(t, m, "enter")
	if !strings.Contains(m.View(), "apps/web/login.tsx") {
		t.Fatal("full commit inspector lost related changed paths")
	}
	m = keyboardContractPress(t, m, "esc", "esc", "esc")
	if m.directory.frame.path != "services" || m.directory.frame.selectedID != "dir:services/auth" || m.directory.frame.query != "auth" {
		t.Fatal("Back lost parent selection or search")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.directory.frame.path != "services" || m.directory.frame.query != "" {
		t.Fatal("Back did not clear search before changing scope")
	}
	m = keyboardContractPress(t, m, "esc", "esc")
	if m.directory.active || m.areaRepoID != "" || m.selectedRepoID != "/mono" {
		t.Fatal("Back did not restore selected repository")
	}
}

func TestDirectoryEvidenceScopeAndBoardSwitches(t *testing.T) {
	m := directoryFixture()
	m = keyboardContractPress(t, m, "enter", "enter", "2")
	if m.glance.frame.areaID != "dir:services" || len(m.glanceAreaPeople()) != 2 {
		t.Fatal("People did not use the current directory")
	}
	m = keyboardContractPress(t, m, "4")
	if m.glance.detailOpen || m.directory.frame.path != "services" {
		t.Fatal("Directories action did not restore ranking")
	}
	m = keyboardContractPress(t, m, "tab", "tab", "tab", "tab")
	if m.glance.detailOpen || m.directory.frame.path != "services" {
		t.Fatal("Tab did not cycle scope evidence back to ranking")
	}
	m = keyboardContractPress(t, m, "c", "p", "a")
	if !m.directory.active || m.directory.frame.path != "services" || !strings.Contains(m.View(), "DIRECTORIES / FILES") {
		t.Fatal("board switching lost directory scope")
	}
	m = keyboardContractPress(t, m, "3")
	if len(m.glanceCochangedAreas()) == 0 {
		t.Fatal("Related evidence became unreachable")
	}
	m = keyboardContractPress(t, m, "enter")
	if !m.glance.frame.relatedFromLiteral || len(m.glanceEvidence()) == 0 {
		t.Fatal("Related drill-down lost source directory identity")
	}
}

func TestDirectoryFiltersKeepScopeAndClearExcludedRepo(t *testing.T) {
	m := directoryFixture()
	m.allRecords[1].Date = time.Now().Add(-48 * time.Hour)
	m.allRecords[0].Author, m.allRecords[0].Email = "agent[bot]", "agent[bot]@example.test"
	m.recomputeAuthors()
	m = keyboardContractPress(t, m, "enter", "enter")
	m.timeIdx = 0
	m.recomputeAuthors()
	if len(m.directoryRows()) != 1 || m.directoryRows()[0].repo.ID != "dir:services/auth" {
		t.Fatal("directory rows ignored the global range")
	}
	m = keyboardContractPress(t, m, "b")
	if len(m.directoryRows()) != 0 || m.directory.frame.path != "services" {
		t.Fatal("empty bot-filtered directory changed scope or retained hidden data")
	}
	m.excludedRepos["/mono"] = true
	m.normalizeAreaScope()
	if m.directory.active || m.areaRepoID != "" {
		t.Fatal("excluded repository retained its directory browser")
	}
}

func TestDirectoryPagingAndFrameFit(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {100, 24}, {120, 36}} {
		m := directoryFixture()
		m.width, m.height = size[0], size[1]
		m.allRecords = nil
		for i := 0; i < 45; i++ {
			m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprint(i), RepoID: "/mono", RepoName: "mono", Author: "Ada", Email: "ada@x", Date: time.Now(), Added: 100 - i, Changes: []git.PathChange{{Path: fmt.Sprintf("dir%02d/file.go", i), Added: 100 - i, LinesKnown: true}}})
		}
		m.recomputeAuthors()
		m = keyboardContractPress(t, m, "enter", "pgdown")
		if m.selectedDirectory(m.directoryRows()) != m.worklistPageSize() {
			t.Fatal("directory paging did not match visible rows")
		}
		m = keyboardContractPress(t, m, "end")
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "dir44/") || strings.Contains(view, "SELECTED") || len(strings.Split(view, "\n")) != m.height {
			t.Fatal("directory ranking lost selected row, height, or full-height layout")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("directory board overflowed terminal")
			}
		}
		m = keyboardContractPress(t, m, "s")
		if m.repositorySort != 1 || m.repositoryMeasureMax(m.directoryRows()) != 100 {
			t.Fatal("sorting by commits changed line bar measure")
		}
	}
}

func TestLiteralRelatedEvidenceExcludesSourcePaths(t *testing.T) {
	for _, id := range []string{"dir:services/auth", "file:services/auth/session.go"} {
		m := directoryFixture()
		m.openDirectoryRepository("/mono")
		m.openDirectoryDetails(id, "services/auth", glanceRelated)
		for _, related := range m.glanceCochangedAreas() {
			for _, record := range related.records {
				for _, change := range record.Changes {
					if strings.HasPrefix(change.Path, "services/auth/") {
						t.Fatal("Related counted the source's own paths")
					}
				}
			}
		}
		if len(m.glanceCochangedAreas()) != 1 || m.glanceCochangedAreas()[0].area.ID != "auto:apps/web" {
			t.Fatal("Related lost a real cross-directory commit")
		}
		m = keyboardContractPress(t, m, "enter")
		if records := m.glanceEvidence(); len(records) != 1 || records[0].Added != 200 || records[0].Removed != 0 {
			t.Fatal("Related detail retained lines from the source scope")
		}
		m = directoryFixture()
		m.allRecords = m.allRecords[:1]
		m.allRecords[0].Changes = m.allRecords[0].Changes[:1]
		m.rebuildAreaDefinitions()
		m.recomputeAuthors()
		m.openDirectoryRepository("/mono")
		m.openDirectoryDetails(id, "services/auth", glanceRelated)
		if len(m.glanceCochangedAreas()) != 0 || len(m.glanceRelatedAreas()) != 0 {
			t.Fatal("source-only work appeared related to itself")
		}
	}
}
