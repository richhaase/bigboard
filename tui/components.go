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
	Duration time.Duration
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

// TimePresetIndex returns the index of the preset whose window best matches d;
// d <= 0 selects ALL. It never returns out of range.
func TimePresetIndex(d time.Duration) int {
	if d <= 0 {
		return len(TimePresets) - 1
	}
	best, bestDiff := DefaultTimeIndex, time.Duration(1<<62)
	for i, p := range TimePresets {
		if p.Duration <= 0 {
			continue
		}
		diff := p.Duration - d
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	return best
}

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

const (
	bannerMinWidth = 82
	chromeInset    = 4
)

func hrule(n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat("━", n)
}

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

	sections = append(sections, RenderFooter(repoCount, excludedCount, width, version))

	sections = append(sections, StyleDimCyan.Render("  "+hrule(width-chromeInset)))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func aiBoxValue(commits, aiCommits int) string {
	if commits <= 0 {
		return FormatNumber(aiCommits)
	}
	pct := aiCommits * 100 / commits
	if pct == 0 {
		return fmt.Sprintf("<1%% (%d)", aiCommits)
	}
	return fmt.Sprintf("%d%% (%d)", pct, aiCommits)
}

func renderStatLineCompact(commits, added, removed, aiCommits, width int) string {
	color := func(c lipgloss.TerminalColor, s string) string {
		return lipgloss.NewStyle().Foreground(c).Bold(true).Render(s)
	}
	commitsPlain := "COMMITS " + FormatNumber(commits)
	segs := []struct{ plain, styled string }{
		{commitsPlain, StyleStatLabel.Render("COMMITS ") + color(ColorCyan, FormatNumber(commits))},
		{"+" + FormatNumber(added), color(ColorGreen, "+"+FormatNumber(added))},
		{"-" + FormatNumber(removed), color(ColorMagenta, "-"+FormatNumber(removed))},
	}
	if aiCommits > 0 {
		ai := aiBoxValue(commits, aiCommits)
		segs = append(segs, struct{ plain, styled string }{"AI " + ai, StyleStatLabel.Render("AI ") + color(ColorAmber, ai)})
	}

	const sep = "  ·  "
	sepW := lipgloss.Width(sep)
	avail := width - 2
	if avail < 1 {
		avail = 1
	}
	out := make([]string, 0, len(segs))
	used := 0
	for i, s := range segs {
		w := lipgloss.Width(s.plain)
		if i > 0 {
			w += sepW
		}
		if i > 0 && used+w > avail {
			break
		}
		out = append(out, s.styled)
		used += w
	}
	line := strings.Join(out, sep)
	if lipgloss.Width(line) > avail {
		line = StyleStatLabel.Render(Truncate(commitsPlain, avail))
	}
	return "  " + line
}

// RenderStatBoxes renders heavy-bordered stat boxes for aggregate metrics,
// falling back to a stacked or compact single line so the row never overflows
// the given terminal width.
func RenderStatBoxes(commits, added, removed, aiCommits, width int) string {
	if width > 0 && width < midTableWidth {
		return renderStatLineCompact(commits, added, removed, aiCommits, width)
	}

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
		ai := box(aiBoxValue(commits, aiCommits), "AI CO-AUTHORED", ColorAmber)
		boxes = append(boxes, " ", ai)
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	if width > 0 && lipgloss.Width(joined)+2 > width {
		joined = lipgloss.JoinVertical(lipgloss.Left, c, a, r)
		if aiCommits > 0 {
			joined = lipgloss.JoinVertical(lipgloss.Left, joined, box(aiBoxValue(commits, aiCommits), "AI CO-AUTHORED", ColorAmber))
		}
	}
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

func writeGradientBar(sb *strings.Builder, width int, bright, mid, dim lipgloss.Style) {
	if width <= 0 {
		return
	}

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
		tail = 0
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

var StyleGreen = lipgloss.NewStyle().Foreground(ColorGreen)

// HelpContext describes the current UI state for context-aware help.
type HelpContext struct {
	View string
	Sort string
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
	default:
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

func padRight(s string, w int) string {
	diff := w - lipgloss.Width(s)
	if diff <= 0 {
		return s
	}
	return s + strings.Repeat(" ", diff)
}

func renderNet(n, width int) string {
	s := fmt.Sprintf("%*s", width, FormatNumber(n))
	if n < 0 {
		return lipgloss.NewStyle().Foreground(ColorRed).Render(s)
	}
	return StyleNumeric.Render(s)
}
