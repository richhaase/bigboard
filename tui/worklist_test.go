package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func TestWorklistLatestAuthorSubjectTie(t *testing.T) {
	m := monorepoFixture()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	m.allRecords = []git.CommitRecord{
		{CommitID: "z", Author: "Zed", Email: "zed@x", RepoID: "/mono", RepoName: "mono", Date: at, Subject: "Second tied subject", Changes: []git.PathChange{{Path: "feature/z.go"}}},
		{CommitID: "a", Author: "Ada", Email: "ada@x", RepoID: "/mono", RepoName: "mono", Date: at, Subject: "First tied subject", Changes: []git.PathChange{{Path: "feature/a.go"}}},
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	for _, areaScope := range []bool{true, false} {
		if !areaScope {
			m.areaRepoID = ""
		}
		rows := m.activityRepositories()
		if len(rows) != 1 {
			t.Fatalf("unexpected rows %+v", rows)
		}
		r := rows[0]
		if r.subject != "First tied subject" || worklistLastAuthor(r) != "Ada" || r.latestID != "a" {
			t.Fatalf("mixed latest evidence: %+v", r)
		}
		if got := worklistAuthorCell(r, 18); !strings.Contains(got, "Ada +1") {
			t.Fatalf("other contributor count wrong: %q", got)
		}
	}
}

func TestWorklistAgeUsesOwnScanAndHandlesFuture(t *testing.T) {
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	m := Model{scannedAt: map[string]time.Time{"a": at, "b": at.Add(2 * time.Hour)}}
	for _, tc := range []struct {
		repo   string
		commit time.Time
		want   string
	}{
		{"a", at.Add(-30 * time.Minute), "30m"}, {"b", at.Add(-30 * time.Minute), "2h"}, {"a", at.Add(time.Minute), "future"}, {"a", time.Time{}, "—"},
	} {
		if got := m.worklistAge(tc.commit, tc.repo); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}

func TestWorklistFreshnessHonestAndScoped(t *testing.T) {
	m := prFixture()
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	m.prState.snapshots["org/repo"] = prSnapshot{prs: []gh.PullRequest{{Repo: "org/repo", Number: 7}}, checked: at.Add(2 * time.Hour), lastGood: at, partial: true, err: errors.New("offline")}
	got := m.worklistPRFreshness("/api")
	for _, want := range []string{"STALE/PARTIAL", "all dates", "checked", "last complete"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s: %s", want, got)
		}
	}
	m.prState.snapshots["org/repo"] = prSnapshot{checked: at, partial: true, err: errors.New("offline")}
	if got = m.worklistPRFreshness("/api"); !strings.Contains(got, "unavailable") || strings.Contains(got, "complete") {
		t.Fatal(got)
	}
	m.prState.snapshots = map[string]prSnapshot{}
	if got = m.worklistPRFreshness("/api"); !strings.Contains(got, "unknown") {
		t.Fatal(got)
	}
}

func TestWorklistLongStaleNamesReserveWarningAndCount(t *testing.T) {
	m := monorepoFixture()
	m.staleRepos["/mono"] = true
	r := repositoryActivity{repo: git.Repository{ID: "auto:feature", Name: strings.Repeat("界a", 40)}, authorID: "email:0@x", subject: strings.Repeat("subject ", 30), commits: 120}
	for i := 0; i < 120; i++ {
		r.people = append(r.people, stats.AuthorStats{ID: fmt.Sprintf("email:%d@x", i), Name: strings.Repeat("界a", 30), Commits: 1})
	}
	for _, width := range []int{36, 76, 116, 136} {
		lines := m.worklistRow(r, true, width, width >= 106)
		text := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(text, "[STALE]") || !strings.Contains(text, "+119") {
			t.Fatalf("width%d missing warning/count: %s", width, text)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width+2 {
				t.Fatalf("cell overflow: %q", line)
			}
		}
	}
}

func TestWorklistSingleFocusRoundTripResizeAndNoColor(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		m := glanceBusyFixture()
		for _, size := range [][2]int{{140, 40}, {120, 36}, {80, 30}, {40, 18}} {
			m.width, m.height = size[0], size[1]
			m = pressAwareness(m, "G")
			id := m.selectedAreaID
			before := ansi.Strip(m.View())
			m = pressAwareness(m, "enter")
			for _, key := range []string{"G", "2", "G", "3", "4", "1"} {
				m = pressAwareness(m, key)
			}
			m = pressAwareness(m, "esc")
			after := ansi.Strip(m.View())
			if m.selectedAreaID != id || after != before {
				t.Fatalf("%v roundtrip changed list/selection", size)
			}
			cursorCount := 0
			for _, line := range strings.Split(m.View(), "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%v width overflow", size)
				}
				if strings.HasPrefix(strings.TrimSpace(ansi.Strip(line)), "› ") {
					cursorCount++
				}
			}
			if cursorCount != 1 {
				t.Fatalf("%v has %d active cursors", size, cursorCount)
			}
			if lipgloss.Height(m.View()) > m.height {
				t.Fatal("height overflow")
			}
			if profile == termenv.Ascii && strings.Contains(m.View(), "\x1b[") {
				t.Fatal("NO_COLOR leaked ANSI")
			}
		}
	}
}

