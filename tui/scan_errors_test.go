package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestScanErrorsCommitAtomicallyAndClearOnRecovery(t *testing.T) {
	m := refreshFixture(t)
	_ = m.startLocalRefresh()
	first := m.repositories[0]
	m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: first, Err: errors.New("permission denied\x1b[2J\ncheck permissions")})
	if len(m.scanErrors) != 0 {
		t.Fatal("partial generation published diagnostics")
	}
	for _, repo := range m.repositories[1:] {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	}
	if got := m.scanErrors[first.ID]; got != "permission denied check permissions" {
		t.Fatalf("unsafe or lost diagnosis: %q", got)
	}
	next, _ := m.Update(key("e"))
	m = next.(Model)
	if !m.showScanErrors || !strings.Contains(ansi.Strip(m.View()), "permission denied") {
		t.Fatal("diagnostics unavailable")
	}
	before := m.scanErrors
	_ = m.startLocalRefresh()
	for _, repo := range m.repositories {
		m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
	}
	if len(m.scanErrors) != 0 || len(before) != 1 {
		t.Fatal("recovery did not clear errors or mutated previous snapshot")
	}
	if !strings.Contains(m.View(), "errors have cleared") {
		t.Fatal("open diagnostic view retained stale error")
	}
}

func TestScanErrorViewScrollsLongDiagnosticAndPreservesNavigation(t *testing.T) {
	m := refreshFixture(t)
	m.width = 40
	m.height = 12
	m.scanErrors = map[string]string{"/api": strings.Repeat("explanation ", 100) + "FINAL CAUSE"}
	selected := m.selectedRepoID
	m, _ = applyLocal(t, m, key("e"))
	m, _ = applyLocal(t, m, key("d"))
	m, _ = applyLocal(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(strings.NewReplacer(" ", "", "\n", "", "│", "").Replace(ansi.Strip(m.View())), "FINALCAUSE") {
		t.Fatalf("cannot inspect end of long diagnostic (%d):\n%s", m.scanErrorOffset, ansi.Strip(m.View()))
	}
	m, _ = applyLocal(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showScanErrors || m.selectedRepoID != selected {
		t.Fatal("diagnostic overlay changed navigation")
	}
}

func TestSearchTypingDoesNotOpenScanErrors(t *testing.T) {
	m := refreshFixture(t)
	m.scanErrors = map[string]string{"/api": "failed"}
	m.glance.searching = true
	m, _ = applyLocal(t, m, key("e"))
	if m.showScanErrors {
		t.Fatal("typing search text opened diagnostics")
	}
}

func TestScanErrorsModalKeepsBoardAndSeparatesTechnicalDetails(t *testing.T) {
	for _, board := range []string{"w", "l", "p"} {
		for _, size := range [][2]int{{40, 12}, {80, 24}, {160, 48}} {
			m := boardLayoutFixture()
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, board, "down")
			before := ansi.Strip(m.View())
			m.scanErrors = map[string]string{m.repositories[0].ID: "partial clone /example/cache/project could not be scanned with automatic fetching disabled: exit status 128: fatal: missing object END-OF-DIAGNOSTIC"}
			m = keyboardContractPress(t, m, "e")
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "Repository update errors") || !strings.Contains(view, "╭") || strings.Contains(view, "/example/cache") {
				t.Fatalf("%s/%v did not render the summary modal:\n%s", board, size, view)
			}
			lines := strings.Split(view, "\n")
			if len(lines) != m.height {
				t.Fatal("modal changed terminal height")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("modal overflowed terminal width")
				}
			}
			if size[0] >= 80 && !strings.Contains(view, "BIGBOARD /") {
				t.Fatal("modal replaced the underlying board")
			}
			m = keyboardContractPress(t, m, "d", "end")
			if !strings.Contains(strings.NewReplacer(" ", "", "\n", "", "│", "", "┃", "").Replace(ansi.Strip(m.View())), "END-OF-DIAGNOSTIC") {
				t.Fatalf("%s/%v technical details are not fully accessible (%d):\n%s", board, size, m.scanErrorOffset, ansi.Strip(m.View()))
			}
			m = keyboardContractPress(t, m, "?", "esc")
			if !m.showScanErrors || !m.scanErrorDetails {
				t.Fatal("contextual help lost diagnostic state")
			}
			m = keyboardContractPress(t, m, "esc")
			m.scanErrors = nil
			if ansi.Strip(m.View()) != before {
				t.Fatal("closing modal changed board state")
			}
		}
	}
}

func TestScanErrorRefreshRetainsModal(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m.scanErrors = map[string]string{m.repositories[0].ID: "fetch failed"}
	m = keyboardContractPress(t, m, "p", "down", "e")
	next, cmd := m.Update(keyboardContractKey("r"))
	refreshed := next.(Model)
	defer refreshed.Close()
	if cmd == nil || !refreshed.refreshing || !refreshed.showScanErrors || refreshed.prRow != m.prRow || !strings.Contains(ansi.Strip(refreshed.View()), "Refreshing…") {
		t.Fatal("refresh lost the modal or its board selection")
	}
}
