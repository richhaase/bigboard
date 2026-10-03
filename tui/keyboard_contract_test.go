package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

// Use actual terminal key types: treating "left" as pasted text would miss
// shortcut leakage while a search or numeric input owns the keyboard.
func keyboardContractKey(s string) tea.KeyMsg {
	keys := map[string]tea.KeyType{
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab,
		"backspace": tea.KeyBackspace, "ctrl+u": tea.KeyCtrlU,
	}
	if typ, ok := keys[s]; ok {
		return tea.KeyMsg{Type: typ}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyboardContractPress(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		scanGeneration, prGeneration := m.scanGeneration, m.prGeneration
		next, cmd := m.Update(keyboardContractKey(k))
		m = next.(Model)
		if cmd != nil || m.scanGeneration != scanGeneration || m.prGeneration != prGeneration {
			t.Fatalf("navigation key %q scheduled work or changed refresh generation", k)
		}
	}
	return m
}

func keyboardContractFixture() Model {
	m := monorepoFixture()
	m.width, m.height = 80, 18
	m.allRecords = nil
	now := time.Now()
	for i := 0; i < 60; i++ {
		m.allRecords = append(m.allRecords, git.CommitRecord{
			CommitID: fmt.Sprintf("key-%03d", i), Author: fmt.Sprintf("Person %02d", i%12),
			Email: fmt.Sprintf("person%02d@example.com", i%12), Date: now.Add(-time.Duration(i) * time.Minute),
			RepoID: "/mono", RepoName: "mono", Subject: fmt.Sprintf("Change %03d", i),
			Changes: []git.PathChange{{Path: fmt.Sprintf("feature/child%02d/file.go", i%10)}, {Path: fmt.Sprintf("area%02d/file.go", i%6)}},
		})
	}
	m.rebuildAreaDefinitions()
	m.recomputeAuthors()
	m.selectedAreaID = "auto:feature"
	return m
}

type keyboardContractContext struct {
	name  string
	setup func(*testing.T) Model
}

func keyboardContractOrdinaryContexts() []keyboardContractContext {
	contexts := []keyboardContractContext{
		{"repositories", func(t *testing.T) Model { return keyboardContractPress(t, awarenessFixture(), "down") }},
		{"areas", func(t *testing.T) Model { return keyboardContractPress(t, keyboardContractFixture(), "down") }},
		{"leaderboard", func(t *testing.T) Model {
			m := keyboardContractFixture()
			m.viewMode = ViewAggregate
			return keyboardContractPress(t, m, "down")
		}},
		{"contributor", func(t *testing.T) Model {
			m := keyboardContractFixture()
			m.viewMode = ViewAggregate
			return keyboardContractPress(t, m, "right", "down", "pgdown")
		}},
		{"person activity", func(t *testing.T) Model {
			return keyboardContractPress(t, keyboardContractFixture(), "right", "2", "down", "enter", "down")
		}},
		{"nested subarea", func(t *testing.T) Model {
			return keyboardContractPress(t, keyboardContractFixture(), "right", "4", "down", "enter", "down")
		}},
		{"contributor overlap", func(t *testing.T) Model {
			return keyboardContractPress(t, keyboardContractFixture(), "right", "3", "o", "down")
		}},
	}
	for tab, name := range glanceTabs {
		contexts = append(contexts, keyboardContractContext{name, func(t *testing.T) Model {
			return keyboardContractPress(t, keyboardContractFixture(), "right", fmt.Sprint(tab+1), "down")
		}})
	}
	return contexts
}

func keyboardContractOwnedContexts() []keyboardContractContext {
	return []keyboardContractContext{
		{"commit inspector", func(t *testing.T) Model {
			m := keyboardContractFixture()
			m.allRecords[0].Subject = strings.Repeat("Long commit evidence ", 120)
			return keyboardContractPress(t, m, "right", "enter", "down")
		}},
		{"PR list", func(t *testing.T) Model {
			return keyboardContractPress(t, prOnlySelectionFixture(), "P", "down")
		}},
		{"PR detail", func(t *testing.T) Model {
			m := prFixture()
			m.width, m.height = 80, 18
			snapshot := m.prState.snapshots["org/repo"]
			for i := 0; i < 40; i++ {
				snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: fmt.Sprintf("feature/file%02d.go", i)})
			}
			m.prState.snapshots["org/repo"] = snapshot
			return keyboardContractPress(t, m, "P", "enter", "down")
		}},
		{"repository overlay", func(t *testing.T) Model {
			return keyboardContractPress(t, awarenessFixture(), "r", "down")
		}},
		{"scan errors", func(t *testing.T) Model {
			m := awarenessFixture()
			m.width, m.height = 40, 18
			m.scanErrors = map[string]string{"/api": strings.Repeat("Long diagnostic ", 120)}
			return keyboardContractPress(t, m, "e", "down")
		}},
		{"range picker", func(t *testing.T) Model {
			return keyboardContractPress(t, awarenessFixture(), "t", "home", "down")
		}},
		{"help", func(t *testing.T) Model { return keyboardContractPress(t, keyboardContractFixture(), "?") }},
	}
}

