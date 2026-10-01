package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func refreshFixture(t *testing.T) Model {
	t.Helper()
	m := awarenessFixture()
	m.scanContext, m.cancelScans = context.WithCancel(context.Background())
	t.Cleanup(m.cancelScans)
	m.refreshTick = 1
	m.prProvider = &fakePRProvider{}
	return m
}

func applyLocal(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestLocalRefreshTimerSingleChainAndNoOverlap(t *testing.T) {
	if localRefreshInterval != time.Minute {
		t.Fatal("refresh cadence must be one minute")
	}
	m := refreshFixture(t)
	old := append([]git.CommitRecord(nil), m.allRecords...)
	m, cmd := applyLocal(t, m, localRefreshMsg{token: 1})
	if cmd == nil || !m.refreshing || m.loading || m.scanGeneration != 1 {
		t.Fatal("timer did not start background refresh")
	}
	if !reflect.DeepEqual(old, m.allRecords) {
		t.Fatal("refresh cleared displayed records")
	}
	batch := cmd().(tea.BatchMsg)
	if len(batch) != 2 {
		t.Fatal("accepted idle tick must schedule scans and one timer")
	}
	generation, remaining := m.scanGeneration, m.pendingRemaining
	m, cmd = applyLocal(t, m, localRefreshMsg{token: 1})
	if cmd != nil || m.refreshTick != 2 {
		t.Fatal("duplicate tick forked timer chain")
	}
	m, cmd = applyLocal(t, m, localRefreshMsg{token: 2})
	if cmd == nil || m.scanGeneration != generation || m.pendingRemaining != remaining {
		t.Fatal("busy tick overlapped scans or lost cadence")
	}
	m, cmd = applyLocal(t, m, key("R"))
	if cmd != nil || m.scanGeneration != generation {
		t.Fatal("manual refresh overlapped active scans")
	}
}

func TestLocalRefreshRejectsLateDuplicateAndUnscheduledResults(t *testing.T) {
	paths := make([]string, 20)
	for i := range paths {
		paths[i] = fmt.Sprintf("/repo-%d", i)
	}
	m := NewModel(paths, stats.SortByTotal, nil, "", DefaultTimeIndex)
	t.Cleanup(m.cancelScans)
	original := m.pendingRemaining
	for _, msg := range []RepoLoadedMsg{
		{Generation: m.scanGeneration - 1, Repository: m.repositories[0]},
		{Generation: m.scanGeneration + 1, Repository: m.repositories[0]},
		{Generation: m.scanGeneration, Repository: m.repositories[19]},
		{Generation: m.scanGeneration, Repository: git.Repository{ID: "unknown"}},
	} {
		var cmd tea.Cmd
		m, cmd = applyLocal(t, m, msg)
		if cmd != nil || m.pendingRemaining != original || m.activeScans != maxConcurrentRepoScans {
			t.Fatal("invalid result changed scan state")
		}
	}
	// Complete out of dispatch order, admitting exactly one replacement each time.
	msg := RepoLoadedMsg{Generation: m.scanGeneration, Repository: m.repositories[7]}
	m, cmd := applyLocal(t, m, msg)
	if cmd == nil || m.nextRepo != 9 || m.activeScans != maxConcurrentRepoScans {
		t.Fatal("out-of-order result lost scan slot")
	}
	m, cmd = applyLocal(t, m, msg)
	if cmd != nil || m.pendingRemaining != original-1 || m.nextRepo != 9 {
		t.Fatal("duplicate result admitted more scans")
	}
	// Finish the remaining scheduled repositories, continually testing the bound.
	for i, repo := range m.repositories {
		if i == 7 {
			continue
		}
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
		if m.activeScans > maxConcurrentRepoScans || m.activeScans < 0 {
			t.Fatal("scan concurrency exceeded bound")
		}
	}
	if m.refreshing || m.pendingRemaining != 0 || len(m.loadedRepos) != 20 {
		t.Fatal("generation failed to complete exactly once")
	}
	m, cmd = applyLocal(t, m, msg)
	if cmd != nil || m.pendingRemaining != 0 {
		t.Fatal("late result reopened completed generation")
	}
	_ = m.startLocalRefresh(false)
	m, cmd = applyLocal(t, m, msg)
	if cmd != nil || m.pendingRemaining != 20 {
		t.Fatal("old generation interfered with new scan")
	}
}

func TestLocalRefreshFailureRetainsLastGoodAndEmptySuccessReplaces(t *testing.T) {
	m := refreshFixture(t)
	before := append([]git.CommitRecord(nil), m.allRecords...)
	scanned := m.scannedAt["/api"]
	_ = m.startLocalRefresh(false)
	for _, repo := range m.repositories {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo, Err: errors.New("unreadable")})
	}
	if !reflect.DeepEqual(m.allRecords, []git.CommitRecord{before[0], before[2], before[1]}) || len(m.loadedRepos) != 3 || len(m.failedRepos) != 3 || m.err != nil {
		t.Fatal("failure discarded last-good snapshot")
	}
	if !m.staleRepos["/api"] || !m.scannedAt["/api"].Equal(scanned) {
		t.Fatal("failure did not preserve freshness evidence")
	}
	_ = m.startLocalRefresh(false)
	for _, repo := range m.repositories {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	}
	if len(m.allRecords) != 0 || len(m.loadedRepos) != 3 || len(m.failedRepos) != 0 || m.staleRepos["/api"] || !m.scannedAt["/api"].After(scanned) {
		t.Fatal("successful empty snapshot did not clear last-good records/stale state")
	}
}

