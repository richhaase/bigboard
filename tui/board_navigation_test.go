package tui

import (
	"reflect"
	"testing"
	"time"
)

func TestBoardSwitchRestoresContributorAndActivityDetail(t *testing.T) {
	m := keyboardContractFixture()
	m = keyboardContractPress(t, m, "c", "down", "enter", "down")
	author, offset := m.activeAuthorID, m.statDetailOffset
	m = keyboardContractPress(t, m, "a", "p", "c")
	if m.viewMode != ViewOperative || m.activeAuthorID != author || m.statDetailOffset != offset {
		t.Fatal("switching boards lost contributor detail")
	}
	m = keyboardContractPress(t, m, "c")
	if m.viewMode != ViewOperative || m.activeAuthorID != author || m.statDetailOffset != offset {
		t.Fatal("the current board key reset contributor detail")
	}
	m = keyboardContractPress(t, m, "a", "right", "enter", "down")
	before, pathOffset, person := m.glance, m.pathOffset, m.personID
	m = keyboardContractPress(t, m, "p", "c", "a", "a")
	if !m.showPaths || m.pathOffset != pathOffset || m.personID != person || !reflect.DeepEqual(m.glance, before) {
		t.Fatal("board navigation lost Activity inspection state")
	}
}

func TestBoardRootBackKeepsAppliedSearch(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		m := keyboardContractPress(t, normalizedPRFixture(), board)
		query := "no matches"
		m = keyboardContractPress(t, m, "/", query, "enter", "esc", "left")
		switch board {
		case "a":
			if m.glanceQuery() != query {
				t.Fatal("Activity root Back removed applied search")
			}
		case "c":
			if m.filterQuery != query {
				t.Fatal("Contributors root Back removed applied search")
			}
		case "p":
			if m.prQuery != query {
				t.Fatal("PR root Back removed applied search")
			}
		}
	}
}

func TestSearchCancelRestoresAppliedQuery(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		m := keyboardContractPress(t, normalizedPRFixture(), board, "/", "original", "enter")
		m = keyboardContractPress(t, m, "/", "ctrl+u", "replacement", "esc")
		query := m.glanceQuery()
		switch board {
		case "c":
			query = m.filterQuery
		case "p":
			query = m.prQuery
		}
		if query != "original" || m.searching || m.prSearching || m.glance.searching {
			t.Fatalf("%s canceled search did not restore its applied value", board)
		}
		m = keyboardContractPress(t, m, "/", "ctrl+u", "replacement", "enter")
		m = keyboardContractPress(t, m, "/", "esc")
		query = m.glanceQuery()
		switch board {
		case "c":
			query = m.filterQuery
		case "p":
			query = m.prQuery
		}
		if query != "replacement" {
			t.Fatalf("%s accepted search did not become the next cancel target", board)
		}
	}
}

func TestSharedFiltersNormalizeRememberedContributorDetails(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		for _, remainsVisible := range []bool{true, false} {
			m := keyboardContractPress(t, awarenessFixture(), "c")
			m.selectContributor("email:grace@x")
			m = keyboardContractPress(t, m, "enter", board)
			m.openFilters("repos")
			m.filters.repos = filterValues{"/web"}
			if remainsVisible {
				m.filters.repos = filterValues{"/api"}
			}
			m.applyFilters()
			if board == "p" && !m.showPRs || board == "a" && m.viewMode != ViewAwareness {
				t.Fatal("normalizing remembered contributor details changed the active board")
			}
			m = keyboardContractPress(t, m, "c")
			if remainsVisible {
				if m.viewMode != ViewOperative || m.activeAuthorID != "email:grace@x" {
					t.Fatal("shared filters closed contributor details that still match")
				}
			} else if m.viewMode != ViewAggregate || m.activeAuthorID != "" || m.contributorsView != ViewAggregate {
				t.Fatal("shared filters reopened remembered details for a missing contributor")
			}
		}
	}
}

