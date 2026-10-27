package tui

import (
	"sort"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func (m Model) areaRepository() (git.Repository, bool) {
	for _, repo := range m.local.repositories {
		if repo.ID == m.areaRepoID && m.repoInView(repo.ID) {
			return repo, true
		}
	}
	return git.Repository{}, false
}

func (m *Model) normalizeAreaScope() {
	if m.areaRepoID == "" {
		m.directory = directoryState{}
		return
	}
	if _, ok := m.areaRepository(); !ok {
		m.directory = directoryState{}
		m.areaRepoID = ""
		m.selectedAreaID = ""
		m.personID = ""
	}
}

func (m *Model) openSingleRepositoryAreas() {
	if m.areaRepoID != "" {
		m.normalizeAreaScope()
		return
	}
	var included []git.Repository
	for _, repo := range m.local.repositories {
		if m.repoInView(repo.ID) {
			included = append(included, repo)
		}
	}
	if len(included) == 1 {
		m.openDirectoryRepository(included[0].ID)
	}
}

func (m Model) selectedScopeID() string {
	if m.areaRepoID != "" {
		return m.selectedAreaID
	}
	return m.selectedRepoID
}
func (m *Model) setSelectedScopeID(id string) {
	if m.areaRepoID != "" {
		m.selectedAreaID = id
	} else {
		m.selectedRepoID = id
	}
}

// Keep the selected repository association while taking canonical identity and
// complete path evidence from the same preferred copy used for board totals.
func (m Model) recordsInRepository(repo git.Repository, records []git.CommitRecord) []git.CommitRecord {
	return stats.UniqueRecordsWithMatchingCopy(records, func(record git.CommitRecord) bool {
		return record.RepoID == repo.ID || record.RepoID == "" && record.RepoName == repo.Name
	})
}

func (m Model) rulesForRepository(repo git.Repository) []stats.WorkAreaRule {
	if rules, ok := m.options.WorkAreas[repo.ID]; ok {
		return rules
	}
	return m.options.WorkAreas[repo.Name]
}

func (m Model) currentWorkAreas() []stats.WorkArea {
	repo, ok := m.areaRepository()
	if !ok {
		return nil
	}
	definition, ok := m.areaDefinitions[repo.ID]
	if !ok {
		definition = stats.DefineWorkAreas(m.recordsInRepository(repo, m.local.records), m.rulesForRepository(repo))
	}
	records := m.recordsInRepository(repo, m.activityRecords())
	areas := definition.Build(records)
	seen := make(map[string]bool)
	for _, area := range areas {
		seen[area.ID] = true
	}
	for _, pr := range m.prsInViewForRepository(repo.ID) {
		for _, area := range m.prAreas(repo, pr) {
			if !seen[area.ID] {
				areas = append(areas, area)
				seen[area.ID] = true
			}
		}
	}
	return areas
}

func (m *Model) rebuildAreaDefinitions() {
	m.areaDefinitions = make(map[string]stats.WorkAreaDefinition)
	for _, repo := range m.local.repositories {
		m.areaDefinitions[repo.ID] = stats.DefineWorkAreas(m.recordsInRepository(repo, m.local.records), m.rulesForRepository(repo))
	}
}

func (m Model) activityAreas() []activityRow {
	var result []activityRow
	for _, area := range m.currentWorkAreas() {
		activity := activityRow{scopeID: area.ID, label: area.Name, repositoryID: m.areaRepoID, commits: len(stats.UniqueRecords(area.Records))}
		activity.people = stats.AggregateWithOptions(area.Records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		sort.Slice(activity.people, func(i, j int) bool {
			if activity.people[i].Name != activity.people[j].Name {
				return activity.people[i].Name < activity.people[j].Name
			}
			return activity.people[i].ID < activity.people[j].ID
		})
		for _, r := range area.Records {
			activity.observe(r)
		}
		result = append(result, activity)
	}
	sortRepositoryActivities(result, m.overviewSort)
	return result
}

func (m Model) scopeEvidenceKey(scope activityRow) string {
	if m.areaRepoID != "" {
		return scope.scopeID
	}
	return scope.label
}

// Wrap every literal path so long filenames and rename/copy origins remain
// reachable even in narrow terminals. The inspector scrolls rendered lines.
func wrappedPathLines(r git.CommitRecord, width int) []string {
	var lines []string
	for _, change := range r.Changes {
		label := displayText(change.Path)
		if change.PreviousPath != "" {
			label += " (from " + displayText(change.PreviousPath) + "; rename/copy origin)"
		}
		if change.Generated {
			label += " [excluded]"
		}
		available := max(1, width-4)
		for len(label) > 0 {
			part := cutToWidth(label, available)
			if part == "" {
				break
			}
			lines = append(lines, "  "+part)
			label = label[len(part):]
		}
	}
	return lines
}
