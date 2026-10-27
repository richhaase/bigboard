package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRUpdatedSinceReviewUsesLatestKnownSubmission(t *testing.T) {
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		login   string
		updated time.Time
		reviews []gh.Review
		want    bool
	}{
		{"later activity", "Viewer", at, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at.Add(-time.Hour)}}, true},
		{"comment review", "viewer", at, []gh.Review{{Author: "VIEWER", State: "COMMENTED", SubmittedAt: at.Add(-time.Second)}}, true},
		{"dismissed submission", "viewer", at, []gh.Review{{Author: "viewer", State: "DISMISSED", SubmittedAt: at.Add(-time.Hour)}}, true},
		{"unknown state with submission", "viewer", at, []gh.Review{{Author: "viewer", State: gh.Unknown, SubmittedAt: at.Add(-time.Hour)}}, true},
		{"same instant", "viewer", at, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at}}, false},
		{"review after update", "viewer", at, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at.Add(time.Hour)}}, false},
		{"missing submission", "viewer", at, []gh.Review{{Author: "viewer", State: "APPROVED"}}, false},
		{"pending is not submitted", "viewer", at, []gh.Review{{Author: "viewer", State: "PENDING", SubmittedAt: at.Add(-time.Hour)}}, false},
		{"other reviewer", "viewer", at, []gh.Review{{Author: "other", State: "APPROVED", SubmittedAt: at.Add(-time.Hour)}}, false},
		{"no review", "viewer", at, nil, false},
		{"no login", "", at, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at.Add(-time.Hour)}}, false},
		{"no update", "viewer", time.Time{}, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at.Add(-time.Hour)}}, false},
		{"latest review wins", "viewer", at, []gh.Review{{Author: "viewer", State: "COMMENTED", SubmittedAt: at.Add(-time.Hour)}, {Author: "VIEWER", State: "APPROVED", SubmittedAt: at}}, false},
		{"latest regardless of order", "viewer", at, []gh.Review{{Author: "viewer", State: "APPROVED", SubmittedAt: at}, {Author: "VIEWER", State: "COMMENTED", SubmittedAt: at.Add(-time.Hour)}}, false},
		{"no team identity", "org/viewer", at, []gh.Review{{Author: "org/viewer", State: "APPROVED", SubmittedAt: at.Add(-time.Hour)}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := gh.PullRequest{UpdatedAt: tc.updated, Reviews: tc.reviews, ReviewDecision: gh.Unknown, CheckState: gh.Unknown}
			if got := (prFilters{mine: filterValues{prMineUpdated}}).matches(pr, tc.login); got != tc.want {
				t.Fatalf("updated since review = %v, want %v", got, tc.want)
			}
			for _, f := range []prFilters{{mine: filterValues{prMineUpdated}, review: filterValues{"Approved"}}, {mine: filterValues{prMineUpdated}, checks: filterValues{"Passing"}}} {
				if f.matches(pr, tc.login) {
					t.Fatal("submitted review evidence promoted unknown aggregate state")
				}
			}
		})
	}
}

func TestPRMineUpdatedReviewStagesCombinesAndPreservesNavigation(t *testing.T) {
	m := mineFixture()
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[2].Reviews = []gh.Review{{Author: "VIEWER", State: "COMMENTED", SubmittedAt: snapshot.prs[2].UpdatedAt.Add(-time.Hour)}}
	m.prState.snapshots["org/repo"] = snapshot
	m.options.Bookmark = &BookmarkOptions{Time: snapshot.prs[2].UpdatedAt.Add(time.Hour)}
	m = chooseFilterOption(t, m, "mine", prMineUpdated)
	if !m.prUI.filters.empty() || len(m.visiblePRs()) != 4 {
		t.Fatal("staging updated-since-review applied early")
	}
	m = keyboardContractPress(t, m, "esc")
	if !m.prUI.filters.empty() {
		t.Fatal("cancel retained updated-since-review")
	}
	m = chooseFilterOption(t, m, "mine", prMineUpdated)
	m = keyboardContractPress(t, m, "enter")
	if got := m.visiblePRs(); len(got) != 1 || got[0].Number != 3 {
		t.Fatalf("confirmed review evidence should match independently of bookmark or partial coverage: %+v", got)
	}
	m = keyboardContractPress(t, m, "c", "p")
	if !slices.Equal(m.prUI.filters.mine, filterValues{prMineUpdated}) || len(m.visiblePRs()) != 1 {
		t.Fatal("board navigation lost updated-since-review")
	}
	m.openContextPRs("/api")
	if len(m.visiblePRs()) != 4 {
		t.Fatal("Activity context inherited saved Mine filter")
	}
	m.closeContextPRs(false)
	if !slices.Equal(m.prUI.filters.mine, filterValues{prMineUpdated}) || len(m.visiblePRs()) != 1 {
		t.Fatal("return from Activity lost saved Mine filter")
	}
	m.prUI.filters.mine = filterValues{prMineUpdated, prMineAuthor}
	if len(m.visiblePRs()) != 3 {
		t.Fatal("review updates did not combine with other Mine values using OR")
	}
	m.prUI.filters.checks = filterValues{"Passing"}
	if len(m.visiblePRs()) != 2 {
		t.Fatal("review updates did not combine with Checks using AND")
	}
	m.prUI.query = "#3"
	if got := m.visiblePRs(); len(got) != 1 || got[0].Number != 3 {
		t.Fatal("review updates ignored search")
	}
	m.prState.login = ""
	if len(m.visiblePRs()) != 0 || !strings.Contains(ansi.Strip(m.View()), "Mine unavailable") {
		t.Fatal("missing account broadened Mine results or hid the explanation")
	}
}
