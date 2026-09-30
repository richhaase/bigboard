package tui

import (
	"errors"
	"github.com/muesli/termenv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Check the visible selected-scope contributor panel, excluding commit author
// names and related scopes, which may repeat the same people elsewhere.
func nightPeopleInOrder(view string, names ...string) bool {
	view = ansi.Strip(view)
	lines := strings.Split(view, "\n")
	var panel []string
	active := false
	for _, line := range lines {
		if strings.Contains(line, "WHO WORKED HERE") {
			active = true
			continue
		}
		if active && strings.Contains(line, "RECENT COMMITS") {
			break
		}
		if active {
			if i := strings.Index(line, "│"); i >= 0 {
				line = line[i+len("│"):]
			}
			panel = append(panel, line)
		}
	}
	text := strings.Join(panel, "\n")
	for _, name := range names {
		i := strings.Index(text, name)
		if i < 0 {
			return false
		}
		text = text[i+len(name):]
	}
	return active
}

func TestNightOpsBoundsAndFocus(t *testing.T) {
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
		for _, key := range []string{"2", "G", "3", "G", "4", "G", "1", "G"} {
			m = pressAwareness(m, key)
			check(key)
		}
		m = pressAwareness(m, "esc")
		if m.selectedAreaID != original {
			t.Fatal("detail navigation changed selected scope")
		}
	}
}

func TestNightOpsUnsetSizePreservesStartupMessages(t *testing.T) {
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

func TestNightOpsSmallSnapshotAndPRTitle(t *testing.T) {
	m := prFixture()
	m.width, m.height = 100, 28
	m.areaRepoID, m.selectedAreaID = "/api", "auto:feature"
	at := time.Date(2026, 9, 30, 15, 12, 0, 0, time.Local)
	m.scannedAt["/api"] = at
	m.failedRepos = []string{"/web"}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Update errors: 1", at.Format("Jan 02 15:04 MST"), "#7 Add feature", "All dates · unfiltered"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q at100x28:\n%s", want, view)
		}
	}
}

func TestNightOpsRelatedAndParentPRProvenance(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 160, 48
	m.selectedAreaID = "auto:services/auth"
	for _, key := range []string{"enter", "3", "enter", "1"} {
		m = pressAwareness(m, key)
	}
	if !strings.Contains(m.View(), "Shared contributors · associations, not collaboration") {
		t.Fatal("related activity lost provenance")
	}
	m = glanceBusyFixture()
	m.width, m.height = 160, 48
	for _, key := range []string{"enter", "4", "enter"} {
		m = pressAwareness(m, key)
	}
	if m.glance.frame.path == "" || !strings.Contains(m.View(), "PARENT AREA PRs") {
		t.Fatal("subarea hides parent PR scope")
	}
}

func TestNightOpsMissingScopeHasNoUnrelatedSidebarFocus(t *testing.T) {
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
	band, _, _ := strings.Cut(nightBand.Render("X"), "X")
	for _, line := range strings.Split(m.renderNightOps(160, 48), "\n") {
		if i := strings.Index(line, "│"); i >= 0 {
			left := line[:i]
			plain := ansi.Strip(left)
			if strings.Contains(plain, "AREAS / COMMITS") {
				continue
			}
			if strings.Contains(plain, "›") || (band != "" && strings.Contains(left, band)) {
				t.Fatalf("unrelated sidebar row selected: %s", plain)
			}
		}
	}
	if m.glance.frame.areaID != "auto:services/auth" {
		t.Fatal("missing scope silently switched")
	}
}

func TestNightOpsCanvasRespectsNoColor(t *testing.T) {
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

func TestNightOpsWordmarkAndPlainRefreshStatus(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 160, 48
	view := ansi.Strip(m.View())
	for _, want := range []string{nightWordmark[0], nightWordmark[1], nightWordmark[2], "Last updated", "Updates every minute", "R refresh now"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, old := range []string{"SNAPSHOT", "No remote Git fetch", "Local scan"} {
		if strings.Contains(view, old) {
			t.Fatalf("technical status remains: %q", old)
		}
	}
	if !strings.HasSuffix(nightWordmark[0], "┏━╮") || !strings.HasSuffix(nightWordmark[1], "┃ ┃") || !strings.HasSuffix(nightWordmark[2], "┗━╯") {
		t.Fatal("D must retain its distinct left stem")
	}
	m.refreshing = true
	if !strings.Contains(m.View(), "Updating…") {
		t.Fatal("background update has no visible status")
	}
	m.width = 80
	if !strings.Contains(m.View(), "BIGBOARD / NIGHT OPS") || !strings.Contains(m.View(), "Last updated") || !strings.Contains(m.View(), "Updating…") {
		t.Fatal("compact header/status differs")
	}
}
