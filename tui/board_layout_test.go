package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func boardLayoutFixture() Model {
	now := time.Now()
	m := Model{viewMode: ViewAwareness, overviewSort: 1, timeIdx: DefaultTimeIndex, sortField: stats.SortByTotal, version: "v0.14.5-dirty", excludedRepos: map[string]bool{}, scannedAt: map[string]time.Time{}, staleRepos: map[string]bool{}}
	m.prState = prState{repositories: map[string]string{}, snapshots: map[string]prSnapshot{}}
	names := []string{"gateway", "web-client", "worker-service", "reporting", "identity", "mobile", "design-library", "infrastructure", "docs", "developer-tools", "billing", "analytics", "search-service", "notifications", "integrations", "test-harness"}
	subjects := []string{"Preserve session state when reconnecting to the gateway", "Add retry handling for delayed background deliveries", "Show reporting totals for the selected period", "Update shared navigation and keyboard focus behavior", "Improve validation messages for incomplete submissions", "Remove obsolete compatibility configuration"}
	for r, name := range names {
		id := "/sample/" + name
		repo := git.Repository{ID: id, Name: name, Path: id}
		m.repositories = append(m.repositories, repo)
		m.loadedRepos = append(m.loadedRepos, repo)
		m.scannedAt[id] = now
		for i := 0; i < 24; i++ {
			author := (r*7+i)%28 + 1
			m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprintf("%040x", r*100+i+1), RepoID: id, RepoName: name, Author: fmt.Sprintf("Contributor %02d", author), Email: fmt.Sprintf("contributor%02d@example.test", author), Date: now.Add(-time.Duration(r*3+i*5+1) * time.Hour), Subject: subjects[(r+i)%len(subjects)], Added: 30 + (r*31+i*67)%2600, Removed: 10 + (r*23+i*13)%700, AIAssisted: i%3 == 0, Changes: []git.PathChange{{Path: "src/session/state.go"}, {Path: "tests/session_test.go"}}})
		}
		remote := "sample/" + name
		m.prState.repositories[id] = remote
		snap := prSnapshot{checked: now, lastGood: now}
		for i := 0; i < 3; i++ {
			number := 100 + r*3 + i
			checks := []string{"SUCCESS", "PENDING", "FAILURE"}[(r+i)%3]
			decision := []string{"APPROVED", "REVIEW_REQUIRED", "CHANGES_REQUESTED"}[(r+i)%3]
			snap.prs = append(snap.prs, gh.PullRequest{Repo: remote, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", remote, number), Title: subjects[(r+i)%len(subjects)], Author: fmt.Sprintf("contributor%02d", (r*7+i)%28+1), UpdatedAt: now.Add(-time.Duration(r*3+i) * time.Hour), Draft: i == 2 && r%3 == 0, CheckState: checks, ReviewDecision: decision, Files: []gh.File{{Path: "src/session/state.go"}}, FilesComplete: true, ContextComplete: true, PreviousPathsComplete: true})
		}
		m.prState.snapshots[remote] = snap
	}
	m.selectedRepoID = m.repositories[0].ID
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	return m
}

func TestMainBoardsAlignFirstRowsAndKeepFrame(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}, {180, 48}} {
		for _, key := range []string{"w", "l", "p"} {
			m := boardLayoutFixture()
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, key)
			view := ansi.Strip(m.View())
			lines := strings.Split(view, "\n")
			if len(lines) != m.height || !strings.HasPrefix(strings.TrimSpace(lines[7]), "› ") {
				t.Fatalf("%s/%v first data row moved or height changed:\n%s", key, size, view)
			}
			if !strings.Contains(lines[5], " of ") {
				t.Fatalf("%s list position missing from heading", key)
			}
			if !strings.Contains(lines[m.height-2], "? help") {
				t.Fatal("footer moved")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%s overflow at %v", key, size)
				}
			}
			if key == "l" && strings.Contains(view, "┏") {
				t.Fatal("Leaderboard retained oversized summary boxes")
			}
		}
	}
}

