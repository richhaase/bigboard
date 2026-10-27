package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestRenderTableBotMarker(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "dependabot[bot]", Bot: true, Commits: 50, TotalChange: 100},
		{Name: "Ada Lovelace", Commits: 10, TotalChange: 50},
	}
	out := AggregateView{}.RenderTable(authors, TableState{VisibleRows: 2, Width: 120, SortField: stats.SortByTotal})
	if !strings.Contains(out, "BOT") {
		t.Errorf("bot contributor should carry a BOT marker:\n%s", out)
	}
}

func TestRenderTableSubOnePercentAI(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "Ada", Commits: 300, AICommits: 1, TotalChange: 100},
	}
	out := AggregateView{}.RenderTable(authors, TableState{VisibleRows: 1, Width: 120, SortField: stats.SortByTotal})
	if !strings.Contains(out, "<1%") {
		t.Errorf("small nonzero AI share should render <1%%:\n%s", out)
	}
	if strings.Contains(out, " 0%") {
		t.Errorf("small nonzero AI share must not render 0%%:\n%s", out)
	}
}

func TestRenderTableRankFollowsMetricWhenAscending(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "Low", Commits: 1, TotalChange: 10},
		{Name: "Mid", Commits: 2, TotalChange: 20},
		{Name: "Top", Commits: 3, TotalChange: 30},
	}
	out := AggregateView{}.RenderTable(authors, TableState{VisibleRows: 3, Width: 120, SortField: stats.SortByTotal, SortAsc: true})
	lowLine, topLine := -1, -1
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Low") {
			lowLine = i
		}
		if strings.Contains(line, "Top") {
			topLine = i
		}
	}
	if lowLine == -1 || topLine == -1 {
		t.Fatalf("missing rows:\n%s", out)
	}
	if !strings.Contains(lines[lowLine], "03") {
		t.Errorf("ascending: lowest contributor should keep rank 03: %q", lines[lowLine])
	}
	if !strings.Contains(lines[topLine], "01") {
		t.Errorf("ascending: top contributor should keep rank 01: %q", lines[topLine])
	}
}

func TestModelBotToggle(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "v", DefaultTimeIndex, Options{
		BotIdentities: []string{"tag@teamsense.com"},
	})
	now := time.Now()
	m.local.records = []git.CommitRecord{
		{Author: "Ada", Email: "ada@example.com", Date: now, Added: 5, RepoID: "/r", RepoName: "r"},
		{Author: "renovate[bot]", Email: "29139614+renovate[bot]@users.noreply.github.com", Date: now, Added: 100, RepoID: "/r", RepoName: "r"},
		{Author: "Tag", Email: "tag@teamsense.com", Date: now, Added: 7, RepoID: "/r", RepoName: "r"},
	}
	m.recomputeAuthors()
	if len(m.authors) != 3 {
		t.Fatalf("bots should be counted by default, got %d authors", len(m.authors))
	}

	m.hideBots = true
	m.recomputeAuthors()
	if len(m.authors) != 1 || m.authors[0].Name != "Ada" {
		t.Fatalf("hideBots should leave only humans, got %+v", m.authors)
	}

	m.hideBots = false
	m.recomputeAuthors()
	if len(m.authors) != 3 {
		t.Fatalf("re-enabling bots should restore them, got %d authors", len(m.authors))
	}
}

func TestContributorScrollKeepsMissingIdentity(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "v", DefaultTimeIndex, Options{})
	m.viewMode = ViewOperative
	m.authors = []stats.AuthorStats{{ID: "email:first@example.test", Name: "First", TotalChange: 30}}
	m.activeOperative, m.activeAuthorID = "Vanished", "email:vanished@example.test"
	m = keyboardContractPress(t, m, "down", "pgdown", "up")
	if m.activeOperative != "Vanished" || m.activeAuthorID != "email:vanished@example.test" {
		t.Fatal("scrolling silently selected a different contributor")
	}
}