func TestWorklistPRScopeIndependentOfPersonFilter(t *testing.T) {
	m := prFixture()
	m.width, m.height = 120, 36
	m.areaRepoID, m.selectedAreaID = "/api", "auto:feature"
	m = pressAwareness(m, "enter")
	m.personID = "email:nonexistent@x"
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "#7 Add feature") || !strings.Contains(view, "all dates") || !strings.Contains(view, "No matching commits") {
		t.Fatalf("PR-only evidence lost or filtered:\n%s", view)
	}
}

func TestWorklistFailureColorSurvivesTruncation(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	red, _, _ := strings.Cut(worklistSignalStyle("FAILURE").Render("X"), "X")
	m := prFixture()
	m.areaRepoID = "/api"
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.err = errors.New("offline")
	snapshot.partial = true
	for i := range snapshot.prs {
		snapshot.prs[i].CheckState = "FAILURE"
	}
	m.prState.snapshots["org/repo"] = snapshot
	r := repositoryActivity{repo: git.Repository{ID: "auto:feature", Name: "Feature"}}
	for _, width := range []int{76, 116, 136} {
		rendered := strings.Join(m.worklistRow(r, true, width, width >= 106), "\n")
		if red == "" || !strings.Contains(rendered, red) {
			t.Fatalf("failure lost red after truncation at%d: %q", width, rendered)
		}
	}
	if worklistSignalStyle("UNKNOWN").GetForeground() != StyleAmber.GetForeground() {
		t.Fatal("unknown check state lost warning color")
	}
	// A title mentioning errors cannot override factual check success.
	if worklistSignalStyle("SUCCESS").GetForeground() != StyleDimWhite.GetForeground() {
		t.Fatal("success state has warning color")
	}
}

func TestWorklistCommitMetadataHierarchy(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, lens := range []string{"overview", "activity", "related"} {
			t.Run(fmt.Sprintf("%s/%d", lens, width), func(t *testing.T) {
				m := monorepoFixture()
				m.width, m.height = width, 36
				m.selectedAreaID = "auto:services/auth"
				if lens == "activity" || (lens == "overview" && width < 110) {
					m = pressAwareness(m, "enter")
				} else if lens == "related" {
					m = pressAwareness(m, "3")
				}
				lines := strings.Split(ansi.Strip(m.View()), "\n")
				found := false
				for i, line := range lines {
					if !strings.Contains(line, "Renew sessions") {
						continue
					}
					if i+2 >= len(lines) || !strings.Contains(lines[i+2], "Changed areas:") {
						continue // A list row may also contain the title.
					}
					found = true
					titleColumn := ansi.StringWidth(line[:strings.Index(line, "Renew sessions")])
					for _, metadata := range lines[i+1 : i+3] {
						indent := len(metadata) - len(strings.TrimLeft(metadata, " "))
						if indent != titleColumn+2 {
							t.Fatalf("metadata indent %d; title starts at %d: %q", indent, titleColumn, metadata)
						}
					}
				}
				if !found && (lens != "related" || width != 40) {
					t.Fatal("commit preview not found")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("line exceeds %d columns: %q", width, line)
					}
				}
			})
		}
	}
}
