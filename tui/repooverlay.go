package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderRepoOverlay renders the full-screen repo toggle overlay.
func (m Model) renderRepoOverlay() string {
	var sections []string

	// Header
	if m.width >= 82 {
		for i, line := range bannerLines {
			style := lipgloss.NewStyle().Foreground(ColorBannerGrad[i])
			sections = append(sections, "  "+style.Render(line))
		}
		sections = append(sections, "")
	}

	sections = append(sections, RenderSectionHeader("REPOSITORY CONTROL", m.width))
	sections = append(sections, "")

	// Repo list with checkboxes
	for i, name := range m.repoNames {
		checkbox := "[x]"
		if m.overlayExcluded[name] {
			checkbox = "[ ]"
		}

		cursor := "  "
		if i == m.overlayCursor {
			cursor = StyleCursor.Render("▸ ")
		}

		checkStyle := StyleCyan
		nameStyle := StyleAuthor
		if m.overlayExcluded[name] {
			checkStyle = StyleDimWhite
			nameStyle = StyleDimWhite
		}

		line := "  " + cursor + checkStyle.Render(checkbox) + " " + nameStyle.Render(name)

		var rowStyle lipgloss.Style
		switch {
		case i == m.overlayCursor:
			rowStyle = StyleRowSelected
		case i%2 == 0:
			rowStyle = StyleRowEven
		default:
			rowStyle = StyleRowOdd
		}
		sections = append(sections, rowStyle.Render(line))
	}

	// Summary
	sections = append(sections, "")
	excluded := len(m.overlayExcluded)
	total := len(m.repoNames)
	summary := RenderRepoCount(total, excluded)
	sections = append(sections, summary)

	// Help bar
	sections = append(sections, "")
	help := StyleDimCyan.Render("  ▐") + StyleHelpKey.Render("space") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("toggle") + "  " +
		StyleDimCyan.Render("▐") + StyleHelpKey.Render("enter/esc") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("done") + "  " +
		StyleDimCyan.Render("▐") + StyleHelpKey.Render("↑↓") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("navigate")
	sections = append(sections, help)

	return strings.Join(sections, "\n")
}
