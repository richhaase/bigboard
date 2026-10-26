package tui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func activityContributorText(m Model, row repositoryActivity, width, height int) string {
	return ansi.Strip(strings.Join(m.activityContributors(row, width, height), "\n"))
}

func TestActivityContributorSummaryUsesSelectedScopeLines(t *testing.T) {
	m := directoryFixture()
	root := m.glanceRepositories()[0]
	text := activityContributorText(m, root, 140, 9)
	for _, want := range []string{"CONTRIBUTORS · mono", "Ada", "+270", "−40", "Grace", "+300", "−50"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repository breakdown lost %q:\n%s", want, text)
		}
	}
	m.openDirectoryRepository("/mono")
	row := directoryRow(t, m, "dir:services")
	text = activityContributorText(m, row, 140, 9)
	for _, want := range []string{"CONTRIBUTORS · mono / services/", "Ada", "+60", "−40", "Grace", "+300", "−50"} {
		if !strings.Contains(text, want) {
			t.Fatalf("directory breakdown lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "+260") || strings.Contains(text, "+270") || strings.Index(text, "Grace") > strings.Index(text, "Ada") {
		t.Fatal("directory contributors used whole-commit lines or were not ranked by added plus removed")
	}
	m.directory.frame.path = "services/auth"
	row = directoryRow(t, m, "file:services/auth/session.go")
	text = activityContributorText(m, row, 140, 9)
	if !strings.Contains(text, "CONTRIBUTORS · mono / services/auth/session.go") || !strings.Contains(text, "+60") || !strings.Contains(text, "−40") || strings.Contains(text, "Grace") {
		t.Fatalf("file breakdown inherited its parent directory's contributors or line totals:\n%s", text)
	}
}

func TestActivityContributorSummaryFollowsActivityFilters(t *testing.T) {
	m := directoryFixture()
	m.timeIdx = 0
	m.allRecords[1].Date = time.Now().Add(-48 * time.Hour)
	m.allRecords[0].Author = "automation[bot]"
	m.allRecords[0].Email = "automation[bot]@example.test"
	m.activityPeople = filterValues{stats.IdentityID(m.allRecords[0])}
	m.hideBots = true
	m.recomputeAuthors()
	m.openDirectoryRepository("/mono")
	row := directoryRow(t, m, "dir:services")
	text := activityContributorText(m, row, 120, 9)
	if !strings.Contains(text, "[BOT]") || !strings.Contains(text, "+60") || strings.Contains(text, "Grace") || strings.Contains(text, "+300") {
		t.Fatalf("summary disagreed with shared dates, Activity people, or independent bot visibility:\n%s", text)
	}
	m.activityHideBots = true
	if len(m.directoryRows()) != 0 {
		t.Fatal("Activity bot filtering retained contributor evidence")
	}
}

func activityContributorSummaryFixture() (Model, repositoryActivity) {
	m := awarenessFixture()
	row := repositoryActivity{repo: git.Repository{ID: "/repo", Name: "repo"}, people: []stats.AuthorStats{
		{ID: "email:alex-two@example.test", Name: "Alex", Commits: 10, Added: 1, TotalChange: 1},
		{ID: "email:unknown@example.test", Name: "Unknown lines", Commits: 2, Added: 5000, TotalChange: 5000, UnknownLineCommits: 1},
		{ID: "email:alex-one@example.test", Name: "Alex", Commits: 3, Added: 900, Removed: 100, TotalChange: 1000},
		{ID: "email:bot@example.test", Name: "automation", Bot: true, Commits: 4, Added: 800, TotalChange: 800},
		{ID: "email:four@example.test", Name: "四四四四四四四四四四四四", Commits: 5, Added: 700, TotalChange: 700},
		{ID: "email:five@example.test", Name: "Fifth", Commits: 6, Added: 600, TotalChange: 600},
		{ID: "email:six@example.test", Name: "Sixth", Commits: 7, Added: 500, TotalChange: 500},
	}}
	return m, row
}

func TestActivityContributorSummaryPreservesIdentityUnknownsAndInput(t *testing.T) {
	m, row := activityContributorSummaryFixture()
	before := slices.Clone(row.people)
	text := activityContributorText(m, row, 180, 9)
	for _, want := range []string{"alex-one@example.test", "[BOT] automation", "+5,000?", "−?", "1–5 of 7", "Tab contributors"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "alex-two@example.test") || strings.Contains(text, "Sixth") {
		t.Fatal("summary did not retain the top five contributors by line change")
	}
	if !reflect.DeepEqual(row.people, before) {
		t.Fatal("rendering changed the source contributor order")
	}
	row.people = []stats.AuthorStats{{ID: "email:binary@example.test", Name: "Binary contributor", Commits: 1}}
	text = activityContributorText(m, row, 120, 9)
	if !strings.Contains(text, "+0") || !strings.Contains(text, "−0") || strings.Contains(text, "?") {
		t.Fatal("known zero line counts were confused with unknown measurements")
	}
}

func TestActivityContributorSummaryFitsAllocatedSpace(t *testing.T) {
	m, row := activityContributorSummaryFixture()
	row.repo.Name = "repository-with-a-long-name-and-an-untrusted-escape-\x1b[2J"
	for _, width := range []int{40, 69, 70, 100, 180} {
		for _, height := range []int{4, 5, 7, 9} {
			lines := m.activityContributors(row, width, height)
			focused := m
			focused.contributorPane = activityContributorPane{focused: true, scope: m.activityContributorKey(row)}
			_, page := m.activityContributorFrame(row, width, height)
			_, focusedPage := focused.activityContributorFrame(row, width, height)
			if focusedPage != page || len(focused.activityContributors(row, width, height)) != len(lines) {
				t.Fatalf("changing focus changed contributor viewport at %dx%d", width, height)
			}
			text := ansi.Strip(strings.Join(lines, "\n"))
			if len(lines) > height || !strings.Contains(text, "Unknown") || !strings.Contains(text, "+5,000?") || !strings.Contains(text, "−?") || !strings.Contains(text, "of 7") {
				t.Fatalf("summary lost evidence or exceeded %dx%d:\n%s", width, height, text)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width || strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\n") {
					t.Fatalf("summary escaped its terminal row at %dx%d: %q", width, height, line)
				}
			}
		}
	}
	if len(m.activityContributors(row, 80, 3)) != 0 {
		t.Fatal("summary occupied space without room for contributor evidence")
	}
	row.people = nil
	if text := activityContributorText(m, row, 120, 9); !strings.Contains(text, "No contributors in this scope and range.") || strings.Contains(text, "more contributors") {
		t.Fatal("quiet scope did not retain an explicit empty state")
	}
}

