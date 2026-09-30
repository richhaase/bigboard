package tui

import (
	"errors"
	"strings"
	"testing"

	gh "github.com/richhaase/bigboard/github"
)

func TestPRGlanceKnownEvidenceAndQualifiers(t *testing.T) {
	m := prFixture()
	if got := m.prGlanceSignal("/api"); got != "1 open · 1 pending" {
		t.Fatal(got)
	}
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[0].CheckState = "FAILURE"
	snapshot.prs = append(snapshot.prs, gh.PullRequest{Repo: "org/repo", Number: 8, CheckState: "PENDING"})
	snapshot.err = errors.New("failed refresh")
	m.prState.snapshots["org/repo"] = snapshot
	got := m.prGlanceSignal("/api")
	for _, s := range []string{"STALE", "2 known open", "1 failing", "1 pending"} {
		if !strings.Contains(got, s) {
			t.Fatal(got)
		}
	}
	snapshot.err = nil
	snapshot.partial = true
	snapshot.prs[0].CheckState = "UNKNOWN"
	snapshot.prs = snapshot.prs[:1]
	m.prState.snapshots["org/repo"] = snapshot
	if got = m.prGlanceSignal("/api"); !strings.Contains(got, "PARTIAL") || !strings.Contains(got, "unknown") {
		t.Fatal(got)
	}
	m.prState.snapshots = nil
	if got = m.prGlanceSignal("/api"); got != "PRs unknown" {
		t.Fatal(got)
	}
}

func TestPRGlanceRespectsAreaAndIndependentLocalFilters(t *testing.T) {
	m := prFixture()
	m.areaRepoID = "/api"
	m.personID = "does-not-exist"
	m.hideBots = true
	if got := m.prGlanceSignal("auto:feature"); !strings.HasPrefix(got, "1 open") {
		t.Fatal(got)
	}
	if got := m.prGlanceSignal("auto:other"); got != "0 open" {
		t.Fatal(got)
	}
}

func TestPRGlanceFailedFirstFetchIsNotZero(t *testing.T) {
	m := prFixture()
	m.prState.snapshots = nil
	m.applyPRResult(prsLoadedMsg{repositories: map[string]string{"/api": "org/repo"}, results: map[string]gh.Result{"org/repo": {Repo: "org/repo", Err: gh.ErrAuthentication}}})
	got := m.prGlanceSignal("/api")
	if strings.Contains(got, "0 open") || !strings.Contains(got, "unknown") {
		t.Fatal(got)
	}
}
