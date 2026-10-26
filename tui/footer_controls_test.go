package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactFooterGroupsContextAndLeavesFullListInHelp(t *testing.T) {
	for _, width := range []int{40, 80, 120, 220} {
		m := boardLayoutFixture()
		m.width, m.height = width, 36
		m = keyboardContractPress(t, m, "p")
		footer := ansi.Strip(screenFooter(m.prFooter(m.visiblePRs()), width))
		if ansi.StringWidth(footer) > width-4 || !strings.Contains(footer, "? help") || !strings.Contains(footer, "f filters") || !strings.Contains(footer, "/ find") {
			t.Fatalf("%d lost essential footer controls: %s", width, footer)
		}
		for _, hidden := range []string{"u author", "k checks", "d state", "x clear", "s/S sort", "R refresh"} {
			if strings.Contains(footer, hidden) {
				t.Fatalf("full key list leaked into footer: %s", footer)
			}
		}
		if width >= 120 && (strings.Count(footer, "│") != 2 || !strings.Contains(footer, "r refresh") || ansi.StringWidth(footer) > 120) {
			t.Fatalf("footer did not stay compact and grouped: %s", footer)
		}
		bindings := m.currentHelp().sections
		keys := map[string]bool{}
		for _, section := range bindings {
			for _, binding := range section.bindings {
				keys[binding.key] = true
			}
		}
		for _, key := range []string{"u", "k", "d", "x", "i", "r"} {
			if !keys[key] {
				t.Fatalf("full help lost %s", key)
			}
		}
	}
}

func TestRefreshAndInclusionHaveDistinctLowercaseKeys(t *testing.T) {
	for _, board := range []string{"a", "c", "p"} {
		base := boardLayoutFixture()
		base.width, base.height = 120, 36
		base = keyboardContractPress(t, base, board)
		included := keyboardContractPress(t, base, "i")
		if included.viewMode != ViewRepoOverlay {
			t.Fatalf("%s: i did not open inclusion", board)
		}
		for _, key := range []string{"r", "R"} {
			next, cmd := base.Update(keyboardContractKey(key))
			m := next.(Model)
			if cmd == nil || !m.refreshing || m.viewMode != base.viewMode || m.showPRs != base.showPRs {
				t.Fatalf("%s/%s: refresh changed location or did not start", board, key)
			}
			m.Close()
		}
		search := keyboardContractPress(t, base, "/", "r", "i")
		if !search.helpSearchActive() || search.refreshing || search.viewMode == ViewRepoOverlay {
			t.Fatalf("%s: search interpreted literal keys", board)
		}
	}
}
