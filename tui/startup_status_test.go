package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestCachedStartupStatusKeepsCoverageInDetails(t *testing.T) {
	m := boardLayoutFixture()
	at := time.Now().Add(-time.Minute).Truncate(time.Second)
	m.startup = startupProgress{pending: true, cachedAt: at}
	m.scan.running, m.scan.phase, m.scan.remaining = true, scanStartupFetch, 1
	id := m.repositories[0].ID
	delete(m.local.scannedAt, id)
	m.local.errors = map[string]string{id: "cached history unreadable"}
	m.local.staleRepos[id] = true
	status := m.gitDataStatus()
	if status.warning || !status.refreshing || statusText(status, false) != "cached · fetching…" {
		t.Fatalf("startup did not distinguish cached history from failed refresh: %+v", status)
	}
	for _, width := range []int{40, 80, 120} {
		line := ansi.Strip(m.compactDataStatus(width))
		if !strings.Contains(line, "cached") || !strings.Contains(line, "e ") || ansi.StringWidth(line) > width-4 {
			t.Fatalf("width %d lost cached status/details or overflowed: %s", width, line)
		}
	}
	details := ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	for _, want := range []string{"Startup phase 2/2", "rescanning changed repositories", "Cached snapshot committed: " + at.Local().Format("Jan 02 15:04:05 MST"), "cached history unreadable", "1 repository updates failed"} {
		if !strings.Contains(details, want) {
			t.Fatalf("startup details lost %q: %s", want, details)
		}
	}
	m.startup.pending, m.scan.running = false, false
	m.startup.fetchedAt = at.Add(time.Minute)
	if !m.gitDataStatus().warning || !strings.Contains(m.gitDataStatus().label, "STALE") {
		t.Fatal("completed failed fetch hid stale coverage")
	}
	details = ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "Fetched snapshot committed: "+m.startup.fetchedAt.Local().Format("Jan 02 15:04:05 MST")) || strings.Contains(details, "Startup phase 2/2") {
		t.Fatal("completed startup did not replace phase progress with snapshot time")
	}
}

func TestStartupStatusPhasesAndManualRefresh(t *testing.T) {
	m := boardLayoutFixture()
	m.startup.pending, m.scan.running = true, true
	m.scan.phase = scanCached
	details := ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "Startup phase 1/2: reading cached history") {
		t.Fatal("offline startup phase missing")
	}
	m.scan.phase = scanStartupFetch
	details = ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "Startup: cloning/fetching cache") || strings.Contains(details, "Cached snapshot committed") {
		t.Fatal("cold startup claimed a cached snapshot")
	}
	m.startup.cachedAt = time.Now()
	m.scan.phase, m.scan.localOnly = scanDefault, true
	details = ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "Startup fetch will resume after the local rescan.") {
		t.Fatal("identity rescan lost pending startup explanation")
	}
	m.startup.pending, m.scan.localOnly = false, false
	details = ansi.Strip(strings.Join(m.dataStatusLines(120), "\n"))
	if !strings.Contains(details, "Refreshing Git. The previous snapshot stays visible.") || strings.Contains(details, "Startup fetch will resume") || statusText(m.gitDataStatus(), false) != "scanned · refreshing" {
		t.Fatal("manual refresh inherited startup phase wording")
	}
}
