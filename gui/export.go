package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func (s *Service) ExportJSON() (string, error) {
	s.mu.Lock()
	snapshot := cloneSnapshot(s.snapshot)
	botIdentities := slices.Clone(s.analysis.BotIdentities)
	sortField := stats.SortFieldFromString(s.analysis.Sort)
	s.mu.Unlock()
	if len(snapshot.Repositories) == 0 {
		return "", errors.New("load repository history before exporting")
	}
	names := make(map[string]string, len(snapshot.Repositories))
	for _, repo := range snapshot.Repositories {
		names[repo.ID] = repo.Name
	}
	records := make([]git.CommitRecord, 0, len(snapshot.Commits))
	for _, commit := range snapshot.Commits {
		name, ok := names[commit.Repo]
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, commit.Date)
		if err != nil {
			return "", fmt.Errorf("cannot export a commit with invalid date: %w", err)
		}
		records = append(records, git.CommitRecord{CommitID: commit.ID, RepoID: commit.Repo, RepoName: name, Author: commit.Author, Email: commit.Email, Date: at, Added: commit.Added, Removed: commit.Removed, AIAssisted: commit.AI, LinesUnknown: commit.LinesUnknown})
	}
	authors := stats.AggregateWithOptions(records, stats.AggregateOptions{BotIdentities: botIdentities})
	stats.Sort(authors, sortField)
	data, err := json.MarshalIndent(authors, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