// Freeze the observable navigation fields before a key, including maps/slices,
// without comparing context cancellation functions or the provider itself.
func keyboardContractState(m Model) string {
	return fmt.Sprintf("%#v", []any{
		m.viewMode, m.returnView, m.glance, m.selectedRepoID, m.areaRepoID, m.selectedAreaID, m.personID,
		m.selectedRow, m.scrollOffset, m.activeAuthorID, m.activeOperative, m.statDetailOffset,
		m.showPaths, m.pathOffset, m.showPRs, m.prAll, m.prDetail, m.prRow, m.prOffset,
		m.prBrowserError, m.showScanErrors, m.scanErrorOffset, m.overlayCursor, m.overlayExcluded, m.excludedRepos,
		m.filterQuery, m.searching, m.rangePicker, m.timeIdx, m.customRangeDays, m.overviewSort,
		m.sortField, m.sortAsc, m.hideBots, m.loading, m.refreshing, m.quitting, m.scanGeneration, m.prGeneration,
	})
}

func TestKeyboardContractRemovedAliasesAreInert(t *testing.T) {
	contexts := append(keyboardContractOrdinaryContexts(), keyboardContractOwnedContexts()...)
	for _, tc := range contexts {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.setup(t)
			// Resolve lazy selection before asserting that a key is a no-op.
			base = keyboardContractPress(t, base, "")
			for _, k := range []string{"h", "j", "k", "g", "G", "v", "L"} {
				t.Run(k, func(t *testing.T) {
					before := keyboardContractState(base)
					after := keyboardContractPress(t, base, k)
					if got := keyboardContractState(after); got != before {
						t.Fatalf("removed alias %q changed navigation\nbefore: %s\nafter:  %s", k, before, got)
					}
				})
			}
		})
	}
}

func TestKeyboardContractLowercaseLRoundTripPreservesWorklist(t *testing.T) {
	for _, tc := range keyboardContractOrdinaryContexts() {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			before := m
			if m.viewMode == ViewOperative {
				m = keyboardContractPress(t, m, "l")
				if keyboardContractState(m) != keyboardContractState(before) {
					t.Fatal("l escaped contributor detail or drilled in")
				}
				return
			}
			m = keyboardContractPress(t, m, "l")
			want := ViewAggregate
			if before.viewMode == ViewAggregate {
				want = ViewAwareness
			}
			if m.viewMode != want || m.showPaths || m.showPRs {
				t.Fatalf("l should toggle leaderboard without drilling in: view=%v, inspector=%v, PRs=%v", m.viewMode, m.showPaths, m.showPRs)
			}
			m = keyboardContractPress(t, m, "l")
			if !reflect.DeepEqual(before.glance, m.glance) || before.selectedRepoID != m.selectedRepoID ||
				before.areaRepoID != m.areaRepoID || before.selectedAreaID != m.selectedAreaID || before.personID != m.personID {
				t.Fatal("leaderboard round trip lost Worklist scope, lens, search, stack, or selected identity")
			}
		})
	}
}

