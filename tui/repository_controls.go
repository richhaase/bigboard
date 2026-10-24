package tui

import (
	"maps"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openRepositoryControls() {
	m.overlayExcluded = maps.Clone(m.excludedRepos)
	if m.overlayExcluded == nil {
		m.overlayExcluded = make(map[string]bool)
	}
	m.overlayCursor = 0
	m.returnView, m.overlayPRs = m.viewMode, m.showPRs
	m.viewMode, m.showPRs = ViewRepoOverlay, false
}

func (m *Model) closeRepositoryControls(apply bool) {
	selected := m.selectedPRKey()
	if apply {
		m.excludedRepos = m.overlayExcluded
		m.recomputeAuthors()
	}
	m.overlayExcluded = nil
	m.viewMode, m.showPRs = m.returnView, m.overlayPRs
	m.overlayPRs = false
	m.selectPR(selected)
	if m.prDetail && selected != m.selectedPRKey() {
		m.prDetail, m.prOffset = false, 0
	}
}

func (m Model) handleRepositoryControls(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "enter":
		m.closeRepositoryControls(true)
	case "esc", "left":
		m.closeRepositoryControls(false)
	case " ":
		if m.overlayCursor >= 0 && m.overlayCursor < len(m.loadedRepos) {
			id := m.loadedRepos[m.overlayCursor].ID
			m.overlayExcluded = maps.Clone(m.overlayExcluded)
			m.overlayExcluded[id] = !m.overlayExcluded[id]
		}
	default:
		if row, ok := glanceMove(msg.String(), m.overlayCursor, len(m.loadedRepos), max(1, m.height-12)); ok {
			m.overlayCursor = row
		}
	}
	return m, nil
}
