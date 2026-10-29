package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRPersonalAttentionCriteria(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		pr   gh.PullRequest
		want bool
	}{
		{"own failed checks", gh.PullRequest{Author: "VIEWER", CheckState: "FAILURE"}, true},
		{"own check error", gh.PullRequest{Author: "viewer", CheckState: "ERROR"}, true},
		{"own requested changes", gh.PullRequest{Author: "viewer", ReviewDecision: "CHANGES_REQUESTED"}, true},
		{"own approved passing", gh.PullRequest{Author: "viewer", ReviewDecision: "APPROVED", CheckState: "SUCCESS"}, true},
		{"own draft approved passing", gh.PullRequest{Author: "viewer", Draft: true, ReviewDecision: "APPROVED", CheckState: "SUCCESS"}, false},
		{"own draft failing", gh.PullRequest{Author: "viewer", Draft: true, CheckState: "FAILURE"}, true},
		{"own pending checks", gh.PullRequest{Author: "viewer", ReviewDecision: "APPROVED", CheckState: "PENDING"}, false},
		{"own awaiting review", gh.PullRequest{Author: "viewer", ReviewDecision: "REVIEW_REQUIRED", CheckState: "SUCCESS"}, false},
		{"unrelated failure", gh.PullRequest{Author: "other", CheckState: "FAILURE", ReviewDecision: "CHANGES_REQUESTED"}, false},
		{"requested individual review", gh.PullRequest{Author: "other", RequestedReviewers: []string{"VIEWER"}}, true},
		{"requested team review", gh.PullRequest{Author: "other", RequestedReviewers: []string{"org/viewer"}}, false},
		{"updated since review", gh.PullRequest{Author: "other", UpdatedAt: now, Reviews: []gh.Review{{Author: "Viewer", State: "APPROVED", SubmittedAt: now.Add(-time.Hour)}}}, true},
		{"unchanged since review", gh.PullRequest{Author: "other", UpdatedAt: now, Reviews: []gh.Review{{Author: "Viewer", State: "APPROVED", SubmittedAt: now}}}, false},
		{"unknown", gh.PullRequest{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := prFilters{mine: filterValues{prMineAttention}}
			if got := filter.matches(tc.pr, "Viewer"); got != tc.want {
				t.Fatalf("attention match = %v, want %v", got, tc.want)
			}
			if filter.matches(tc.pr, "") {
				t.Fatal("attention guessed an unavailable account")
			}
		})
	}
}

func TestPRQuickFiltersPreserveScopeAndOtherFacets(t *testing.T) {
	m := mineFixture()
	m.prUI.query = "Change"
	m.prUI.filters.checks = filterValues{"Passing"}
	m.sharedRepos = filterValues{"/api"}
	for _, tc := range []struct {
		key, value string
		count      int
	}{{"m", prMineAttention, 1}, {"M", prMineAuthor, 1}, {"n", prMineReview, 1}} {
		m = keyboardContractPress(t, m, tc.key)
		if !slices.Equal(m.prUI.filters.mine, filterValues{tc.value}) || len(m.visiblePRs()) != tc.count {
			t.Fatalf("%s did not apply its personal filter", tc.key)
		}
		if m.filters.open || m.prUI.query != "Change" || !slices.Equal(m.prUI.filters.checks, filterValues{"Passing"}) || !slices.Equal(m.sharedRepos, filterValues{"/api"}) {
			t.Fatal("quick filter changed another facet or opened a dialog")
		}
		m = keyboardContractPress(t, m, tc.key)
		if len(m.prUI.filters.mine) != 0 || len(m.visiblePRs()) != 3 {
			t.Fatal("repeating a quick filter did not restore All Mine choices")
		}
	}
	m.prState.login = ""
	m = keyboardContractPress(t, m, "m")
	if len(m.visiblePRs()) != 0 || !strings.Contains(ansi.Strip(m.View()), "Mine unavailable") {
		t.Fatal("missing account did not explain an empty personal filter")
	}
}

func TestPROwnershipHighlightSurvivesSelectionAndResponsiveRows(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := mineFixture()
	pr := m.prState.snapshots["org/repo"].prs[0]
	for _, width := range []int{40, 70, 80, 110, 180} {
		for _, selected := range []bool{false, true} {
			m.prState.login = "Viewer"
			rows := m.prTableRow(pr, "repo", selected, 0, width)
			highlighted := strings.Join(rows, "\n")
			m.prState.login = "other"
			ordinary := strings.Join(m.prTableRow(pr, "repo", selected, 0, width), "\n")
			if highlighted == ordinary || ansi.Strip(highlighted) != ansi.Strip(ordinary) || !strings.Contains(ansi.Strip(highlighted), "@viewer") {
				t.Fatalf("%d selected=%v should distinguish ownership through styling alone", width, selected)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatal("ownership highlight overflowed the row")
				}
			}
		}
	}
	m.prState.login = ""
	unknown := strings.Join(m.prTableRow(pr, "repo", false, 0, 120), "\n")
	m.prState.login = "other"
	if unknown != strings.Join(m.prTableRow(pr, "repo", false, 0, 120), "\n") {
		t.Fatal("ownership highlight guessed an unavailable account")
	}
}

