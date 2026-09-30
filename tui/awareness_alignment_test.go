package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func borderColumns(line string, glyphs string) []int {
	var result []int
	column := 0
	for _, r := range ansi.Strip(line) {
		if strings.ContainsRune(glyphs, r) {
			result = append(result, column)
		}
		column += ansi.StringWidth(string(r))
	}
	return result
}

func TestAwarenessSummaryBordersAlignWithAndWithoutANSI(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		for _, count := range []int{0, 14, 123456789} {
			m := awarenessFixture()
			m.authors[0].Commits = count
			lines := strings.Split(m.awarenessSummary(count), "\n")
			if len(lines) != 4 {
				t.Fatalf("box height=%d", len(lines))
			}
			border := lipgloss.ThickBorder()
			want := borderColumns(lines[0], border.TopLeft+border.TopRight)
			if len(want) != 8 || want[0] != 2 {
				t.Fatalf("top columns=%v", want)
			}
			for i, line := range lines[1:] {
				glyphs := border.Left + border.Right
				if i == len(lines)-2 {
					glyphs = border.BottomLeft + border.BottomRight
				}
				got := borderColumns(line, glyphs)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("profile=%v row=%d borders=%v want=%v", profile, i+1, got, want)
				}
				if ansi.StringWidth(line) != ansi.StringWidth(lines[0]) {
					t.Fatal("box row widths differ")
				}
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
