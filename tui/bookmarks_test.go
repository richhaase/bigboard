package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestBookmarkAdvancesOnlyOnNormalExitAfterFetchedHistory(t *testing.T) {
	for _, scenario := range []string{"cached-only", "normal", "abnormal", "failed-cold"} {
		t.Run(scenario, func(t *testing.T) {
			m, f := newStartupFixture(t, "org/repo")
			f.revisions["org/repo"] = "cached"
			f.fetched["org/repo"] = "fetched"
			if scenario == "failed-cold" {
				delete(f.revisions, "org/repo")
				f.failures["org/repo"] = errors.New("unavailable")
			}
			var saved time.Time
			m.options.Bookmark = &BookmarkOptions{Time: time.Now().Add(-time.Hour), Save: func(at time.Time) error { saved = at; return nil }}
			m, fetch := finishStartupPhase(t, m, m.Init())
			if !m.bookmarkSeenAt.IsZero() {
				t.Fatal("offline scan made the bookmark eligible")
			}
			if scenario != "cached-only" {
				m = runRefreshCommands(t, m, fetch)
			}
			if err := m.SaveBookmark(); err != nil || !saved.IsZero() {
				t.Fatal("bookmark saved before normal exit")
			}
			if scenario == "abnormal" {
				m.Close()
			} else {
				next, _ := m.quit()
				m = next.(Model)
			}
			if err := m.SaveBookmark(); err != nil {
				t.Fatal(err)
			}
			if scenario == "normal" {
				if saved.IsZero() || !saved.Equal(m.startup.fetchedAt) || !saved.Equal(m.bookmarkSeenAt) {
					t.Fatalf("saved %v instead of the fetched snapshot time %v", saved, m.startup.fetchedAt)
				}
			} else if !saved.IsZero() {
				t.Fatal("incomplete or abnormal session advanced its bookmark")
			}
		})
	}
}

func TestBookmarkManualRefreshAdvancesCandidateButLocalRescanDoesNot(t *testing.T) {
	m, f := newStartupFixture(t, "org/repo")
	f.revisions["org/repo"] = "same"
	m = runRefreshCommands(t, m, m.Init())
	first := m.bookmarkSeenAt
	m.resetPending()
	m.scan.localOnly = true
	m = runRefreshCommands(t, m, m.loadCmds())
	if !m.bookmarkSeenAt.Equal(first) {
		t.Fatal("offline rescan advanced the seen snapshot")
	}
	cmd := m.startLocalRefresh()
	m = runRefreshCommands(t, m, cmd)
	if !m.bookmarkSeenAt.After(first) {
		t.Fatal("completed manual fetch did not advance the seen snapshot")
	}
}

func TestBookmarkSettingsReloadRequiresNewFetchedSnapshot(t *testing.T) {
	m := settingsTestModel(t, settingsTestOptions())
	m.options.Bookmark = &BookmarkOptions{Time: time.Now().Add(-time.Hour)}
	m.bookmarkSeenAt = time.Now()
	m.sinceLastLookRange = true
	m.settings.open = true
	next, _ := m.applySavedSettings(settingsSavedMsg{generation: m.settingsGeneration})
	updated := next.(Model)
	t.Cleanup(updated.Close)
	if !updated.bookmarkSeenAt.IsZero() || updated.options.Bookmark != m.options.Bookmark || !updated.sinceLastLookRange {
		t.Fatal("settings reload lost bookmark scope or reused previous fetched eligibility")
	}
}

func TestBookmarkSaveFailureAndStatus(t *testing.T) {
	m := boardLayoutFixture()
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	failure := errors.New("write failed")
	m.options.Bookmark = &BookmarkOptions{Selection: "Group: backend", Path: "/state/last-seen.json", Error: "No saved bookmark", Save: func(time.Time) error { return failure }}
	text := ansi.Strip(strings.Join(m.dataStatusLines(100), "\n"))
	for _, want := range []string{"SINCE LAST LOOK", "Group: backend", "No saved bookmark", "normal exit after a fetched snapshot"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q", want)
		}
	}
	m.options.Bookmark.Time = at
	m.bookmarkSeenAt = at.Add(time.Minute)
	m.quitting = true
	if err := m.SaveBookmark(); !errors.Is(err, failure) {
		t.Fatalf("save failure lost: %v", err)
	}
	text = ansi.Strip(strings.Join(m.bookmarkStatusLines(), "\n"))
	if !strings.Contains(text, "Last look: "+at.Local().Format("Jan 02 15:04:05 MST")) || !strings.Contains(text, "On normal exit, save snapshot time:") {
		t.Fatal("status lost the fixed bookmark or candidate time")
	}
}
