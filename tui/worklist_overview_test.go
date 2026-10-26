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
	l := repositoryOverviewWidths(m.width - 4)
	start := 7 + l.name + 1
	for _, cell := range []struct {
		start, width int
		value        string
	}{{start, l.commits, "5"}, {start + l.commits + 1, l.people, "5"}} {
		if strings.TrimSpace(ansi.Cut(lines[7], cell.start, cell.start+cell.width)) != cell.value {
			t.Fatalf("wrong repository metric at column %d: %s", cell.start, lines[7])
		}
	}
	if !strings.Contains(lines[6], "COMMITS") || !strings.Contains(lines[6], "PEOPLE") || !strings.Contains(lines[5], "quiet") {
		t.Fatal("overview lost comparison labels or quiet repository context")
	}
	m.prState.snapshots = nil
	withoutPRs := strings.Split(ansi.Strip(m.View()), "\n")
	if withoutPRs[6] != lines[6] || withoutPRs[7] != lines[7] {
		t.Fatal("PR availability shifted the activity comparison columns")
	}
}

func TestRepositoryOverviewPagesWithoutSelectedPreview(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {110, 28}, {120, 36}, {180, 48}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		m.staleRepos["/sample/gateway"] = true
		m = keyboardContractPress(t, m, "pgdown", "end")
		view := ansi.Strip(m.View())
		rows := m.glanceRepositories()
		selected := rows[m.selectedRepository(rows)]
		if !strings.Contains(view, selected.repo.Name) || len(strings.Split(view, "\n")) != m.height {
			t.Fatal("paging hid the selected repository or changed frame height")
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
