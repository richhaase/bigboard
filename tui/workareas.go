package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
		activity := repositoryActivity{repo: git.Repository{ID: area.ID, Name: area.Name}}
		activity.people = stats.AggregateWithOptions(area.Records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		sort.Slice(activity.people, func(i, j int) bool {
			if !activity.people[i].LastCommit.Equal(activity.people[j].LastCommit) {
				return activity.people[i].LastCommit.After(activity.people[j].LastCommit)
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
	sort.Slice(result, func(i, j int) bool {
		if !result[i].latest.Equal(result[j].latest) {
			return result[i].latest.After(result[j].latest)
		}
		return result[i].repo.Name < result[j].repo.Name
	})
	return result
}

func (m Model) awarenessBreadcrumb() string {
	if repo, ok := m.areaRepository(); ok {
		return "  REPOSITORIES › " + displayText(repo.Name) + " › WORK AREAS · Esc back"
	}
	return "  WORK RELATIONSHIPS · Local Git history · author dates"
}

func changedPathSummary(r git.CommitRecord) string {
	if r.PathsUnknown {
		return "Paths unknown: shallow history boundary"
	}
	if len(r.Changes) == 0 {
		return "No file changes"
	}
	labels := make([]string, 0, len(r.Changes))
	for _, change := range r.Changes {
		label := displayText(change.Path)
		if change.PreviousPath != "" {
			label += " (from " + displayText(change.PreviousPath) + ")"
		}
		if change.Generated {
			label += " [excluded]"
		}
		labels = append(labels, label)
	}
	if len(labels) > 3 {
		return strings.Join(labels[:3], ", ") + fmt.Sprintf(" · +%d paths", len(labels)-3)
	}
	return strings.Join(labels, ", ")
}

func (m Model) scopeEvidenceKey(scope repositoryActivity) string {
	if m.areaRepoID != "" {
		return scope.repo.ID
	}
	return scope.repo.Name
}

func (m Model) renderPathInspector(r git.CommitRecord, width, height int) string {
	lines := renderBanner(min(width, bannerMinWidth-1))
	lines = append(lines, RenderSectionHeader("CHANGED PATHS", width), "  "+displayText(r.Subject), "  "+r.Date.Local().Format("2006-01-02 15:04 MST")+" · "+displayText(r.Author), "  Commit "+displayText(r.CommitID))
	if repo, ok := m.areaRepository(); ok {
		lines = append(lines, "  Repository: "+displayText(repo.Name)+" · selected area paths")
	}
	if r.PathsUnknown {
		lines = append(lines, "  Paths unknown: shallow history boundary")
	}
	if len(r.Changes) == 0 && !r.PathsUnknown {
		lines = append(lines, "  No file changes")
	}
	pathLines := wrappedPathLines(r, width)
	budget := max(1, height-len(lines)-2)
	offset := max(0, min(m.pathOffset, len(pathLines)-1))
	lines = append(lines, pathLines[offset:min(len(pathLines), offset+budget)]...)
	if len(pathLines) > 0 {
		lines = append(lines, fmt.Sprintf("  %d paths · lines %d–%d/%d", len(r.Changes), offset+1, min(len(pathLines), offset+budget), len(pathLines)))
	}
	lines = append(lines, "  ↑↓ paths · Enter/Esc back · q quit")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
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
