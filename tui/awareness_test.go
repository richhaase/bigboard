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

func awarenessFixture() Model {
	now := time.Now()
	repos := []git.Repository{{ID: "/api", Name: "api"}, {ID: "/web", Name: "web"}, {ID: "/empty", Name: "empty"}}
	m := Model{viewMode: ViewAwareness, loadedRepos: repos, repositories: repos, timeIdx: len(TimePresets) - 1, width: 100, height: 30, excludedRepos: map[string]bool{}, selectedRepoID: "/api", scannedAt: map[string]time.Time{"/api": now}, staleRepos: map[string]bool{}}
	m.allRecords = []git.CommitRecord{
		{CommitID: "aaa11111", Author: "Ada", Email: "ada@x", Date: now, RepoID: "/api", RepoName: "api", Subject: "Fix login"},
		{CommitID: "bbb22222", Author: "Ada", Email: "ada@x", Date: now.Add(-time.Hour), RepoID: "/web", RepoName: "web", Subject: "Connect UI"},
		{CommitID: "ccc33333", Author: "Grace", Email: "grace@x", Date: now.Add(-2 * time.Hour), RepoID: "/api", RepoName: "api", Subject: "Add endpoint"},
	}
	m.recomputeAuthors()
	return m
}
func pressAwareness(m Model, key string) Model {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}
func TestAwarenessDefaultAndStatsRoundTrip(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByCommits, nil, "test", DefaultTimeIndex, Options{})
	defer m.cancelScans()
	if m.overviewSort != 1 {
		t.Fatal("Worklist must start with Recent sorting")
	}
	if m.viewMode != ViewAwareness {
		t.Fatal("relationships must be the initial screen")
	}
	m = pressAwareness(m, "v")
	if m.viewMode != ViewAggregate {
		t.Fatal("stats unavailable")
	}
	m = pressAwareness(m, "v")
	if m.viewMode != ViewAwareness {
		t.Fatal("cannot return to relationships")
	}
}
func TestAwarenessFocusAndEvidence(t *testing.T) {
	m := awarenessFixture()
	m = pressAwareness(m, "enter") // repository to areas
	m = pressAwareness(m, "enter") // area to dedicated detail
	m = pressAwareness(m, "tab")
	m = pressAwareness(m, "down")
	m = pressAwareness(m, "enter")
	if m.personID != "email:ada@x" {
		t.Fatalf("focus=%s", m.personID)
	}
	out := m.View()
	for _, text := range []string{"Fix login", "DIRECT COLLABORATION EVIDENCE", "Local:"} {
		if !strings.Contains(out, text) {
			t.Fatalf("missing %q:\n%s", text, out)
		}
	}
	if strings.Contains(out, "Add endpoint") {
		t.Fatal("unfocused author's evidence leaked")
	}
	m = pressAwareness(m, "esc")
	if m.personID != "" || m.glance.frame.tab != glancePeople {
		t.Fatal("escape must clear focus")
	}
	m = pressAwareness(m, "r")
	m = pressAwareness(m, "enter")
	if m.viewMode != ViewAwareness {
		t.Fatal("overlay lost origin view")
	}
}
func TestAwarenessSharedCommitAttribution(t *testing.T) {
	m := awarenessFixture()
	copy := m.allRecords[0]
	copy.RepoID = "/web"
	copy.RepoName = "web"
	copy.Author = "Other"
	copy.Email = "other@x"
	m.allRecords = append(m.allRecords, copy)
	m.recomputeAuthors()
	evidence := m.evidence("web", "email:ada@x")
	if len(evidence) != 2 {
		t.Fatalf("shared association lost: %#v", evidence)
	}
	if len(m.evidence("web", "email:other@x")) != 0 {
		t.Fatal("copy attribution differs from stats")
	}
	m.excludedRepos["/api"] = true
	m.recomputeAuthors()
	if len(m.evidence("web", "email:other@x")) != 1 {
		t.Fatal("excluded attribution source retained")
	}
}
func TestAwarenessFailureRetainsLastGood(t *testing.T) {
	m := awarenessFixture()
	original := m.scannedAt["/api"]
	m.loading = true
	m.resetPending()
	for _, repo := range m.repositories {
		next, _ := m.Update(RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo, Err: errors.New("unreadable")})
		m = next.(Model)
	}
	if len(m.allRecords) != 3 || m.err != nil {
		t.Fatal("failed refresh discarded last-good history")
	}
	if !m.scannedAt["/api"].Equal(original) || !m.staleRepos["/api"] {
		t.Fatal("failed scan changed freshness")
	}
	if !strings.Contains(m.View(), "STALE") {
		t.Fatal("stale data is not labeled")
	}
	m.loading = true
	m.resetPending()
	for _, repo := range m.repositories {
		next, _ := m.Update(RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
		m = next.(Model)
	}
	if len(m.allRecords) != 0 || len(m.failedRepos) != 0 || len(m.staleRepos) != 0 {
		t.Fatal("successful empty refresh must replace old data")
	}
}
func TestAwarenessBoundsAndSanitization(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {42, 18}, {20, 6}, {1, 1}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := awarenessFixture()
			m.width = size[0]
			m.height = size[1]
			m.allRecords[0].Subject = "Unsafe\x1b[2J\nsubject"
			out := m.View()
			if lipgloss.Height(out) > m.height {
				t.Fatalf("height overflow: %d", lipgloss.Height(out))
			}
			for _, line := range strings.Split(out, "\n") {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("width overflow: %q", line)
				}
			}
			if strings.Contains(out, "\x1b[2J") {
				t.Fatal("terminal control leaked")
			}
		})
	}
}
func TestAwarenessEvidenceScrollAndFiltering(t *testing.T) {
	m := awarenessFixture()
	for i := 0; i < 50; i++ {
		r := m.allRecords[0]
		r.CommitID = fmt.Sprintf("%08d", i)
		r.Subject = fmt.Sprintf("Evidence %d", i)
		m.allRecords = append(m.allRecords, r)
	}
	m.recomputeAuthors()
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	for i := 0; i < 100; i++ {
		m = pressAwareness(m, "down")
	}
	if m.glance.frame.rows[glanceActivity] != 51 {
		t.Fatalf("selected row=%d", m.glance.frame.rows[glanceActivity])
	}
	if !strings.Contains(m.View(), "Add endpoint") {
		t.Fatal("last evidence is unreachable")
	}
	m.excludedRepos["/api"] = true
	m.recomputeAuthors()
	if strings.Contains(m.View(), "Fix login") {
		t.Fatal("excluded repository leaked")
	}
}

