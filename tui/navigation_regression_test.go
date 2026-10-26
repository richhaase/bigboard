package tui

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
	"strings"
	"testing"
	"time"
)

func TestLeaderboardPreservesContributorAcrossFilterAndSort(t *testing.T) {
	for _, key := range []string{"q", "esc", "left", "/", "s", "S", "b", "enter", "backspace"} {
		t.Run(key, func(t *testing.T) {
			m := modelWithData()
			m.allRecords = append(m.allRecords, git.CommitRecord{Author: "Linus", Email: "linus@example.com", Date: time.Now(), Added: 25, RepoName: "engine"})
			m.sortField = stats.SortByTotal
			m.recomputeAuthors()
			if key == "s" || key == "S" || key == "b" {
				m.selectedRow = 1
			} else {
				m.filterQuery = "Grace"
			}
			if key == "enter" || key == "backspace" {
				m.searching = true
			}
			want := "email:grace@x.com"
			if key == "left" || key == "backspace" {
				kind := tea.KeyLeft
				if key == "backspace" {
					kind = tea.KeyBackspace
				}
				next, _ := m.Update(tea.KeyMsg{Type: kind})
				m = next.(Model)
			} else {
				m = pressAwareness(m, key)
			}
			if got := m.displayedAuthors()[m.selectedRow].ID; got != want {
				t.Fatalf("%s selected %q, want %q", key, got, want)
			}
			if m.selectedRow < m.scrollOffset || m.selectedRow >= m.scrollOffset+m.tableViewport() {
				t.Fatal("selected contributor outside viewport")
			}
		})
	}
	m := modelWithData()
	m.filterQuery, m.searching = "Grace", true
	want := "email:grace@x.com"
	m = pressAwareness(m, "esc")
	if m.searching || m.filterQuery != "" || m.displayedAuthors()[m.selectedRow].ID != want {
		t.Fatal("canceling search changed selection")
	}
}

func TestLeaderboardResizeKeepsSelectedContributorVisible(t *testing.T) {
	m := modelWithData()
	m.authors = nil
	for i := 0; i < 50; i++ {
		m.authors = append(m.authors, stats.AuthorStats{ID: fmt.Sprint(i), Name: fmt.Sprint(i)})
	}
	m.height, m.selectedRow = 100, 35
	m.clampScroll()
	for _, size := range [][2]int{{100, 24}, {40, 18}, {120, 60}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		if m.selectedRow != 35 || m.selectedRow < m.scrollOffset || m.selectedRow >= m.scrollOffset+m.tableViewport() {
			t.Fatalf("size=%v selected=%d viewport=[%d,%d)", size, m.selectedRow, m.scrollOffset, m.scrollOffset+m.tableViewport())
		}
	}
}

func TestRepoOverlayScrollsWithSelectionAndResize(t *testing.T) {
	m := awarenessFixture()
	m.loadedRepos = nil
	for i := 0; i < 45; i++ {
		m.loadedRepos = append(m.loadedRepos, git.Repository{ID: fmt.Sprintf("/r%d", i), Name: fmt.Sprintf("repo-%02d", i)})
	}
	m = pressAwareness(m, "i")
	check := func() {
		t.Helper()
		out := m.View()
		if len(strings.Split(out, "\n")) > m.height {
			t.Fatalf("overlay exceeds height %d", m.height)
		}
		name := m.loadedRepos[m.overlayCursor].Name
		selected := false
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("overlay exceeds width %d", m.width)
			}
			if strings.Contains(line, "▸") && strings.Contains(line, name) {
				selected = true
			}
		}
		if !selected || !strings.Contains(out, "Space") {
			t.Fatalf("selection or controls missing at row %d:\n%s", m.overlayCursor, out)
		}
	}
	for _, size := range [][2]int{{100, 24}, {40, 18}, {100, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		check()
		for i := 0; i < 44; i++ {
			m = pressAwareness(m, "down")
			check()
		}
		m = pressAwareness(m, " ")
		if !m.overlayExcluded["/r44"] {
			t.Fatal("wrong repository toggled")
		}
		m = pressAwareness(m, " ")
		for i := 0; i < 44; i++ {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
			m = next.(Model)
			check()
		}
	}
}

func TestErrorScreenOwnsKeysAndRetry(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		for _, key := range []string{"/", "i", "t"} {
			t.Run(fmt.Sprintf("%d/%s", view, key), func(t *testing.T) {
				m := awarenessFixture()
				m.viewMode = view
				m.err = errors.New("all repositories failed")
				m.loadedRepos = nil
				m = pressAwareness(m, key)
				if m.showHelp || m.glance.searching || m.searching || m.showPRs || m.filters.open || m.viewMode != view {
					t.Fatal("error screen changed hidden navigation")
				}
				next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
				m = next.(Model)
				if cmd == nil || !m.refreshing {
					t.Fatal("retry was swallowed")
				}
			})
		}
	}
	m := awarenessFixture()
	m.err = errors.New("all repositories failed")
	m.scanErrors = map[string]string{"/api": "unreadable"}
	m.glance.searching, m.searching, m.showPRs = true, true, true
	m.filters.open = true
	m = pressAwareness(m, "e")
	if !m.showScanErrors {
		t.Fatal("error inspector unavailable")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "q")
	if !m.quitting {
		t.Fatal("error screen swallowed quit")
	}
}