func TestLocalRefreshPreservesNavigationAndDoesNotRefreshGitHub(t *testing.T) {
	m := refreshFixture(t)
	m.areaRepoID = "/api"
	m.rebuildAreaDefinitions()
	m.glance.detailOpen = true
	m.glance.frame = glanceFrame{areaID: "missing-area", path: "retired", tab: glancePeople, queries: [4]string{"query", "Ada"}, ids: [4]string{"aaa11111"}}
	m.glance.searching = true
	m.filterQuery = "Ada"
	m.overviewSort = 2
	m.selectedAreaID = "missing-area"
	frame := m.glance.frame
	prCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancelPRs = cancel
	m.prGeneration = 42
	m.prState.loading = true
	records := append([]git.CommitRecord(nil), m.allRecords...)
	_ = m.startLocalRefresh(false)
	for _, repo := range m.repositories {
		var recs []git.CommitRecord
		for _, r := range records {
			if r.RepoID == repo.ID {
				recs = append(recs, r)
			}
		}
		var cmd tea.Cmd
		m, cmd = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo, Records: recs})
		if m.pendingRemaining == 0 && cmd != nil {
			t.Fatal("automatic completion scheduled GitHub")
		}
	}
	if m.prGeneration != 42 || !m.prState.loading || prCtx.Err() != nil {
		t.Fatal("automatic local refresh interrupted GitHub")
	}
	if m.loading || m.refreshing || !m.glance.searching || !m.glance.detailOpen || m.glance.frame != frame || m.selectedAreaID != "missing-area" || m.areaRepoID != "/api" || m.filterQuery != "Ada" || m.overviewSort != 2 {
		t.Fatal("refresh changed navigation/search identity")
	}
}

func TestManualLocalRefreshBackgroundAndGitHubSchedule(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		m := refreshFixture(t)
		m.viewMode = view
		generation := m.prGeneration
		m, cmd := applyLocal(t, m, key("R"))
		if cmd == nil || m.loading || !m.refreshing || m.prGeneration != generation {
			t.Fatal("manual refresh must update locally without canceling remote work")
		}
		for _, repo := range m.repositories {
			m, cmd = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
		}
		if cmd == nil || !m.prState.loading {
			t.Fatal("manual refresh completion must refresh PR context")
		}
	}
}

func TestLocalRefreshQuitCancelsTimerAndIgnoresLateMessages(t *testing.T) {
	m := refreshFixture(t)
	timer := localRefreshCmd(m.scanContext, m.refreshTick)
	_ = m.startLocalRefresh(false)
	generation := m.scanGeneration
	next, _ := m.quit()
	m = next.(Model)
	if m.scanContext.Err() == nil {
		t.Fatal("quit did not cancel local scan context")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- timer() }()
	select {
	case result := <-done:
		if result != nil {
			t.Fatal("canceled timer emitted refresh")
		}
	case <-time.After(time.Second):
		t.Fatal("timer did not stop on quit")
	}
	for _, msg := range []tea.Msg{localRefreshMsg{token: m.refreshTick}, RepoLoadedMsg{Generation: generation, Repository: m.repositories[0]}, key("R")} {
		var cmd tea.Cmd
		m, cmd = applyLocal(t, m, msg)
		if cmd != nil || m.refreshing {
			t.Fatal("late event restarted work after quit")
		}
	}
}

