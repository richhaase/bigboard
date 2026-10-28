package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) handlePRSort(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+c", "q":
		return m.quit()
	case "esc", "left":
		m.prUI.sortOpen = false
	case "enter", "1", "2", "3", "4", "5":
		selected := m.selectedPRKey()
		if key != "enter" {
			m.prUI.sortRow = int(key[0] - '1')
		}
		m.prUI.sort, m.prUI.reverse = m.prUI.sortRow, false
		m.prUI.sortOpen = false
		m.selectPR(selected)
	default:
		if row, ok := glanceMove(key, m.prUI.sortRow, len(prSortLabels), len(prSortLabels)); ok {
			m.prUI.sortRow = row
		}
	}
	return m, nil
}

func (m Model) renderPRSort(background string) string {
	if !filterMinimum.fits(m.width, m.height) {
		return renderCenteredModal(background, []string{"Sort pull requests", "Resize to 40×18", "Esc cancels"}, max(6, min(30, m.width)), m.width, m.height)
	}
	width := max(6, min(54, m.width-2))
	inner := max(1, width-4)
	lines := []string{StyleTitle.Render("Sort pull requests"), ""}
	for i, label := range prSortLabels {
		line := fmt.Sprintf("%s %d  %s", glanceCursor(i == m.prUI.sortRow), i+1, label)
		if i == m.prUI.sort {
			line += " · active"
		}
		lines = append(lines, ansi.Truncate(line, inner, "…"))
	}
	lines = append(lines, "", StyleDimWhite.Render(ansi.Truncate("Authors: A–Z, newest PRs first per author", inner, "…")), StyleCyan.Render(ansi.Truncate("1–5 choose · ↑↓ Enter · Esc cancel", inner, "…")))
	return renderCenteredModal(background, lines, width, m.width, m.height)
}
