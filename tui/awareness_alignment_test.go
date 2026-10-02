package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestAwarenessSummaryIsOneScopedLineWithAndWithoutANSI(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		m := awarenessFixture()
		text := ansi.Strip(m.awarenessSummary(3))
		if strings.Contains(text, "\n") {
			t.Fatal("overview summary consumes multiple rows")
		}
		for _, want := range []string{"Range: All time", "3 commits", "2 people", "3 repositories"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q: %s", want, text)
			}
		}
	}
}
func TestAwarenessRowsShareGuttersAndCursorSlots(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, width := range []int{40, 95, 120} {
		repo := ansi.Strip(awarenessRow("  ▸  Repo", true, 0, width))
		person := ansi.Strip(awarenessRow("  ▸  Person", true, 0, width))
		for _, line := range []string{repo, person} {
			if ansi.StringWidth(line) > width || !strings.HasPrefix(line, "  ▸  ") {
				t.Fatalf("bad row %q", line)
			}
		}
		if ansi.StringWidth(repo) != width-2 || ansi.StringWidth(person) != width-2 {
			t.Fatal("row gutters differ")
		}
	}
}
