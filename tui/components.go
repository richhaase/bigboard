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

// ── ASCII art banner (figlet banner3, # → █) ────────────────────────────

var bannerLines = [7]string{
	`████████  ████  ██████      ████████   ███████     ███    ████████  ████████`,
	`██     ██  ██  ██    ██     ██     ██ ██     ██   ██ ██   ██     ██ ██     ██`,
	`██     ██  ██  ██           ██     ██ ██     ██  ██   ██  ██     ██ ██     ██`,
	`████████   ██  ██   ████    ████████  ██     ██ ██     ██ ████████  ██     ██`,
	`██     ██  ██  ██    ██     ██     ██ ██     ██ █████████ ██   ██   ██     ██`,
	`██     ██  ██  ██    ██     ██     ██ ██     ██ ██     ██ ██    ██  ██     ██`,
	`████████  ████  ██████      ████████   ███████  ██     ██ ██     ██ ████████`,
}

var bannerWidth = lipgloss.Width(bannerLines[0])

// Layout constants. Previously these were magic numbers scattered across views.
const (
	bannerMinWidth = 82 // below this, the figlet banner falls back to a compact title
	chromeInset    = 4  // left+right margin reserved around full-width rules
)

// hrule returns a heavy horizontal rule of n cells, clamped at zero so a tiny
// or not-yet-sized terminal width can't trigger a negative strings.Repeat.
func hrule(n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat("━", n)
}

// renderBanner returns the header banner as lines: the figlet banner with a
// vertical color gradient when the terminal is wide enough, or a compact title
// otherwise. Centralizing this keeps the four call sites (loading, header,
// operative, overlay) identical and gives narrow terminals a title everywhere.
func renderBanner(width int) []string {
	if width >= bannerMinWidth {
		lines := make([]string, len(bannerLines))
		for i, line := range bannerLines {
			style := lipgloss.NewStyle().Foreground(ColorBannerGrad[i])
			lines[i] = "  " + style.Render(line)
		}
		return lines
	}
	return []string{StyleTitle.Render("  ░▒▓█  B I G   B O A R D  █▓▒░")}
}

