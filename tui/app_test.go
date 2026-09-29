package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// modelWithData returns a loaded Model with two contributors across two repos.
func modelWithData() Model {
	now := time.Now()
	m := Model{
		allRecords: []git.CommitRecord{
			{Author: "Ada Lovelace", Email: "ada@x.com", Date: now, Added: 100, Removed: 10, RepoName: "engine"},
			{Author: "Grace Hopper", Email: "grace@x.com", Date: now, Added: 50, Removed: 5, RepoName: "compiler"},
		},
		loadedRepos: []git.Repository{
			{ID: "compiler", Name: "compiler"},
			{ID: "engine", Name: "engine"},
		},
		excludedRepos: map[string]bool{},
		timeIdx:       len(TimePresets) - 1, // ALL
		width:         100,
		height:        40,
	}
	m.recomputeAuthors()
	return m
}

// TestFilteredRecordsExcludesRepos guards that the shared filter (used by both
// the leaderboard and the operative timeline) honors repo exclusions.
func TestFilteredRecordsExcludesRepos(t *testing.T) {
	now := time.Now()
	m := Model{
		allRecords: []git.CommitRecord{
			{Author: "A", Email: "a@x.com", Date: now, Added: 10, RepoName: "keep"},
			{Author: "B", Email: "b@x.com", Date: now, Added: 20, RepoName: "drop"},
		},
		excludedRepos: map[string]bool{"drop": true},
		timeIdx:       len(TimePresets) - 1, // ALL
	}
	recs := m.filteredRecords()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after exclusion, got %d", len(recs))
	}
	if recs[0].RepoName != "keep" {
		t.Errorf("excluded repo leaked into filtered records: %q", recs[0].RepoName)
	}
}

// TestRecomputeAuthorsClampsSelectedRow guards that narrowing the time range
// can never leave the cursor pointing past the end of the author list.
func TestRecomputeAuthorsClampsSelectedRow(t *testing.T) {
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	m := Model{
		allRecords: []git.CommitRecord{
			{Author: "Recent", Email: "r@x.com", Date: now, Added: 5, RepoName: "r"},
			{Author: "Old", Email: "o@x.com", Date: old, Added: 5, RepoName: "r"},
		},
		timeIdx:     len(TimePresets) - 1, // ALL → 2 authors
		selectedRow: 1,
	}
	m.recomputeAuthors()
	if len(m.authors) != 2 || m.selectedRow != 1 {
		t.Fatalf("setup: authors=%d selectedRow=%d", len(m.authors), m.selectedRow)
	}
	// Narrow to 7d: only "Recent" survives, so the cursor must clamp to 0.
	m.timeIdx = 1 // 7d
	m.recomputeAuthors()
	if len(m.authors) != 1 {
		t.Fatalf("expected 1 author after narrowing, got %d", len(m.authors))
	}
	if m.selectedRow != 0 {
		t.Errorf("expected selectedRow clamped to 0, got %d", m.selectedRow)
	}
}

func TestHandleKeySortCycles(t *testing.T) {
	m := Model{viewMode: ViewAggregate, sortField: stats.SortByTotal}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if got := updated.(Model).sortField; got != stats.SortByCommits {
		t.Errorf("after 's', sortField = %v, want SortByCommits", got)
	}
}

