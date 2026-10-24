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

	if m.height > 0 && m.height < 5 {
		return fitGlanceLines([]string{"Resize terminal · Esc back"}, max(1, m.width), m.height)
	}
	if m.height > 0 && m.height < len(sections)+8 {
		sections = []string{RenderSectionHeader("REPOSITORY CONTROL", m.width), ""}
	}
	visible := len(m.loadedRepos)
	if m.height > 0 {
		visible = max(1, m.height-len(sections)-5)
	}
	start := max(0, min(m.overlayCursor-visible+1, len(m.loadedRepos)-visible))
	end := min(len(m.loadedRepos), start+visible)
	for i := start; i < end; i++ {
		repo := m.loadedRepos[i]
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
	help := StyleHelpKey.Render("  ? help · Enter apply · Esc cancel · space toggle · ↑↓ select")
	boards := "  " + boardShortcuts(m.width)
	if m.width < 70 {
		help = "  ? help · space toggle · ↑↓ select"
		boards = "  Enter apply · Esc cancel · " + boardShortcuts(m.width)
	}
	sections = append(sections, help, boards)

	if m.width > 0 && m.height > 0 {
		return fitGlanceLines(sections, m.width, m.height)
	}
	return strings.Join(sections, "\n")
}
