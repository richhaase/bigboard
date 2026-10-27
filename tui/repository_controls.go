package tui

import (
	"maps"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openRepositoryControls() {
	m.cacheRemoveConfirm, m.cacheError = "", ""
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
	if m.cacheRemoving {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, nil
	}
	if m.cacheRemoveConfirm != "" {
		switch msg.String() {
		case "enter":
			return m.removeCachedRepository()
		case "esc", "left":
			m.cacheRemoveConfirm = ""
		case "q", "ctrl+c":
			return m.quit()
		}
		return m, nil
	}
	rows := m.repositoryControlRows()
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "enter":
		m.closeRepositoryControls(true)
	case "esc", "left":
		m.closeRepositoryControls(false)
	case " ":
		if m.overlayCursor >= 0 && m.overlayCursor < len(rows) {
			id := rows[m.overlayCursor].ID
			if m.orphanedRepository(id) {
				return m, nil
			}
			m.overlayExcluded = maps.Clone(m.overlayExcluded)
			m.overlayExcluded[id] = !m.overlayExcluded[id]
		}
	case "d":
		if m.overlayCursor >= 0 && m.overlayCursor < len(rows) && m.orphanedRepository(rows[m.overlayCursor].ID) && m.options.RemoveCachedRepository != nil {
			m.cacheRemoveConfirm = rows[m.overlayCursor].ID
		}
	default:
		_, page := repositoryModalSize(m.width, m.height)
		if row, ok := glanceMove(msg.String(), m.overlayCursor, len(rows), page); ok {
			m.overlayCursor = row
		}
	}
	return m, nil
}
