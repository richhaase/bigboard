package tui

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func TestHiddenModalConfirmationWaitsForVisibleDialog(t *testing.T) {
	for _, kind := range []string{"settings", "identity", "repository", "filters"} {
		for _, dimension := range []string{"width", "height"} {
			t.Run(kind+"/"+dimension, func(t *testing.T) {
				calls := 0
				var m Model
				switch kind {
				case "settings":
					options := settingsTestOptions()
					options.Save = func(context.Context, SettingsConfig, []git.Repository) ([]git.Repository, error) {
						calls++
						return nil, nil
					}
					m = settingsTestOpen(t, options)
					next, cmd := applyLocal(t, m, key("enter"))
					m = settingsTestApplyCommand(t, next, cmd)
					m = keyboardContractPress(t, m, "enter")
				case "identity":
					m = keyboardContractPress(t, stagedIdentityMerge(t), "enter")
					m.options.SaveIdentities = func(map[string]git.Identity) error { calls++; return nil }
				case "repository":
					m = awarenessFixture()
					m.repositories = []git.Repository{{ID: "org/listed", Name: "org/listed"}}
					m.cacheInfo.Orphans = []git.Repository{{ID: "org/orphan", Name: "org/orphan"}}
					m.options.RemoveCachedRepository = func(string) error { calls++; return nil }
					m.options.CacheInfo = func() (git.CacheInfo, error) { return git.CacheInfo{}, nil }
					m.openRepositoryControls()
					m.overlayCursor = 1
					m = keyboardContractPress(t, m, "d")
					if m.cacheRemoveConfirm == "" {
						t.Fatal("removal was not staged")
					}
				case "filters":
					m = keyboardContractPress(t, boardLayoutFixture(), "p", "f")
					m.filters.query = "pending search"
				}
				t.Cleanup(m.Close)
				minimum := m.modalMinimum()
				width, height := minimum.width, minimum.height
				if dimension == "width" {
					width--
				} else {
					height--
				}
				m, _ = applyLocal(t, m, tea.WindowSizeMsg{Width: width, Height: height})
				if !strings.Contains(ansi.Strip(m.View()), "Resize") {
					t.Fatal("expected resize placeholder")
				}
				m = keyboardContractPress(t, m, "f1", "enter")
				if m.showHelp {
					t.Fatal("Enter did not dismiss help")
				}
				m, cmd := applyLocal(t, m, key("enter"))
				if cmd != nil || calls != 0 {
					t.Fatal("hidden confirmation scheduled an action")
				}
				if kind == "filters" && (!m.filters.open || m.prUI.query != "") {
					t.Fatal("hidden filter draft was applied")
				}
				canceled := keyboardContractPress(t, m, "esc")
				switch kind {
				case "settings":
					if !canceled.settings.open || canceled.settings.step != 1 {
						t.Fatal("Back failed from hidden settings review")
					}
				case "identity":
					if !canceled.identityMerge.open || canceled.identityMerge.review {
						t.Fatal("Back failed from hidden identity review")
					}
				case "repository":
					if canceled.cacheRemoveConfirm != "" || canceled.viewMode != ViewRepoOverlay {
						t.Fatal("cancel should dismiss only removal confirmation")
					}
				case "filters":
					if canceled.filters.open {
						t.Fatal("hidden filter cancellation failed")
					}
				}
				m, _ = applyLocal(t, m, tea.WindowSizeMsg{Width: 100, Height: 32})
				m, cmd = applyLocal(t, m, key("enter"))
				if kind == "filters" {
					if m.filters.open || m.prUI.query != "pending search" {
						t.Fatal("visible filter could not apply preserved draft")
					}
				} else {
					if cmd == nil {
						t.Fatal("visible confirmation did not resume")
					}
					cmd()
					if calls != 1 {
						t.Fatalf("confirmation calls = %d", calls)
					}
				}
			})
		}
	}
}

