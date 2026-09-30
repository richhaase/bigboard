package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func glanceBusyFixture() Model {
	m := monorepoFixture()
	m.allRecords = nil
	now := time.Now()
	for i := 0; i < 1200; i++ {
		area := fmt.Sprintf("area%02d", i%40)
		if i < 1100 {
			area = fmt.Sprintf("feature/child%02d", i%41)
		}
		m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprintf("oid-%04d", i), Author: fmt.Sprintf("Person %03d", i%120), Email: fmt.Sprintf("person%03d@example.com", i%120), Date: now.Add(-time.Duration(i) * time.Minute), RepoID: "/mono", RepoName: "mono", Subject: fmt.Sprintf("Exact subject %04d", i), Changes: []git.PathChange{{Path: area + "/file.go"}}})
	}
	m.rebuildAreaDefinitions()
	m.recomputeAuthors()
	m.selectedAreaID = "auto:feature"
	return m
}

func TestGlanceOverviewDensityAndNumericContext(t *testing.T) {
	row := regexp.MustCompile(`^\s*[▸ ]\s+(feature|area\d\d)\s+[\d,]+\s+\d+\s+\w+`)
	for _, test := range []struct{ w, h, min int }{{110, 40, 12}, {70, 24, 8}} {
		m := glanceBusyFixture()
		m.width = test.w
		m.height = test.h
		view := ansi.Strip(m.View())
		count := 0
		for _, line := range strings.Split(view, "\n") {
			if row.MatchString(line) {
				count++
			}
		}
		if count < test.min {
			t.Fatalf("%dx%d has %d useful rows, want %d:\n%s", test.w, test.h, count, test.min, view)
		}
		if !strings.Contains(view, "1,100") || !strings.Contains(view, "120") || !strings.Contains(view, "Latest: Exact subject") {
			t.Fatal("busy area counts or selected preview missing")
		}
		if strings.Contains(view, "Person 000") {
			t.Fatal("overview leaked unbounded contributor inventory")
		}
		if test.w == 110 && !strings.Contains(view, bannerLines[0]) {
			t.Fatal("full gradient banner disappeared")
		}
		if lipgloss.Height(view) > test.h {
			t.Fatal("overview height overflow")
		}
	}
}
func TestGlanceSortAndBackPreserveIdentityAndSearch(t *testing.T) {
	m := glanceBusyFixture()
	m = pressAwareness(m, "s")
	m = pressAwareness(m, "s")
	if m.selectedAreaID != "auto:feature" || m.overviewSort != 2 {
		t.Fatal("sort changed identity")
	}
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "feature")
	m = pressAwareness(m, "enter")
	if len(m.glanceRepositories()) != 1 {
		t.Fatal("area search failed")
	}
	m = pressAwareness(m, "enter")
	if !m.glance.detailOpen {
		t.Fatal("detail not open")
	}
	m = pressAwareness(m, "esc")
	if m.glance.detailOpen || m.selectedAreaID != "auto:feature" || m.glanceQuery() != "feature" {
		t.Fatal("back lost overview identity or search")
	}
	m = pressAwareness(m, "esc")
	if m.glanceQuery() != "" || m.areaRepoID == "" {
		t.Fatal("first escape should only clear search")
	}
}
func TestGlanceAllPeopleAndCommitEvidenceRemainReachable(t *testing.T) {
	m := glanceBusyFixture()
	m = pressAwareness(m, "enter")
	if len(m.glanceEvidence()) != 1100 {
		t.Fatal("commits lost")
	}
	m = pressAwareness(m, "G")
	if m.evidenceOffset != 1099 || !strings.Contains(m.View(), "Exact subject 1099") {
		t.Fatal("last commit unreachable")
	}
	m = pressAwareness(m, "2")
	if len(m.glancePeopleList()) != 120 {
		t.Fatal("identities lost")
	}
	m = pressAwareness(m, "G")
	if !strings.Contains(m.View(), "Person 119") {
		t.Fatal("last person unreachable")
	}
	m = pressAwareness(m, "enter")
	if m.personID != "email:person119@example.com" || len(m.glanceEvidence()) == 0 {
		t.Fatal("people filter failed")
	}
	for _, r := range m.glanceEvidence() {
		if r.Email != "person119@example.com" {
			t.Fatal("other person's evidence leaked")
		}
	}
	m = pressAwareness(m, "esc")
	if m.personID != "" || m.glance.frame.tab != glancePeople {
		t.Fatal("filter escape lost people focus")
	}
}
func TestGlanceRelatedAndSubareaBackNavigation(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "3")
	related := m.glanceRelatedAreas()
	if len(related) != 2 || related[0].shared != 1 {
		t.Fatal("shared identity counts wrong")
	}
	m = pressAwareness(m, "enter")
	if m.glance.frame.relatedFromID != "auto:services/auth" || m.glance.frame.tab != glancePeople {
		t.Fatal("related shared-people destination missing")
	}
	m = pressAwareness(m, "esc")
	if m.selectedAreaID != "auto:services/auth" || m.glance.frame.tab != glanceRelated {
		t.Fatal("related back lost source")
	}
	m = glanceBusyFixture()
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "4")
	if len(m.glanceSubareas()) != 41 {
		t.Fatal("child path groups missing")
	}
	m = pressAwareness(m, "enter")
	if !strings.HasPrefix(m.glance.frame.path, "feature/child") || len(m.glanceEvidence()) == 0 {
		t.Fatal("subarea navigation failed")
	}
	m = pressAwareness(m, "4")
	m = pressAwareness(m, "enter")
	if !strings.HasPrefix(m.glance.frame.leafID, "direct:") {
		t.Fatal("direct files leaf missing")
	}
	m = pressAwareness(m, "4")
	if len(m.glanceSubareas()) != 0 {
		t.Fatal("direct file leaf loops forever")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "esc")
	if m.glance.frame.path != "" || m.glance.frame.tab != glanceSubareas {
		t.Fatal("subarea back lost root focus")
	}
}
func TestGlanceDetailAndInspectorKeepStaleWarning(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	m.loading = true
	m.resetPending()
	next, _ := m.Update(RepoLoadedMsg{Repository: m.repositories[0], Err: errors.New("unreadable")})
	m = next.(Model)
	if !strings.Contains(m.View(), "STALE") {
		t.Fatal("detail disguises stale scan")
	}
	m = pressAwareness(m, "enter")
	if !strings.Contains(m.View(), "STALE") {
		t.Fatal("inspector disguises stale scan")
	}
}
func TestGlanceFullInspectorWrapsSubjectsAndAllPaths(t *testing.T) {
	m := monorepoFixture()
	m.width = 40
	m.height = 18
	subject := strings.Repeat("長い件名 ", 25) + "FINAL SUBJECT TOKEN"
	m.allRecords[0].Subject = subject
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	var seen strings.Builder
	for i := 0; i < 120; i++ {
		for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			seen.WriteString(strings.TrimPrefix(line, "  "))
		}
		m = pressAwareness(m, "down")
	}
	for _, want := range []string{"FINAL SUBJECT TOKEN", "apps/web/login.tsx", "services/auth/session.go", "Commit: auth", "ada@x"} {
		if !strings.Contains(seen.String(), want) {
			t.Fatalf("full evidence %q not reachable", want)
		}
	}
	if lipgloss.Height(m.View()) > 18 {
		t.Fatal("inspector exceeds height")
	}
}
func TestGlanceSearchUnicodeAndFilterEmptyScope(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "session.go")
	m = pressAwareness(m, "enter")
	if len(m.glanceEvidence()) != 1 {
		t.Fatal("path evidence search failed")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "2")
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "ada@x")
	m = pressAwareness(m, "enter")
	if len(m.glancePeopleList()) != 1 {
		t.Fatal("identity search failed")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "1")
	m.allRecords = nil
	m.recomputeAuthors()
	if !m.glance.detailOpen || !strings.Contains(m.View(), "No matching commits") {
		t.Fatal("empty detail silently switched area")
	}
}
func TestGlanceDetailResizeAndSelectionRetainedAfterWindowChange(t *testing.T) {
	m := reviewBusyFixture()
	for i := 0; i < 35; i++ {
		m = pressAwareness(m, "down")
	}
	id := m.glance.frame.ids[glanceActivity]
	for _, size := range [][2]int{{40, 18}, {70, 24}, {110, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		if m.glance.frame.ids[glanceActivity] != id || !strings.Contains(m.View(), "Unique review subject 035") {
			t.Fatal("resize loses selected commit")
		}
	}
}

func TestGlanceNarrowActiveFiltersAndDuplicateNamesStayVisible(t *testing.T) {
	m := monorepoFixture()
	m.width = 40
	m.height = 18
	m.allRecords[0].Author = strings.Repeat("Long identical display name ", 3)
	m.allRecords[1].Author = m.allRecords[0].Author
	m.allRecords[1].Changes = []git.PathChange{{Path: "services/auth/second.go"}}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.hideBots = true
	m.selectedAreaID = "auto:services/auth"
	if !strings.Contains(m.View(), "bots hidden") {
		t.Fatal("overview hides active bot filter")
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "2")
	for _, want := range []string{"ada@x", "grace@x", "bots hidden"} {
		if !strings.Contains(m.View(), want) {
			t.Fatalf("narrow People hides %q:\n%s", want, m.View())
		}
	}
	m = pressAwareness(m, "down")
	m = pressAwareness(m, "enter")
	if !strings.Contains(m.View(), "Activity: ada@x") || !strings.Contains(m.View(), "Esc clears") || !strings.Contains(m.View(), "bots hidden") {
		t.Fatalf("active filter invisible:\n%s", m.View())
	}
	if lipgloss.Height(m.View()) > m.height {
		t.Fatal("filtered detail exceeds height")
	}
}
