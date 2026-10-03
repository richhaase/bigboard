package tui

import (
	"errors"
	"github.com/muesli/termenv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

// Check the visible selected-scope contributor panel, excluding commit author
// names and related scopes, which may repeat the same people elsewhere.
func worklistPeopleInOrder(view string, names ...string) bool {
	view = ansi.Strip(view)
	_, text, ok := strings.Cut(view, "SELECTED  ")
	if !ok {
		return false
	}
	_, text, _ = strings.Cut(text, "\n")
	text, _, _ = strings.Cut(text, "RECENT COMMITS")
	for _, name := range names {
		i := strings.Index(text, name)
		if i < 0 {
			return false
		}
		text = text[i+len(name):]
	}
	return true
}

func TestWorklistBoundsAndFocus(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {110, 40}, {160, 48}, {80, 30}} {
		m := glanceBusyFixture()
		m.width, m.height = size[0], size[1]
		check := func(stage string) {
			t.Helper()
			view := m.View()
			if lipgloss.Height(view) > m.height {
				t.Fatalf("%v %s exceeds height", size, stage)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%v %s exceeds width: %s", size, stage, ansi.Strip(line))
				}
			}
			if !strings.Contains(view, "author dates") {
				t.Fatalf("%v %s lost evidence footer", size, stage)
			}
		}
		check("overview")
		original := m.selectedAreaID
		m = pressAwareness(m, "enter")
		check("activity")
		for _, key := range []string{"2", "end", "3", "end", "4", "end", "1", "end"} {
			m = pressAwareness(m, key)
			check(key)
		}
		m = pressAwareness(m, "esc")
		if m.selectedAreaID != original {
			t.Fatal("detail navigation changed selected scope")
		}
	}
}

func TestWorklistUnsetSizePreservesStartupMessages(t *testing.T) {
	m := awarenessFixture()
	m.width, m.height = 0, 0
	m.err = errors.New("scan access denied")
	if !strings.Contains(m.View(), "scan access denied") {
		t.Fatal("unsized error was clipped")
	}
	m.err = nil
	m.loading = true
	if !strings.Contains(m.View(), "SCANNING REPOSITORIES") {
		t.Fatalf("unsized loading was clipped: %s", m.View())
	}
}

func TestWorklistSmallSnapshotAndPRTitle(t *testing.T) {
	m := prFixture()
	m.width, m.height = 120, 36
	m.areaRepoID, m.selectedAreaID = "/api", "auto:feature"
	at := time.Date(2026, 9, 30, 15, 12, 0, 0, time.Local)
	m.scannedAt["/api"] = at
	m.failedRepos = []string{"/web"}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Update errors: 1", at.Format("Jan 02 15:04 MST"), "#7 Add feature", "all dates · local filters do not apply"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q at120x36:\n%s", want, view)
		}
	}
}

func TestWorklistRelatedAndParentPRProvenance(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 160, 48
	m.selectedAreaID = "auto:services/auth"
	for _, key := range []string{"enter", "3", "enter", "1"} {
		m = pressAwareness(m, key)
	}
	if !strings.Contains(m.View(), "Commits touching both areas") {
		t.Fatal("related activity lost provenance")
	}
	m = glanceBusyFixture()
	m.width, m.height = 160, 48
	m.prState = prState{repositories: map[string]string{"/mono": "org/mono"}, snapshots: map[string]prSnapshot{
		"org/mono": {lastGood: time.Now(), prs: []gh.PullRequest{
			{Repo: "org/mono", Number: 7, Title: "Parent area change", Files: []gh.File{{Path: "feature/another-child/file.go"}}, FilesComplete: true, ContextComplete: true},
			{Repo: "org/mono", Number: 8, Title: "Unrelated area change", Files: []gh.File{{Path: "unrelated/file.go"}}, FilesComplete: true, ContextComplete: true},
		}},
	}}
	for _, key := range []string{"enter", "4", "enter"} {
		m = pressAwareness(m, key)
	}
	view := ansi.Strip(m.View())
	if m.glance.frame.path == "" || !strings.Contains(view, "PARENT AREA PRs") || !strings.Contains(view, "#7 Parent area change") || strings.Contains(view, "#8 Unrelated area change") {
		t.Fatalf("child lost parent-area scope:\n%s", view)
	}
	m = pressAwareness(m, "p")
	inventory := m.visiblePRs()
	if len(inventory) != 1 || inventory[0].Number != 7 {
		t.Fatalf("PR overlay changed parent scope: %+v", inventory)
	}

}

func TestWorklistMissingScopeHasNoUnrelatedSidebarFocus(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := monorepoFixture()
	m.width, m.height = 160, 48
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	kept := m.allRecords[:0]
	for _, r := range m.allRecords {
		if r.CommitID != "auth" {
			kept = append(kept, r)
		}
	}
	m.allRecords = kept
	m.rebuildAreaDefinitions()
	m.recomputeAuthors()
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "›") {
			t.Fatalf("missing scope must not select unrelated evidence: %s", line)
		}
	}

	if m.glance.frame.areaID != "auto:services/auth" {
		t.Fatal("missing scope silently switched")
	}
}

func TestWorklistCanvasRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.Ascii)
	m := monorepoFixture()
	m.width, m.height = 160, 48
	if strings.Contains(m.View(), "\x1b[") {
		t.Fatal("ASCII renderer received forced ANSI canvas")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	if !strings.Contains(m.View(), "\x1b[") {
		t.Fatal("color renderer lost styling")
	}
}

func TestWorklistIdentityAndPlainRefreshStatus(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 160, 48
	view := ansi.Strip(m.View())
	for _, want := range []string{"BIGBOARD / Worklist", "Local:", "R refresh", "LAST AUTHOR", "LATEST COMMIT"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(view, "NIGHT OPS") || strings.Contains(view, "Updates every minute") {
		t.Fatal("obsolete theme or timer hint")
	}
	m.refreshing = true
	if !strings.Contains(m.View(), "Updating…") {
		t.Fatal("refresh status hidden")
	}
	m.width = 80
	if !strings.Contains(m.View(), "BIGBOARD / Worklist") || !strings.Contains(m.View(), "Updating…") {
		t.Fatal("compact status differs")
	}
}

func TestWorklistHelpExplainsManualRefresh(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 160, 48
	m = pressAwareness(m, "?")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "WORKLIST · HELP") || !strings.Contains(view, "Local data updates on launch or R; no auto-refresh") || strings.Contains(view, "every minute") {
		t.Fatalf("help does not explain manual refresh: %s", view)
	}
}
