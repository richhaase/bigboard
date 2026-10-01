package tui

import (
	"sort"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// Repository associations describe Git history, not live presence. Counts and
// identities use the same canonical records as the statistics pipeline.
type repositoryActivity struct {
	repo     git.Repository
	people   []stats.AuthorStats
	commits  int
	latest   time.Time
	subject  string
	authorID string
	latestID string
}

// Latest author and subject always describe the same canonical commit. Stable
// object-ID ties keep the row and evidence pane in agreement.
func (a *repositoryActivity) observe(r git.CommitRecord) {
	if a.latest.IsZero() || r.Date.After(a.latest) || (r.Date.Equal(a.latest) && r.CommitID < a.latestID) {
		a.latest, a.subject, a.authorID, a.latestID = r.Date, r.Subject, stats.IdentityID(r), r.CommitID
	}
}

func (m Model) activityRepositories() []repositoryActivity {
	if m.areaRepoID != "" {
		return m.activityAreas()
	}
	var result []repositoryActivity
	records := m.filteredRecords()
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			continue
		}
		activity := repositoryActivity{repo: repo}
		var kept []git.CommitRecord
		for _, r := range m.recordsInRepository(repo, records) {
			if !visible[stats.IdentityID(r)] {
				continue
			}
			kept = append(kept, r)
			activity.commits++
			activity.observe(r)
		}
		activity.people = stats.AggregateWithOptions(kept, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		sortPeople(activity.people)
		result = append(result, activity)
	}
	sortRepositoryActivities(result, m.overviewSort)
	return result
}

// Activity is volume, not priority or a ranking of people. Ties are stable.
func sortRepositoryActivities(rows []repositoryActivity, mode int) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch mode {
		case 0:
			if a.commits != b.commits {
				return a.commits > b.commits
			}
		case 1:
			if !a.latest.Equal(b.latest) {
				return a.latest.After(b.latest)
			}
		}
		if a.repo.Name != b.repo.Name {
			return a.repo.Name < b.repo.Name
		}
		return a.repo.ID < b.repo.ID
	})
}

func sortPeople(people []stats.AuthorStats) {
	sort.Slice(people, func(i, j int) bool {
		if people[i].Name != people[j].Name {
			return people[i].Name < people[j].Name
		}
		return people[i].ID < people[j].ID
	})
}

func (m *Model) selectedRepository(rows []repositoryActivity) int {
	for i, row := range rows {
		if row.repo.ID == m.selectedScopeID() {
			return i
		}
	}
	if len(rows) > 0 {
		m.setSelectedScopeID(rows[0].repo.ID)
	}
	return 0
}

// evidence preserves all repository associations while selecting one canonical
// identity per object ID. A copied commit must not become two different people.
func (m Model) evidence(repoName, personID string) []git.CommitRecord {
	records := m.filteredRecords()
	if m.areaRepoID != "" {
		records = nil
		for _, area := range m.currentWorkAreas() {
			if area.ID == repoName {
				records = area.Records
				break
			}
		}
		repoName = ""
	}
	inRepository := make(map[string]bool)
	for _, r := range records {
		if r.RepoName == repoName && r.CommitID != "" {
			inRepository[r.CommitID] = true
		}
	}
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	var out []git.CommitRecord
	for _, r := range stats.UniqueRecords(records) {
		if !visible[stats.IdentityID(r)] || (personID != "" && stats.IdentityID(r) != personID) {
			continue
		}
		if repoName != "" && !inRepository[r.CommitID] && (r.CommitID != "" || r.RepoName != repoName) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].CommitID < out[j].CommitID
	})
	return out
}

func (m *Model) normalizeAwarenessFocus() {
	if m.personID == "" {
		return
	}
	for _, a := range m.authors {
		if a.ID == m.personID {
			return
		}
	}
	m.personID = ""
	m.evidenceOffset = 0
	m.showPaths = false
	m.pathOffset = 0
}
