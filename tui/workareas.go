package tui

import (
	"sort"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func (m Model) areaRepository() (git.Repository, bool) {
	for _, repo := range m.loadedRepos {
		if repo.ID == m.areaRepoID && !m.excludedRepos[repo.ID] {
			return repo, true
		}
	}
	return git.Repository{}, false
}

func (m *Model) normalizeAreaScope() {
	if m.areaRepoID == "" {
		return
	}
	if _, ok := m.areaRepository(); !ok {
		m.areaRepoID = ""
		m.selectedAreaID = ""
		m.personID = ""
		m.evidenceOffset = 0
	}
}

func (m *Model) openSingleRepositoryAreas() {
	if m.areaRepoID != "" {
		m.normalizeAreaScope()
		return
	}
	var included []git.Repository
	for _, repo := range m.loadedRepos {
		if !m.excludedRepos[repo.ID] {
			included = append(included, repo)
		}
	}
	if len(included) == 1 {
		m.areaRepoID = included[0].ID
		m.selectedRepoID = included[0].ID
		m.awarenessPane = 0
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
	members := make(map[string]bool)
	var result []git.CommitRecord
	for _, r := range records {
		if r.RepoID != repo.ID && (r.RepoID != "" || r.RepoName != repo.Name) {
			continue
		}
		if r.CommitID == "" {
			result = append(result, r)
		} else {
			members[r.CommitID] = true
		}
	}
	for _, r := range stats.UniqueRecords(records) {
		if r.CommitID != "" && members[r.CommitID] {
			result = append(result, r)
		}
	}
	return result
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
		definition = stats.DefineWorkAreas(m.recordsInRepository(repo, m.allRecords), m.rulesForRepository(repo))
	}
	records := m.recordsInRepository(repo, m.filteredRecords())
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	kept := make([]git.CommitRecord, 0, len(records))
	for _, r := range records {
		if visible[stats.IdentityID(r)] {
			kept = append(kept, r)
		}
	}
	areas := definition.Build(kept)
	seen := make(map[string]bool)
	for _, area := range areas {
		seen[area.ID] = true
	}
	for _, pr := range m.prsForRepository(repo.ID) {
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
	for _, repo := range m.loadedRepos {
		m.areaDefinitions[repo.ID] = stats.DefineWorkAreas(m.recordsInRepository(repo, m.allRecords), m.rulesForRepository(repo))
	}
}

func (m Model) activityAreas() []repositoryActivity {
	var result []repositoryActivity
	for _, area := range m.currentWorkAreas() {
		activity := repositoryActivity{repo: git.Repository{ID: area.ID, Name: area.Name}, commits: len(stats.UniqueRecords(area.Records))}
		activity.people = stats.AggregateWithOptions(area.Records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		sort.Slice(activity.people, func(i, j int) bool {
			if activity.people[i].Name != activity.people[j].Name {
				return activity.people[i].Name < activity.people[j].Name
			}
			return activity.people[i].ID < activity.people[j].ID
		})
		for _, r := range area.Records {
			if r.Date.After(activity.latest) {
				activity.latest = r.Date
				activity.subject = r.Subject
			}
		}
		result = append(result, activity)
	}
	sortRepositoryActivities(result, m.overviewSort)
	return result
}

func (m Model) scopeEvidenceKey(scope repositoryActivity) string {
	if m.areaRepoID != "" {
		return scope.repo.ID
	}
	return scope.repo.Name
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
