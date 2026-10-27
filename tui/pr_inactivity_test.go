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
	gh "github.com/richhaase/bigboard/github"
)

func TestPRInactivityUsesSnapshotCheckedTime(t *testing.T) {
	checked := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	for _, stale := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			updated time.Time
			want    string
		}{
			{"before boundary", checked.Add(-14*24*time.Hour + time.Nanosecond), "13d"},
			{"boundary", checked.Add(-14 * 24 * time.Hour), "!14d"},
			{"after boundary", checked.Add(-14*24*time.Hour - time.Nanosecond), "!14d"},
			{"older", checked.Add(-21 * 24 * time.Hour), "!21d"},
			{"unknown", time.Time{}, "—"},
			{"future", checked.Add(time.Nanosecond), "future"},
		} {
			t.Run(fmt.Sprintf("%s/stale=%v", tc.name, stale), func(t *testing.T) {
				m := prFixture()
				m.width, m.height = 80, 24
				snapshot := m.prState.snapshots["org/repo"]
				snapshot.checked = checked
				if stale {
					snapshot.partial = true
					snapshot.err = errors.New("refresh failed")
					snapshot.lastGood = checked.Add(-24 * time.Hour)
				}
				m.prState.snapshots["org/repo"] = snapshot
				pr := snapshot.prs[0]
				pr.UpdatedAt = tc.updated
				if got := ansi.Strip(m.prUpdatedAge(pr)); got != tc.want {
					t.Fatalf("age = %q, want %q", got, tc.want)
				}
				wantInactive := strings.HasPrefix(tc.want, "!")
				for label, lines := range map[string][]string{
					"preview": m.prSelectedPreview([]gh.PullRequest{pr}, m.width),
					"detail":  m.prDetailLines(pr, m.width),
				} {
					text := ansi.Strip(strings.Join(lines, "\n"))
					if strings.Contains(text, "! Inactive · no PR update for 14+ days") != wantInactive {
						t.Fatalf("%s disagrees with age %q: %s", label, tc.want, text)
					}
					if label == "detail" && stale && !strings.Contains(text, "STALE/PARTIAL snapshot") {
						t.Fatal("inactivity displaced snapshot coverage")
					}
				}
			})
		}
	}
}

func TestPRInactivityFallsBackToCurrentTimeWithoutSnapshot(t *testing.T) {
	m := prFixture()
	pr := gh.PullRequest{Repo: "org/missing", UpdatedAt: time.Now().Add(-15 * 24 * time.Hour)}
	if got := ansi.Strip(m.prUpdatedAge(pr)); got != "!15d" {
		t.Fatalf("age without snapshot = %q, want !15d", got)
	}
}

