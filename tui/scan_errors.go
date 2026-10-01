package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) scanErrorLines() []string {
	var lines []string
	for _, repo := range m.repositories {
		if detail := m.scanErrors[repo.ID]; detail != "" {
			lines = append(lines, wrapGlanceText(displayText(repo.Name)+": "+displayText(detail), max(1, m.width-4))...)
			lines = append(lines, "")
		}
	}
	return lines
}

func (m Model) renderScanErrors() string {
	lines := m.scanErrorLines()
	if len(lines) == 0 {
		lines = []string{"All local scan errors have cleared."}
	}
	budget := max(1, m.height-3)
	offset := max(0, min(m.scanErrorOffset, len(lines)-budget))
	visible := []string{"LOCAL SCAN ERRORS"}
	visible = append(visible, lines[offset:min(len(lines), offset+budget)]...)
	help := "↑↓ PgUp/PgDn scroll · R retry · Esc back · q quit"
	if m.width < 60 {
		help = "↑↓/PgUp/PgDn · R retry · Esc back"
	}
	visible = append(visible, help)
	return fitGlanceLines(visible, max(1, m.width), max(1, m.height))
}

func (m Model) handleScanErrorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "esc", "e":
		m.showScanErrors = false
	case "R":
		return m, m.startLocalRefresh()
	case "up", "k":
		m.scanErrorOffset--
	case "down", "j":
		m.scanErrorOffset++
	case "pgup":
		m.scanErrorOffset -= max(1, m.height-3)
	case "pgdown":
		m.scanErrorOffset += max(1, m.height-3)
	case "home":
		m.scanErrorOffset = 0
	case "end":
		m.scanErrorOffset = len(m.scanErrorLines())
	}
	m.scanErrorOffset = max(0, min(m.scanErrorOffset, len(m.scanErrorLines())-max(1, m.height-3)))
	return m, nil
}
