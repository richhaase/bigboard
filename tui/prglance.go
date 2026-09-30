package tui

import (
	"fmt"
	"strings"
)

// prGlanceSignal is a bounded factual summary of the separate all-open PR
// inventory. It never treats unknown or incomplete evidence as success.
func (m Model) prGlanceSignal(scope string) string {
	id := scope
	if m.areaRepoID != "" {
		id = m.areaRepoID
	}
	snapshot, ok := m.prState.snapshots[m.prState.repositories[id]]
	if !ok {
		return "PRs unknown"
	}
	prs := m.scopePRs(scope)
	incomplete := snapshot.partial || snapshot.err != nil || m.prState.errors[id] != nil
	if incomplete && len(snapshot.prs) == 0 && snapshot.lastGood.IsZero() {
		return "PRs unknown · unavailable/partial"
	}
	prefix := ""
	if snapshot.err != nil || m.prState.errors[id] != nil {
		prefix = "STALE · "
	} else if snapshot.partial {
		prefix = "PARTIAL · "
	}
	failing, pending, review, unknown := 0, 0, 0, 0
	for _, pr := range prs {
		switch pr.CheckState {
		case "FAILURE", "ERROR":
			failing++
		case "PENDING", "EXPECTED":
			pending++
		case "SUCCESS":
		default:
			unknown++
		}
		if pr.ReviewDecision == "REVIEW_REQUIRED" {
			review++
		}
	}
	count := fmt.Sprintf("%d open", len(prs))
	if incomplete {
		count = fmt.Sprintf("%d known open", len(prs))
	}
	parts := []string{count}
	if failing > 0 {
		parts = append(parts, fmt.Sprintf("%d failing", failing))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if review > 0 && failing == 0 && pending == 0 {
		parts = append(parts, fmt.Sprintf("%d need review", review))
	}
	if unknown > 0 && failing == 0 && pending == 0 && review == 0 {
		parts = append(parts, fmt.Sprintf("%d checks unknown", unknown))
	}
	return prefix + strings.Join(parts, " · ")
}