func TestPRInactivityAndBookmarkPreserveResponsiveLayout(t *testing.T) {
	for _, width := range []int{40, 70, 80, 110, 120, 180} {
		for _, browserError := range []string{"", "Could not open browser: unavailable"} {
			t.Run(fmt.Sprintf("width=%d/error=%v", width, browserError != ""), func(t *testing.T) {
				m := prFixture()
				m.width, m.height, m.showPRs = width, 24, true
				m.prBrowserError = browserError
				snapshot := m.prState.snapshots["org/repo"]
				pr := snapshot.prs[0]
				pr.UpdatedAt = snapshot.checked.Add(-14 * 24 * time.Hour)
				pr.Title = strings.Repeat("界 long title ", 100)
				pr.Author = strings.Repeat("a", 39)
				pr.Draft, pr.ReviewDecision, pr.CheckState = true, "CHANGES_REQUESTED", "FAILURE"
				snapshot.prs = nil
				for i := range 8 {
					row := pr
					row.Number += i
					snapshot.prs = append(snapshot.prs, row)
				}
				m.prState.snapshots["org/repo"] = snapshot
				m.options.Bookmark = &BookmarkOptions{Time: snapshot.checked.Add(-15 * 24 * time.Hour)}
				rows := m.prTableRow(pr, "repo", true, 0, width)
				if len(rows) != m.prListRowHeight() {
					t.Fatal("inactivity changed the row height")
				}
				text := ansi.Strip(strings.Join(rows, "\n"))
				for _, want := range []string{"+!14d", "◇", "Changes req.", "Failing"} {
					if !strings.Contains(text, want) {
						t.Fatalf("row lost %q: %s", want, text)
					}
				}
				view := strings.Split(m.View(), "\n")
				if len(view) != m.height {
					t.Fatalf("board height = %d, want %d", len(view), m.height)
				}
				for _, line := range append(rows, view...) {
					if ansi.StringWidth(line) > width {
						t.Fatalf("line overflowed %d columns: %s", width, ansi.Strip(line))
					}
				}
				if !strings.Contains(ansi.Strip(view[len(view)-1]), "! inactive ≥14d") {
					t.Fatal("coverage clipped the inactivity legend")
				}
				if browserError != "" && !strings.Contains(ansi.Strip(strings.Join(view, "\n")), browserError) {
					t.Fatal("inactivity displaced browser diagnostics")
				}
				preview := m.prSelectedPreview(m.visiblePRs(), width)
				if width < 70 {
					if len(preview) != 0 {
						t.Fatal("narrow terminal lost list space to preview")
					}
					return
				}
				if m.prListPageSize() < 3 {
					t.Fatal("inactivity reduced the list below three PRs")
				}
				previewText := ansi.Strip(strings.Join(preview, "\n"))
				visiblePreview := ansi.Strip(strings.Join(view[m.height-screenFooterRows-len(preview):m.height-screenFooterRows], "\n"))
				for _, want := range []string{"+ updated since last look", "! Inactive · no PR update for 14+ days", "Enter full detail", githubIdentity(pr.Author), "Review: Changes req. · Checks: Failing"} {
					if !strings.Contains(previewText, want) || !strings.Contains(visiblePreview, want) {
						t.Fatalf("preview lost %q: %s", want, visiblePreview)
					}
				}
			})
		}
	}
}

func TestPRInactivityKeepsStatusAndBookmarkColors(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	amber, _, _ := strings.Cut(StyleAmber.Render("!"), "!")
	cyan, _, _ := strings.Cut(StyleCyan.Render("+"), "+")
	m := prFixture()
	snapshot := m.prState.snapshots["org/repo"]
	pr := snapshot.prs[0]
	pr.UpdatedAt = snapshot.checked.Add(-14 * 24 * time.Hour)
	pr.ReviewDecision, pr.CheckState = "APPROVED", "SUCCESS"
	m.options.Bookmark = &BookmarkOptions{Time: snapshot.checked.Add(-15 * 24 * time.Hour)}
	for _, width := range []int{40, 70, 80, 110, 120, 180} {
		for _, selected := range []bool{false, true} {
			row := strings.Join(m.prTableRow(pr, "repo", selected, 0, width), "\n")
			if strings.Count(row, "\x1b[38;2;157;219;162m") < 2 {
				t.Fatal("inactivity replaced approved or passing colors")
			}
			if !strings.Contains(row, amber) || !strings.Contains(row, cyan) {
				t.Fatalf("inactivity and bookmark markers lost their distinct colors at width %d selected=%v: %q", width, selected, row)
			}
		}
	}
}

func TestPRDetailShowsKnownReviewSubmissionTimes(t *testing.T) {
	m := prFixture()
	pr := m.allPRs()[0]
	submitted := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	pr.Reviews = []gh.Review{{Author: "reviewer", State: "APPROVED", SubmittedAt: submitted}, {Author: "other", State: "COMMENTED"}}
	lines := m.prDetailLines(pr, 120)
	for _, want := range []string{"@reviewer · Approved · " + prDetailTime(submitted), "@other · Commented"} {
		found := false
		for _, line := range lines {
			found = found || ansi.Strip(line) == want
		}
		if !found {
			t.Fatalf("review detail missing %q", want)
		}
	}
}
