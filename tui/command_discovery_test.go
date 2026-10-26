package tui

import (
	"strings"
	"testing"
)

func commandDiscoveryBindings(t *testing.T, m Model) map[string]string {
	t.Helper()
	bindings := map[string]string{}
	for _, section := range m.currentHelp().sections {
		for _, binding := range section.bindings {
			if previous, exists := bindings[binding.key]; exists {
				t.Fatalf("help repeats %q: %q and %q", binding.key, previous, binding.action)
			}
			bindings[binding.key] = binding.action
		}
	}
	return bindings
}

func TestCommandDiscoverySharedActionsAppearOnce(t *testing.T) {
	contexts := keyboardContractOrdinaryContexts()
	for _, context := range keyboardContractOwnedContexts() {
		if context.name == "PR list" || context.name == "PR detail" || context.name == "commit inspector" {
			contexts = append(contexts, context)
		}
	}
	contexts = append(contexts, keyboardContractContext{"contributor pane", func(t *testing.T) Model {
		return keyboardContractPress(t, activityContributorScrollFixture(), "tab")
	}})
	for _, context := range contexts {
		t.Run(context.name, func(t *testing.T) {
			bindings := commandDiscoveryBindings(t, context.setup(t))
			for _, key := range []string{"f", "t", "x"} {
				if bindings[key] == "" {
					t.Fatalf("shared action %q is missing", key)
				}
			}
			if bindings["i"] != "Repository controls: choose tracked repositories" {
				t.Fatalf("repository controls has an inconsistent name: %q", bindings["i"])
			}
		})
	}
}

func TestCommandDiscoveryFilterHelpDescribesOwnedControls(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		for _, editing := range []bool{false, true} {
			name := board + "/choices"
			if editing {
				name = board + "/text"
			}
			t.Run(name, func(t *testing.T) {
				m := boardLayoutFixture()
				m.width, m.height = 120, 36
				m = keyboardContractPress(t, m, board, "f")
				if editing {
					m = keyboardContractPress(t, m, "/")
				}
				bindings := commandDiscoveryBindings(t, m)
				for _, key := range []string{"e", "r", "i", "f", "t", "x"} {
					if _, exists := bindings[key]; exists {
						t.Fatalf("filter help advertises inactive board action %q", key)
					}
				}
				if editing {
					if m.currentHelp().title != "Filter text" || bindings["F1"] == "" || bindings["Ctrl+C"] == "" || !strings.Contains(bindings["Type / Backspace"], "literal") {
						t.Fatalf("text editor help is incomplete: %#v", bindings)
					}
					if !strings.Contains(bindings["Enter"], "Keep this edit") || !strings.Contains(bindings["Esc"], "Cancel this edit") {
						t.Fatalf("text editor help confuses editing with applying filters: %#v", bindings)
					}
				} else if bindings["Enter"] != "Apply all pending changes" || !strings.Contains(bindings["Esc"], "Cancel pending changes") || bindings["Space"] == "" {
					t.Fatalf("filter help lost staging, Apply or Cancel: %#v", bindings)
				}
			})
		}
	}
}

func TestCommandDiscoveryPRBackMatchesCurrentContext(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		keys       []string
	}{
		{"root", "Stay at the PRs root; keep filters", []string{"p"}},
		{"details", "Close details; return to the PR list; keep filters", []string{"p", "enter"}},
		{"from Activity", "Return to Activity; restore saved PR filters", []string{"v"}},
		{"details from Activity", "Close details; return to the PR list; keep filters", []string{"v", "enter"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := boardLayoutFixture()
			m.width, m.height = 120, 36
			m = keyboardContractPress(t, m, tc.keys...)
			bindings := commandDiscoveryBindings(t, m)
			if bindings["← / Esc"] != tc.want {
				t.Fatalf("Back help = %q, want %q", bindings["← / Esc"], tc.want)
			}
		})
	}
}

func TestCommandDiscoveryContributorPaneOffersFocusInsteadOfOpen(t *testing.T) {
	m := keyboardContractPress(t, activityContributorScrollFixture(), "tab")
	if !m.contributorPane.focused {
		t.Fatal("fixture did not focus the contributor pane")
	}
	bindings := commandDiscoveryBindings(t, m)
	for key := range bindings {
		if strings.Contains(key, "Enter") {
			t.Fatalf("contributor pane advertises an unavailable Enter action: %q", key)
		}
	}
	if bindings["← / Esc"] != "Return focus to ranking" || bindings["Tab / Shift+Tab"] != "Return focus to ranking" {
		t.Fatalf("contributor help does not explain returning focus: %#v", bindings)
	}
}
