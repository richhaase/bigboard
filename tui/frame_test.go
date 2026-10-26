package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	gh "github.com/richhaase/bigboard/github"
)

func TestScreenFrameAcrossViews(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 40}} {
			for _, screen := range []string{"Activity", "Leaderboard", "Contributor", "PR list", "PR detail"} {
				t.Run(fmt.Sprintf("%d/%dx%d/%s", profile, size[0], size[1], screen), func(t *testing.T) {
					m := prFixture()
					m.width, m.height = size[0], size[1]
					m.version = "v-test"
					switch screen {
					case "Leaderboard", "Contributor":
						m.viewMode = ViewAggregate
						if screen == "Contributor" {
							m = pressAwareness(m, "enter")
						}
					case "PR list", "PR detail":
						m.showPRs = true
						m.prDetail = screen == "PR detail"
					}
					raw := m.View()
					lines := strings.Split(ansi.Strip(raw), "\n")
					if len(lines) != m.height || !strings.HasPrefix(lines[0], "  BIGBOARD / ") {
						t.Fatalf("missing shared title or wrong height: %s", raw)
					}
					for _, line := range strings.Split(raw, "\n") {
						if ansi.StringWidth(line) > m.width {
							t.Fatalf("line exceeds %d columns: %s", m.width, line)
						}
					}
					if screen == "PR detail" {
						plain := ansi.Strip(raw)
						if !strings.Contains(plain, "╭") || !strings.Contains(plain, "PR #7") || !strings.Contains(plain, "Esc close") {
							t.Fatal("PR detail lost its modal frame")
						}
						if profile == termenv.Ascii && strings.Contains(raw, "\x1b[") {
							t.Fatal("plain modal contains ANSI styling")
						}
						return
					}
					if !strings.Contains(lines[3], "Git") || !strings.Contains(lines[3], "PR") || !strings.Contains(lines[4], "────") {
						t.Fatal("compact status row moved or disappeared")
					}
					if !strings.Contains(lines[m.height-2], "? help") || !strings.Contains(lines[m.height-3], "────") {
						t.Fatal("footer is not pinned to the bottom")
					}
					if m.width >= 90 && !strings.Contains(lines[0], "v-test") {
						t.Fatal("version missing from wide title band")
					}
					if m.showPRs && (!strings.Contains(lines[1], "Shared:") || !strings.Contains(lines[m.height-1], "PR updated time")) {
						t.Fatal("PR screen lost its date scope")
					}
					if profile == termenv.Ascii && strings.Contains(raw, "\x1b[") {
						t.Fatal("plain output contains ANSI styling")
					}
				})
			}
		}
	}
}

func TestStatisticsFrameUsesAllIncludedRepositoryFreshness(t *testing.T) {
	m := prFixture()
	m.viewMode = ViewAggregate
	m.width, m.height = 120, 40
	old := time.Now().Add(-48 * time.Hour)
	for _, repo := range m.loadedRepos {
		m.scannedAt[repo.ID] = time.Now()
	}
	m.scannedAt["/web"] = old
	m.staleRepos["/web"] = true
	m.prState.repositories["/web"] = "org/web"
	m.prState.snapshots["org/web"] = prSnapshot{err: gh.ErrRateLimited}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Git: STALE") || !strings.Contains(view, "PRs: STALE") {
		t.Fatal("selected repository masked stale data elsewhere")
	}
	if details := ansi.Strip(strings.Join(m.dataStatusLines(m.width), "\n")); !strings.Contains(details, old.Local().Format("Jan 02 15:04:05 MST")) || !strings.Contains(details, "web") {
		t.Fatal("status details lost the oldest scan or failing repository")
	}
	m.excludedRepos["/web"] = true
	if view = ansi.Strip(m.View()); strings.Contains(view, "STALE") {
		t.Fatal("excluded repository spoiled freshness")
	}
	delete(m.scannedAt, "/empty")
	if !strings.Contains(m.View(), "Git: PARTIAL") {
		t.Fatal("missing local scan was presented as current")
	}
}

func TestContributorFrameStaysPinnedWhilePaging(t *testing.T) {
	m := statisticsViewportFixture()
	before := strings.Split(ansi.Strip(m.View()), "\n")
	m = statisticsViewportKey(m, tea.KeyEnd)
	after := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Join(before[:screenHeaderRows], "\n") != strings.Join(after[:screenHeaderRows], "\n") {
		t.Fatal("paging scrolled the frame away")
	}
	if !strings.Contains(strings.Join(after, "\n"), "less") || !strings.Contains(after[m.height-2], "back") {
		t.Fatal("final detail content or navigation is unreachable")
	}
}

func TestContributorRefreshPreservesDetail(t *testing.T) {
	m := refreshFixture(t)
	m.viewMode = ViewOperative
	m.activeAuthorID, m.activeOperative = m.authors[0].ID, m.authors[0].Name
	next, cmd := m.handleKey(key("R"))
	after := next.(Model)
	if cmd == nil || !after.refreshing || after.viewMode != ViewOperative || after.activeAuthorID != m.activeAuthorID {
		t.Fatal("advertised refresh did not preserve contributor detail")
	}
}

func TestLeaderboardSelectionFitsSharedFrame(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 18}, {120, 40}} {
		m := modelWithData()
		m.width, m.height = size[0], size[1]
		base := m.authors[0]
		m.authors = nil
		for i := range 50 {
			author := base
			author.Name = fmt.Sprintf("Person%03d", i)
			m.authors = append(m.authors, author)
		}
		m.selectedRow = 49
		m.clampScroll()
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Person049") || !strings.Contains(view, "of 50") {
			t.Fatalf("%dx%d hid the selection or table position:\n%s", m.width, m.height, view)
		}
	}
}

func TestSwitchingViewsPreservesGlobalStatusAndCoverage(t *testing.T) {
	m := prFixture()
	m.width, m.height = 220, 50
	m.loadedRepos = m.loadedRepos[:2]
	m.failedRepos = []string{"empty"}
	m.staleRepos["/empty"] = true
	m.prState.snapshots = nil
	m.prState.loading = true
	worklist := strings.Split(ansi.Strip(m.View()), "\n")
	m.viewMode = ViewAggregate
	leaderboard := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Join(worklist[1:screenHeaderRows], "\n") != strings.Join(leaderboard[1:screenHeaderRows], "\n") {
		t.Fatal("switching views changed the global scope or freshness text")
	}
	if worklist[m.height-1] != leaderboard[m.height-1] {
		t.Fatal("coverage text or indentation differs between views")
	}
	for _, lines := range [][]string{worklist, leaderboard} {
		if !strings.Contains(lines[0], "2/3 repos") || !strings.Contains(lines[3], "STALE") {
			t.Fatal("global header masked a failed repository")
		}
		if strings.Contains(strings.Join(lines[screenHeaderRows:], "\n"), "update failed") {
			t.Fatal("global warning was duplicated in one screen body")
		}
	}
	if details := strings.Join(m.dataStatusLines(m.width), "\n"); !strings.Contains(details, "1 repository updates failed") || !strings.Contains(details, "empty") {
		t.Fatal("global status details lost failed repository coverage")
	}
}
