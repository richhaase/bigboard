package tui

import (
	"sort"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// Repository associations describe Git history, not live presence. Counts and
// identities use the same canonical records as the statistics pipeline.
type activityRow struct {
	scopeID      string
	label        string
	repositoryID string
	people       []stats.AuthorStats
	commits      int
	latest       time.Time
	subject      string
	authorID     string
	latestID     string
}

// Latest author and subject always describe the same canonical commit. Stable
// object-ID ties keep the row and evidence pane in agreement.
func (a *activityRow) observe(r git.CommitRecord) {
	if a.latest.IsZero() || r.Date.After(a.latest) || (r.Date.Equal(a.latest) && r.CommitID < a.latestID) {
		a.latest, a.subject, a.authorID, a.latestID = r.Date, r.Subject, stats.IdentityID(r), r.CommitID
	}
}

func (m Model) activityRepositories() []activityRow {
	if m.areaRepoID != "" {
		return m.activityAreas()
	}
	var result []activityRow
	records := m.activityRecords()
	selection := stats.NewRecordSelection(records)
	for _, repo := range m.local.repositories {
		if !m.repoInView(repo.ID) {
			continue
		}
		activity := activityRow{scopeID: repo.ID, label: repo.Name, repositoryID: repo.ID}
		var kept []git.CommitRecord
		for _, r := range selection.MatchingCopy(func(record git.CommitRecord) bool {
			return record.RepoID == repo.ID || record.RepoID == "" && record.RepoName == repo.Name
		}) {
			kept = append(kept, r)
			activity.commits++
			activity.observe(r)
		}
		activity.people = stats.AggregateWithOptions(kept, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
		sortPeople(activity.people)
		result = append(result, activity)
	}
	sortChangeActivities(result, m.repositorySort)
	return result
}

func sortChangeActivities(rows []activityRow, mode int) {
	if mode == 0 {
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			aa, ar, _ := repositoryLineTotals(a)
			ba, br, _ := repositoryLineTotals(b)
			if aa+ar != ba+br {
				return aa+ar > ba+br
			}
			if a.commits != b.commits {
				return a.commits > b.commits
			}
			if a.label != b.label {
				return a.label < b.label
			}
			return a.scopeID < b.scopeID
		})
	} else {
		sortRepositoryActivities(rows, mode-1)
	}
}

// Activity is volume, not priority or a ranking of people. Ties are stable.
func sortRepositoryActivities(rows []activityRow, mode int) {
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
		if a.label != b.label {
			return a.label < b.label
		}
		return a.scopeID < b.scopeID
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

func (m *Model) selectedRepository(rows []activityRow) int {
	for i, row := range rows {
		if row.scopeID == m.selectedScopeID() {
			return i
		}
	}
	if len(rows) > 0 {
		m.setSelectedScopeID(rows[0].scopeID)
	}
	return 0
}

// evidence preserves all repository associations while selecting one canonical
// identity per object ID. A copied commit must not become two different people.
func (m Model) evidence(repoName, personID string) []git.CommitRecord {
	records := m.activityRecords()
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
	if repoName == "" {
		records = stats.UniqueRecords(records)
	} else {
		records = stats.UniqueRecordsWithMatchingCopy(records, func(record git.CommitRecord) bool {
			return record.RepoName == repoName
		})
	}
	var out []git.CommitRecord
	for _, r := range records {
		if personID != "" && stats.IdentityID(r) != personID {
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
	for _, record := range stats.UniqueRecords(m.activityRecords()) {
		if stats.IdentityID(record) == m.personID {
			return
		}
	}
	m.showPaths = false
	m.pathOffset = 0
}
