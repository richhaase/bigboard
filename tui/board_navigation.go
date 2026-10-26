package tui

type prContextState struct {
	active                  bool
	filters                 prFilters
	repoID, query, selected string
	row, offset, sort       int
	reverse, detail         bool
	browserError            string
}

func (m Model) switchBoard(key string) Model {
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
	if m.prSearching {
		selected := m.selectedPRKey()
		m.prQuery = m.prSearchBefore
		m.selectPR(selected)
	}
	if m.glance.searching {
		m.setGlanceQuery(m.glanceSearchBefore)
	}
	m.searching, m.prSearching, m.glance.searching = false, false, false
	m.showHelp, m.helpOffset = false, 0
	m.showScanErrors, m.scanErrorOffset = false, 0
	m.filters = sharedFilterState{}
	if !samePRBoard {
		m.prDetail = false
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
			active: true, filters: m.prFilters.clone(), repoID: m.prRepoID,
			query: m.prQuery, selected: m.selectedPRKey(), row: m.prRow,
			offset: m.prOffset, sort: m.prSort, reverse: m.prReverse,
			detail: m.prDetail, browserError: m.prBrowserError,
		}
	}
	m.showPRs, m.prDetail = true, false
	m.prFilters = prFilters{}
	m.prRepoID, m.prQuery, m.prRow, m.prOffset = repoID, "", 0, 0
	m.prSearching = false
	m.prBrowserError = ""
}

func (m *Model) closeContextPRs(returnToOrigin bool) {
	if !m.prContext.active {
		return
	}
	saved := m.prContext
	m.prContext = prContextState{}
	m.prFilters, m.prRepoID, m.prQuery = saved.filters.clone(), saved.repoID, saved.query
	m.prRow, m.prOffset, m.prSort, m.prReverse = saved.row, saved.offset, saved.sort, saved.reverse
	m.prDetail, m.prBrowserError = saved.detail, saved.browserError
	m.prSearching = false
	m.selectPR(saved.selected)
	if m.prDetail && m.selectedPRKey() != saved.selected {
		m.prDetail, m.prOffset = false, 0
	}
	if returnToOrigin {
		m.showPRs, m.viewMode = false, ViewAwareness
	}
}
