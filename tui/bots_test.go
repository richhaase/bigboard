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
	m.allRecords = []git.CommitRecord{
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

func TestStepOperativeKeepsPositionWhenActiveDropsOut(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "v", DefaultTimeIndex, Options{})
	m.authors = []stats.AuthorStats{
		{Name: "First", TotalChange: 30},
		{Name: "Second", TotalChange: 20},
		{Name: "Third", TotalChange: 10},
	}
	m.activeOperative = "Vanished"
	m.selectedRow = 1
	m.stepOperative(1)
	if m.activeOperative != "Second" {
		t.Errorf("expected position retained at Second, got %q", m.activeOperative)
	}

	m.activeOperative = "Second"
	m.stepOperative(1)
	if m.activeOperative != "Third" {
		t.Errorf("expected step to Third, got %q", m.activeOperative)
	}
}

func TestFillMonthGaps(t *testing.T) {
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	apr := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	months := fillMonthGaps([]MonthActivity{
		{Month: jan, Commits: 2},
		{Month: apr, Commits: 3},
	})
	if len(months) != 4 {
		t.Fatalf("expected 4 months (gaps filled), got %d: %+v", len(months), months)
	}
	if months[1].Month.Month() != time.February || months[1].Commits != 0 {
		t.Errorf("expected empty February, got %+v", months[1])
	}
	if months[2].Month.Month() != time.March || months[2].Commits != 0 {
		t.Errorf("expected empty March, got %+v", months[2])
	}
	if months[3].Commits != 3 {
		t.Errorf("expected April data preserved, got %+v", months[3])
	}

	single := fillMonthGaps([]MonthActivity{{Month: jan, Commits: 1}})
	if len(single) != 1 {
		t.Errorf("single month should be unchanged, got %d", len(single))
	}
	if got := fillMonthGaps(nil); len(got) != 0 {
		t.Errorf("nil input should stay empty, got %d", len(got))
	}
}
