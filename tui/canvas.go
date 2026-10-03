package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The shared dark canvas also styles retained statistics and inspectors.
// Worklist owns its layout in terminal cells, independently of ANSI styling.
var (
	worklistBase   = lipgloss.NewStyle().Foreground(lipgloss.Color("#EFF4FA")).Background(lipgloss.Color("#0F1922"))
	worklistAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("#9FD0FF"))
	worklistBand   = worklistAccent.Background(lipgloss.Color("#24445D"))
)

func worklistLine(s string, width int) string { return padCells(s, width) }
func worklistCompactBanner(width int) []string {
	return []string{worklistBand.Render(worklistLine("  BIGBOARD / Worklist", max(1, width)))}
}

// Restore the canvas after nested Lipgloss spans reset their SGR attributes.
// Derive escapes from the active renderer so NO_COLOR / ASCII stays respected.
func worklistCanvas(content string, width, height int) string {
	lines := strings.Split(content, "\n")
	// A theme may pad a view, never discard its content. View-specific
	// renderers own viewport sizing and navigation.
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", max(0, width)))
	}
	content = strings.Join(lines, "\n")
	prefix, suffix, _ := strings.Cut(worklistBase.Render("X"), "X")
	if prefix == "" {
		return content
	}
	content = strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+prefix)
	return prefix + content + suffix
}