func TestHiddenModalEditorsPauseButCanCancelAndOpenHelp(t *testing.T) {
	for _, kind := range []string{"settings", "identity", "filters"} {
		t.Run(kind, func(t *testing.T) {
			var m Model
			switch kind {
			case "settings":
				m = keyboardContractPress(t, settingsTestOpen(t, settingsTestOptions()), "n", "draft")
			case "identity":
				m = keyboardContractPress(t, stagedIdentityMerge(t), "n", "draft")
			case "filters":
				m = keyboardContractPress(t, boardLayoutFixture(), "f", "/", "draft")
			}
			t.Cleanup(m.Close)
			input := func(m Model) string {
				switch kind {
				case "settings":
					return m.settings.input
				case "identity":
					return m.identityMerge.input
				default:
					return m.filters.input
				}
			}
			before := input(m)
			m, _ = applyLocal(t, m, tea.WindowSizeMsg{Width: 29, Height: 9})
			m = keyboardContractPress(t, m, "a", "x", "backspace", "ctrl+u", "enter")
			if input(m) != before || !m.helpSearchActive() {
				t.Fatal("hidden editor modified or accepted its draft")
			}
			m = keyboardContractPress(t, m, "f1")
			if !m.showHelp || m.currentHelp().title != "Resize dialog" {
				t.Fatal("hidden editor help was unavailable")
			}
			m = keyboardContractPress(t, m, "esc", "esc")
			if m.helpSearchActive() {
				t.Fatal("hidden editor could not cancel")
			}
		})
	}
}

func TestSettingsHelpBoardsAndStepCommands(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		for _, editing := range []bool{false, true} {
			m := settingsTestOpen(t, settingsTestOptions())
			if editing {
				m = keyboardContractPress(t, m, "n", "draft")
			}
			m = keyboardContractPress(t, m, "f1", board)
			if m.settings.open || m.showHelp || m.showPRs != (board == "p") {
				t.Fatalf("settings help failed board switch %q", board)
			}
		}
	}
	m := settingsTestOpen(t, settingsTestOptions())
	for step := range 3 {
		m.settings.step = step
		bindings := commandDiscoveryBindings(t, m)
		if bindings["Enter"] == "" || bindings["Esc / ←"] == "" {
			t.Fatal("missing confirmation/cancel help")
		}
		for _, key := range []string{"↑ / ↓", "PgUp / PgDn", "Home / End", "Space", "/", "r"} {
			if (bindings[key] != "") != (step < 2) {
				t.Fatalf("step %d has wrong command %q", step, key)
			}
		}
		if (bindings["n"] != "") != (step == 0) || (bindings["A"] != "") != (step == 1) {
			t.Fatal("step-specific commands leaked")
		}
	}
	m.settings.required, m.showHelp = true, true
	m = keyboardContractPress(t, m, "p")
	if !m.settings.open || !m.showHelp || m.showPRs {
		t.Fatal("required setup allowed board departure")
	}
	m.settings.busy = "saving"
	m = keyboardContractPress(t, m, "p", "esc", "q")
	if !m.settings.open || m.quitting {
		t.Fatal("saving controls changed")
	}
}

func TestInputLineKeepsUnicodeTailAndCursor(t *testing.T) {
	for _, input := range []string{"", "short", "é", strings.Repeat("界👩🏽‍💻é", 20) + "TAIL"} {
		for width := range 64 {
			got := inputLine("Find: ", input, width)
			if !utf8.ValidString(got) || ansi.StringWidth(got) > width {
				t.Fatalf("width %d: invalid input line %q", width, got)
			}
			if width > 0 && !strings.HasSuffix(got, "▌") {
				t.Fatalf("cursor hidden at width %d: %q", width, got)
			}
			if width >= 12 && strings.HasSuffix(input, "TAIL") && !strings.HasSuffix(got, "TAIL▌") {
				t.Fatalf("tail hidden: %q", got)
			}
		}
	}
}

func TestBoardSearchRemainsVisibleAndLiteralAtNarrowWidths(t *testing.T) {
	query := strings.Repeat("界", 40) + "TAIL"
	for _, board := range []string{"a", "c", "p"} {
		t.Run(board, func(t *testing.T) {
			m := keyboardContractPress(t, boardLayoutFixture(), board, "/", query)
			m, _ = applyLocal(t, m, tea.WindowSizeMsg{Width: 40, Height: 18})
			label := m.boardFiltersLabel(40)
			if !strings.Contains(label, "Find:") || !strings.HasSuffix(label, "TAIL▌") || strings.Contains(label, "f edit") || strings.Contains(label, "x clear") {
				t.Fatalf("active search hidden by hints: %q", label)
			}
			m = keyboardContractPress(t, m, "x", "f")
			if !strings.HasSuffix(m.boardFiltersLabel(40), "TAILxf▌") || !m.helpSearchActive() {
				t.Fatal("search shortcuts were not literal")
			}
			m = keyboardContractPress(t, m, "esc")
			if m.helpSearchActive() {
				t.Fatal("search could not cancel")
			}
		})
	}
}
