package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRepositoryOverviewShowsScopedCountsAndRecency(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m.timeIdx = 0
	m.repositorySort = 2
	m.recomputeAuthors()
	rows := m.glanceRepositories()
	r := rows[0]
	if r.commits != 5 || len(r.people) != 5 {
		t.Fatal("fixture must contain five recent commits by distinct people")
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	heading, columns, firstRow := screenHeaderRows, screenHeaderRows+1, screenHeaderRows+2
	l := repositoryOverviewWidths(m.width - 4)
	start := 7 + l.name + 1
	for _, cell := range []struct {
		start, width int
		value        string
	}{{start, l.commits, "5"}, {start + l.commits + 1, l.people, "5"}} {
		if strings.TrimSpace(ansi.Cut(lines[firstRow], cell.start, cell.start+cell.width)) != cell.value {
			t.Fatalf("wrong repository metric at column %d: %s", cell.start, lines[firstRow])
		}
	}
	if !strings.Contains(lines[columns], "COMMITS") || !strings.Contains(lines[columns], "PEOPLE") || !strings.Contains(lines[heading], "quiet") {
		t.Fatal("overview lost comparison labels or quiet repository context")
	}
	m.prState.snapshots = nil
	withoutPRs := strings.Split(ansi.Strip(m.View()), "\n")
	if withoutPRs[columns] != lines[columns] || withoutPRs[firstRow] != lines[firstRow] {
		t.Fatal("PR availability shifted the activity comparison columns")
	}
}

func TestRepositoryOverviewPagesWithContributorBreakdown(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {110, 28}, {120, 36}, {180, 48}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		m.local.staleRepos["/sample/gateway"] = true
		m = keyboardContractPress(t, m, "pgdown", "end")
		view := ansi.Strip(m.View())
		rows := m.glanceRepositories()
		selected := rows[m.selectedRepository(rows)]
		panel := activityContributorsHeight(m.width, m.height)
		listEnd := m.height - screenFooterRows - panel
		frame := strings.Split(view, "\n")
		if !strings.Contains(strings.Join(frame[screenHeaderRows+2:listEnd], "\n"), selected.label) || len(frame) != m.height {
			t.Fatal("paging hid the selected repository or changed frame height")
		}
		if panel > 0 && !strings.Contains(strings.Join(frame[listEnd:], "\n"), "CONTRIBUTORS · "+selected.label) {
			t.Fatal("contributor breakdown did not follow the highlighted repository")
		}
		if strings.Contains(view, "SELECTED") || strings.Contains(view, "RECENT COMMITS") {
			t.Fatal("repository ranking retained the selected text preview")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("repository overview overflowed")
			}
		}
		m.repositorySort = 2
		m = keyboardContractPress(t, m, "home")
		if !strings.Contains(ansi.Strip(m.View()), "[STALE]") {
			t.Fatal("root comparison lost stale warning")
		}
	}
}

func TestActivityPanelHeightDoesNotChangeWithSelection(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	page := m.worklistPageSize()
	m.selectedRepoID = "/sample/gateway"
	m.local.records = nil
	m.recomputeAuthors()
	view := ansi.Strip(m.View())
	if m.worklistPageSize() != page || !strings.Contains(view, "No contributors in this scope and range.") {
		t.Fatal("quiet repository changed the paging stride or hid its empty contributor state")
	}
}
