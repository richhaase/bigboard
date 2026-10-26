package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func scanErrorSummary(detail string) string {
	switch {
	case strings.Contains(detail, "partial clone") && strings.Contains(detail, "automatic fetching disabled"):
		return "Partial clone cannot be scanned offline. Required Git objects may be missing locally."
	case strings.Contains(detail, "partial clone") && strings.Contains(detail, "requires Git"):
		return "Git must be updated before this partial clone can be scanned offline."
	case strings.Contains(detail, "fetch failed"):
		return "Remote update failed. Remote-tracking branches may be out of date."
	default:
		return detail
	}
}

func (m Model) scanErrorLines() []string {
	var lines []string
	inner := max(1, m.width-4)
	for _, repo := range m.repositories {
		detail := displayText(m.scanErrors[repo.ID])
		if detail == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		name := strings.Split(ansi.Wrap(displayText(repo.Name), inner, ""), "\n")
		for _, line := range name {
			lines = append(lines, StyleTitle.Render(line))
		}
		summary := strings.Split(ansi.Wrap(scanErrorSummary(detail), max(1, inner-2), ""), "\n")
		if len(summary) > 2 {
			summary = summary[:2]
			summary[1] = ansi.Truncate(summary[1]+"…", max(1, inner-2), "…")
		}
		for _, line := range summary {
			lines = append(lines, StyleAmber.Render("  "+line))
		}
		if m.scanErrorDetails {
			lines = append(lines, StyleDimWhite.Render("  Technical details"))
			for _, line := range strings.Split(ansi.Wrap(detail, max(1, inner-2), ""), "\n") {
				lines = append(lines, "  "+line)
			}
		}
	}
	return lines
}

func scanErrorSize(width, height int) (int, int) {
	return max(6, min(96, width-2)), max(1, min(20, height-10))
}

func (m Model) scanErrorModalLines() ([]string, int, int) {
	panelWidth, page := scanErrorSize(m.width, m.height)
	return m.dataStatusLines(panelWidth), panelWidth, page
}

func (m Model) renderScanErrors(background string, width, height int) string {
	if width < 30 || height < 10 {
		return fitGlanceLines([]string{"Data status", "Resize to read status", "Esc closes · r refreshes"}, width, height)
	}
	m.width, m.height = width, height
	lines, panelWidth, page := m.scanErrorModalLines()
	offset := max(0, min(m.scanErrorOffset, len(lines)-page))
	end := min(len(lines), offset+page)
	content := []string{StyleTitle.Render("Data status"), StyleDimWhite.Render(ansi.Truncate("All tracked repositories · independent of view filters", panelWidth-4, "…")), ""}
	content = append(content, lines[offset:end]...)
	for len(content) < min(page, len(lines))+3 {
		content = append(content, "")
	}
	progress := fmt.Sprintf("%d–%d of %d · ↑↓ scroll · ? help", offset+1, end, len(lines))
	if panelWidth < 60 {
		progress = fmt.Sprintf("%d–%d/%d · ↑↓ scroll · ? help", offset+1, end, len(lines))
	}
	details := "d diagnostics"
	if panelWidth < 60 {
		details = "d more"
	}
	if m.scanErrorDetails {
		details = "d less"
	}
	content = append(content, "", StyleDimWhite.Render(ansi.Truncate(progress, panelWidth-4, "…")), StyleCyan.Render("Esc close · "+details+" · r refresh"))
	return renderCenteredModal(background, content, panelWidth, width, height)
}

func (m Model) handleScanErrorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	lines, _, page := m.scanErrorModalLines()
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "esc", "left", "e":
		m.showScanErrors = false
	case "r", "R":
		return m, m.startLocalRefresh()
	case "d":
		m.scanErrorDetails = !m.scanErrorDetails
		m.scanErrorOffset = 0
		return m, nil
	case "up":
		m.scanErrorOffset--
	case "down":
		m.scanErrorOffset++
	case "pgup":
		m.scanErrorOffset -= page
	case "pgdown":
		m.scanErrorOffset += page
	case "home":
		m.scanErrorOffset = 0
	case "end":
		m.scanErrorOffset = len(lines)
	}
	m.scanErrorOffset = max(0, min(m.scanErrorOffset, len(lines)-page))
	return m, nil
}
