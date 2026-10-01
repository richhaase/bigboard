package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	gh "github.com/richhaase/bigboard/github"
)

// A deterministic test fixture, not a snapshot obtained from live GitHub.
func prStatusVisibilityFixture(width, height int) Model {
	m := prFixture()
	m.width, m.height = width, height
	m.areaRepoID, m.selectedAreaID = "/api", "auto:feature"
	m = pressAwareness(m, "enter")
	return m
}

func TestPRStatusWarningsVisibleAcrossLenses(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []struct {
		name  string
		value termenv.Profile
	}{{"plain", termenv.Ascii}, {"truecolor", termenv.TrueColor}} {
		t.Run(profile.name, func(t *testing.T) {
			lipgloss.SetColorProfile(profile.value)
			for _, size := range [][2]int{{100, 28}, {160, 48}, {80, 30}} {
				for _, state := range []struct {
					name, warning            string
					failed, partial, loading bool
				}{
					{name: "stale", warning: "STALE", failed: true},
					{name: "partial", warning: "PARTIAL", partial: true},
					{name: "retained_stale_refresh", warning: "STALE", failed: true, loading: true},
					{name: "retained_partial_refresh", warning: "PARTIAL", partial: true, loading: true},
				} {
					for tab, lens := range []string{"Activity", "People", "Related", "Subareas"} {
						t.Run(fmt.Sprintf("%dx%d/%s/%s", size[0], size[1], state.name, lens), func(t *testing.T) {
							m := prStatusVisibilityFixture(size[0], size[1])
							snapshot := m.prState.snapshots["org/repo"]
							snapshot.partial = state.partial
							if state.failed {
								snapshot.err = gh.ErrRateLimited
							}
							m.prState.snapshots["org/repo"] = snapshot
							m.prState.loading = state.loading
							m = pressAwareness(m, fmt.Sprint(tab+1))
							if !m.glance.detailOpen || m.glance.frame.tab != tab {
								t.Fatalf("fixture did not select %s", lens)
							}
							raw := m.View()
							view := ansi.Strip(raw)
							want := "PRs: " + state.warning
							if !strings.Contains(view, want) {
								t.Fatalf("complete warning %q missing:\n%s", want, view)
							}
							if strings.Contains(view, "0 known open") {
								t.Fatalf("retained PR inventory became zero:\n%s", view)
							}
							if lipgloss.Height(raw) > m.height {
								t.Fatalf("height overflow: %d > %d", lipgloss.Height(raw), m.height)
							}
							for _, line := range strings.Split(raw, "\n") {
								if ansi.StringWidth(line) > m.width {
									t.Fatalf("width overflow: %q", ansi.Strip(line))
								}
							}
							if profile.value == termenv.Ascii && strings.Contains(raw, "\x1b[") {
								t.Fatal("plain output contains ANSI styling")
							}
							if profile.value == termenv.TrueColor && !strings.Contains(raw, "\x1b[") {
								t.Fatal("truecolor fixture lost styling")
							}
						})
					}
				}
			}
		})
	}
}

func TestPRStatusEmptyInventoryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		loading, checked, failed, partial bool
		want                              string
	}{
		{name: "initial_unknown", want: "unknown"},
		{name: "initial_loading", loading: true, want: "refresh"},
		{name: "partial_empty", checked: true, partial: true, want: "partial"},
		{name: "partial_empty_refresh", checked: true, partial: true, loading: true, want: "partial"},
		{name: "complete_empty", checked: true, want: "0 known open"},
		{name: "failed_empty", checked: true, failed: true, want: "unavailable"},
		{name: "failed_empty_refresh", checked: true, failed: true, loading: true, want: "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := prStatusVisibilityFixture(160, 48)
			m.prState = prState{repositories: map[string]string{"/api": "org/repo", "/web": "org/repo", "/empty": "org/repo"}, snapshots: map[string]prSnapshot{}, loading: tc.loading}
			if tc.checked {
				snapshot := prSnapshot{partial: tc.partial, checked: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
				if tc.failed {
					snapshot.err = gh.ErrRateLimited
					snapshot.partial = true
				} else if !tc.partial {
					snapshot.lastGood = snapshot.checked
				}
				m.prState.snapshots["org/repo"] = snapshot
			}
			status := m.prStatus()
			if !strings.Contains(strings.ToLower(status), tc.want) {
				t.Fatalf("missing %q: %s", tc.want, status)
			}
			if (!tc.checked || tc.failed || tc.partial) && strings.Contains(status, "0 known open") {
				t.Fatalf("unknown inventory reported as known zero: %s", status)
			}
			view := ansi.Strip(m.View())
			if (!tc.checked || tc.failed || tc.partial) && strings.Contains(view, "0 known open") {
				t.Fatalf("view reports false known zero:\n%s", view)
			}
		})
	}
}
