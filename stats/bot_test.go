package stats_test

import (
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestIsBotIdentity(t *testing.T) {
	cases := []struct {
		name  string
		email string
		extra []string
		want  bool
	}{
		{"dependabot[bot]", "49699333+dependabot[bot]@users.noreply.github.com", nil, true},
		{"renovate[bot]", "29139614+renovate[bot]@users.noreply.github.com", nil, true},
		{"Renovate Bot", "bot@renovateapp.com", nil, true},
		{"github-actions[bot]", "41898282+github-actions[bot]@users.noreply.github.com", nil, true},
		{"snyk-bot", "snyk-bot@snyk.io", nil, true},
		{"Rich Haase", "rich@example.com", nil, false},
		{"Robotics Lab", "robots@example.com", nil, false},
		{"Tag Worker", "tag-1@teamsense.com", []string{"tag-1@teamsense.com"}, true},
		{"Tag Worker", "tag-2@agents.teamsense.com", []string{"@agents.teamsense.com"}, true},
		{"Fleet Runner", "fr@example.com", []string{"fleet runner"}, true},
		{"Fleet Runner", "fr@example.com", []string{"other"}, false},
	}
	for _, tc := range cases {
		if got := stats.IsBotIdentity(tc.name, tc.email, tc.extra); got != tc.want {
			t.Errorf("IsBotIdentity(%q, %q, %v) = %v, want %v", tc.name, tc.email, tc.extra, got, tc.want)
		}
	}
}

func TestAggregateFlagsBots(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "dependabot[bot]", Email: "49699333+dependabot[bot]@users.noreply.github.com", Date: now, Added: 10, RepoID: "/r", RepoName: "r"},
		{Author: "Alice", Email: "alice@example.com", Date: now, Added: 5, RepoID: "/r", RepoName: "r"},
		{Author: "Tag Worker", Email: "tag-1@teamsense.com", Date: now, Added: 3, RepoID: "/r", RepoName: "r"},
	}
	authors := stats.AggregateWithOptions(records, stats.AggregateOptions{
		BotIdentities: []string{"tag-1@teamsense.com"},
	})
	if len(authors) != 3 {
		t.Fatalf("expected 3 authors, got %d", len(authors))
	}
	byName := map[string]stats.AuthorStats{}
	for _, a := range authors {
		byName[a.Name] = a
	}
	if !byName["dependabot[bot]"].Bot {
		t.Error("dependabot should be flagged as bot")
	}
	if byName["Alice"].Bot {
		t.Error("Alice should not be flagged as bot")
	}
	if !byName["Tag Worker"].Bot {
		t.Error("configured bot identity should be flagged as bot")
	}
}
