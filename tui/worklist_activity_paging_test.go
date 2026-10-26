package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestWorklistActivityPagingMatchesVisibleRecords(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}, {160, 48}} {
		for _, withContext := range []bool{false, true} {
			m := glanceBusyFixture()
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, "enter")
			if withContext {
				m.personID = "email:person000@example.com"
				m.glance.frame.relatedFromID = m.glance.frame.areaID
				m.prState = prState{repositories: map[string]string{"/mono": "sample/mono"}, snapshots: map[string]prSnapshot{
					"sample/mono": {prs: []gh.PullRequest{{Repo: "sample/mono", Number: 1, Title: "A scoped PR", Files: []gh.File{{Path: "feature/file.go"}}, FilesComplete: true}}},
				}}
			}
			view := ansi.Strip(m.View())
			visible := strings.Count(view, "Exact subject")
			if visible == 0 || visible != min(len(m.glanceEvidence()), m.worklistActivityPageSize()) {
				t.Fatalf("%v context=%t visible budget mismatch:\n%s", size, withContext, view)
			}
			if size[1] >= 36 && visible <= 3 {
				t.Fatal("tall Activity still stops at three commits")
			}
			m = keyboardContractPress(t, m, "pgdown")
			if m.glance.frame.rows[glanceActivity] != min(visible, len(m.glanceEvidence())-1) {
				t.Fatal("Page Down skipped beyond the visible records")
			}
			m = keyboardContractPress(t, m, "pgup")
			if m.glance.frame.rows[glanceActivity] != 0 {
				t.Fatal("Page Up did not restore the first selection")
			}
			m = keyboardContractPress(t, m, "end")
			records := m.glanceEvidence()
			view = ansi.Strip(m.View())
			action := "Enter commit details"
			if m.width-4 < 70 {
				action = "Enter details"
			}
			if !strings.Contains(view, "› "+records[len(records)-1].Subject) || !strings.Contains(view, action) {
				t.Fatalf("%v lost final selection or action:\n%s", size, view)
			}
			lines := strings.Split(view, "\n")
			if len(lines) != m.height {
				t.Fatal("Activity changed terminal height")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("Activity overflowed terminal width")
				}
			}
		}
	}
}

func TestWorklistOverviewPreviewRemainsBounded(t *testing.T) {
	m := glanceBusyFixture()
	m.width, m.height = 160, 48
	areaRows := m.glanceRepositories()
	area := areaRows[m.selectedRepository(areaRows)]
	preview := ansi.Strip(strings.Join(m.worklistEvidence(area, 156, 32, false), "\n"))
	if strings.Count(preview, "Exact subject") != 3 {
		t.Fatal("area preview lost its bounded evidence")
	}
	m.areaRepoID = ""
	rows := m.glanceRepositories()
	preview = ansi.Strip(strings.Join(m.worklistEvidence(rows[0], 156, 32, false), "\n"))
	if strings.Count(preview, "Exact subject") != 3 {
		t.Fatal("repository preview lost its bounded evidence")
	}
}
