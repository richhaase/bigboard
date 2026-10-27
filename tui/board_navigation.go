package tui

type prContextState struct {
	active   bool
	ui       prUIState
	selected string
}

func (m Model) switchBoard(key string) Model {
	m.identityMerge = identityMergeState{}
	if m.viewMode == ViewRepoOverlay {
		m.closeRepositoryControls(false)
	}
	samePRBoard := key == "p" && m.showPRs && !m.prContext.active
	if m.prContext.active {
		m.closeContextPRs(false)
	}
	if !m.showPRs && (m.viewMode == ViewAggregate || m.viewMode == ViewOperative) {
		m.contributorsView = m.viewMode
	}
	if m.searching {
		selected := m.selectedContributorID()
		m.filterQuery = m.searchBefore
		m.selectContributor(selected)
	}
	if m.prUI.searching {
		selected := m.selectedPRKey()
		m.prUI.query = m.prUI.searchBefore
		m.selectPR(selected)
	}
	if m.glance.searching {
		m.setGlanceQuery(m.glanceSearchBefore)
	}
	m.searching, m.prUI.searching, m.glance.searching = false, false, false
	m.showHelp, m.helpOffset = false, 0
	m.showScanErrors, m.scanErrorOffset = false, 0
	m.filters = sharedFilterState{}
	if !samePRBoard {
		m.prUI.detail = false
	}
	m.err = nil
	m.showPRs = key == "p"
	if !m.showPRs {
		if key == "c" || key == "l" {
			m.viewMode = ViewAggregate
			if m.contributorsView == ViewOperative {
				m.viewMode = ViewOperative
			}
		} else {
			m.viewMode = ViewAwareness
		}
	}
	return m
}

func (m *Model) normalizeContributorDetail() {
	if m.viewMode != ViewOperative && m.contributorsView != ViewOperative {
		return
	}
	for _, author := range m.displayedAuthors() {
		if author.ID == m.activeAuthorID && m.activeAuthorID != "" {
			m.clampStatDetailScroll()
			return
		}
	}
	m.contributorsView = ViewAggregate
	if m.viewMode == ViewOperative {
		m.viewMode = ViewAggregate
	}
	m.activeAuthorID, m.activeOperative, m.statDetailOffset = "", "", 0
}

func (m *Model) openContextPRs(repoID string) {
	if !m.prContext.active {
		m.prContext = prContextState{
			active: true, ui: m.prUI.clone(), selected: m.selectedPRKey(),
		}
	}
	m.showPRs, m.prUI.detail = true, false
	m.prUI.filters = prFilters{}
	m.prUI.repoID, m.prUI.query, m.prUI.row, m.prUI.offset = repoID, "", 0, 0
	m.prUI.searching = false
	m.prUI.browserError = ""
}

func (m *Model) closeContextPRs(returnToOrigin bool) {
	if !m.prContext.active {
		return
	}
	saved := m.prContext
	m.prContext = prContextState{}
	m.prUI = saved.ui.clone()
	m.prUI.searching = false
	m.selectPR(saved.selected)
	if m.prUI.detail && m.selectedPRKey() != saved.selected {
		m.prUI.detail, m.prUI.offset = false, 0
	}
	if returnToOrigin {
		m.showPRs, m.viewMode = false, ViewAwareness
	}
}
