package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderRepoOverlay() string {
	var sections []string

	sections = append(sections, renderBanner(m.width)...)
	sections = append(sections, "")

	sections = append(sections, RenderSectionHeader("REPOSITORY CONTROL", m.width))
	sections = append(sections, "")

	for i, repo := range m.loadedRepos {
		checkbox := "[x]"
		if m.overlayExcluded[repo.ID] {
			checkbox = "[ ]"
		}

		cursor := "  "
		if i == m.overlayCursor {
			cursor = StyleCursor.Render("▸ ")
		}

		checkStyle := StyleCyan
		nameStyle := StyleAuthor
		if m.overlayExcluded[repo.ID] {
			checkStyle = StyleDimWhite
			nameStyle = StyleDimWhite
		}

		line := "  " + cursor + checkStyle.Render(checkbox) + " " + nameStyle.Render(displayText(repo.Name))

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

	sections = append(sections, "")
	excluded := 0
	for _, repo := range m.loadedRepos {
		if m.overlayExcluded[repo.ID] {
			excluded++
		}
	}
	total := len(m.loadedRepos)
	summary := RenderRepoCount(total, excluded)
	sections = append(sections, summary)

	sections = append(sections, "")
	help := StyleDimCyan.Render("  ▐") + StyleHelpKey.Render("space") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("toggle") + "  " +
		StyleDimCyan.Render("▐") + StyleHelpKey.Render("enter/esc") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("done") + "  " +
		StyleDimCyan.Render("▐") + StyleHelpKey.Render("↑↓") + StyleDimCyan.Render("▌") + StyleHelpDesc.Render("navigate")
	sections = append(sections, help)

	return strings.Join(sections, "\n")
}
