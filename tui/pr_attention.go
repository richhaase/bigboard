package tui

import (
	"cmp"
	"strings"

	gh "github.com/richhaase/bigboard/github"
)

func prReviewRequestedFrom(pr gh.PullRequest, login string) bool {
	if login == "" {
		return false
	}
	for _, reviewer := range pr.RequestedReviewers {
		if !strings.Contains(reviewer, "/") && strings.EqualFold(reviewer, login) {
			return true
		}
	}
	return false
}

func prAttentionRank(pr gh.PullRequest, login string) int {
	switch {
	case prReviewRequestedFrom(pr, login):
		return 0
	case pr.CheckState == "FAILURE" || pr.CheckState == "ERROR":
		return 1
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return 2
	case pr.ReviewDecision == "APPROVED":
		return 3
	default:
		return 4
	}
}

func comparePRAttention(a, b gh.PullRequest, login string) int {
	if comparison := cmp.Compare(prAttentionRank(a, login), prAttentionRank(b, login)); comparison != 0 {
		return comparison
	}
	if a.UpdatedAt.IsZero() != b.UpdatedAt.IsZero() {
		if a.UpdatedAt.IsZero() {
			return 1
		}
		return -1
	}
	return a.UpdatedAt.Compare(b.UpdatedAt)
}
