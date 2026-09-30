package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"strings"
	"testing"
)

func TestNightCanvasNeverCropsEitherDimension(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		source := StyleMagenta.Render("長いリポジトリ name: 123456 commits +987654 -456789 NET 530865") + "\n" + StyleNumeric.Render("tail: AI 37% · unknown ?")
		got := nightCanvas(source, 20, 1)
		if ansi.Strip(got) != ansi.Strip(source) {
			t.Fatalf("canvas changed overflow content: %q", ansi.Strip(got))
		}
		padded := nightCanvas("short", 20, 3)
		if lipgloss.Width(padded) != 20 || lipgloss.Height(padded) != 3 || !strings.HasPrefix(ansi.Strip(padded), "short") {
			t.Fatal("canvas no longer pads small content")
		}
	}
}
