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
	{Label: "30d", Duration: 30 * 24 * time.Hour},
	{Label: "90d", Duration: 90 * 24 * time.Hour},
	{Label: "1y", Duration: 365 * 24 * time.Hour},
	{Label: "ALL", Duration: 0},
}

// RenderHeader renders the title, subtitle, and glitch line.
func RenderHeader(width int) string {
	title := StyleTitle.Render("⟐ BIG BOARD ⟐")
	subtitle := StyleSubtitle.Render("// CONTRIBUTOR INTELLIGENCE SYSTEM")
	glitch := StyleGlitchLine.Render(strings.Repeat("═", width))

	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, glitch)
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

// RenderRepoTags renders repo tag pills constrained to maxWidth.
// Scrolls horizontally to keep the focused tag visible.
// Always shows overflow indicators when there are hidden repos.
func RenderRepoTags(repos []string, focusedIdx int, hasFocus bool, maxWidth int) string {
	if len(repos) == 0 {
		return ""
	}

	// Pre-render all tags to measure widths
	rendered := make([]string, len(repos))
	widths := make([]int, len(repos))
	for i, r := range repos {
		if hasFocus && i == focusedIdx {
			rendered[i] = StyleRepoTagActive.Render(r)
		} else {
			rendered[i] = StyleRepoTag.Render(r)
		}
		widths[i] = lipgloss.Width(rendered[i])
	}

	// First pass: see if everything fits without indicators
	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	if totalWidth <= maxWidth {
		return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	}

	// We need scrolling — use fixed-width indicator zones
	const indicatorW = 6 // e.g. "◂ 12 " or " 12 ▸"
	availableWidth := maxWidth - (indicatorW * 2)

	// Find a window of tags that fits, centered on focusedIdx
	start := focusedIdx
	end := focusedIdx + 1
	used := widths[focusedIdx]

	for {
		expanded := false
		if start > 0 && used+widths[start-1] <= availableWidth {
			start--
			used += widths[start]
			expanded = true
		}
		if end < len(repos) && used+widths[end] <= availableWidth {
			used += widths[end]
			end++
			expanded = true
		}
		if !expanded {
			break
		}
	}

	// Render fixed-width indicators using lipgloss.Width for stable layout
	leftStyle := lipgloss.NewStyle().Width(indicatorW).Foreground(ColorCyan)
	rightStyle := lipgloss.NewStyle().Width(indicatorW).Align(lipgloss.Right).Foreground(ColorCyan)

	var parts []string

	if start > 0 {
		parts = append(parts, leftStyle.Render(fmt.Sprintf("◂ %d", start)))
	} else {
		parts = append(parts, leftStyle.Render(""))
	}

	for i := start; i < end; i++ {
		parts = append(parts, rendered[i])
	}

	if end < len(repos) {
		remaining := len(repos) - end
		parts = append(parts, rightStyle.Render(fmt.Sprintf("%d ▸", remaining)))
	} else {
		parts = append(parts, rightStyle.Render(""))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// RenderImpactBar renders a gradient bar: first half cyan, second half magenta.
// Empty for 0 value. Min 1 filled block for any positive value.
func RenderImpactBar(value, maxValue, barWidth int) string {
	if value <= 0 || maxValue <= 0 {
		return strings.Repeat(" ", barWidth)
	}

	filled := value * barWidth / maxValue
	if filled < 1 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}

	half := barWidth / 2
	var sb strings.Builder
	for i := 0; i < filled; i++ {
		if i < half {
			sb.WriteString(StyleBarCyan.Render("█"))
		} else {
			sb.WriteString(StyleBarMagenta.Render("█"))
		}
	}
	// pad remainder with spaces
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

// RenderHelpBar renders key binding hints.
func RenderHelpBar() string {
	bindings := []struct{ key, desc string }{
		{"[q]", "uit"},
		{"[s]", "ort"},
		{"[↵]", "drill"},
		{"[←→]", "time"},
		{"[tab]", "focus"},
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
	for i, ch := range s {
		pos := len(s) - i
		if i > 0 && pos%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
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
