package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestBookmarkAgeMarkersRequireKnownStrictlyNewerTime(t *testing.T) {
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := prFixture()
	m.options.Bookmark = &BookmarkOptions{Time: at}
	m.local.scannedAt["/api"] = at.Add(2 * time.Hour)
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.checked = at.Add(2 * time.Hour)
	m.prState.snapshots["org/repo"] = snapshot
	for _, tc := range []struct {
		name string
		when time.Time
		want string
	}{
		{"before", at.Add(-time.Hour), "3h"},
		{"equal", at, "2h"},
		{"after", at.Add(time.Hour), "+1h"},
		{"future", at.Add(3 * time.Hour), "+future"},
		{"unknown", time.Time{}, "—"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitAge := ansi.Strip(m.worklistAge(tc.when, "/api"))
			prAge := ansi.Strip(m.prUpdatedAge(gh.PullRequest{Repo: "org/repo", UpdatedAt: tc.when}))
			if gitAge != tc.want || prAge != tc.want {
				t.Fatalf("age markers disagree: Git=%q PR=%q want=%q", gitAge, prAge, tc.want)
			}
		})
	}
	for _, bookmark := range []*BookmarkOptions{nil, {}} {
		m.options.Bookmark = bookmark
		if strings.Contains(m.worklistAge(at.Add(time.Hour), "/api"), "+") || m.bookmarkMarkerLegend() != "" {
			t.Fatal("absent bookmark generated markers or a legend")
		}
	}
}

func TestBookmarkActivityMarkersFollowRepositoryDirectoryAndFile(t *testing.T) {
	m := directoryFixture()
	now := time.Now().Truncate(time.Second)
	for i := range m.local.records {
		m.local.records[i].Date = now.Add(-2 * time.Hour)
	}
	m.local.scannedAt["/mono"] = now
	m.recomputeAuthors()
	for _, scope := range []string{"repository", "directory", "file"} {
		t.Run(scope, func(t *testing.T) {
			current := m
			row := current.glanceRepositories()[0]
			if scope != "repository" {
				current.openDirectoryRepository("/mono")
				if scope == "file" {
					current.directory.frame.path = "services/auth"
					row = directoryRow(t, current, "file:services/auth/session.go")
				} else {
					row = directoryRow(t, current, "dir:services")
				}
			}
			before := row
			current.options.Bookmark = &BookmarkOptions{Time: now.Add(-3 * time.Hour)}
			for _, width := range []int{40, 80, 120, 180} {
				lines := current.worklistRepositoryRow(row, true, width-4, 1, 1000)
				if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "+2h") {
					t.Fatalf("%s at %d columns lost the latest marker", scope, width)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("%s marker overflowed %d columns", scope, width)
					}
				}
			}
			if !reflect.DeepEqual(before, row) {
				t.Fatal("marker rendering changed activity evidence")
			}
		})
	}
}

func TestBookmarkPRMarkersPreserveRowsAndPreviewBudget(t *testing.T) {
	for _, width := range []int{40, 70, 80, 110, 120, 180} {
		m := prFixture()
		m.width, m.height, m.showPRs = width, 24, true
		snapshot := m.prState.snapshots["org/repo"]
		pr := snapshot.prs[0]
		pr.UpdatedAt = snapshot.checked.Add(-3 * time.Hour)
		pr.Title = strings.Repeat("界 long title ", 40)
		pr.Author = strings.Repeat("a", 39)
		pr.Draft, pr.ReviewDecision, pr.CheckState = true, "CHANGES_REQUESTED", "FAILURE"
		snapshot.prs[0] = pr
		m.prState.snapshots["org/repo"] = snapshot
		m.options.Bookmark = &BookmarkOptions{Time: snapshot.checked.Add(-4 * time.Hour)}
		rows := m.prTableRow(pr, "repo", true, 0, width)
		text := ansi.Strip(strings.Join(rows, "\n"))
		for _, want := range []string{"+3h", "◇", "Changes req.", "Failing"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d lost %q: %s", width, want, text)
			}
		}
		view := strings.Split(m.View(), "\n")
		if len(view) != m.height {
			t.Fatalf("width %d preview changed board height", width)
		}
		for _, line := range view {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d preview overflowed", width)
			}
		}
		preview := ansi.Strip(strings.Join(m.prSelectedPreview([]gh.PullRequest{pr}, width), "\n"))
		if width >= 70 && !strings.Contains(preview, "+ updated since last look") {
			t.Fatalf("width %d preview omitted new activity explanation", width)
		}
		detail := ansi.Strip(strings.Join(m.prDetailLines(pr, width), "\n"))
		if !strings.Contains(detail, "+ updated since last look") || !strings.Contains(ansi.Strip(strings.Join(m.helpLines(80), "\n")), "saved bookmark") {
			t.Fatal("PR details or legend omitted bookmark meaning")
		}
	}
}