func activityContributorScrollFixture() Model {
	m := awarenessFixture()
	m.width, m.height = 120, 36
	m.allRecords = nil
	for i := 0; i < 31; i++ {
		m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprintf("person-%02d", i), Author: fmt.Sprintf("Person %02d", i), Email: fmt.Sprintf("person%02d@example.test", i), Date: time.Now(), RepoID: "/api", RepoName: "api", Added: 1000 - i, Changes: []git.PathChange{{Path: "src/file.go", Added: 1000 - i, LinesKnown: true}}})
	}
	m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: "web", Author: "Web person", Email: "web@example.test", Date: time.Now(), RepoID: "/web", RepoName: "web", Added: 1})
	m.recomputeAuthors()
	return m
}

func TestActivityContributorPaneScrollsEveryPersonWithoutChangingRanking(t *testing.T) {
	m := activityContributorScrollFixture()
	m = keyboardContractPress(t, m, "tab")
	if !m.contributorPane.focused || !strings.Contains(ansi.Strip(m.View()), "┃ CONTRIBUTORS ·") || !strings.Contains(ansi.Strip(m.View()), "Tab ranking") {
		t.Fatal("Tab did not visibly focus contributors")
	}
	row, _ := m.activityContributorSelection()
	_, page := m.activityContributorFrame(row, m.width, activityContributorsHeight(m.width, m.height))
	m = keyboardContractPress(t, m, "pgdown")
	if m.contributorPane.offset != page || m.selectedRepoID != "/api" {
		t.Fatal("Page Down disagreed with the visible contributor count or moved repository selection")
	}
	seen := map[string]bool{}
	m = keyboardContractPress(t, m, "home")
	for i := 0; i < 31; i++ {
		text := ansi.Strip(m.View())
		for person := 0; person < 31; person++ {
			name := fmt.Sprintf("Person %02d", person)
			seen[name] = seen[name] || strings.Contains(text, name)
		}
		m = keyboardContractPress(t, m, "down")
	}
	for name, visible := range seen {
		if !visible {
			t.Fatalf("contributor %s was unreachable", name)
		}
	}
	m = keyboardContractPress(t, m, "end", "enter", "right")
	if m.contributorPane.offset != 31-page || m.directory.active || !strings.Contains(ansi.Strip(m.View()), "31 of 31") {
		t.Fatal("pane end or Enter changed scope instead of retaining the viewport")
	}
	m = keyboardContractPress(t, m, "pgup", "up")
	if m.contributorPane.offset != max(0, 31-2*page-1) {
		t.Fatal("contributor backward scrolling disagreed with page size")
	}
	kept := m.contributorPane
	m = keyboardContractPress(t, m, "c", "p", "a")
	if m.contributorPane != kept || m.selectedRepoID != "/api" {
		t.Fatal("board switching discarded Activity pane position")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.contributorPane.focused || m.areaRepoID != "" {
		t.Fatal("Escape did not return focus to the repository ranking")
	}
	m = keyboardContractPress(t, m, "down")
	if m.selectedRepoID != "/web" || m.contributorPane.offset != 0 {
		t.Fatal("changing repository retained the previous scope's contributor offset")
	}
}

func TestActivityContributorPaneDirectoryFocusAndBack(t *testing.T) {
	m := activityContributorScrollFixture()
	m = keyboardContractPress(t, m, "enter", "tab", "end")
	if !m.directory.active || !m.contributorPane.focused || m.glance.detailOpen || !strings.Contains(ansi.Strip(m.View()), "api / src/") {
		t.Fatal("directory Tab did not focus contributors for its selected child")
	}
	m = keyboardContractPress(t, m, "left")
	if !m.directory.active || m.contributorPane.focused || m.directory.frame.path != "" {
		t.Fatal("left navigated out of the directory before returning focus")
	}
	m = keyboardContractPress(t, m, "enter")
	if m.directory.frame.path != "src" || m.contributorPane.offset != 0 {
		t.Fatal("new directory scope inherited a contributor offset")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if !m.contributorPane.focused || !strings.Contains(ansi.Strip(m.View()), "api / src/file.go") {
		t.Fatal("ShiftTab did not focus the selected file's contributors")
	}
	m = keyboardContractPress(t, m, "tab", "2")
	if m.contributorPane.focused || !m.glance.detailOpen || m.glance.frame.tab != glancePeople {
		t.Fatal("numeric People detail path was lost")
	}
}

func TestActivityContributorPaneClampsAfterResizeFilterAndRefresh(t *testing.T) {
	m := activityContributorScrollFixture()
	m = keyboardContractPress(t, m, "tab", "end")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	m = next.(Model)
	row, _ := m.activityContributorSelection()
	_, page := m.activityContributorFrame(row, m.width, activityContributorsHeight(m.width, m.height))
	m = keyboardContractPress(t, m, "home", "pgdown")
	if m.contributorPane.offset != page {
		t.Fatal("resizing left contributor keyboard paging on the old layout")
	}
	m = keyboardContractPress(t, m, "end")
	m.activityPeople = filterValues{"email:person00@example.test"}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = next.(Model)
	if m.contributorPane.offset != 0 || !strings.Contains(ansi.Strip(m.View()), "1–1 of 1") {
		t.Fatal("filtering retained an empty contributor viewport")
	}
	m.activityPeople = nil
	m = keyboardContractPress(t, m, "end")
	m.resetPending()
	m.pendingRemaining = 1
	next, _ = m.Update(RepoLoadedMsg{Generation: m.scanGeneration, Repository: m.repositories[0], Records: m.allRecords[:2]})
	m = next.(Model)
	if m.contributorPane.offset != 0 || !strings.Contains(ansi.Strip(m.View()), "1–2 of 2") {
		t.Fatal("refresh left the contributor viewport beyond retained evidence")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 18})
	m = next.(Model)
	m = keyboardContractPress(t, m, "tab")
	if m.contributorPane.focused || strings.Contains(ansi.Strip(m.View()), "┃ CONTRIBUTORS ·") {
		t.Fatal("Tab focused a pane hidden by the terminal size")
	}
	m = keyboardContractPress(t, m, "enter", "2")
	if !m.glance.detailOpen || m.glance.frame.tab != glancePeople {
		t.Fatal("short terminal lost its contributor detail path")
	}
}
