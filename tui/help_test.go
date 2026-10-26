package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpAvailableAcrossScreensAndRestoresNavigation(t *testing.T) {
	contexts := append(keyboardContractOrdinaryContexts(), keyboardContractOwnedContexts()...)
	contexts = append(contexts,
		keyboardContractContext{"loading", func(t *testing.T) Model {
			m := awarenessFixture()
			m.loading = true
			return m
		}},
		keyboardContractContext{"error", func(t *testing.T) Model {
			m := awarenessFixture()
			m.err = errors.New("unreadable")
			return m
		}},
		keyboardContractContext{"custom range", func(t *testing.T) Model {
			return keyboardContractPress(t, awarenessFixture(), "t", "d", "enter")
		}},
	)
	for _, tc := range contexts {
		if tc.name == "help" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			for _, opener := range []string{"?", "f1"} {
				m := tc.setup(t)
				before := keyboardContractState(m)
				m = keyboardContractPress(t, m, opener)
				if !m.showHelp || m.currentHelp().title == "" || !strings.Contains(ansi.Strip(m.View()), "Help ·") {
					t.Fatalf("%s has no contextual help", opener)
				}
				m = keyboardContractPress(t, m, "R", "r", "t", "right", " ")
				m = keyboardContractPress(t, m, "esc")
				if m.showHelp || keyboardContractState(m) != before {
					t.Fatal("help leaked a shortcut or changed the underlying screen")
				}
			}
		})
	}
}

func TestHelpContextContainsOnlyRelevantControls(t *testing.T) {
	for _, tc := range []struct {
		name, want, absent string
		setup              func(Model) Model
	}{
		{"repository list", "Open repository directories", "Next / previous lens", func(m Model) Model { return m }},
		{"people", "Show this contributor's activity", "Shared commits / contributor overlap", func(m Model) Model {
			return keyboardContractPress(t, m, "enter", "enter", "2")
		}},
		{"related", "Shared commits / contributor overlap", "Cycle overview sort", func(m Model) Model {
			return keyboardContractPress(t, m, "enter", "enter", "3")
		}},
		{"PR list", "Open the selected PR in your browser", "Show / hide bots", func(m Model) Model { m.showPRs = true; return m }},
		{"PR detail", "Scroll PR details", "Select a pull request", func(m Model) Model { m.showPRs, m.prDetail = true, true; return m }},
		{"repository controls", "Include / exclude the repository", "Refresh repos and PRs", func(m Model) Model { m.viewMode = ViewRepoOverlay; return m }},
		{"range", "Apply all pending changes", "Show / hide bots", func(m Model) Model { m.openRangePicker(); return m }},
		{"contributor", "Scroll contributor details", "Change sort / reverse order", func(m Model) Model { m.viewMode = ViewOperative; return m }},
		{"scan errors", "Retry repository refresh", "Include / exclude the repository", func(m Model) Model { m.showScanErrors = true; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(awarenessFixture())
			text := ansi.Strip(strings.Join(m.helpLines(80), "\n"))
			if !strings.Contains(text, tc.want) || strings.Contains(text, tc.absent) {
				t.Fatalf("wrong controls for %s:\n%s", tc.name, text)
			}
		})
	}
}

func TestHelpModalFitsTerminalAndKeepsBackdrop(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}, {20, 8}, {1, 1}} {
		m := awarenessFixture()
		m.width, m.height = size[0], size[1]
		before := ansi.Strip(m.View())
		m = keyboardContractPress(t, m, "?")
		view := ansi.Strip(m.View())
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Fatalf("%v: modal has %d lines", size, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("%v: modal exceeds terminal width: %q", size, line)
			}
		}
		if m.width >= 40 {
			if lines[0] != strings.Split(before, "\n")[0] || !strings.Contains(view, "╭") || !strings.Contains(view, "╰") || !strings.Contains(view, "close help") {
				t.Fatalf("%v: help is not a bounded modal over the current screen:\n%s", size, view)
			}
		}
	}
}

func TestHelpScrollAndDismissKeys(t *testing.T) {
	m := awarenessFixture()
	m.width, m.height = 40, 18
	m = keyboardContractPress(t, m, "?", "end")
	if m.helpOffset == 0 || !strings.Contains(ansi.Strip(m.View()), "location") {
		t.Fatal("cannot reach the final help bindings")
	}
	m = keyboardContractPress(t, m, "home")
	if m.helpOffset != 0 {
		t.Fatal("Home did not return to the first binding")
	}
	for _, closeKey := range []string{"?", "f1", "enter", "esc"} {
		m = keyboardContractPress(t, m, closeKey)
		if m.showHelp {
			t.Fatalf("%s did not dismiss help", closeKey)
		}
		m = keyboardContractPress(t, m, "?")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	if !m.showHelp || !strings.Contains(ansi.Strip(m.View()), "close help") {
		t.Fatal("resize lost the modal or its dismiss hint")
	}
}

func TestSearchKeepsQuestionMarksAndOffersF1Help(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		m := awarenessFixture()
		m.viewMode = view
		m = keyboardContractPress(t, m, "/", "?")
		query := m.glanceQuery()
		if view == ViewAggregate {
			query = m.filterQuery
		}
		if query != "?" || m.showHelp {
			t.Fatal("question mark escaped search")
		}
		m = keyboardContractPress(t, m, "f1")
		if !m.showHelp || m.currentHelp().title != "Search" {
			t.Fatal("F1 did not open search help")
		}
		m = keyboardContractPress(t, m, "esc")
		if !m.helpSearchActive() {
			t.Fatal("closing help ended the search")
		}
	}
}
