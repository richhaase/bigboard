package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestRefreshIndicatorLifecycle(t *testing.T) {
	m := boardLayoutFixture()
	m.prState.loading = true
	if m.refreshTickCmd() != nil {
		t.Fatal("animation started before terminal readiness")
	}
	m, tick := applyLocal(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if tick == nil || m.refreshAnimation.pending == 0 {
		t.Fatal("visible refresh did not start an animation")
	}
	sequence := m.refreshAnimation.pending
	m, duplicate := applyLocal(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if duplicate != nil || m.refreshAnimation.pending != sequence {
		t.Fatal("resize duplicated the pending animation")
	}
	before, glyph := m, m.refreshGlyph()
	m, successor := applyLocal(t, m, refreshTickMsg{sequence: sequence})
	if successor == nil || m.refreshGlyph() == glyph || m.refreshAnimation.pending <= sequence {
		t.Fatal("active animation did not advance and schedule one successor")
	}
	if m.scan.generation != before.scan.generation || m.prGeneration != before.prGeneration || !reflect.DeepEqual(m.local, before.local) || !reflect.DeepEqual(m.contributorPane, before.contributorPane) {
		t.Fatal("animation changed data, refresh generations, or pane navigation")
	}
	pending := m.refreshAnimation.pending
	m, duplicate = applyLocal(t, m, refreshTickMsg{sequence: sequence})
	if duplicate != nil || m.refreshAnimation.pending != pending {
		t.Fatal("obsolete tick duplicated the animation")
	}
	m.prState.loading = false
	frame := m.refreshAnimation.frame
	m, successor = applyLocal(t, m, refreshTickMsg{sequence: pending})
	if successor != nil || m.refreshAnimation.pending != 0 || m.refreshAnimation.frame != frame {
		t.Fatal("idle animation scheduled more work")
	}
	m.prState.loading = true
	m, tick = applyLocal(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if tick == nil || m.refreshAnimation.pending <= pending {
		t.Fatal("new refresh did not restart the visual clock")
	}
}

func TestRefreshIndicatorStopsOnQuitOrCancellation(t *testing.T) {
	for _, reason := range []string{"quit", "canceled", "setup"} {
		t.Run(reason, func(t *testing.T) {
			m := boardLayoutFixture()
			m.prState.loading = true
			m.refreshAnimation = refreshAnimation{ready: true, sequence: 1, pending: 1}
			switch reason {
			case "quit":
				m.quitting = true
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				m.scanContext = ctx
			case "setup":
				m.settings.open, m.settings.required = true, true
			}
			m, next := applyLocal(t, m, refreshTickMsg{sequence: 1})
			if next != nil || m.refreshAnimation.pending != 0 || m.refreshAnimation.frame != 0 {
				t.Fatal("inactive model continued its animation")
			}
		})
	}
}

func TestRefreshIndicatorSurvivesSettingsModelReplacement(t *testing.T) {
	m := settingsTestModel(t, settingsTestOptions())
	m.prState.loading = true
	m, _ = applyLocal(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	pending := m.refreshAnimation.pending
	m.settings.open = true
	m, reload := applyLocal(t, m, settingsSavedMsg{
		generation:   m.settingsGeneration,
		config:       SettingsConfig{Organizations: []string{"acme"}},
		repositories: []git.Repository{{ID: "acme/api", Name: "acme/api"}},
	})
	t.Cleanup(m.Close)
	if reload == nil || !m.refreshAnimation.ready || m.refreshAnimation.pending != pending {
		t.Fatal("settings replacement lost or duplicated the pending UI tick")
	}
	if _, ok := reload().(settingsReloadMsg); !ok {
		t.Fatal("settings replacement scheduled an extra animation chain")
	}
	m, next := applyLocal(t, m, refreshTickMsg{sequence: pending})
	if next == nil || m.refreshAnimation.pending <= pending || m.refreshAnimation.frame != 1 {
		t.Fatal("replacement model did not continue the single UI clock")
	}
}

func TestRefreshIndicatorStatusLayoutKeepsWarningsAndTruthfulProgress(t *testing.T) {
	m := boardLayoutFixture()
	id := m.repositories[0].ID
	m.local.staleRepos[id] = true
	remote := m.prState.repositories[id]
	snapshot := m.prState.snapshots[remote]
	snapshot.err, snapshot.partial = gh.ErrRateLimited, true
	m.prState.snapshots[remote] = snapshot
	m.scan.running, m.scan.remaining, m.prState.loading = true, 4, true
	for _, width := range []int{40, 80, 120} {
		line := ansi.Strip(m.compactDataStatus(width))
		if strings.Count(line, "STALE") != 2 || !strings.Contains(line, "e ") || strings.Count(line, m.refreshGlyph()) != 2 || ansi.StringWidth(line) > width-4 {
			t.Fatalf("width %d lost warnings, activity, or details: %s", width, line)
		}
		if width >= 80 && strings.Count(line, "12/16") != 1 {
			t.Fatalf("width %d lost actual repository progress: %s", width, line)
		}
	}
	m.scan.running = false
	line := ansi.Strip(m.compactDataStatus(120))
	if strings.Count(line, m.refreshGlyph()) != 1 || strings.Contains(line, "12/16") {
		t.Fatal("PR-only refresh fabricated repository progress")
	}
	m.prState.loading = false
	if strings.Contains(m.compactDataStatus(120), m.refreshGlyph()) {
		t.Fatal("idle status retained its working indicator")
	}
}
