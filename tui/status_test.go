package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestDataStatusUsesTrackingScopeAndKeepsWarningsDuringRefresh(t *testing.T) {
	m := boardLayoutFixture()
	id, remote := m.repositories[1].ID, m.prState.repositories[m.repositories[1].ID]
	m.sharedRepos = filterValues{m.repositories[0].ID}
	m.local.staleRepos[id] = true
	m.local.errors = map[string]string{id: "fetch failed"}
	snapshot := m.prState.snapshots[remote]
	snapshot.partial, snapshot.err = true, gh.ErrRateLimited
	m.prState.snapshots[remote] = snapshot
	m.scan.running, m.prState.loading = true, true
	for _, width := range []int{40, 80, 120} {
		line := ansi.Strip(m.compactDataStatus(width))
		if strings.Count(line, "STALE") != 2 || !strings.Contains(line, "e ") || ansi.StringWidth(line) > width-4 {
			t.Fatalf("width %d hides retained-state warnings or details: %s", width, line)
		}
		if !strings.Contains(line, "refreshing") && !strings.Contains(line, "↻") {
			t.Fatal("refresh state disappeared")
		}
	}
	m.excludedRepos[id] = true
	if m.gitDataStatus().warning || m.prDataStatus().warning {
		t.Fatal("excluded repository kept global status unhealthy")
	}
	delete(m.local.scannedAt, m.repositories[0].ID)
	if !m.gitDataStatus().warning || m.gitDataStatus().label != "PARTIAL" {
		t.Fatal("loaded repository without a scan timestamp appeared healthy")
	}
}

func TestDataStatusDistinguishesEmptyUnknownAndUnsupportedPRs(t *testing.T) {
	for _, state := range []string{"complete", "partial", "failed", "unknown", "unsupported"} {
		m := boardLayoutFixture()
		m.prState.snapshots = map[string]prSnapshot{}
		m.prState.errors = map[string]error{}
		for _, repo := range m.repositories {
			remote := m.prState.repositories[repo.ID]
			snapshot := prSnapshot{checked: time.Now()}
			switch state {
			case "partial":
				snapshot.partial = true
			case "failed":
				snapshot.err = gh.ErrAuthentication
			case "unsupported":
				m.prState.errors[repo.ID] = gh.ErrUnsupportedOrigin
			}
			if state != "unknown" && state != "unsupported" {
				m.prState.snapshots[remote] = snapshot
			}
		}
		status := m.prDataStatus()
		details := ansi.Strip(strings.Join(m.dataStatusLines(100), "\n"))
		switch state {
		case "complete":
			if status.warning || !strings.Contains(details, "0 known open PRs") {
				t.Fatal("complete empty inventory was not distinguished")
			}
		case "unsupported":
			if status.warning || status.label != "not applicable" {
				t.Fatal("unsupported origins were treated as failed PRs")
			}
		default:
			if !status.warning || strings.Contains(details, "0 known open PRs") || !strings.Contains(details, "count is unknown") {
				t.Fatalf("%s empty inventory was presented as reliable zero", state)
			}
		}
	}
}

func TestDataStatusDetailsSeparateScanTimesFromCheckAttempts(t *testing.T) {
	m := boardLayoutFixture()
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	recent := time.Now().Add(-time.Minute).Truncate(time.Second)
	id := m.repositories[0].ID
	m.local.scannedAt[id] = old
	snapshot := m.prState.snapshots[m.prState.repositories[id]]
	snapshot.checked, snapshot.lastGood, snapshot.partial = recent, old, true
	snapshot.err = gh.ErrRateLimited
	m.prState.snapshots[m.prState.repositories[id]] = snapshot
	details := ansi.Strip(strings.Join(m.dataStatusLines(100), "\n"))
	for _, want := range []string{
		"Oldest retained scan: " + old.Local().Format("Jan 02 15:04:05 MST"),
		"Last checked: " + recent.Local().Format("Jan 02 15:04:05 MST"),
		"Last complete: " + old.Local().Format("Jan 02 15:04:05 MST"),
		"Retained PRs may be outdated",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("status details lost %q", want)
		}
	}
}

func TestStatusDetailsAvailableAndConsistentAcrossBoards(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		var previous string
		for _, board := range []string{"a", "c", "p"} {
			m := boardLayoutFixture()
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, board, "down")
			before := m.View()
			status := strings.Split(ansi.Strip(before), "\n")[3]
			if previous != "" && status != previous {
				t.Fatal("board switching changed status meaning")
			}
			previous = status
			m = keyboardContractPress(t, m, "e")
			if !m.showScanErrors || !strings.Contains(m.View(), "Data status") {
				t.Fatal("healthy board has no status details action")
			}
			for _, line := range strings.Split(m.View(), "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("status details overflowed terminal width")
				}
			}
			m = keyboardContractPress(t, m, "e")
			if m.View() != before {
				t.Fatal("status details changed board navigation")
			}
		}
	}
}
