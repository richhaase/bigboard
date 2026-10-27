package tui

import (
	"slices"
	"strings"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type directoryFrame struct {
	path, selectedID, query string
}

type directoryState struct {
	active bool
	frame  directoryFrame
	stack  []directoryFrame
}

func (m *Model) openDirectoryRepository(repoID string) {
	m.areaRepoID, m.selectedRepoID = repoID, repoID
	m.selectedAreaID, m.personID = "", ""
	m.directory = directoryState{active: true}
	m.glance.detailOpen, m.glance.searching = false, false
}

func (m *Model) leaveDirectory() {
	if n := len(m.directory.stack); n > 0 {
		m.directory.frame = m.directory.stack[n-1]
		m.directory.stack = m.directory.stack[:n-1]
	} else {
		m.directory = directoryState{}
		m.areaRepoID = ""
	}
}

func (m Model) directoryRows() []activityRow {
	entries := stats.DirectoryChildren(m.glanceScopeRecords(), m.directory.frame.path)
	var rows []activityRow
	query := strings.ToLower(m.directory.frame.query)
	for _, entry := range entries {
		name := entry.Name
		if entry.IsDir {
			name += "/"
		}
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		row := activityRow{scopeID: entry.ID, label: name, repositoryID: m.areaRepoID, commits: len(entry.Records)}
		row.people = stats.AggregateWithOptions(entry.Records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		for _, record := range entry.Records {
			row.observe(record)
		}
		rows = append(rows, row)
	}
	sortChangeActivities(rows, m.repositorySort)
	if m.overviewReverse {
		slices.Reverse(rows)
	}
	return rows
}

func outsideDirectoryRecords(records []git.CommitRecord, id string) []git.CommitRecord {
	kind, path, _ := strings.Cut(id, ":")
	if kind != "dir" && kind != "file" {
		return records
	}
	var outside []git.CommitRecord
	for _, record := range records {
		var changes []git.PathChange
		for _, change := range record.Changes {
			inside := kind == "file" && change.Path == path || kind == "dir" && (path == "" || strings.HasPrefix(change.Path, path+"/"))
			if !inside && !change.Generated {
				changes = append(changes, change)
			}
		}
		if len(changes) > 0 {
			record.Changes = changes
			outside = append(outside, record)
		}
	}
	return stats.DirectoryRecords(outside, "dir:")
}

func (m Model) relatedScopeAreas() []stats.WorkArea {
	areas := m.currentWorkAreas()
	if !m.glance.frame.literal {
		return areas
	}
	for i := range areas {
		areas[i].Records = outsideDirectoryRecords(areas[i].Records, m.glance.frame.areaID)
	}
	return areas
}

func (m *Model) selectedDirectory(rows []activityRow) int {
	for i, row := range rows {
		if row.scopeID == m.directory.frame.selectedID {
			return i
		}
	}
	if len(rows) > 0 {
		m.directory.frame.selectedID = rows[0].scopeID
	}
	return 0
}

func (m *Model) openDirectoryDetails(id, name string, tab int) {
	m.glance.detailOpen = true
	m.glance.frame = glanceFrame{areaID: id, name: name, literal: true, tab: tab}
	m.glance.stack = nil
	m.personID = ""
}

func (m Model) handleDirectoryKey(key string) Model {
	rows := m.directoryRows()
	selected := m.selectedDirectory(rows)
	switch key {
	case "s":
		m.repositorySort = (m.repositorySort + 1) % len(changeSortLabels)
	case "S":
		m.overviewReverse = !m.overviewReverse
	case "enter":
		if len(rows) == 0 {
			return m
		}
		row := rows[selected]
		if path, ok := strings.CutPrefix(row.scopeID, "dir:"); ok {
			m.directory.stack = append(append([]directoryFrame(nil), m.directory.stack...), m.directory.frame)
			m.directory.frame = directoryFrame{path: path}
		} else {
			name := row.label
			if path, ok := strings.CutPrefix(row.scopeID, "file:"); ok {
				name = path
			}
			m.openDirectoryDetails(row.scopeID, name, glanceActivity)
		}
	case "1", "2", "3":
		tab := int(key[0] - '1')
		name := m.directory.frame.path
		if name == "" {
			name = "."
		}
		m.openDirectoryDetails("dir:"+m.directory.frame.path, name, tab)
	default:
		if next, ok := glanceMove(key, selected, len(rows), m.worklistPageSize()); ok && len(rows) > 0 {
			m.directory.frame.selectedID = rows[next].scopeID
		}
	}
	return m
}

func (m Model) renderDirectoryBoard(width, height int) string {
	repo, _ := m.areaRepository()
	path := m.directory.frame.path
	if path == "" {
		path = "."
	}
	lines := m.screenHeader("Activity / "+displayText(repo.Name)+" / "+displayText(path), width)
	rows := m.directoryRows()
	selected := m.selectedDirectory(rows)
	page := m.worklistPageSize()
	start := max(0, min(selected-page/2, len(rows)-page))
	end := min(len(rows), start+page)
	lines = append(lines, boardListHeading("DIRECTORIES / FILES", m.overviewSortLabel(), nil, start, end, len(rows), width))
	lines = append(lines, "  "+m.repositoryOverviewColumns(width-4))
	maxValue := m.repositoryMeasureMax(rows)
	for i := start; i < end; i++ {
		lines = append(lines, m.worklistRepositoryRow(rows[i], i == selected, width-4, i+1, maxValue)...)
	}
	if len(rows) == 0 {
		message := "No file changes in this directory and range."
		if m.directory.frame.query != "" {
			message = "No matching paths. x clears board filters."
		}
		lines = append(lines, "  "+message)
	} else {
		lines = m.appendActivityContributors(lines, rows[selected], width, height)
	}
	local := []footerHint{{"Esc", "back"}, {"f", "filters"}}
	if activityContributorsHeight(width, height) > 0 {
		local = append(local, footerHint{"Tab", "contributors"})
	}
	local = append(local, footerHint{"2", "people"}, footerHint{"/", "find"}, footerHint{"Enter", "open"}, footerHint{"t", "date range"}, footerHint{"s", "sort"})
	footer := footerHints{local: local}
	if m.contributorPane.focused {
		footer = focusedContributorsFooter()
	}
	if m.glance.searching {
		footer = searchFooter()
	}
	coverage := "Lines belong to files here · commits can touch multiple entries"
	return screenFinish(lines, footer, coverage, width, height)
}