// TestViewStatesRender exercises every View() branch to prove the render paths
// don't panic and that the refactored states show their expected markers.
func TestViewStatesRender(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		out := Model{loading: true, width: 100, pendingRemaining: 3}.View()
		if !strings.Contains(out, "SCANNING REPOSITORIES") || !strings.Contains(out, "0/3 repos") {
			t.Errorf("loading view missing boot markers:\n%s", out)
		}
	})

	t.Run("boot-log-lines", func(t *testing.T) {
		m := Model{loading: true, width: 100, pendingRemaining: 1}
		m.bootLines = []string{bootLine("engine", true), bootLine("compiler", false)}
		out := m.View()
		if !strings.Contains(out, "engine") || !strings.Contains(out, "unreadable") {
			t.Errorf("boot log should show ok + failed repos:\n%s", out)
		}
	})

	t.Run("loading-narrow", func(t *testing.T) {
		out := Model{loading: true, width: 40}.View()
		if !strings.Contains(out, "BIG") && !strings.Contains(out, "B I G") {
			t.Errorf("narrow loading view missing compact title:\n%s", out)
		}
	})

	t.Run("error", func(t *testing.T) {
		out := Model{err: errors.New("boom"), width: 100}.View()
		if !strings.Contains(out, "ERROR") || !strings.Contains(out, "boom") {
			t.Errorf("error view missing error header or message:\n%s", out)
		}
	})

	t.Run("empty", func(t *testing.T) {
		m := Model{width: 100, height: 40, excludedRepos: map[string]bool{}, timeIdx: 0}
		out := m.View()
		if !strings.Contains(out, "NO SIGNAL") {
			t.Errorf("empty view missing NO SIGNAL marker:\n%s", out)
		}
	})

	t.Run("aggregate", func(t *testing.T) {
		out := modelWithData().View()
		if !strings.Contains(out, "Ada Lovelace") || !strings.Contains(out, "CONTRIBUTOR") {
			t.Errorf("aggregate view missing expected content:\n%s", out)
		}
	})

	t.Run("operative", func(t *testing.T) {
		m := modelWithData()
		m.viewMode = ViewOperative
		m.activeOperative = m.authors[0].Name
		out := m.View()
		if !strings.Contains(strings.ToUpper(out), strings.ToUpper(m.authors[0].Name)) {
			t.Errorf("operative view missing contributor name:\n%s", out)
		}
	})

	t.Run("overlay", func(t *testing.T) {
		m := modelWithData()
		m.viewMode = ViewRepoOverlay
		m.overlayExcluded = map[string]bool{}
		out := m.View()
		if !strings.Contains(out, "engine") || !strings.Contains(out, "compiler") {
			t.Errorf("overlay view missing repo names:\n%s", out)
		}
	})

	t.Run("failed-repos-warning", func(t *testing.T) {
		m := modelWithData()
		m.failedRepos = []string{"brokenrepo"}
		out := m.View()
		if !strings.Contains(out, "unreadable") || !strings.Contains(out, "brokenrepo") {
			t.Errorf("aggregate view missing failed-repo warning:\n%s", out)
		}
	})
}

// key builds a KeyMsg for a named key or a literal rune string.
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m Model, keys ...string) Model {
	for _, k := range keys {
		updated, _ := m.handleKey(key(k))
		m = updated.(Model)
	}
	return m
}

func TestGatedKeysInOverlay(t *testing.T) {
	m := modelWithData()
	m.viewMode = ViewRepoOverlay
	m.overlayExcluded = map[string]bool{}
	startIdx := m.timeIdx

	m = send(m, "R")
	if m.loading {
		t.Error("R in overlay should not trigger a reload")
	}
	if m.viewMode != ViewRepoOverlay {
		t.Errorf("R in overlay should leave view unchanged, got %v", m.viewMode)
	}

	m = send(m, "left", "right")
	if m.timeIdx != startIdx {
		t.Errorf("left/right in overlay should not change timeIdx, got %d want %d", m.timeIdx, startIdx)
	}
}

func TestTimeKeysInAggregateAndOperative(t *testing.T) {
	m := modelWithData()
	m.timeIdx = 2
	m = send(m, "left")
	if m.timeIdx != 1 {
		t.Errorf("left in aggregate should decrement timeIdx, got %d", m.timeIdx)
	}
	m = send(m, "right")
	if m.timeIdx != 2 {
		t.Errorf("right in aggregate should increment timeIdx, got %d", m.timeIdx)
	}

	m.viewMode = ViewOperative
	m.activeOperative = m.authors[0].Name
	m = send(m, "left")
	if m.timeIdx != 1 {
		t.Errorf("left in operative should decrement timeIdx, got %d", m.timeIdx)
	}
}

func TestSearchFilter(t *testing.T) {
	m := modelWithData() // Ada Lovelace, Grace Hopper
	m = send(m, "/")
	if !m.searching {
		t.Fatal("'/' should open search")
	}
	m = send(m, "g", "r", "a", "c", "e")
	disp := m.displayedAuthors()
	if len(disp) != 1 || disp[0].Name != "Grace Hopper" {
		t.Fatalf("filter 'grace' => %d authors, want Grace Hopper", len(disp))
	}
	m = send(m, "enter")
	if m.searching || m.filterQuery != "grace" {
		t.Errorf("enter should apply+close: searching=%v query=%q", m.searching, m.filterQuery)
	}
	m = send(m, "esc")
	if m.filterQuery != "" {
		t.Errorf("esc should clear filter, got %q", m.filterQuery)
	}
}