func TestKeyboardContractOwnedViewsIsolateLeaderboardShortcut(t *testing.T) {
	for _, tc := range keyboardContractOwnedContexts() {
		t.Run(tc.name, func(t *testing.T) {
			m := keyboardContractPress(t, tc.setup(t), "")
			before := keyboardContractState(m)
			m = keyboardContractPress(t, m, "l")
			if keyboardContractState(m) != before {
				t.Fatal("l escaped the active overlay or acted as drill-in")
			}
		})
	}
}

func TestKeyboardContractSearchOwnsAllLetterShortcuts(t *testing.T) {
	for _, tc := range keyboardContractOrdinaryContexts() {
		if tc.name == "contributor" {
			continue // Contributor detail has no search input.
		}
		t.Run(tc.name, func(t *testing.T) {
			m := keyboardContractPress(t, tc.setup(t), "/")
			view, scan, prs := m.viewMode, m.scanGeneration, m.prGeneration
			const typed = "hjklgGvLpPoRré"
			for _, r := range typed {
				m = keyboardContractPress(t, m, string(r))
			}
			for _, k := range []string{"left", "right", "up", "down", "home", "end", "pgup", "pgdown"} {
				m = keyboardContractPress(t, m, k)
			}
			query := m.glanceQuery()
			if view == ViewAggregate {
				query = m.filterQuery
			}
			if query != typed || m.viewMode != view || m.scanGeneration != scan || m.prGeneration != prs ||
				m.showPaths || m.showPRs || m.rangePicker.open || m.showScanErrors {
				t.Fatalf("search lost text or leaked shortcut: query=%q, view=%v", query, m.viewMode)
			}
			m = keyboardContractPress(t, m, "backspace", "enter")
			if m.searching || m.glance.searching {
				t.Fatal("Enter did not finish search")
			}
			m = keyboardContractPress(t, m, "/", "esc")
			if m.searching || m.glance.searching || (view == ViewAggregate && m.filterQuery != "") ||
				(view == ViewAwareness && m.glanceQuery() != "") {
				t.Fatal("Esc did not cancel search")
			}
		})
	}
}

func TestKeyboardContractCustomRangeRejectsLettersWithoutNavigation(t *testing.T) {
	m := keyboardContractPress(t, keyboardContractFixture(), "right", "4", "down", "t", "c", "enter", "ctrl+u", "21")
	before := m.glance
	for _, k := range []string{"h", "j", "k", "l", "g", "G", "v", "p", "P", "o", "R", "r"} {
		m = keyboardContractPress(t, m, k)
		if !m.rangePicker.open || !m.rangePicker.custom || m.rangePicker.input != "21" || m.rangePicker.error == "" ||
			m.viewMode != ViewAwareness || !reflect.DeepEqual(before, m.glance) {
			t.Fatalf("%q escaped numeric input or was accepted as days", k)
		}
	}
	m = keyboardContractPress(t, m, "backspace", "0", "enter")
	if m.rangePicker.open || m.customRangeDays != 20 || !reflect.DeepEqual(before, m.glance) {
		t.Fatal("numeric editing and Enter no longer apply the custom range in place")
	}
}

func TestKeyboardContractFocusedNavigationRetainsArrowsAndPaging(t *testing.T) {
	for tab, name := range glanceTabs {
		t.Run(name, func(t *testing.T) {
			m := keyboardContractPress(t, keyboardContractFixture(), "right", fmt.Sprint(tab+1))
			count := len(m.glanceDetailIDs())
			if count < 3 {
				t.Fatal("fixture must expose at least three rows")
			}
			for _, tc := range []struct {
				key string
				row int
			}{{"down", 1}, {"up", 0}, {"end", count - 1}, {"home", 0}, {"pgdown", min(count-1, m.height-10)}, {"pgup", 0}} {
				m = keyboardContractPress(t, m, tc.key)
				if m.glance.frame.rows[tab] != tc.row {
					t.Fatalf("%s selected row %d, want %d", tc.key, m.glance.frame.rows[tab], tc.row)
				}
			}
			m = keyboardContractPress(t, m, "down") // Skip the People lens's all-contributors row.
			before := m.glance.frame
			m = keyboardContractPress(t, m, "right")
			if !m.showPaths && reflect.DeepEqual(before, m.glance.frame) && m.personID == "" {
				t.Fatal("right did not open selected lens evidence")
			}
			m = keyboardContractPress(t, m, "left")
			if m.showPaths || m.personID != "" || m.glance.frame.tab != tab {
				t.Fatalf("left did not return to the originating lens: tab=%d want=%d person=%q paths=%v detail=%v", m.glance.frame.tab, tab, m.personID, m.showPaths, m.glance.detailOpen)
			}
		})
	}
}

