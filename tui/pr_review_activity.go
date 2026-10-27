package tui

import (
	"strings"
	"time"

	gh "github.com/richhaase/bigboard/github"
)

func prUpdatedSinceReview(pr gh.PullRequest, login string) bool {
	if login == "" || pr.UpdatedAt.IsZero() {
		return false
	}
	var latest time.Time
	for _, review := range pr.Reviews {
		if !strings.Contains(review.Author, "/") && strings.EqualFold(review.Author, login) && review.State != "PENDING" && review.SubmittedAt.After(latest) {
			latest = review.SubmittedAt
		}
	}
	return !latest.IsZero() && pr.UpdatedAt.After(latest)
}