func TestPRRowsProtectReviewChecksAgeAndDraft(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		for _, width := range []int{40, 80, 110, 120, 180} {
			m := prFixture()
			m.width, m.height = width, 24
			snap := m.prState.snapshots["org/repo"]
			snap.prs[0].Title = strings.Repeat("界 long title ", 40)
			snap.prs[0].Draft = true
			snap.prs[0].ReviewDecision = "CHANGES_REQUESTED"
			snap.prs[0].CheckState = "FAILURE"
			snap.prs[0].UpdatedAt = snap.checked.Add(-3 * time.Hour)
			m.prState.snapshots["org/repo"] = snap
			m = keyboardContractPress(t, m, "p")
			rows := strings.Split(m.View(), "\n")[7 : 7+m.prListRowHeight()]
			text := ansi.Strip(strings.Join(rows, "\n"))
			for _, want := range []string{"#7", "◇", "Changes req.", "Failing", "3h"} {
				if !strings.Contains(text, want) {
					t.Fatalf("%d lost %s:\n%s", width, want, text)
				}
			}
			if profile == termenv.TrueColor {
				red, _, _ := strings.Cut(worklistSignalStyle("FAILURE").Render("X"), "X")
				if !strings.Contains(strings.Join(rows, "\n"), red) {
					t.Fatal("failing checks lost their warning color")
				}
			}
		}
	}
}

func TestWorklistPRColumnHasLabelAndReservedCells(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	a, p, s, pr, _ := m.worklistColumnWidths(116)
	start := 2 + 2 + a + 1 + p + 1 + s + 1
	if strings.TrimSpace(ansi.Cut(lines[6], start, start+pr)) != "PRs" {
		t.Fatal("Worklist PR signals have no column label")
	}
	if !strings.Contains(ansi.Cut(lines[7], start, start+pr), "3 open") {
		t.Fatal("Worklist PR column does not line up")
	}
	if strings.Contains(ansi.Cut(lines[7], 4+a+1+p+1, start-1), "3 open") {
		t.Fatal("PR signals leaked into commit title cells")
	}
}

func TestBoardFiltersAndSortStayInPredictableRows(t *testing.T) {
	for _, key := range []string{"w", "l", "p"} {
		m := boardLayoutFixture()
		m.width, m.height = 120, 36
		m = keyboardContractPress(t, m, key, "/", "contributor", "enter")
		if key == "w" {
			m = keyboardContractPress(t, m, "esc", "/", "gateway", "enter")
		}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if !strings.Contains(lines[1], "/ ") || !strings.Contains(lines[1], "Scope:") || !strings.Contains(lines[5], "↓") {
			t.Fatalf("%s misplaced filter, scope or sort", key)
		}
	}
}

func TestBoardFootersHideWholeHintsAndSeparateCoverage(t *testing.T) {
	for _, width := range []int{40, 80, 120, 180} {
		for _, key := range []string{"w", "l", "p"} {
			m := boardLayoutFixture()
			m.width, m.height = width, 36
			m = keyboardContractPress(t, m, key)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			footer := lines[m.height-2]
			if strings.Contains(footer, "…") {
				t.Fatalf("%s/%d clipped a command", key, width)
			}
			if strings.Contains(lines[m.height-1], "open PR") {
				t.Fatal("browser command appeared in coverage row")
			}
		}
	}
}

func TestPRSelectionAndPagingUsePhysicalRowBudget(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := boardLayoutFixture()
		m.width, m.height = width, 24
		m = keyboardContractPress(t, m, "p", "pgdown")
		if m.prRow != m.prListPageSize() {
			t.Fatal("paging ignored row height")
		}
		m = keyboardContractPress(t, m, "end")
		selected := m.visiblePRs()[m.prRow]
		body := strings.Split(ansi.Strip(m.View()), "\n")[screenHeaderRows : m.height-screenFooterRows]
		if !strings.Contains(strings.Join(body, "\n"), fmt.Sprintf("#%d", selected.Number)) {
			t.Fatalf("%d hid selected PR", width)
		}
	}
}

func TestNarrowLeaderboardKeepsMetricsTogether(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 40, 18
	m = keyboardContractPress(t, m, "l")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(lines[7], "Contributor 03") || !strings.Contains(lines[7], "16") || !strings.Contains(lines[8], "NET 12,080 · AI 25%") {
		t.Fatalf("narrow contributor row scattered its metrics:\n%s", m.View())
	}
	a := m.authors[0]
	a.Commits, a.AICommits = 1000, 1
	if authorAIShare(a) != "<1%" {
		t.Fatal("small AI share became zero")
	}
}

func TestPRBrowserErrorSurvivesRepositoryDiagnostics(t *testing.T) {
	m := prFixture()
	m.width, m.height = 80, 18
	m.showPRs = true
	m.prState.snapshots = nil
	m.prState.errors = map[string]error{}
	for _, repo := range m.loadedRepos {
		m.prState.errors[repo.ID] = fmt.Errorf("repository snapshot unavailable")
	}
	m.prBrowserError = "Could not open browser: unavailable"
	if !strings.Contains(m.View(), m.prBrowserError) {
		t.Fatal("repository diagnostics displaced browser error")
	}
}