func TestKeyboardContractInspectorAndErrorsRetainScrolling(t *testing.T) {
	for _, tc := range keyboardContractOwnedContexts() {
		if tc.name != "commit inspector" && tc.name != "scan errors" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			offset := func(m Model) int {
				if tc.name == "commit inspector" {
					return m.pathOffset
				}
				return m.scanErrorOffset
			}
			m = keyboardContractPress(t, m, "home", "down")
			if offset(m) != 1 {
				t.Fatal("down did not scroll one line")
			}
			m = keyboardContractPress(t, m, "up")
			if offset(m) != 0 {
				t.Fatal("up did not scroll back")
			}
			m = keyboardContractPress(t, m, "pgdown")
			if offset(m) <= 1 {
				t.Fatal("PageDown did not scroll a page")
			}
			m = keyboardContractPress(t, m, "pgup")
			if offset(m) != 0 {
				t.Fatal("PageUp did not return to first page")
			}
			m = keyboardContractPress(t, m, "end")
			if offset(m) <= 1 {
				t.Fatal("End did not reach final evidence")
			}
			m = keyboardContractPress(t, m, "esc")
			if m.showPaths || m.showScanErrors {
				t.Fatal("Esc did not close owned view")
			}
		})
	}
}

func TestKeyboardContractPRScopeAndBrowserActionsStayLocal(t *testing.T) {
	m := prOnlySelectionFixture()
	m.areaRepoID, m.selectedAreaID = "/api", "root"
	provider := &fakePRProvider{}
	m.prProvider = provider
	m = keyboardContractPress(t, m, "p")
	if m.prAll || len(m.visiblePRs()) != 2 {
		t.Fatal("p did not open the selected area scope")
	}
	m = keyboardContractPress(t, m, "down", "enter", "P")
	if !m.prAll || m.prDetail || m.prRow != 0 || len(m.visiblePRs()) != 3 {
		t.Fatal("P inside PR detail did not select all open PRs")
	}
	m = keyboardContractPress(t, m, "down", "up", "enter", "p")
	if m.prAll || m.prDetail || m.prRow != 0 || len(m.visiblePRs()) != 2 {
		t.Fatal("p inside PR detail did not restore parent area scope")
	}
	if provider.resolves != 0 || provider.fetches != 0 {
		t.Fatal("PR scope/navigation spent the remote request budget")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.showPRs || m.selectedAreaID != "root" {
		t.Fatal("Esc lost the selected Worklist scope")
	}
	// Outside PR overlays, o continues to mean contributor overlap evidence.
	m = keyboardContractPress(t, keyboardContractFixture(), "right", "3", "o")
	if !m.glance.frame.relatedOverlap || m.showPRs {
		t.Fatal("o no longer opens related contributor evidence")
	}
	for _, tc := range keyboardContractOrdinaryContexts() {
		m = tc.setup(t)
		m.prBrowserOpener = func(string) error { t.Fatal("o opened a browser outside PR context"); return nil }
		m = keyboardContractPress(t, m, "o")
		if m.showPRs {
			t.Fatalf("o in %s opened PR context", tc.name)
		}
	}
}

func TestKeyboardContractOverviewRetainsArrowsAndPaging(t *testing.T) {
	for _, tc := range []keyboardContractContext{
		{"repositories", func(*testing.T) Model { return awarenessFixture() }},
		{"areas", func(*testing.T) Model { return keyboardContractFixture() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			rows := m.glanceRepositories()
			m = keyboardContractPress(t, m, "home")
			if m.selectedScopeID() != rows[0].repo.ID {
				t.Fatal("Home did not select first row")
			}
			m = keyboardContractPress(t, m, "down")
			if m.selectedScopeID() != rows[1].repo.ID {
				t.Fatal("down did not select next row")
			}
			m = keyboardContractPress(t, m, "up", "pgdown")
			if m.selectedScopeID() != rows[min(len(rows)-1, m.worklistPageSize())].repo.ID {
				t.Fatal("PageDown did not select a page of rows")
			}
			m = keyboardContractPress(t, m, "pgup")
			if m.selectedScopeID() != rows[0].repo.ID {
				t.Fatal("PageUp did not return to first row")
			}
			m = keyboardContractPress(t, m, "end")
			if m.selectedScopeID() != rows[len(rows)-1].repo.ID {
				t.Fatal("End did not reach final row")
			}
		})
	}
	m := keyboardContractPress(t, awarenessFixture(), "left")
	if m.quitting {
		t.Fatal("left at Worklist root quit")
	}
	selected := m.selectedRepoID
	m = keyboardContractPress(t, m, "right", "right", "right")
	if !m.showPaths {
		t.Fatal("right did not reach commit evidence from repository root")
	}
	m = keyboardContractPress(t, m, "left", "left", "left", "left")
	if m.showPaths || m.glance.detailOpen || m.areaRepoID != "" || m.selectedRepoID != selected || m.quitting {
		t.Fatal("left did not unwind and preserve the Worklist root selection")
	}
}

func TestKeyboardContractLeaderboardAndContributorRetainNavigation(t *testing.T) {
	m := keyboardContractPress(t, keyboardContractFixture(), "l", "down")
	if m.selectedRow != 1 || m.viewMode != ViewAggregate {
		t.Fatal("leaderboard down did not move selection")
	}
	m = keyboardContractPress(t, m, "up", "right")
	if m.viewMode != ViewOperative || m.activeAuthorID != m.displayedAuthors()[0].ID {
		t.Fatal("right did not open selected contributor")
	}
	m = keyboardContractPress(t, m, "down")
	if m.activeAuthorID != m.displayedAuthors()[1].ID {
		t.Fatal("contributor down did not select next identity")
	}
	m = keyboardContractPress(t, m, "up", "pgdown")
	if m.activeAuthorID != m.displayedAuthors()[0].ID || m.statDetailOffset == 0 {
		t.Fatal("contributor arrows or PageDown no longer work")
	}
	m = keyboardContractPress(t, m, "pgup")
	if m.statDetailOffset != 0 {
		t.Fatal("PageUp did not return contributor detail to its first page")
	}
	m = keyboardContractPress(t, m, "end")
	if m.statDetailOffset == 0 {
		t.Fatal("End did not reach the last contributor detail page")
	}
	m = keyboardContractPress(t, m, "home", "left")
	if m.viewMode != ViewAggregate || m.activeAuthorID != "" || m.statDetailOffset != 0 {
		t.Fatal("Home/left failed to return to leaderboard")
	}
	m = keyboardContractPress(t, m, "enter", "esc", "l", "left")
	if m.viewMode != ViewAwareness || m.quitting {
		t.Fatal("Enter/Esc and l did not restore Worklist")
	}
}

func TestKeyboardContractRepositoryAndRangePickersRetainNavigation(t *testing.T) {
	for _, closeKey := range []string{"enter", "esc"} {
		m := keyboardContractPress(t, awarenessFixture(), "r", "down", "up", "down", " ")
		if m.overlayCursor != 1 || !m.overlayExcluded[m.loadedRepos[1].ID] {
			t.Fatal("repository arrows/space no longer select and toggle the target row")
		}
		m = keyboardContractPress(t, m, closeKey)
		if m.viewMode != ViewAwareness || !m.excludedRepos[m.loadedRepos[1].ID] {
			t.Fatalf("repository %s did not apply selection and return", closeKey)
		}
	}
	m := keyboardContractPress(t, awarenessFixture(), "t", "home", "down", "up", "down", "enter")
	if m.rangePicker.open || m.timeIdx != 1 {
		t.Fatal("range arrows/Enter did not apply the selected preset")
	}
	m = keyboardContractPress(t, m, "t", "end", "enter", "ctrl+u", "30", "esc")
	if m.rangePicker.open || m.timeIdx != 1 || m.customRangeDays != 0 {
		t.Fatal("custom range Esc changed the applied range")
	}
}
