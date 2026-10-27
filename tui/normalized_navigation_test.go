package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func normalizedPRFixture() Model {
	m := prFixture()
	m.width, m.height = 100, 20
	m.prState.repositories["/web"] = "org/web"
	m.prState.snapshots["org/web"] = prSnapshot{checked: time.Now()}
	for _, remote := range []string{"org/repo", "org/web"} {
		snapshot := m.prState.snapshots[remote]
		snapshot.prs = nil
		for i := 0; i < 30; i++ {
			snapshot.prs = append(snapshot.prs, gh.PullRequest{Repo: remote, Number: i + 1,
				Title: fmt.Sprintf("Change %02d", i), Author: fmt.Sprintf("author%02d", 29-i),
				UpdatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
				Files:     []gh.File{{Path: strings.Repeat("long/", 200) + "file.go"}},
			})
		}
		m.prState.snapshots[remote] = snapshot
	}
	return m
}

func TestNormalizedRootsAndQuit(t *testing.T) {
	for _, board := range []string{"c", "a", "p"} {
		m := keyboardContractPress(t, normalizedPRFixture(), board, "esc", "left", "esc")
		if m.quitting || board == "p" && !m.showPRs || board == "a" && m.areaRepoID != "" {
			t.Fatalf("%s back left the board root", board)
		}
		if board == "c" {
			m.filterQuery = "robot"
		}
		next, cmd := m.Update(keyboardContractKey("q"))
		if !next.(Model).quitting || cmd == nil {
			t.Fatalf("%s q did not quit", board)
		}
	}
}

func TestNormalizedBoardListPaging(t *testing.T) {
	for _, board := range []string{"c", "a", "p"} {
		m := normalizedPRFixture()
		if board == "c" {
			m = keyboardContractFixture()
		}
		m = keyboardContractPress(t, m, board)
		row := func(m Model) int {
			switch board {
			case "c":
				return m.selectedRow
			case "p":
				return m.prRow
			default:
				return m.selectedRepository(m.glanceRepositories())
			}
		}
		m = keyboardContractPress(t, m, "home", "pgdown")
		if row(m) == 0 {
			t.Fatalf("%s PageDown did not advance", board)
		}
		m = keyboardContractPress(t, m, "pgup")
		if row(m) != 0 {
			t.Fatalf("%s PageUp did not return", board)
		}
		m = keyboardContractPress(t, m, "end")
		if row(m) == 0 {
			t.Fatalf("%s End did not advance", board)
		}
		m = keyboardContractPress(t, m, "home")
		if row(m) != 0 {
			t.Fatalf("%s Home did not return", board)
		}
	}
}

func TestContextPRScopeIsIndependentAndBoardKeysRestoreNormalPRs(t *testing.T) {
	m := keyboardContractPress(t, normalizedPRFixture(), "a", "v")
	if m.prRepoID != "/api" || len(m.visiblePRs()) != 30 || m.excludedRepos["/web"] {
		t.Fatal("context scope did not focus the PR board independently of inclusion")
	}
	m = keyboardContractPress(t, m, "right", "enter")
	if !m.prDetail {
		t.Fatal("Enter navigated backward from detail")
	}
	m = keyboardContractPress(t, m, "end")
	if m.prOffset == 0 {
		t.Fatal("PR details did not scroll to end")
	}
	m = keyboardContractPress(t, m, "home", "pgdown")
	if m.prOffset == 0 {
		t.Fatal("PR details did not page")
	}
	m = keyboardContractPress(t, m, "left")
	if m.prDetail || m.prRepoID != "/api" {
		t.Fatal("detail back lost repository scope")
	}
	m = keyboardContractPress(t, m, "left")
	if m.prContext.active || m.showPRs || m.viewMode != ViewAwareness {
		t.Fatal("context Back did not return to Activity")
	}
	m = keyboardContractPress(t, m, "v", "p")
	if !m.showPRs || m.prContext.active || m.prRepoID != "" || len(m.visiblePRs()) != 60 {
		t.Fatal("PR board key did not restore its regular scope")
	}
}

func TestPRSearchSortAndRefreshRetainIdentity(t *testing.T) {
	m := keyboardContractPress(t, normalizedPRFixture(), "p", "down", "down")
	selected := m.selectedPRKey()
	for _, key := range []string{"s", "s", "s", "S", "s"} {
		m = keyboardContractPress(t, m, key)
		if m.selectedPRKey() != selected {
			t.Fatal("sorting changed selected PR")
		}
	}
	m = keyboardContractPress(t, m, "/", "Change 02", "enter")
	if len(m.visiblePRs()) != 2 || m.prQuery != "Change 02" {
		t.Fatal("PR title search failed")
	}
	m = keyboardContractPress(t, m, "/")
	if m.prQuery != "Change 02" {
		t.Fatal("reopening search cleared the query")
	}
	m = keyboardContractPress(t, m, "esc", "/", "ctrl+u", "lwp?Roq", "f1", "esc")
	if !m.prSearching || m.prQuery != "lwp?Roq" || !m.showPRs || m.showHelp {
		t.Fatal("PR search leaked shortcuts")
	}
	m = keyboardContractPress(t, m, "esc", "x", "/", "org/web", "enter")
	if len(m.visiblePRs()) != 30 {
		t.Fatal("repository text search failed")
	}
	m = keyboardContractPress(t, m, "x", "/", "#30", "enter")
	if len(m.visiblePRs()) != 2 {
		t.Fatal("PR number search failed")
	}
	m = keyboardContractPress(t, m, "x", "/", "author29", "enter")
	if len(m.visiblePRs()) != 2 {
		t.Fatal("PR author search failed")
	}
	m = keyboardContractPress(t, m, "x", "a", "v", "end", "right")
	selected = m.selectedPRKey()
	old := m.prState.snapshots["org/repo"]
	m.applyPRResult(prsLoadedMsg{results: map[string]gh.Result{"org/repo": {Complete: true, PRs: old.prs}}, checked: time.Now()})
	if m.selectedPRKey() != selected || !m.prDetail || m.prRepoID != "/api" {
		t.Fatal("refresh lost scoped detail identity")
	}
}

