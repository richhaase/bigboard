package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRPreviewFollowsSelectionAndPreservesFullTitle(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p")
	for _, key := range []string{"down", "pgdown", "end", "home", "s", "S"} {
		m = keyboardContractPress(t, m, key)
		pr := m.visiblePRs()[m.prUI.row]
		preview := ansi.Strip(strings.Join(m.prSelectedPreview(m.visiblePRs(), m.width), "\n"))
		for _, want := range []string{fmt.Sprintf("SELECTED  %s #%d", pr.Repo, pr.Number), pr.Title, githubIdentity(pr.Author), "Review: " + prReviewLabel(pr.ReviewDecision), "Checks: " + prCheckLabel(pr.CheckState)} {
			if !strings.Contains(preview, want) {
				t.Fatalf("%s: selected preview missing %q:\n%s", key, want, preview)
			}
		}
		view := strings.Split(ansi.Strip(m.View()), "\n")
		if !strings.Contains(view[screenHeaderRows+1], "AUTHOR") || !strings.Contains(view[screenHeaderRows+1], "REVIEW") {
			t.Fatal("wide table did not preserve author and review columns")
		}
		if !strings.Contains(strings.Join(view[len(view)-5:len(view)-3], "\n"), githubIdentity(pr.Author)) {
			t.Fatal("preview is not pinned above the footer")
		}
	}
	m = keyboardContractPress(t, m, "/", "no-matching-pr", "enter")
	if strings.Contains(ansi.Strip(m.View()), "SELECTED") {
		t.Fatal("empty search retained a stale selection preview")
	}
}

func TestPRPreviewReservesListAndDiagnosticSpace(t *testing.T) {
	for _, width := range []int{40, 70, 80, 110, 120, 180} {
		for _, height := range []int{18, 24, 36, 48} {
			m := boardLayoutFixture()
			m.width, m.height = width, height
			for repo, snap := range m.prState.snapshots {
				for i := range snap.prs {
					snap.prs[i].Title = strings.Repeat("界 long title ", 100)
				}
				m.prState.snapshots[repo] = snap
			}
			m = keyboardContractPress(t, m, "p", "end")
			m.prUI.browserError = "Could not open browser: unavailable"
			view := ansi.Strip(m.View())
			lines := strings.Split(view, "\n")
			if len(lines) != height || !strings.Contains(view, m.prUI.browserError) {
				t.Fatalf("%dx%d lost frame or browser diagnostic", width, height)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%dx%d overflow: %s", width, height, line)
				}
			}
			preview := m.prSelectedPreview(m.visiblePRs(), width)
			if width < 70 || height < 24 {
				if len(preview) != 0 {
					t.Fatal("small terminal lost list space to preview")
				}
				continue
			}
			if !strings.Contains(view, "Enter full detail") || m.prListPageSize() < 3 {
				t.Fatalf("%dx%d did not bound long title while preserving list", width, height)
			}
			pr := m.visiblePRs()[m.prUI.row]
			list := strings.Join(lines[screenHeaderRows+2:height-screenFooterRows-len(preview)-1], "\n")
			if !strings.Contains(list, "› ") || !strings.Contains(list, fmt.Sprintf("#%d", pr.Number)) {
				t.Fatalf("%dx%d selected PR is missing from the list:\n%s", width, height, list)
			}
		}
	}
}

func TestPRRepositoryLabelsRetainAmbiguousOwners(t *testing.T) {
	m := boardLayoutFixture()
	first := m.prState.snapshots["sample/gateway"]
	second := m.prState.snapshots["sample/web-client"]
	second.prs = []gh.PullRequest{{Repo: "another/gateway", Number: 1}}
	m.prState.snapshots["sample/web-client"] = second
	labels := m.prRepositoryLabels()
	if labels[first.prs[0].Repo] != "sample/gateway" || labels["another/gateway"] != "another/gateway" || labels["sample/worker-service"] != "worker-service" {
		t.Fatalf("ambiguous repository labels: %v", labels)
	}
	m.prUI.query = "another/gateway"
	if m.prRepositoryLabels()["another/gateway"] != "another/gateway" {
		t.Fatal("filtering changed repository identity labels")
	}
}