func TestLocalRefreshCommitsFreshnessAtomically(t *testing.T) {
	m := refreshFixture(t)
	m.staleRepos["/api"] = true
	_ = m.startLocalRefresh(false)
	before := m
	first := m.repositories[0]
	m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: first})
	if !m.scannedAt[first.ID].Equal(before.scannedAt[first.ID]) || !m.staleRepos[first.ID] || len(m.allRecords) != len(before.allRecords) {
		t.Fatal("partial result changed visible snapshot freshness")
	}
	if !before.inFlight[first.ID] || len(before.pendingScannedAt) != 0 || len(before.pendingStaleRepos) != 0 {
		t.Fatal("result mutated previous model maps")
	}
	for _, repo := range m.repositories[1:] {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo, Err: errors.New("failed")})
	}
	if !m.scannedAt[first.ID].After(before.scannedAt[first.ID]) || m.staleRepos[first.ID] || !m.staleRepos["/web"] {
		t.Fatal("completed snapshot did not commit freshness")
	}
	if !before.staleRepos[first.ID] || before.staleRepos["/web"] {
		t.Fatal("commit mutated previous visible maps")
	}
}

func TestManualRefreshCoalescesWithAutomaticScan(t *testing.T) {
	m := refreshFixture(t)
	_ = m.startLocalRefresh(false)
	generation, remaining := m.scanGeneration, m.pendingRemaining
	m, cmd := applyLocal(t, m, key("R"))
	if cmd != nil || !m.refreshPRs || m.scanGeneration != generation || m.pendingRemaining != remaining {
		t.Fatal("manual request did not join ongoing automatic scan")
	}
	prGeneration := m.prGeneration
	m, cmd = applyLocal(t, m, key("R"))
	if cmd != nil || m.prGeneration != prGeneration {
		t.Fatal("repeated manual request did not coalesce")
	}
	for _, repo := range m.repositories {
		m, cmd = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	}
	if cmd == nil || !m.prState.loading {
		t.Fatal("joined explicit request lost GitHub refresh")
	}
}

func TestAutomaticRefreshRecoversInitialFailure(t *testing.T) {
	m := NewModel([]string{"/repo"}, stats.SortByTotal, nil, "", DefaultTimeIndex)
	t.Cleanup(m.cancelScans)
	m.prProvider = &fakePRProvider{}
	repo := m.repositories[0]
	m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo, Err: errors.New("failed")})
	if m.err == nil || m.loading {
		t.Fatal("initial failure not surfaced")
	}
	m, _ = applyLocal(t, m, localRefreshMsg{token: m.refreshTick})
	m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	if m.err != nil || m.refreshing || len(m.loadedRepos) != 1 || m.areaRepoID != "" {
		t.Fatal("automatic retry failed or unexpectedly changed navigation")
	}
}

func TestAutomaticRefreshPreservesVanishedPersonAndInspector(t *testing.T) {
	m := refreshFixture(t)
	m.areaRepoID = "/api"
	m.rebuildAreaDefinitions()
	m.glance.detailOpen = true
	m.personID = stats.IdentityID(m.allRecords[0])
	m.glance.frame = glanceFrame{areaID: "removed-area", tab: glanceActivity, ids: [4]string{"aaa11111"}}
	m.showPaths = true
	m.pathOffset, m.evidenceOffset = 3, 2
	person := m.personID
	_ = m.startLocalRefresh(false)
	for _, repo := range m.repositories {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	}
	if m.personID != person || !m.showPaths || m.pathOffset != 3 || m.evidenceOffset != 2 || m.glance.frame.ids[glanceActivity] != "aaa11111" {
		t.Fatal("refresh silently broadened vanished person or closed inspector")
	}
	if view := m.View(); !strings.Contains(view, "COMMIT") || !strings.Contains(view, "No matching commit") {
		t.Fatalf("vanished inspector did not retain empty evidence: %s", view)
	}
	if _, ok := m.selectedGlanceCommit(); ok {
		t.Fatal("vanished commit retargeted")
	}
	// Leaving the inspector must not broaden the person's activity implicitly.
	m, _ = applyLocal(t, m, key("esc"))
	if m.personID != person || m.showPaths {
		t.Fatal("next navigation key discarded preserved person intent")
	}
}
