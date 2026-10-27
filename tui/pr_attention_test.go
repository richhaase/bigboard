package tui

import (
	"slices"
	"testing"
	"time"

	gh "github.com/richhaase/bigboard/github"
)

func attentionFixture(prs []gh.PullRequest) Model {
	m := prFixture()
	m.prState.login, m.prSort, m.showPRs = "Viewer", 4, true
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = prs
	for i := range snapshot.prs {
		snapshot.prs[i].Repo = "org/repo"
	}
	m.prState.snapshots["org/repo"] = snapshot
	return m
}

func assertPRAttentionOrder(t *testing.T, m Model, want []int) {
	t.Helper()
	var got []int
	for _, pr := range m.visiblePRs() {
		got = append(got, pr.Number)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("PR order = %v, want %v", got, want)
	}
}

func TestPRAttentionOrderAndReverse(t *testing.T) {
	now := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	older := now.Add(-time.Hour)
	m := attentionFixture([]gh.PullRequest{
		{Number: 1, RequestedReviewers: []string{"VIEWER"}, CheckState: "FAILURE", ReviewDecision: "APPROVED", UpdatedAt: now},
		{Number: 2, RequestedReviewers: []string{"viewer"}, ReviewDecision: "CHANGES_REQUESTED", UpdatedAt: older},
		{Number: 3, CheckState: "FAILURE", ReviewDecision: "CHANGES_REQUESTED", UpdatedAt: now},
		{Number: 4, CheckState: "ERROR", ReviewDecision: "APPROVED", UpdatedAt: older},
		{Number: 5, ReviewDecision: "CHANGES_REQUESTED", UpdatedAt: now},
		{Number: 6, ReviewDecision: "APPROVED", CheckState: gh.Unknown, UpdatedAt: now},
		{Number: 7, ReviewDecision: gh.Unknown, CheckState: "SUCCESS", UpdatedAt: now},
		{Number: 8, ReviewDecision: gh.Unknown, CheckState: gh.Unknown, UpdatedAt: older},
		{Number: 9, ReviewDecision: "REVIEW_REQUIRED"},
		{Number: 10, ReviewDecision: "UNRECOGNIZED", CheckState: "UNRECOGNIZED", UpdatedAt: older, Reviews: []gh.Review{{Author: "someone", State: "APPROVED"}}},
		{Number: 11, ReviewDecision: "APPROVED"},
		{Number: 12, RequestedReviewers: []string{"Viewer"}},
	})
	want := []int{2, 1, 12, 4, 3, 5, 6, 11, 10, 8, 7, 9}
	assertPRAttentionOrder(t, m, want)
	m.prReverse = true
	slices.Reverse(want)
	assertPRAttentionOrder(t, m, want)
}

func TestPRAttentionOnlyPromotesIndividualReviewRequestsForKnownLogin(t *testing.T) {
	for _, tc := range []struct {
		name, login string
		want        []int
	}{
		{"known", "vIeWeR", []int{2, 1, 3, 4}},
		{"missing", "", []int{1, 2, 3, 4}},
		{"different", "other", []int{1, 2, 3, 4}},
		{"team", "org/viewer", []int{1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := attentionFixture([]gh.PullRequest{
				{Number: 1, CheckState: "FAILURE"},
				{Number: 2, RequestedReviewers: []string{"Viewer"}},
				{Number: 3, RequestedReviewers: []string{"org/viewer"}},
				{Number: 4, RequestedReviewers: []string{""}},
			})
			m.prState.login = tc.login
			assertPRAttentionOrder(t, m, tc.want)
		})
	}
}

func TestPRAttentionSortCycleRetainsSelectionAndDefault(t *testing.T) {
	m := prOnlySelectionFixture()
	m = keyboardContractPress(t, m, "p", "down")
	selected := m.selectedPRKey()
	if m.prSortLabel() != "Updated · newest first" {
		t.Fatal("default PR sorting changed")
	}
	for _, want := range []string{"Repository · A–Z", "Author · A–Z", "Title · A–Z", "Attention · priority first", "Updated · newest first"} {
		m = keyboardContractPress(t, m, "s")
		if m.prSortLabel() != want || m.selectedPRKey() != selected {
			t.Fatalf("sort cycle = %q, selected = %q; want %q, %q", m.prSortLabel(), m.selectedPRKey(), want, selected)
		}
	}
	m = keyboardContractPress(t, m, "s", "s", "s", "s", "S")
	if m.prSortLabel() != "Attention · priority last" || m.selectedPRKey() != selected {
		t.Fatal("reversing Attention did not retain the selected PR")
	}
}
