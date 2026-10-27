package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRDetailModalKeepsIdentityWarningsAndDismissalState(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {40, 18}, {80, 24}, {160, 48}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		snapshot := m.prState.snapshots["sample/gateway"]
		snapshot.partial = true
		for range 40 {
			snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: "src/another-file.go"})
		}
		snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: "FINAL-PATH.go"})
		m.prState.snapshots["sample/gateway"] = snapshot
		m = keyboardContractPress(t, m, "p")
		before := ansi.Strip(m.View())
		m = keyboardContractPress(t, m, "enter")
		for _, key := range []string{"pgdown", "end", "home"} {
			m = keyboardContractPress(t, m, key)
			view := ansi.Strip(m.View())
			for _, want := range []string{"PR #100", "STALE/PARTIAL", "Esc close", "╭"} {
				if !strings.Contains(view, want) {
					t.Fatalf("%v/%s modal missing %s:\n%s", size, key, want, view)
				}
			}
			if key == "end" && !strings.Contains(view, "FINAL-PATH.go") {
				t.Fatal("modal cannot reach the final file")
			}
			lines := strings.Split(view, "\n")
			if len(lines) != m.height {
				t.Fatal("modal changed terminal height")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("modal overflowed terminal width")
				}
			}
		}
		m = keyboardContractPress(t, m, "f", "esc")
		if !m.prUI.detail {
			t.Fatal("canceling a filter lost the detail modal")
		}
		m = keyboardContractPress(t, m, "?", "esc", "esc")
		if m.prUI.detail || ansi.Strip(m.View()) != before {
			t.Fatal("closing the detail modal changed the underlying PR list")
		}
	}
}

func TestPRDetailSectionsPreserveEvidenceAndUnknownStates(t *testing.T) {
	m := prFixture()
	pr := m.prState.snapshots["org/repo"].prs[0]
	pr.ReviewDecision, pr.CheckState, pr.Mergeable = "APPROVED", "SUCCESS", "MERGEABLE"
	pr.Draft = true
	pr.Reviews = []gh.Review{{Author: "reviewer", State: "COMMENTED"}}
	pr.RequestedReviewersComplete, pr.ReviewsComplete = false, false
	lines := ansi.Strip(strings.Join(m.prDetailLines(pr, 96), "\n"))
	for _, want := range []string{pr.Title, "◇ Draft", "Approved", "Passing", "None reported", "merge readiness", "Reviewers", "Requested: Unknown", "@reviewer · Commented", "PARTIAL review history", "Affected areas", "GitHub link", pr.URL, "Changed files", "feature/new.go", "ui/new.go"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("detail lost %q:\n%s", want, lines)
		}
	}
	pr.RequestedReviewersComplete, pr.ReviewsComplete = true, true
	pr.Reviews = nil
	pr.ReviewDecision, pr.CheckState, pr.Mergeable = "", "", ""
	lines = ansi.Strip(strings.Join(m.prDetailLines(pr, 96), "\n"))
	for _, want := range []string{"Requested: None", "No reviews recorded", "Review    Unknown", "Checks    Unknown", "Conflicts Unknown"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing explicit state %q:\n%s", want, lines)
		}
	}
}
