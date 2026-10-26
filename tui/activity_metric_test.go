package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/stats"
)

func measureFixture() Model {
	m := awarenessFixture()
	m.width, m.height = 120, 36
	m.allRecords[0].Added, m.allRecords[0].Removed = 100, 400
	m.allRecords[1].Added = 800
	m.recomputeAuthors()
	return m
}

func TestCommitSortingPreservesLineBarsAndSelection(t *testing.T) {
	m := measureFixture()
	if rows := m.glanceRepositories(); rows[0].repo.ID != "/web" {
		t.Fatal("default repository order must use added plus removed lines")
	}
	selectedRepo := m.selectedRepoID
	for _, width := range []int{40, 80, 120, 180} {
		m.width = width
		rows := m.glanceRepositories()
		maxValue := m.repositoryMeasureMax(rows)
		bars := map[string]int{}
		for _, row := range rows {
			text := ansi.Strip(strings.Join(m.worklistRepositoryRow(row, false, width-4, 1, maxValue), "\n"))
			for _, cell := range text {
				if strings.ContainsRune("█▓▒░", cell) {
					bars[row.repo.ID]++
				}
			}
		}
		if bars["/web"] <= bars["/api"] {
			t.Fatal("lines bars must scale to total additions and removals")
		}
	}
	m = keyboardContractPress(t, m, "s")
	if m.repositorySort != 1 || m.glanceRepositories()[0].repo.ID != "/api" {
		t.Fatal("commit sorting did not change ranking")
	}
	if m.selectedRepoID != selectedRepo || m.repositoryMeasureMax(m.glanceRepositories()) != 800 {
		t.Fatal("sort change lost selection or changed the line bar measure")
	}
	m = keyboardContractPress(t, m, "c", "s")
	if !strings.Contains(ansi.Strip(m.View()), "CONTRIBUTORS · Commits") {
		t.Fatal("Contributors lost commit sorting")
	}
	m = keyboardContractPress(t, m, "m", "w")
	if m.sortField != stats.SortByCommits || strings.Contains(ansi.Strip(m.View()), "m measure") {
		t.Fatal("measure toggle remained active")
	}
	m = keyboardContractPress(t, m, "/", "m")
	if m.glanceQuery() != "m" {
		t.Fatal("search lost typed letter")
	}
}

func TestRepositoryMeasuresRespectScopeAndUnknownLines(t *testing.T) {
	m := measureFixture()
	m.allRecords[0].LinesUnknown = true
	m.allRecords[2].Date = time.Now().Add(-48 * time.Hour)
	m.timeIdx = 0
	m.recomputeAuthors()
	rows := m.glanceRepositories()
	for _, row := range rows {
		if row.repo.ID == "/api" {
			added, removed, unknown := repositoryLineTotals(row)
			if row.commits != 1 || added != 0 || removed != 0 || unknown != 1 {
				t.Fatal("repository measure included unknown or out-of-range lines")
			}
			for _, width := range []int{40, 80, 120} {
				text := ansi.Strip(strings.Join(m.worklistRepositoryRow(row, false, width-4, 1, m.repositoryMeasureMax(rows)), "\n"))
				if !strings.Contains(text, "+?") || !strings.Contains(text, "−?") {
					t.Fatal("unknown line evidence was displayed as zero")
				}
			}
		}
	}
	m.excludedRepos["/web"] = true
	m.recomputeAuthors()
	for _, row := range m.glanceRepositories() {
		if row.repo.ID == "/web" {
			t.Fatal("excluded repository remained in the ranking")
		}
	}
}

func TestRepositoryRankingAndContributorsShareHeightAndPageByVisibleRows(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 20}, {120, 36}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "home")
		page := m.worklistPageSize()
		m = keyboardContractPress(t, m, "pgdown")
		rows := m.glanceRepositories()
		if m.selectedRepository(rows) != min(page, len(rows)-1) {
			t.Fatal("repository paging skipped more than the visible rows")
		}
		available := m.height - screenHeaderRows - screenFooterRows - 2
		panel := activityContributorsHeight(m.width, m.height)
		rowHeight := activityRankingRowHeight(m.width)
		if page*rowHeight+panel > available || available-page*rowHeight-panel >= rowHeight {
			t.Fatal("ranking and contributor panel do not fit the available body")
		}
		if m.width == 120 && m.height == 36 && (panel != 9 || page != 16) {
			t.Fatal("wide board lost its balanced list and contributor panel")
		}
		if m.width == 40 && m.height == 18 && panel != 0 {
			t.Fatal("small terminal sacrificed the ranking to the contributor panel")
		}
	}
}

func TestRepositoryCountsPreserveUnknownQualifiersWhenLarge(t *testing.T) {
	m := measureFixture()
	row := m.glanceRepositories()[0]
	row.people = []stats.AuthorStats{{Added: 12345678, Removed: 12345678, UnknownLineCommits: 1}}
	for _, width := range []int{40, 80, 100, 180} {
		text := ansi.Strip(strings.Join(m.worklistRepositoryRow(row, false, width-4, 1, 24691356), "\n"))
		added := strings.Contains(text, "+12.3M?") || strings.Contains(text, "+12,345,678?")
		removed := strings.Contains(text, "−12.3M?") || strings.Contains(text, "−12,345,678?")
		if strings.Count(text, "?") != 2 || !added || !removed {
			t.Fatalf("large partial counts lost their meaning at %d: %s", width, text)
		}
	}
}
