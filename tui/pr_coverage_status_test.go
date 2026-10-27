package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRFileCoverageDoesNotMakeInventoryPartial(t *testing.T) {
	m := boardLayoutFixture()
	m.repositories = m.repositories[:1]
	m.local.repositories = m.local.repositories[:1]
	remote := m.prState.repositories[m.repositories[0].ID]
	snapshot := m.prState.snapshots[remote]
	for i := range snapshot.prs {
		snapshot.prs[i].RequestedReviewersComplete = true
		snapshot.prs[i].ReviewsComplete = true
	}
	snapshot.prs[0].FilesComplete = false
	snapshot.prs[0].ContextComplete = false
	snapshot.prs[1].ReviewsComplete = false
	snapshot.filesErr = gh.ErrBudgetExceeded
	m.prState.snapshots[remote] = snapshot
	m.prState.requestBudget = 8
	m.prState.inventoryRequests = 3
	m.prState.fileRequests = 5
	if status := m.prDataStatus(); status.warning || status.label != "checked" {
		t.Fatalf("file coverage changed inventory status: %+v", status)
	}
	details := ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	for _, want := range []string{"3 known open PRs", "2/3 PRs have complete current-file lists", "Areas unknown for 1 PRs", "1 PRs have incomplete reviewer/review details", "8/8 · account 0 · inventory 3 · additional files 5", "inventory complete"} {
		if !strings.Contains(details, want) {
			t.Fatalf("missing %q:\n%s", want, details)
		}
	}
	if strings.Contains(details, "Retained PRs may be outdated") {
		t.Fatal("incomplete files made current metadata look stale")
	}
	m.scanErrorDetails = true
	details = ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "File details: "+gh.ErrBudgetExceeded.Error()) {
		t.Fatal("file failure diagnostic missing")
	}
	snapshot.partial = true
	m.prState.snapshots[remote] = snapshot
	if status := m.prDataStatus(); !status.warning || status.label != "PARTIAL" {
		t.Fatal("incomplete inventory lost its warning")
	}
}

func TestPRDetailCoverageNamesTheMissingEvidence(t *testing.T) {
	m := boardLayoutFixture()
	pr := m.prState.snapshots["sample/gateway"].prs[0]
	pr.RequestedReviewersComplete, pr.ReviewsComplete = true, true
	pr.PreviousPathsComplete, pr.ContextComplete = false, false
	lines := ansi.Strip(strings.Join(m.prDetailLines(pr, 96), "\n"))
	if !strings.Contains(lines, "Rename origins unavailable") || strings.Contains(lines, "PARTIAL") {
		t.Fatal("missing rename origins confused with incomplete reviews or inventory")
	}
	pr.FilesComplete = false
	lines = ansi.Strip(strings.Join(m.prDetailLines(pr, 96), "\n"))
	if !strings.Contains(lines, "Areas unknown") || strings.Contains(lines, "PARTIAL reviewer") {
		t.Fatal("missing files confused with incomplete review details")
	}
	modal := ansi.Strip(m.renderPRDetailModal(m.View(), pr, 120, 36))
	if !strings.Contains(modal, "Areas unknown · files incomplete") {
		t.Fatal("PR modal hides area uncertainty")
	}
}
