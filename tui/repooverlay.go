package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func repositoryModalSize(width, height int) (int, int) {
	return max(6, min(76, width-2)), max(1, min(16, height-10))
}

func (m Model) renderRepoOverlay() string {
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	if width < 30 || height < 10 {
		return fitGlanceLines([]string{"Repository controls", "Resize terminal · Esc cancel"}, width, height)
	}
	background := m
	background.width, background.height = width, height
	background.viewMode, background.showPRs = m.returnView, m.overlayPRs
	if background.viewMode == ViewRepoOverlay {
		background.viewMode = ViewAggregate
	}
	panelWidth, page := repositoryModalSize(width, height)
	inner := panelWidth - 4
	if m.cacheRemoveConfirm != "" {
		lines := []string{StyleTitle.Render("Remove cached repository?"), "", displayText(m.cacheRemoveConfirm), "Only BigBoard's cached clone will be removed.", "", "Enter remove · Esc cancel"}
		if m.cacheRemoving {
			lines[len(lines)-1] = "Removing cached clone…"
		}
		if m.cacheError != "" {
			lines = append(lines, m.cacheError)
		}
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, inner, "…")
		}
		return renderCenteredModal(background.viewContent(), lines, panelWidth, width, height)
	}
	rows := m.repositoryControlRows()
	sections := []string{
		StyleTitle.Render("Repository controls"),
		StyleDimWhite.Render(padCells("Changes apply when you press Enter", inner)),
		worklistRule.Render(strings.Repeat("─", inner)),
	}
	selected := max(0, min(m.overlayCursor, len(rows)-1))
	start := max(0, min(selected-page/2, len(rows)-page))
	end := min(len(rows), start+page)
	for i := start; i < end; i++ {
		repo := rows[i]
		checkbox, style := "[x] ", StyleAuthor
		if m.overlayExcluded[repo.ID] {
			checkbox, style = "[ ] ", StyleDimWhite
		}
		if m.orphanedRepository(repo.ID) {
			checkbox, style = "[−] ", StyleDimWhite
		}
		cursor := "  "
		if i == selected {
			cursor = "▸ "
		}
		line := style.Render(padCells(cursor+checkbox+displayText(repo.Name), inner))
		if i == selected {
			line = selectedRowBackground(StyleRowSelected.Render(line))
		}
		sections = append(sections, line)
	}
	if len(rows) == 0 {
		sections = append(sections, StyleDimWhite.Render("No repositories available"))
	}
	for len(sections) < 3+page {
		sections = append(sections, "")
	}
	excluded := 0
	for _, repo := range rows {
		if m.overlayExcluded[repo.ID] || m.orphanedRepository(repo.ID) {
			excluded++
		}
	}
	total := len(rows)
	position := fmt.Sprintf("%d–%d of %d · %d included", start+1, end, total, total-excluded)
	if inner < 50 {
		position = fmt.Sprintf("%d–%d/%d · %d included", start+1, end, total, total-excluded)
	}
	if total == 0 {
		position = "0 repositories · 0 included"
	}
	hints := "Esc cancel · ↑↓ select · ? help"
	if len(m.cacheInfo.Orphans) > 0 {
		hints = "[−] no longer listed · d remove cache · Esc cancel"
	}
	if m.cacheError != "" {
		hints = m.cacheError
	}
	boards := boardShortcuts(inner)
	if ansi.StringWidth(hints+" · "+boards) <= inner {
		hints += " · " + boards
	} else if inner < 50 && m.cacheError == "" {
		hints = "Esc cancel · ? help"
		if len(m.cacheInfo.Orphans) > 0 {
			hints = "d remove unlisted · ? help"
		}
		if ansi.StringWidth(hints+" · "+boards) <= inner {
			hints += " · " + boards
		}
	}
	sections = append(sections,
		StyleDimWhite.Render(ansi.Truncate(position, inner, "…")),
		StyleTitle.Render("Space toggle · Enter apply"),
		StyleDimWhite.Render(hints),
	)
	for i, line := range sections {
		sections[i] = ansi.Truncate(line, inner, "…")
	}
	return renderCenteredModal(background.viewContent(), sections, panelWidth, width, height)
}
