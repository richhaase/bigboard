package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFooterKeepsContextAndWidthPriorities(t *testing.T) {
	for _, test := range []struct {
		name    string
		width   int
		context bool
		search  bool
		pane    bool
		want    string
	}{
		{"narrow PRs", 40, false, false, false, "c/a/p  │ f filters  / find │ ? help"},
		{"before board label", 69, false, false, false, "c/a/p  │ f filters  / find  Enter open  o browser │ ? help"},
		{"compact board label", 70, false, false, false, "c/a/p boards │ f filters  / find  Enter open  o browser │ ? help"},
		{"before full board names", 159, false, false, false, "c/a/p boards │ f filters  / find  Enter open  o browser  t date range │ ? help  r refresh"},
		{"full board names", 160, false, false, false, "c contributors  a activity  p PRs │ f filters  / find  Enter open  o browser  t date range │ ? help  r refresh"},
		{"context return", 40, true, false, false, "c/a/p  │ Esc back  f filters │ ? help"},
		{"search", 160, false, true, false, "Enter accept  Esc cancel │ F1 help"},
		{"focused contributors", 40, false, false, true, "Tab ranking  ↑↓ scroll │ ? help"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := prFixture()
			m.width, m.height = test.width, 36
			m.prContext.active, m.prUI.searching = test.context, test.search
			footer := m.prFooter(m.visiblePRs())
			if test.pane {
				m.contributorPane.focused = true
				footer = m.worklistFooter(m.width, m.height, false)
			}
			if got := ansi.Strip(screenFooter(footer, test.width)); got != test.want {
				t.Fatalf("want %q, got %q", test.want, got)
			}
		})
	}
}

func TestFooterModeAndHintOrderDoNotDependOnLabels(t *testing.T) {
	local := []footerHint{{"Enter", "accept"}, {"Esc", "cancel"}}
	before := append([]footerHint(nil), local...)
	footer := footerHints{local: local}
	browse := ansi.Strip(fitFooter(footer, 160))
	for _, want := range []string{"c contributors", "Enter accept  Esc cancel", "? help", "r refresh"} {
		if !strings.Contains(browse, want) {
			t.Fatalf("labels changed browsing mode: %s", browse)
		}
	}
	footer.editing = true
	if got := ansi.Strip(fitFooter(footer, 160)); got != "Enter accept  Esc cancel │ F1 help" {
		t.Fatalf("explicit editing mode ignored: %s", got)
	}
	for _, width := range []int{20, 40, 70, 160} {
		fitFooter(footer, width)
	}
	if !reflect.DeepEqual(local, before) {
		t.Fatal("fitting changed the caller's hints")
	}
}
