package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func statisticsViewportFixture() Model {
	m := modelWithData()
	m.width, m.height = 100, 28
	m = send(m, "enter")
	return m
}
func statisticsViewportKey(m Model, k tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(Model)
}
func checkStatisticsViewportBounds(t *testing.T, m Model) {
	t.Helper()
	view := m.View()
	if lipgloss.Height(view) > m.height {
		t.Fatalf("detail exceeds %d rows: %d", m.height, lipgloss.Height(view))
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("detail exceeds %d columns", m.width)
		}
	}
}
func TestStatisticsCanvasPreservesAllContentLines(t *testing.T) {
	var lines []string
	for i := 0; i < 43; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	view := ansi.Strip(nightCanvas(strings.Join(lines, "\n"), 100, 28))
	if lipgloss.Height(view) != 43 || !strings.Contains(view, "line 42") {
		t.Fatalf("canvas discarded overflow: height%d", lipgloss.Height(view))
	}
}
func TestStatisticsViewportReachesAllSections(t *testing.T) {
	m := statisticsViewportFixture()
	top := ansi.Strip(m.View())
	if m.viewMode != ViewOperative {
		t.Fatal("fixture failed to enter detail")
	}
	checkStatisticsViewportBounds(t, m)
	seen := top
	m = statisticsViewportKey(m, tea.KeyPgDown)
	seen += ansi.Strip(m.View())
	checkStatisticsViewportBounds(t, m)
	if ansi.Strip(m.View()) == top {
		t.Fatal("PageDown did not advance contributor detail")
	}
	m = statisticsViewportKey(m, tea.KeyEnd)
	seen += ansi.Strip(m.View())
	checkStatisticsViewportBounds(t, m)
	for _, want := range []string{"ACTIVITY TIMELINE", "ACTIVITY MATRIX", "back"} {
		if !strings.Contains(seen, want) {
			t.Fatalf("section %q never reachable", want)
		}
	}
	m = statisticsViewportKey(m, tea.KeyHome)
	if ansi.Strip(m.View()) != top {
		t.Fatal("Home did not restore first page")
	}
	m = statisticsViewportKey(m, tea.KeyEnd)
	m = statisticsViewportKey(m, tea.KeyPgUp)
	if !strings.Contains(ansi.Strip(m.View()), "CONTRIBUTOR:") {
		t.Fatal("PageUp did not restore top for short detail")
	}
}
func TestStatisticsViewportResetsForAuthorTimeAndReopen(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyLeft} {
		m := statisticsViewportFixture()
		oldAuthor := m.activeAuthorID
		oldTime := m.timeIdx
		m = statisticsViewportKey(m, tea.KeyEnd)
		m = statisticsViewportKey(m, k)
		if !strings.Contains(ansi.Strip(m.View()), "CONTRIBUTOR:") {
			t.Fatalf("%v retained stale detail scroll", k)
		}
		if k == tea.KeyDown && m.activeAuthorID == oldAuthor {
			t.Fatal("arrow no longer switches author")
		}
		if k == tea.KeyLeft && m.timeIdx == oldTime {
			t.Fatal("time range did not change")
		}
	}
	m := statisticsViewportFixture()
	top := ansi.Strip(m.View())
	m = statisticsViewportKey(m, tea.KeyEnd)
	m = send(m, "esc", "enter")
	if ansi.Strip(m.View()) != top {
		t.Fatal("reopening detail retained old scroll")
	}
}
func TestStatisticsViewportResizeClampsAndAwarenessStaysBounded(t *testing.T) {
	m := statisticsViewportFixture()
	m = statisticsViewportKey(m, tea.KeyEnd)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 80})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "CONTRIBUTOR:") {
		t.Fatal("taller resize did not clamp to beginning")
	}
	checkStatisticsViewportBounds(t, m)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = statisticsViewportKey(m, tea.KeyEnd)
	checkStatisticsViewportBounds(t, m)
	if !strings.Contains(ansi.Strip(m.View()), "back") {
		t.Fatal("small resize hides bottom help")
	}
	for _, size := range [][2]int{{100, 28}, {160, 48}, {80, 30}} {
		m := glanceBusyFixture()
		m.width, m.height = size[0], size[1]
		checkStatisticsViewportBounds(t, m)
	}
}

func TestStatisticsViewportTinyHeightGivesResizeHint(t *testing.T) {
	for _, height := range []int{1, 2} {
		m := statisticsViewportFixture()
		m.height = height
		for _, k := range []tea.KeyType{tea.KeyPgDown, tea.KeyEnd, tea.KeyHome} {
			m = statisticsViewportKey(m, k)
			checkStatisticsViewportBounds(t, m)
			view := ansi.Strip(m.View())
			for _, want := range []string{"Resize terminal", "Esc back", "q quit"} {
				if !strings.Contains(view, want) {
					t.Fatalf("height%d missing %q: %s", height, want, view)
				}
			}
		}
	}
}
