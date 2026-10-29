package gui

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"
)

func demoSnapshot(now time.Time, settings Settings) Snapshot {
	result := emptySnapshot(settings, "")
	result.Mode = "demo"
	result.GeneratedAt = now.Format(time.RFC3339)
	result.Viewer = "mira"
	result.Status.Message = "Synthetic example workspace. No GitHub requests or repository changes."
	names := []string{"northstar/atlas", "northstar/console", "northstar/relay", "northstar/docs", "northstar/toolkit", "northstar/archive"}
	people := []struct{ name, login string }{{"Mira Chen", "mira"}, {"Theo Park", "theo"}, {"Inez Flores", "inez"}, {"Jules Rowan", "jules"}, {"Sam Okafor", "sam"}, {"Dependency bot", "dependencies[bot]"}}
	paths := [][]string{
		{"src/engine/planner.go", "src/engine/worker.go", "src/api/routes.go", "src/api/session.go", "tests/planner_test.go", "README.md"},
		{"src/components/Activity.tsx", "src/components/Filters.tsx", "src/views/Overview.tsx", "src/styles/tokens.css", "tests/navigation.test.ts", "README.md"},
		{"internal/queue/consumer.go", "internal/queue/retry.go", "internal/events/schema.go", "cmd/relay/main.go", "tests/delivery_test.go", "README.md"},
		{"guides/getting-started.md", "guides/architecture.md", "reference/configuration.md", "reference/api.md", "examples/quickstart.md", "README.md"},
		{"src/format.ts", "src/date.ts", "src/identity.ts", "tests/format.test.ts", "tests/date.test.ts", "README.md"},
	}
	subjects := []string{"Preserve selection across refresh", "Add bounded retry for interrupted requests", "Improve empty state guidance", "Separate refresh status from cached evidence", "Add coverage for partial responses", "Simplify directory traversal", "Fix date boundary handling", "Document local setup", "Reduce redundant object reads", "Normalize contributor identities", "Keep focus after closing filters", "Update dependency lockfile"}
	for i, id := range names {
		result.Repositories = append(result.Repositories, Repository{ID: id, Name: id, Branch: "main", Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:40]})
		if i == len(names)-1 {
			continue
		}
		for day := 0; day < 90; day++ {
			count := 1 + (day*3+i*7)%4
			if day%7 == 5 || day%7 == 6 {
				count = day % 2
			}
			for n := 0; n < count; n++ {
				person := people[(day+i+n*2)%len(people)]
				added := 17 + (day*29+i*83+n*13)%420
				removed := 3 + (day*7+i*31+n*19)%125
				first := paths[i][(day+n)%len(paths[i])]
				second := paths[i][(day+n+2)%len(paths[i])]
				date := now.AddDate(0, 0, -day).Add(-time.Duration(n*37+i*11) * time.Minute)
				result.Commits = append(result.Commits, Commit{
					ID: fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", id, day, n))))[:40], Repo: id,
					Author: person.name, Email: person.login + "@example.test", Date: date.Format(time.RFC3339), Subject: subjects[(day+i+n)%len(subjects)],
					Added: added, Removed: removed, AI: (day+n+i)%4 == 0, Bot: person.login == "dependencies[bot]",
					Files: []File{{Path: first, Added: added * 2 / 3, Removed: removed * 2 / 3}, {Path: second, Added: added - added*2/3, Removed: removed - removed*2/3}},
				})
			}
		}
	}
	titles := []string{"Keep activity context when switching repositories", "Add delivery acknowledgements to the event stream", "Make custom date ranges keyboard accessible", "Consolidate contributor identity mapping", "Document the repository cache lifecycle", "Reduce duplicate requests during refresh", "Handle a canceled scan without losing history", "Add timeline selection and hover details", "Upgrade development dependencies", "Expose partial coverage in repository summaries", "Add accessible labels to chart controls", "Prototype repository comparison view"}
	reviews := []string{"REVIEW_REQUIRED", "CHANGES_REQUESTED", "APPROVED", "REVIEW_REQUIRED", "APPROVED", "UNKNOWN"}
	checks := []string{"SUCCESS", "FAILURE", "SUCCESS", "PENDING", "SUCCESS", "UNKNOWN"}
	for i, title := range titles {
		repo := i % 5
		reviewers := []string{people[(i+2)%5].login}
		if i%3 == 0 {
			reviewers = append(reviewers, "mira")
		}
		result.PullRequests = append(result.PullRequests, PR{Repo: names[repo], Number: 142 + i*7, Title: title, Author: people[i%len(people)].login, Review: reviews[i%len(reviews)], Checks: checks[i%len(checks)], UpdatedAt: now.Add(-time.Duration(i*i+1) * time.Hour).Format(time.RFC3339), Draft: i == 7 || i == 11, Complete: i != 5, Files: []string{paths[repo][i%6], paths[repo][(i+1)%6]}, Reviewers: reviewers})
	}
	if len(settings.Repositories) > 0 {
		result.Repositories = slices.DeleteFunc(result.Repositories, func(repo Repository) bool { return !slices.Contains(settings.Repositories, repo.ID) })
		result.Commits = slices.DeleteFunc(result.Commits, func(commit Commit) bool { return !slices.Contains(settings.Repositories, commit.Repo) })
		result.PullRequests = slices.DeleteFunc(result.PullRequests, func(pr PR) bool { return !slices.Contains(settings.Repositories, pr.Repo) })
	}
	return result
}