func TestSortDirectionToggle(t *testing.T) {
	m := modelWithData() // default total desc: Ada(110) before Grace(20)
	if m.authors[0].Name != "Ada Lovelace" {
		t.Fatalf("setup: expected Ada first, got %q", m.authors[0].Name)
	}
	m = send(m, "S")
	if !m.sortAsc {
		t.Fatal("'S' should toggle ascending")
	}
	if m.authors[0].Name != "Grace Hopper" {
		t.Errorf("ascending should put smallest first, got %q", m.authors[0].Name)
	}
}

func TestStepOperative(t *testing.T) {
	m := modelWithData()
	m.viewMode = ViewOperative
	m.activeOperative = "Ada Lovelace"
	m = send(m, "j")
	if m.activeOperative != "Grace Hopper" {
		t.Errorf("j should step to next contributor, got %q", m.activeOperative)
	}
	m = send(m, "k")
	if m.activeOperative != "Ada Lovelace" {
		t.Errorf("k should step back, got %q", m.activeOperative)
	}
}

func TestScrollReachesAllContributors(t *testing.T) {
	now := time.Now()
	var recs []git.CommitRecord
	for i := 0; i < 30; i++ {
		recs = append(recs, git.CommitRecord{
			Author:   string(rune('a'+i%26)) + string(rune('A'+i)),
			Email:    string(rune('a'+i)) + "@x.com",
			Date:     now,
			Added:    30 - i, // distinct totals so order is stable
			RepoName: "r",
		})
	}
	m := Model{allRecords: recs, excludedRepos: map[string]bool{}, timeIdx: len(TimePresets) - 1, width: 100, height: 24}
	m.recomputeAuthors()
	if len(m.authors) != 30 {
		t.Fatalf("expected 30 authors, got %d", len(m.authors))
	}
	// Navigate to the last contributor — previously unreachable past row 20.
	for i := 0; i < 40; i++ {
		m = send(m, "down")
	}
	if m.selectedRow != 29 {
		t.Errorf("expected to reach row 29, got %d", m.selectedRow)
	}
	visible := m.tableViewport()
	if m.selectedRow < m.scrollOffset || m.selectedRow >= m.scrollOffset+visible {
		t.Errorf("cursor %d outside window [%d,%d)", m.selectedRow, m.scrollOffset, m.scrollOffset+visible)
	}
}

func TestStreamingLoadFinalizes(t *testing.T) {
	now := time.Now()
	m := NewModel([]string{"/x/repoA", "/y/repoB"}, stats.SortByTotal, map[string]bool{}, "v", DefaultTimeIndex)
	if !m.loading || m.pendingRemaining != 2 {
		t.Fatalf("initial: loading=%v remaining=%d", m.loading, m.pendingRemaining)
	}
	u, _ := m.Update(RepoLoadedMsg{Repository: m.repositories[0], Records: []git.CommitRecord{
		{Author: "A", Email: "a@x.com", Date: now, Added: 5, RepoName: "repoA"},
	}})
	m = u.(Model)
	if !m.loading {
		t.Error("should still be loading after 1 of 2")
	}
	u, _ = m.Update(RepoLoadedMsg{Repository: m.repositories[1], Err: errors.New("boom")})
	m = u.(Model)
	if m.loading {
		t.Error("should finalize after 2 of 2")
	}
	if len(m.failedRepos) != 1 || m.failedRepos[0] != "repoB" {
		t.Errorf("expected repoB failed, got %v", m.failedRepos)
	}
	if len(m.authors) != 1 || m.authors[0].Name != "A" {
		t.Errorf("expected 1 author A, got %v", m.authors)
	}
}

func TestInitReturnsLoadCmd(t *testing.T) {
	m := NewModel([]string{"/x/repoA"}, stats.SortByTotal, map[string]bool{}, "v", DefaultTimeIndex)
	if m.Init() == nil {
		t.Error("Init should return a load command")
	}
}