func TestRepositoryControlsApplyAndCancelFromEveryBoard(t *testing.T) {
	for _, board := range []string{"c", "a", "p"} {
		for _, close := range []string{"enter", "esc", "left", "c", "a", "p"} {
			m := keyboardContractPress(t, normalizedPRFixture(), board, "i", "down", " ", close)
			if m.excludedRepos["/web"] != (close == "enter") || m.viewMode == ViewRepoOverlay {
				t.Fatalf("%s/%s applied or canceled incorrectly", board, close)
			}
			if close == "enter" || close == "esc" || close == "left" {
				if m.showPRs != (board == "p") {
					t.Fatal("repository controls changed boards")
				}
			}
		}
	}
	m := keyboardContractPress(t, normalizedPRFixture(), "p")
	for i, pr := range m.visiblePRs() {
		if pr.Repo == "org/repo" {
			m.prRow = i
			break
		}
	}
	m = keyboardContractPress(t, m, "right", "i", " ", "enter")
	if m.prDetail {
		t.Fatal("excluding selected PR's repository silently retargeted detail")
	}
}

func TestWorklistReverseSortKeepsSelection(t *testing.T) {
	m := keyboardContractPress(t, awarenessFixture(), "down")
	id := m.selectedScopeID()
	before := m.glanceRepositories()
	m = keyboardContractPress(t, m, "S")
	after := m.glanceRepositories()
	if m.selectedScopeID() != id || len(before) != len(after) {
		t.Fatal("reverse lost selection")
	}
	for i := range before {
		if before[i].scopeID != after[len(after)-1-i].scopeID {
			t.Fatal("Worklist order did not reverse")
		}
	}
	m = keyboardContractPress(t, keyboardContractFixture(), "right", "4", "down")
	id = m.glance.frame.ids[glanceSubareas]
	beforeSubareas := m.glanceSubareas()
	m = keyboardContractPress(t, m, "S")
	afterSubareas := m.glanceSubareas()
	if m.glance.frame.ids[glanceSubareas] != id {
		t.Fatal("subarea sort lost identity")
	}
	for i := range beforeSubareas {
		if beforeSubareas[i].ID != afterSubareas[len(afterSubareas)-1-i].ID {
			t.Fatal("subarea order did not reverse")
		}
	}
}

func TestInspectorRefreshAndForwardNavigation(t *testing.T) {
	m := keyboardContractPress(t, keyboardContractFixture(), "right", "enter", "enter")
	if !m.showPaths {
		t.Fatal("Enter closed the commit inspector")
	}
	m.repositories = nil
	m.scan.running = false
	m.repositories = m.local.repositories
	next, cmd := m.Update(keyboardContractKey("R"))
	m = next.(Model)
	if cmd == nil || !m.scan.running || !m.showPaths {
		t.Fatal("inspector R did not refresh in place")
	}
	if m.cancelScans != nil {
		m.cancelScans()
	}
}

func TestPRScopePickerFitsAndExplainsControls(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		m := normalizedPRFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "p", "f", "right", "end")
		for _, line := range strings.Split(m.View(), "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("scope picker overflowed")
			}
		}
		if len(strings.Split(m.View(), "\n")) != m.height || !strings.Contains(m.View(), "empty") {
			t.Fatal("scope picker clipped selected repo")
		}
		m = keyboardContractPress(t, m, "?")
		if m.currentHelp().title != "Filters" {
			t.Fatal("scope picker help is not contextual")
		}
	}
}

func TestSearchCtrlCAlwaysQuits(t *testing.T) {
	m := keyboardContractPress(t, normalizedPRFixture(), "p", "/")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !next.(Model).quitting || cmd == nil {
		t.Fatal("PR search Ctrl+C failed")
	}
}

func TestContextControlsStayVisibleAtNarrowWidths(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}} {
		m := normalizedPRFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "p")
		footer := strings.Split(ansi.Strip(m.View()), "\n")[m.height-2]
		if !strings.Contains(footer, "f filters") || !strings.Contains(footer, "/ find") {
			t.Fatalf("PR controls clipped: %s", footer)
		}
		m = keyboardContractPress(t, m, "f")
		if !strings.Contains(ansi.Strip(m.View()), "Esc cancel") {
			t.Fatal("scope modal cancellation clipped")
		}
		m = keyboardContractPress(t, m, "esc", "i")
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Enter apply") || !strings.Contains(view, "Esc cancel") {
			t.Fatal("inclusion dialog apply/cancel hints clipped")
		}
	}
}
