package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func repositoryModalCheckboxRows(m Model) []string {
	var rows []string
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		for _, marker := range []string{"[x]", "[✓]", "[ ]"} {
			if at := strings.Index(line, marker); at >= 0 {
				row, _, _ := strings.Cut(line[at:], "│")
				rows = append(rows, strings.TrimSpace(row))
				break
			}
		}
	}
	return rows
}

func TestRepositoryModalPreservesBoardAndDetailOnCancel(t *testing.T) {
	for _, keys := range [][]string{{"a", "down"}, {"a", "enter"}, {"c", "down"}, {"c", "enter"}, {"p", "down"}, {"p", "enter"}} {
		t.Run(strings.Join(keys, "-"), func(t *testing.T) {
			m := boardLayoutFixture()
			m.width, m.height = 120, 36
			m = keyboardContractPress(t, m, keys...)
			before := m.View()
			m = keyboardContractPress(t, m, "i")
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "╭") || !strings.Contains(view, "BIGBOARD /") {
				t.Fatalf("repository controls did not overlay the retained board:\n%s", view)
			}
			m = keyboardContractPress(t, m, "down", " ", "esc")
			if m.viewMode == ViewRepoOverlay || m.View() != before {
				t.Fatal("canceling repository controls changed the underlying board or detail")
			}
			if m.excludedRepos[m.loadedRepos[1].ID] {
				t.Fatal("canceling the modal applied a pending repository choice")
			}
		})
	}
}

func TestRepositoryModalPagingAndResizeMatchVisibleOptions(t *testing.T) {
	m := boardLayoutFixture()
	m.loadedRepos = nil
	for i := 0; i < 45; i++ {
		m.loadedRepos = append(m.loadedRepos, git.Repository{ID: fmt.Sprintf("/r%02d", i), Name: fmt.Sprintf("repo-%02d", i)})
	}
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "i")
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 18}, {32, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		m = keyboardContractPress(t, m, "home")
		page := len(repositoryModalCheckboxRows(m))
		if page < 1 {
			t.Fatalf("no visible repository options at %v", size)
		}
		m = keyboardContractPress(t, m, "pgdown")
		if m.overlayCursor != page {
			t.Fatalf("paging moved %d rows with %d options visible at %v", m.overlayCursor, page, size)
		}
		m = keyboardContractPress(t, m, "end")
		rows := repositoryModalCheckboxRows(m)
		if m.overlayCursor != 44 || !strings.Contains(strings.Join(rows, "\n"), "repo-44") {
			t.Fatalf("last repository was unreachable at %v", size)
		}
		view := strings.Split(m.View(), "\n")
		if len(view) != m.height {
			t.Fatalf("modal changed terminal height at %v", size)
		}
		for _, line := range view {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("modal overflowed terminal width at %v", size)
			}
		}
	}
}