// RenderHeader renders the ASCII banner with a vertical color gradient, a status
// line, and a heavy separator.
func RenderHeader(width, repoCount, excludedCount int, version string) string {
	sections := renderBanner(width)

	sections = append(sections, "")

	// Status line with version right-aligned
	sections = append(sections, RenderFooter(repoCount, excludedCount, width, version))

	// Heavy separator
	sections = append(sections, StyleDimCyan.Render("  "+hrule(width-chromeInset)))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// RenderStatBoxes renders heavy-bordered stat boxes for aggregate metrics.
func RenderStatBoxes(commits, added, removed, aiCommits int) string {
	box := func(value, label string, valColor lipgloss.TerminalColor) string {
		v := lipgloss.NewStyle().Foreground(valColor).Bold(true).Render(value)
		l := StyleStatLabel.Render(label)
		inner := lipgloss.JoinVertical(lipgloss.Center, v, l)
		return StyleStatBox.Render(inner)
	}

	c := box(FormatNumber(commits), "COMMITS", ColorCyan)
	a := box("+"+FormatNumber(added), "ADDED", ColorGreen)
	r := box("-"+FormatNumber(removed), "REMOVED", ColorMagenta)

	boxes := []string{c, " ", a, " ", r}

	if aiCommits > 0 {
		var aiVal string
		if commits > 0 {
			pct := aiCommits * 100 / commits
			aiVal = fmt.Sprintf("%d%% (%d)", pct, aiCommits)
		} else {
			aiVal = FormatNumber(aiCommits)
		}
		ai := box(aiVal, "AI CO-AUTHORED", ColorAmber)
		boxes = append(boxes, " ", ai)
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	return lipgloss.NewStyle().MarginLeft(2).Render(joined)
}

// RenderTimePicker renders time presets with the active one styled differently.
func RenderTimePicker(activeIdx int) string {
	parts := make([]string, len(TimePresets))
	for i, p := range TimePresets {
		if i == activeIdx {
			parts[i] = StyleDimCyan.Render("▐") + StyleTimePickerActive.Render(p.Label) + StyleDimCyan.Render("▌")
		} else {
			parts[i] = " " + StyleTimePickerInactive.Render(p.Label) + " "
		}
	}
	return "  " + strings.Join(parts, " ")
}

// RenderRepoCount renders the repo count indicator.
func RenderRepoCount(total, excluded int) string {
	if excluded > 0 {
		return StyleSubtitle.Render(fmt.Sprintf("  %d/%d repos", total-excluded, total))
	}
	return StyleSubtitle.Render(fmt.Sprintf("  %d repos", total))
}

// RenderImpactBar renders a gradient bar: solid core fading to ▓▒░ at the edge.
// Cyan for added, magenta for removed.
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

	// Split proportionally
	addedFill := filled
	if total > 0 {
		addedFill = added * filled / total
	}
	removedFill := filled - addedFill
	if removedFill < 0 {
		removedFill = 0
	}

	var sb strings.Builder
	writeGradientBar(&sb, addedFill, StyleBarCyan, StyleBarCyanMid, StyleBarCyanDim)
	writeGradientBar(&sb, removedFill, StyleBarMagenta, StyleBarMagentaMid, StyleBarMagentaDim)
	for i := filled; i < barWidth; i++ {
		sb.WriteString(" ")
	}
	return sb.String()
}

// writeGradientBar writes a section of the impact bar with a trailing glow fade.
func writeGradientBar(sb *strings.Builder, width int, bright, mid, dim lipgloss.Style) {
	if width <= 0 {
		return
	}

	// Gradient tail: up to 3 chars (▓▒░), only if bar is wide enough
	glyphs := []struct {
		ch    string
		style lipgloss.Style
	}{
		{"▓", mid},
		{"▒", dim},
		{"░", dim},
	}

	tail := len(glyphs)
	if width < 6 {
		tail = 0 // too short for gradient — solid fill
	} else if tail > width {
		tail = width
	}
	core := width - tail

	for i := 0; i < core; i++ {
		sb.WriteString(bright.Render("█"))
	}
	for i := 0; i < tail; i++ {
		sb.WriteString(glyphs[i].style.Render(glyphs[i].ch))
	}
}

// RenderSectionHeader renders a labeled separator line: ── LABEL ──────────
func RenderSectionHeader(label string, width int) string {
	prefix := "──╸ "
	suffix := " ╺"
	labelStr := StyleCyan.Render(label)
	fillLen := width - 4 - lipgloss.Width(prefix) - lipgloss.Width(labelStr) - lipgloss.Width(suffix)
	if fillLen < 2 {
		fillLen = 2
	}
	return "  " + StyleDimCyan.Render(prefix) + labelStr + StyleDimCyan.Render(suffix+strings.Repeat("─", fillLen))
}

// RenderFooter renders the repo count and timestamp status line.
// The timestamp right-aligns to the separator width (width - 4).
func RenderFooter(repoCount, excludedCount, width int, version string) string {
	repos := fmt.Sprintf("%d repos", repoCount)
	if excludedCount > 0 {
		repos = fmt.Sprintf("%d/%d repos", repoCount-excludedCount, repoCount)
	}
	left := "  " + StyleFooter.Render(repos)
	if version == "" || bannerWidth == 0 {
		return left
	}
	ver := StyleDimCyan.Render(version)
	pad := bannerWidth + 2 - lipgloss.Width(left) - lipgloss.Width(ver)
	if pad < 2 {
		pad = 2
	}
	return left + strings.Repeat(" ", pad) + ver
}

// StyleGreen is used for status indicators.
var StyleGreen = lipgloss.NewStyle().Foreground(ColorGreen)

// HelpContext describes the current UI state for context-aware help.
type HelpContext struct {
	View string // "aggregate", "operative"
	Sort string // current sort label, appended to the 's' hint when set
}

// RenderHelpBar renders context-aware key binding hints with bracket styling.
func RenderHelpBar(ctx HelpContext) string {
	var bindings []struct{ key, desc string }

	switch ctx.View {
	case "operative":
		bindings = []struct{ key, desc string }{
			{"↑↓", "prev/next"},
			{"esc", "back"},
			{"←→", "time"},
			{"q", "quit"},
		}
	default: // aggregate
		sortDesc := "sort"
		if ctx.Sort != "" {
			sortDesc = "sort:" + ctx.Sort
		}
		bindings = []struct{ key, desc string }{
			{"↑↓", "nav"},
			{"↵", "detail"},
			{"←→", "time"},
			{"/", "find"},
			{"s", sortDesc},
			{"r", "repos"},
			{"R", "refresh"},
			{"q", "quit"},
		}
	}

	parts := make([]string, len(bindings))
	for i, b := range bindings {
		parts[i] = StyleDimCyan.Render("▐") + StyleHelpKey.Render(b.key) + StyleDimCyan.Render("▌") + StyleHelpDesc.Render(b.desc)
	}
	return "  " + strings.Join(parts, "  ")
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

// Truncate shortens s to a display width of at most width cells, appending
// "..." when it has to cut. It measures and slices on display-width / rune
// boundaries (via lipgloss.Width) so multibyte names (CJK, accented Latin) are
// never split mid-rune into invalid UTF-8 and wide glyphs are counted correctly.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	const ellipsis = "..."
	if width <= len(ellipsis) {
		return cutToWidth(s, width)
	}
	return cutToWidth(s, width-len(ellipsis)) + ellipsis
}

// cutToWidth returns the longest prefix of s whose display width is <= w.
func cutToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if cur+rw > w {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String()
}

// padRight pads s on the right with spaces to a display width of w. Unlike
// fmt's %-*s (which counts bytes), this aligns columns containing multibyte or
// wide glyphs correctly.
func padRight(s string, w int) string {
	diff := w - lipgloss.Width(s)
	if diff <= 0 {
		return s
	}
	return s + strings.Repeat(" ", diff)
}