func TestRefreshNormalizesRememberedContributorDetails(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		m := keyboardContractPress(t, awarenessFixture(), "c")
		m.selectContributor("email:grace@x")
		m = keyboardContractPress(t, m, "enter", board)
		m.resetPending()
		for _, repo := range m.repositories {
			m.scan.admit(RepoLoadedMsg{Generation: m.scan.generation, Repository: repo, Records: m.recordsInRepository(repo, m.local.records[:2])}, time.Now())
		}
		m.finalizeLoad()
		if board == "p" && !m.showPRs || board == "a" && m.viewMode != ViewAwareness {
			t.Fatal("refresh changed the active board")
		}
		m = keyboardContractPress(t, m, "c")
		if m.viewMode != ViewAggregate || m.contributorsView != ViewAggregate || m.activeAuthorID != "" {
			t.Fatal("refresh restored details for a contributor no longer in the snapshot")
		}
	}
}

func TestBoardSwitchCancelSearchKeepsSelectedIdentity(t *testing.T) {
	m := keyboardContractPress(t, normalizedPRFixture(), "p")
	m.prRow = 20
	selectedPR := m.selectedPRKey()
	query := m.visiblePRs()[m.prRow].Title
	m = keyboardContractPress(t, m, "/", query, "f1", "c", "p")
	if m.prQuery != "" || m.selectedPRKey() != selectedPR {
		t.Fatal("canceling PR search through a board switch changed the selected PR")
	}
	m = keyboardContractPress(t, keyboardContractFixture(), "c")
	m.selectedRow = 5
	selectedContributor := m.selectedContributorID()
	query = m.displayedAuthors()[m.selectedRow].Name
	m = keyboardContractPress(t, m, "/", query, "f1", "a", "c")
	if m.filterQuery != "" || m.selectedContributorID() != selectedContributor {
		t.Fatal("canceling contributor search through a board switch changed the selected identity")
	}
}

func TestContextPRsRestoreDestinationWithoutRollingBackData(t *testing.T) {
	m := normalizedPRFixture()
	m.prFilters.author = filterValues{"author29"}
	m.prQuery, m.prRepoID, m.prSort, m.prReverse = "Change 00", "/web", 3, true
	m.prOffset, m.prBrowserError = 7, "previous browser error"
	selected := m.selectedPRKey()
	before := m.glance
	m.openContextPRs("/api")
	if !m.showPRs || !m.prContext.active || m.prRepoID != "/api" || !m.prFilters.empty() || m.prQuery != "" || len(m.visiblePRs()) != 30 {
		t.Fatal("context jump inherited unrelated destination filters")
	}
	checked := time.Now().Add(time.Minute)
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.checked = checked
	m.prState.snapshots["org/repo"] = snapshot
	m = keyboardContractPress(t, m, "enter", "esc")
	if !m.showPRs || !m.prContext.active {
		t.Fatal("closing detail skipped the context list")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.showPRs || m.prContext.active || m.viewMode != ViewAwareness || !reflect.DeepEqual(before, m.glance) {
		t.Fatal("context Back did not restore its Activity origin")
	}
	if m.prRepoID != "/web" || m.prQuery != "Change 00" || m.prSort != 3 || !m.prReverse || m.prOffset != 7 || m.selectedPRKey() != selected || !reflect.DeepEqual(m.prFilters.author, filterValues{"author29"}) {
		t.Fatal("context Back did not restore saved PR board state")
	}
	if !m.prState.snapshots["org/repo"].checked.Equal(checked) {
		t.Fatal("context Back rolled back refreshed snapshot data")
	}
	m.openContextPRs("/api")
	m = keyboardContractPress(t, m, "p")
	if !m.showPRs || m.prContext.active || m.prRepoID != "/web" || m.prQuery != "Change 00" || m.selectedPRKey() != selected {
		t.Fatal("PR board key did not leave temporary context for saved PR state")
	}
}

func TestBoardSwitchCancelsDialogsWithoutChangingUnderlyingState(t *testing.T) {
	m := keyboardContractPress(t, normalizedPRFixture(), "p", "down")
	selected := m.selectedPRKey()
	m.prQuery = "Change"
	for _, open := range []string{"?", "t", "i", "f"} {
		m = keyboardContractPress(t, m, open, "p")
		if !m.showPRs || m.showHelp || m.filters.open || m.viewMode == ViewRepoOverlay || m.selectedPRKey() != selected || m.prQuery != "Change" {
			t.Fatalf("%s changed the PR board while dismissing a dialog", open)
		}
	}
}
