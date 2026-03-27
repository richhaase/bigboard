package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TimePreset represents a time range filter option.
type TimePreset struct {
	Label    string
	Duration time.Duration // 0 means "all time"
}

var TimePresets = []TimePreset{
	{Label: "1d", Duration: 24 * time.Hour},
	{Label: "7d", Duration: 7 * 24 * time.Hour},
	{Label: "14d", Duration: 14 * 24 * time.Hour},
	{Label: "30d", Duration: 30 * 24 * time.Hour},
	{Label: "90d", Duration: 90 * 24 * time.Hour},
	{Label: "1y", Duration: 365 * 24 * time.Hour},
	{Label: "ALL", Duration: 0},
}

// RenderHeader renders the title, subtitle, and separator line.
func RenderHeader(width int) string {
	title := StyleTitle.Render("⟐ BIG BOARD ⟐")
	subtitle := StyleSubtitle.Render("// CONTRIBUTOR INTELLIGENCE SYSTEM")
	separator := StyleDimCyan.Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, separator)
}

// RenderStatBoxes renders 3 bordered stat boxes horizontally joined.
func RenderStatBoxes(commits, added, removed int) string {
	box := func(value, label string) string {
		v := StyleStatValue.Render(value)
		l := StyleStatLabel.Render(label)
		inner := lipgloss.JoinVertical(lipgloss.Center, v, l)
		return StyleStatBox.Render(inner)
	}

	c := box(FormatNumber(commits), "COMMITS")
	a := box("+"+FormatNumber(added), "ADDED")
	r := box("-"+FormatNumber(removed), "REMOVED")

	return lipgloss.JoinHorizontal(lipgloss.Top, c, a, r)
}

// RenderTimePicker renders time presets with the active one styled differently.
func RenderTimePicker(activeIdx int) string {
	parts := make([]string, len(TimePresets))
	for i, p := range TimePresets {
		if i == activeIdx {
			parts[i] = StyleTimePickerActive.Render(p.Label)
		} else {
			parts[i] = StyleTimePickerInactive.Render(p.Label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// RenderImpactBar renders a bar showing added (cyan) vs removed (magenta) proportionally.
// Total bar width represents totalChange relative to maxValue.
// Within the bar, cyan portion = added, magenta portion = removed.
func RenderImpactBar(added, removed, maxValue, barWidth int) string {
	total := added + removed
	if total <= 0 || maxValue <= 0 {
		return strings.Repeat(" ", barWidth)
	}

	filled := total * barWidth / maxValue
	if filled < 1 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}

	// Split filled portion proportionally between added and removed
	addedFill := filled
	if total > 0 {
		addedFill = added * filled / total
	}
	removedFill := filled - addedFill
	if removedFill < 0 {
		removedFill = 0
	}

	var sb strings.Builder
	for i := 0; i < addedFill; i++ {
		sb.WriteString(StyleBarCyan.Render("█"))
	}
	for i := 0; i < removedFill; i++ {
		sb.WriteString(StyleBarMagenta.Render("█"))
	}
	for i := filled; i < barWidth; i++ {
		sb.WriteString(" ")
	}
	return sb.String()
}

// RenderFooter renders status on the left and timestamp on the right.
func RenderFooter(repoCount int, width int) string {
	left := StyleFooter.Render(fmt.Sprintf("SYS.STATUS: NOMINAL // %d repos scanned", repoCount))
	right := StyleFooter.Render(time.Now().Format("2006-01-02 15:04:05"))

	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// HelpContext describes the current UI state for context-aware help.
type HelpContext struct {
	View string // "aggregate", "operative"
}

// RenderHelpBar renders context-aware key binding hints.
func RenderHelpBar(ctx HelpContext) string {
	var bindings []struct{ key, desc string }

	switch ctx.View {
	case "operative":
		bindings = []struct{ key, desc string }{
			{"[esc]", "back"},
			{"[←→]", "time"},
			{"[q]", "uit"},
		}
	default: // aggregate
		bindings = []struct{ key, desc string }{
			{"[q]", "uit"},
			{"[s]", "ort"},
			{"[↵]", "detail"},
			{"[←→]", "time"},
		}
	}

	parts := make([]string, len(bindings))
	for i, b := range bindings {
		parts[i] = StyleHelpKey.Render(b.key) + StyleHelpDesc.Render(b.desc)
	}
	return strings.Join(parts, " ")
}

// FormatNumber formats an integer with thousand separators.
func FormatNumber(n int) string {
	if n < 0 {
		return "-" + FormatNumber(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i := 0; i < len(s); i++ {
		pos := len(s) - i
		if i > 0 && pos%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, s[i])
	}
	return string(result)
}

// Truncate truncates s to width, adding "..." if needed.
func Truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}
