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
	m, _ = applyLocal(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(m.View(), "FINAL CAUSE") {
		t.Fatal("cannot inspect end of long diagnostic")
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
