package tui

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

type filterValues []string

func (values filterValues) clone() filterValues {
	return slices.Clone(values)
}

func (values filterValues) includes(value string) bool {
	return len(values) == 0 || slices.Contains(values, value)
}

func (values filterValues) toggle(value string) filterValues {
	if value == "" {
		return nil
	}
	result := values.clone()
	if index := slices.Index(result, value); index >= 0 {
		return slices.Delete(result, index, index+1)
	}
	return append(result, value)
}

func (m Model) repoInView(id string) bool {
	return !m.excludedRepos[id] && m.sharedRepos.includes(id)
}

func (m Model) filterSharedRecords(records []git.CommitRecord) []git.CommitRecord {
	if len(m.sharedRepos) == 0 {
		return records
	}
	var result []git.CommitRecord
	for _, record := range records {
		included := record.RepoID != "" && m.repoInView(record.RepoID)
		if record.RepoID == "" {
			included = m.repoInView(record.RepoName)
			for _, repo := range m.loadedRepos {
				if repo.Name == record.RepoName && m.repoInView(repo.ID) {
					included = true
					break
				}
			}
		}
		if included {
			result = append(result, record)
		}
	}
	return result
}

func (m Model) activityFilterRecords(records []git.CommitRecord) []git.CommitRecord {
	people := stats.AggregateWithOptions(records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
	visible := make(map[string]bool)
	for _, person := range people {
		if m.activityPeople.includes(person.ID) && (!m.activityHideBots || !person.Bot) {
			visible[person.ID] = true
		}
	}
	canonical := make(map[string]string)
	for _, record := range stats.UniqueRecords(records) {
		if record.CommitID != "" {
			canonical[record.CommitID] = stats.IdentityID(record)
		}
	}
	var result []git.CommitRecord
	for _, record := range records {
		id := stats.IdentityID(record)
		if record.CommitID != "" {
			id = canonical[record.CommitID]
		}
		if visible[id] {
			result = append(result, record)
		}
	}
	return result
}

func (m Model) activityRecords() []git.CommitRecord {
	return m.activityFilterRecords(m.filteredRecords())
}

func (m Model) activityPeopleOptions() []prFilterOption {
	people := stats.AggregateWithOptions(m.filteredRecords(), stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
	labels := make(map[string]string)
	for _, person := range people {
		label := glancePersonLabel(person, people)
		if person.Bot {
			label += " [bot]"
		}
		labels[person.ID] = label
	}
	for _, id := range m.activityPeople {
		if _, exists := labels[id]; !exists {
			labels[id] = displayText(strings.TrimPrefix(id, "email:")) + " (not in range)"
		}
	}
	options := []prFilterOption{{"", "All contributors"}}
	for value, label := range labels {
		options = append(options, prFilterOption{value, label})
	}
	sort.Slice(options[1:], func(i, j int) bool {
		a, b := options[i+1], options[j+1]
		if a.label != b.label {
			return a.label < b.label
		}
		return a.value < b.value
	})
	return options
}

func (m Model) prsInViewForRepository(id string) []gh.PullRequest {
	if !m.repoInView(id) {
		return nil
	}
	duration := m.rangeDuration()
	cutoff := time.Now().Add(-duration)
	var result []gh.PullRequest
	for _, pr := range m.prsForRepository(id) {
		if duration == 0 || pr.UpdatedAt.After(cutoff) {
			result = append(result, pr)
		}
	}
	return result
}

func (m Model) sharedPRs() []gh.PullRequest {
	unique := make(map[string]gh.PullRequest)
	for _, repo := range m.loadedRepos {
		for _, pr := range m.prsInViewForRepository(repo.ID) {
			unique[pr.Key()] = pr
		}
	}
	result := make([]gh.PullRequest, 0, len(unique))
	for _, pr := range unique {
		result = append(result, pr)
	}
	return result
}

func (m Model) prTouchesLiteralScope(pr gh.PullRequest, scope string) bool {
	if scope == "dir:" {
		return true
	}
	if scope == "unknown" {
		return !pr.FilesComplete
	}
	kind, path, _ := strings.Cut(scope, ":")
	counted := false
	for _, file := range pr.Files {
		if !m.options.IncludeGenerated && git.IsGeneratedPath(file.Path) {
			continue
		}
		counted = true
		if kind == "file" && file.Path == path || kind == "dir" && strings.HasPrefix(file.Path, path+"/") {
			return true
		}
	}
	return scope == "empty" && pr.FilesComplete && !counted
}