func TestAwarenessFocusRemovedByFilters(t *testing.T) {
	m := awarenessFixture()
	m.personID = "email:grace@x"
	m.excludedRepos["/api"] = true
	m.recomputeAuthors()
	if m.personID != "" {
		t.Fatal("filtered focus survived")
	}
}
func TestAwarenessDuplicateNamesAndHostileFailure(t *testing.T) {
	m := awarenessFixture()
	m.allRecords[2].Author = "Ada"
	m.recomputeAuthors()
	m.failedRepos = []string{"bad\x1b[2J\x1b]0;hostile\x07"}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "tab")
	out := m.View()
	if !strings.Contains(out, "ada@x") || !strings.Contains(out, "grace@x") {
		t.Fatalf("duplicate names lack identity: %s", out)
	}
	if strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x1b]0;") {
		t.Fatal("failure name injected terminal sequences")
	}
}
func TestAwarenessCompactEvidenceStaysVisible(t *testing.T) {
	m := awarenessFixture()
	m.height = 18
	m.width = 40
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	m.allRecords[0].Author = "A very long contributor display name"
	m.failedRepos = []string{"unreadable"}
	m.allRecords[0].LinesUnknown = true
	m.recomputeAuthors()
	out := m.View()
	if !strings.Contains(out, "Fix login") {
		t.Fatalf("active evidence hidden: %s", out)
	}
}
func TestAwarenessHiddenBotsDoNotSetRecency(t *testing.T) {
	m := awarenessFixture()
	r := m.allRecords[0]
	r.Author = "dependabot[bot]"
	r.Email = "dependabot[bot]@users.noreply.github.com"
	r.RepoName = "empty"
	r.RepoID = "/empty"
	r.CommitID = "bot"
	r.Date = time.Now().Add(time.Hour)
	m.allRecords = append(m.allRecords, r)
	m.hideBots = true
	m.recomputeAuthors()
	for _, repository := range m.activityRepositories() {
		if repository.repo.ID == "/empty" && (!repository.latest.IsZero() || len(repository.people) != 0) {
			t.Fatal("hidden bot leaked into repository activity")
		}
	}
}

func TestAwarenessNarrowRangeAlwaysVisible(t *testing.T) {
	m := awarenessFixture()
	m.width = 40
	m.height = 18
	if !strings.Contains(m.View(), "Range: All time") {
		t.Fatal("active range hidden on narrow terminal")
	}
}

func TestAwarenessKeepsBigBoardVisualIdentity(t *testing.T) {
	m := awarenessFixture()
	m.width = 110
	m.height = 40
	out := m.View()
	for _, text := range []string{"BIGBOARD / Worklist", "REPOSITORIES", "LAST AUTHOR", "RECENT COMMITS", "Local:"} {
		if !strings.Contains(out, text) {
			t.Fatalf("missing original-style element %q", text)
		}
	}
	if strings.Contains(strings.ToLower(out), "room") {
		t.Fatal("repository terminology regressed")
	}
	m.width = 40
	m.height = 18
	if !strings.Contains(m.View(), "BIGBOARD / Worklist") {
		t.Fatalf("compact Big Board banner missing:\n%s", m.View())
	}
}
