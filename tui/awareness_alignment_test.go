package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestWorklistHeaderShowsRangeWithAndWithoutANSI(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		m := awarenessFixture()
		header := m.worklistHeader(120)
		if len(header) != 5 || !strings.Contains(ansi.Strip(header[1]), "Range: All time") {
			t.Fatalf("range missing from Worklist header: %v", header)
		}
		for _, line := range header {
			if strings.Contains(line, "\n") || ansi.StringWidth(line) > 120 {
				t.Fatalf("header exceeds one row: %q", line)
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