func TestPRQuickFilterClosesDetailWhenSelectionIsRemoved(t *testing.T) {
	m := mineFixture()
	m = keyboardContractPress(t, m, "M", "enter")
	if !m.prUI.detail {
		t.Fatal("fixture did not open an owned PR")
	}
	m = keyboardContractPress(t, m, "n")
	if m.prUI.detail || len(m.visiblePRs()) != 1 || m.visiblePRs()[0].Number != 2 {
		t.Fatal("quick filter silently replaced the PR in an open detail modal")
	}
}

func TestPRDirectAuthorSortAndPicker(t *testing.T) {
	m := prOnlySelectionFixture()
	snapshot := m.prState.snapshots["org/repo"]
	now := time.Now()
	for i, author := range []string{"Zulu", "ada", "ADA"} {
		snapshot.prs[i].Author = author
		snapshot.prs[i].UpdatedAt = now.Add(time.Duration(i) * time.Minute)
	}
	m.prState.snapshots["org/repo"] = snapshot
	m = keyboardContractPress(t, m, "p")
	selected := m.selectedPRKey()
	m = keyboardContractPress(t, m, "A")
	prs := m.visiblePRs()
	if prs[0].Number != 262 || prs[1].Number != 261 || prs[2].Number != 260 || m.selectedPRKey() != selected {
		t.Fatal("author sort did not group case-insensitively with newest PRs first and retain selection")
	}
	m = keyboardContractPress(t, m, "z", "end", "esc")
	if m.prUI.sort != 2 || m.prUI.sortOpen {
		t.Fatal("canceling sort changed the applied order")
	}
	m = keyboardContractPress(t, m, "z", "1")
	if m.prUI.sort != 0 || m.prUI.sortOpen || m.selectedPRKey() != selected {
		t.Fatal("numbered sort did not apply directly")
	}
	m = keyboardContractPress(t, m, "z", "end", "enter")
	if m.prUI.sort != 4 || m.prUI.sortOpen {
		t.Fatal("sort picker did not apply the highlighted choice")
	}
	m = keyboardContractPress(t, m, "z", "a", "p")
	if m.prUI.sortOpen || m.prUI.sort != 4 {
		t.Fatal("board switching retained a pending sort picker")
	}
	m = keyboardContractPress(t, m, "z")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = next.(Model)
	m = keyboardContractPress(t, m, "1")
	if !m.prUI.sortOpen || m.prUI.sort != 4 || !strings.Contains(ansi.Strip(m.View()), "Resize to 40×18") {
		t.Fatal("hidden sort options accepted input")
	}
}

func TestPRExpandedPreviewScrollsWithoutMovingSelection(t *testing.T) {
	for _, width := range []int{70, 80, 120, 180} {
		for _, height := range []int{24, 36, 48} {
			m := prOnlySelectionFixture()
			m.width, m.height = width, height
			snapshot := m.prState.snapshots["org/repo"]
			for i := range 45 {
				snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: fmt.Sprintf("src/change-%02d.go", i)})
			}
			snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: "FINAL-PATH.go"})
			m.prState.snapshots["org/repo"] = snapshot
			m = keyboardContractPress(t, m, "p", "tab")
			selected := m.selectedPRKey()
			m = keyboardContractPress(t, m, "pgdown", "end")
			if !m.prUI.previewFocus || m.prUI.detail || m.selectedPRKey() != selected || !strings.Contains(ansi.Strip(m.View()), "FINAL-PATH.go") {
				t.Fatalf("%dx%d preview could not reach its final file independently of selection", width, height)
			}
			lines := strings.Split(m.View(), "\n")
			if len(lines) != height || m.prListPageSize() < 3 {
				t.Fatal("expanded preview displaced the frame or minimum list")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatal("expanded preview exceeded terminal width")
				}
			}
			m = keyboardContractPress(t, m, "esc", "down")
			if m.prUI.previewFocus || m.selectedPRKey() == selected || m.prUI.previewOffset != 0 {
				t.Fatal("returning to list did not restore navigation and reset the next PR's preview")
			}
		}
	}
}

func TestPRCompactPreviewPreservesUnknownCoverage(t *testing.T) {
	text := prPreviewMetadata(gh.PullRequest{})
	for _, want := range []string{"Reviewers: unknown", "Files: unknown", "Conflicts: unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview silently filled missing evidence: %s", text)
		}
	}
}