func TestRepositoryLoadingIsBounded(t *testing.T) {
	paths := make([]string, 20)
	for i := range paths {
		paths[i] = fmt.Sprintf("/workspace/repo-%02d", i)
	}
	m := NewModel(paths, stats.SortByTotal, nil, "v", DefaultTimeIndex)
	if m.activeScans != maxConcurrentRepoScans || m.nextRepo != maxConcurrentRepoScans {
		t.Fatalf("initial active/next = %d/%d, want %d/%d", m.activeScans, m.nextRepo, maxConcurrentRepoScans, maxConcurrentRepoScans)
	}
	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", msg)
	}
	if len(batch) != maxConcurrentRepoScans {
		t.Fatalf("initial batch has %d scans, want %d", len(batch), maxConcurrentRepoScans)
	}

	updated, next := m.Update(RepoLoadedMsg{Repository: m.repositories[0]})
	m = updated.(Model)
	if next == nil {
		t.Fatal("completed scan should schedule the next repository")
	}
	if m.activeScans != maxConcurrentRepoScans || m.nextRepo != maxConcurrentRepoScans+1 {
		t.Fatalf("replacement active/next = %d/%d", m.activeScans, m.nextRepo)
	}
}

func TestNewModelClampsTimeIndexAndHandlesNoRepos(t *testing.T) {
	m := NewModel([]string{"/workspace/repo"}, stats.SortByTotal, nil, "v", -10)
	if m.timeIdx != DefaultTimeIndex {
		t.Fatalf("timeIdx = %d, want %d", m.timeIdx, DefaultTimeIndex)
	}

	empty := NewModel(nil, stats.SortByTotal, nil, "v", DefaultTimeIndex)
	if empty.loading || empty.Init() != nil {
		t.Fatalf("empty model should be idle: loading=%v init=%v", empty.loading, empty.Init())
	}
}

func TestQuitCancelsRepositoryScans(t *testing.T) {
	m := NewModel([]string{"/workspace/repo"}, stats.SortByTotal, nil, "v", DefaultTimeIndex)
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return a quit command")
	}
	m = updated.(Model)
	select {
	case <-m.scanContext.Done():
	default:
		t.Fatal("ctrl+c did not cancel repository scans")
	}
}

func TestTableViewportAccountsForActualStats(t *testing.T) {
	m := Model{
		authors: []stats.AuthorStats{{
			Commits:   1_000_000_000,
			Added:     1_000_000_000,
			Removed:   1_000_000_000,
			AICommits: 1,
		}},
		loadedRepos: []git.Repository{{ID: "repo", Name: "repo"}},
		timeIdx:     DefaultTimeIndex,
		width:       78,
		height:      40,
	}
	commits, added, removed, ai := m.aggregateTotals()
	above := []string{RenderHeader(m.width, len(m.loadedRepos), 0, m.version), "", RenderTimePicker(m.timeIdx), "", RenderStatBoxes(commits, added, removed, ai, m.width), ""}
	expected := m.height - lipgloss.Height(strings.Join(above, "\n")) - lipgloss.Height(RenderHelpBar(HelpContext{View: "aggregate"})) - tableChromeLines
	if expected < 3 {
		expected = 3
	}
	if got := m.tableViewport(); got != expected {
		t.Fatalf("tableViewport = %d, want %d based on rendered stats", got, expected)
	}
}

// TestRepoBreakdownStableOrder guards that equal-change repos keep a stable
// order across renders (no arbitrary reshuffling in the detail view).
func TestRepoBreakdownStableOrder(t *testing.T) {
	as := &stats.AuthorStats{
		Name: "Dev",
		PerRepo: map[string]*stats.RepoContribution{
			"zeta":  {TotalChange: 100},
			"alpha": {TotalChange: 100}, // tie with zeta
			"mid":   {TotalChange: 100}, // tie
		},
	}
	v := OperativeView{}
	first := v.renderRepoBreakdown(as, 100)
	for i := 0; i < 20; i++ {
		if v.renderRepoBreakdown(as, 100) != first {
			t.Fatal("repo breakdown order is not stable across renders")
		}
	}
	// Ties resolve by name, so alpha precedes mid precedes zeta.
	ai := strings.Index(first, "alpha")
	mi := strings.Index(first, "mid")
	zi := strings.Index(first, "zeta")
	if ai >= mi || mi >= zi {
		t.Errorf("expected name-sorted tie order alpha<mid<zeta, got %d/%d/%d", ai, mi, zi)
	}
}
