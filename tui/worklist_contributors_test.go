package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestWorklistContributorPreviewFollowsSelection(t *testing.T) {
	m := monorepoFixture()
	m.width = 120
	m.selectedAreaID = "auto:services/auth"
	if !worklistPeopleInOrder(m.View(), "Ada") {
		t.Fatal("selected area does not reveal its contributors")
	}
	m = pressAwareness(m, "down")
	if !worklistPeopleInOrder(m.View(), "Grace") || m.glance.detailOpen {
		t.Fatal("moving selection did not update the inline contributor list")
	}
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "auth")
	m = pressAwareness(m, "enter")
	if !worklistPeopleInOrder(m.View(), "Ada") {
		t.Fatal("searched selection retained another area's contributors")
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "2")
	if len(m.glancePeopleList()) != 1 || m.glancePeopleList()[0].Name != "Ada" {
		t.Fatal("full People list is no longer reachable")
	}
	m = pressAwareness(m, "esc")
	if m.selectedAreaID != "auto:services/auth" || m.glanceQuery() != "auth" {
		t.Fatal("detail round trip lost the selected search match")
	}

	m = awarenessFixture()
	m.width = 120
	if !worklistPeopleInOrder(m.View(), "Ada", "Grace") {
		t.Fatal("repository preview did not reveal its contributors")
	}
	m = pressAwareness(m, "down")
	if !worklistPeopleInOrder(m.View(), "Ada") || worklistPeopleInOrder(m.View(), "Grace") {
		t.Fatal("repository selection retained another repository's contributor")
	}
}

func TestWorklistContributorPreviewOrdersByScopedCommitCount(t *testing.T) {
	m := monorepoFixture()
	m.width = 120
	m.allRecords = nil
	now := time.Now()
	add := func(name, email, repo, path string, count int, date time.Time) {
		for i := 0; i < count; i++ {
			m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprintf("%s-%s-%s-%d", email, repo, path, i), Author: name, Email: email, Date: date, RepoID: repo, RepoName: strings.TrimPrefix(repo, "/"), Subject: "Change", Changes: []git.PathChange{{Path: path}}})
		}
	}
	add("Zoe", "zoe@x", "/mono", "feature/file.go", 3, now)
	add("Ada", "ada@x", "/mono", "feature/file.go", 1, now)
	add("Ada", "ada@x", "/mono", "other/file.go", 10, now)
	add("Zoe", "zoe@x", "/copy", "elsewhere/file.go", 30, now)
	add("Ada", "ada@x", "/mono", "feature/old.go", 5, now.Add(-48*time.Hour))
	m.loadedRepos = append(m.loadedRepos, git.Repository{ID: "/copy", Name: "copy"})
	m.timeIdx = 0
	m.rebuildAreaDefinitions()
	m.recomputeAuthors()
	m.selectedAreaID = "auto:feature"
	if !worklistPeopleInOrder(m.View(), "Zoe", "Ada") {
		t.Fatal("area preview used alphabetical, global, or out-of-range commit totals")
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "2")
	if got := m.glancePeopleList(); len(got) != 2 || got[0].Name != "Ada" {
		t.Fatal("preview ordering changed the alphabetical People lens")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "esc")
	if !worklistPeopleInOrder(m.View(), "Ada", "Zoe") {
		t.Fatal("repository preview used global author totals instead of repository totals")
	}
}

func TestWorklistContributorPreviewBoundsIdentityAndTies(t *testing.T) {
	people := []stats.AuthorStats{
		{ID: "email:z@x", Name: "Same name", Commits: 1},
		{ID: "email:a@x", Name: "Same name", Commits: 1},
		{ID: "email:b@x", Name: "Bot", Commits: 2, Bot: true},
	}
	view := ansi.Strip(strings.Join(worklistPeople(people, 110, 2), "\n"))
	if !strings.Contains(view, "[BOT] Bot 2 · a@x · Same name 1 · z@x · Same name 1") {
		t.Fatalf("preview lost bot tags, duplicate identities, or deterministic ties:\n%s", view)
	}
	if people[0].ID != "email:z@x" {
		t.Fatal("rendering mutated the original contributor order")
	}
	people = nil
	for i := 0; i < 120; i++ {
		people = append(people, stats.AuthorStats{ID: fmt.Sprintf("email:%d@x", i), Name: fmt.Sprintf("%03d %s", i, strings.Repeat("長い名前", 15)), Commits: 120 - i})
	}
	for _, test := range []struct{ width, rows, remaining int }{{40, 1, 119}, {70, 1, 119}, {110, 2, 118}} {
		lines := worklistPeople(people, test.width, test.rows)
		if len(lines) != test.rows {
			t.Fatalf("want %d bounded preview rows, got %d", test.rows, len(lines))
		}
		view := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(view, "000") || !strings.Contains(view, fmt.Sprintf("+%d more", test.remaining)) {
			t.Fatalf("long list hides first contributor or remaining count:\n%s", view)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > test.width {
				t.Fatalf("preview exceeds %d columns: %s", test.width, line)
			}
		}
	}
}

func TestWorklistContributorPreviewFiltersAndEmptyPRScope(t *testing.T) {
	m := monorepoFixture()
	m.width = 120
	bot := m.allRecords[0]
	bot.CommitID, bot.Author, bot.Email = "bot", "robot[bot]", "robot[bot]@x"
	m.allRecords = append(m.allRecords, bot)
	m.allRecords[1].Changes = []git.PathChange{{Path: "services/auth/old.go"}}
	m.allRecords[1].Date = time.Now().Add(-48 * time.Hour)
	m.rebuildAreaDefinitions()
	m.recomputeAuthors()
	m.selectedAreaID = "auto:services/auth"
	if !strings.Contains(m.View(), "Grace") || !strings.Contains(m.View(), "[BOT] robot[bot]") {
		t.Fatal("unfiltered preview dropped contributors")
	}
	m = pressAwareness(m, "b")
	m.timeIdx = 0
	m.recomputeAuthors()
	if !worklistPeopleInOrder(m.View(), "Ada") || strings.Contains(m.View(), "Grace") || strings.Contains(m.View(), "robot[bot]") {
		t.Fatal("date or bot filter did not update the contributor preview")
	}
	m = prFixture()
	m.width = 120
	m.height = 36
	m.areaRepoID, m.selectedAreaID = "/api", "auto:feature"
	if !strings.Contains(m.View(), "No contributors in this range") || worklistPeopleInOrder(m.View(), "robot[bot]") {
		t.Fatalf("PR-only area fabricated a local contributor:\n%s", ansi.Strip(m.View()))
	}
	for _, size := range [][2]int{{40, 18}, {70, 24}, {110, 40}} {
		for _, repo := range []bool{false, true} {
			m := glanceBusyFixture()
			m.width, m.height = size[0], size[1]
			if repo {
				m.areaRepoID = ""
			}
			view := m.View()
			if lipgloss.Height(view) > m.height || !strings.Contains(view, "Person 000") || !strings.Contains(view, "author dates") {
				t.Fatalf("preview or footer hidden at %dx%d (repository=%t):\n%s", m.width, m.height, repo, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("preview overflows terminal width")
				}
			}
		}
	}
}
