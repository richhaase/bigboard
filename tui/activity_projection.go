package tui

type activityProjection struct {
	rows         []activityRow
	directory    bool
	contributors bool
}

func (m Model) prepareActivityProjection() activityProjection {
	if m.glance.detailOpen || m.showPaths {
		return activityProjection{}
	}
	if m.directory.active {
		return activityProjection{rows: m.directoryRows(), directory: true, contributors: true}
	}
	return activityProjection{rows: m.glanceRepositories(), contributors: m.areaRepoID == ""}
}

func (p activityProjection) contributorSelection(m *Model) (activityRow, bool) {
	if !p.contributors || len(p.rows) == 0 {
		return activityRow{}, false
	}
	if p.directory {
		return p.rows[m.selectedDirectory(p.rows)], true
	}
	return p.rows[m.selectedRepository(p.rows)], true
}
