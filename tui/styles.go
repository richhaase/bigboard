package tui

import "github.com/charmbracelet/lipgloss"

// ── Cyberpunk color palette (adaptive: light/dark) ───────────────────────
//
// Each color carries both a Light and Dark variant. Lipgloss picks the right
// one at render time based on terminal background detection (overridable via
// the --theme flag in main.go).

var (
	// Primary neons
	ColorCyan    = lipgloss.AdaptiveColor{Light: "#006B86", Dark: "#00FFFF"}
	ColorMagenta = lipgloss.AdaptiveColor{Light: "#9C27B0", Dark: "#FF00FF"}
	ColorGreen   = lipgloss.AdaptiveColor{Light: "#1B7F3B", Dark: "#00FF88"}
	ColorAmber   = lipgloss.AdaptiveColor{Light: "#B25900", Dark: "#FFB000"}
	ColorRed     = lipgloss.AdaptiveColor{Light: "#C8002A", Dark: "#FF0040"}

	// Banner vertical gradient (top → bottom).
	// Dark mode: bright cyan fading to dark navy.
	// Light mode: dark teal fading to lighter teal — matches the same
	// "depth" feel against a light background.
	ColorBannerGrad = [7]lipgloss.AdaptiveColor{
		{Light: "#003D55", Dark: "#00FFFF"},
		{Light: "#00536F", Dark: "#00EEFF"},
		{Light: "#006782", Dark: "#00CCDD"},
		{Light: "#00789A", Dark: "#00AACC"},
		{Light: "#008CB0", Dark: "#0088AA"},
		{Light: "#009AC2", Dark: "#006688"},
		{Light: "#00ACDD", Dark: "#005577"},
	}

	// Cyan shades for bars
	ColorCyanMid = lipgloss.AdaptiveColor{Light: "#3F8CA0", Dark: "#00BBDD"}
	ColorCyanDim = lipgloss.AdaptiveColor{Light: "#7CAEB8", Dark: "#005577"}

	// Magenta shades for bars
	ColorMagentaMid = lipgloss.AdaptiveColor{Light: "#B449C4", Dark: "#CC00CC"}
	ColorMagentaDim = lipgloss.AdaptiveColor{Light: "#D29ED9", Dark: "#660066"}

	// Backgrounds & chrome
	ColorBg        = lipgloss.AdaptiveColor{Light: "#FAFAF5", Dark: "#050510"}
	ColorDimCyan   = lipgloss.AdaptiveColor{Light: "#5E8590", Dark: "#005566"}
	ColorDimWhite  = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#555555"}
	ColorBrightWht = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E0E0E0"}
	ColorRowEven   = lipgloss.AdaptiveColor{Light: "#F0F2F8", Dark: "#0A0A1A"}
	ColorRowOdd    = lipgloss.AdaptiveColor{Light: "#FAFAFA", Dark: "#070714"}
	ColorRowSelect = lipgloss.AdaptiveColor{Light: "#C5E0EC", Dark: "#0C2030"}

	// Rank podium — keep gold/silver/bronze readable on white.
	ColorGold   = lipgloss.AdaptiveColor{Light: "#B8860B", Dark: "#FFD700"}
	ColorSilver = lipgloss.AdaptiveColor{Light: "#6E6E6E", Dark: "#C0C0C0"}
	ColorBronze = lipgloss.AdaptiveColor{Light: "#8B5A2B", Dark: "#CD7F32"}

	// Repo overlay tag border (dim magenta).
	ColorRepoTagBorder = lipgloss.AdaptiveColor{Light: "#D199D8", Dark: "#550055"}
)

// ── Layout styles ────────────────────────────────────────────────────────

var (
	StyleApp = lipgloss.NewStyle().Background(ColorBg)

	// Header / title
	StyleTitle          = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleSubtitle       = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleClassification = lipgloss.NewStyle().Foreground(ColorMagenta).Bold(true)

	// Stat boxes — heavy border, more padding
	StyleStatBox = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(ColorDimCyan).
			Padding(0, 3).
			Align(lipgloss.Center)

	StyleStatValue = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleStatLabel = lipgloss.NewStyle().Foreground(ColorDimCyan)

	// Table header
	StyleTableHeader  = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleSectionLabel = lipgloss.NewStyle().Foreground(ColorDimCyan)

	// Row backgrounds
	StyleRowEven     = lipgloss.NewStyle().Background(ColorRowEven)
	StyleRowOdd      = lipgloss.NewStyle().Background(ColorRowOdd)
	StyleRowSelected = lipgloss.NewStyle().Background(ColorRowSelect).Bold(true).Foreground(ColorCyan)

	// Rank column
	StyleRank       = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleRankGold   = lipgloss.NewStyle().Foreground(ColorGold).Bold(true)
	StyleRankSilver = lipgloss.NewStyle().Foreground(ColorSilver).Bold(true)
	StyleRankBronze = lipgloss.NewStyle().Foreground(ColorBronze).Bold(true)

	// Data cells
	StyleAuthor  = lipgloss.NewStyle().Foreground(ColorBrightWht)
	StyleNumeric = lipgloss.NewStyle().Foreground(ColorGreen)

	// Impact bars
	StyleBarCyan       = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleBarCyanMid    = lipgloss.NewStyle().Foreground(ColorCyanMid)
	StyleBarCyanDim    = lipgloss.NewStyle().Foreground(ColorCyanDim)
	StyleBarMagenta    = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleBarMagentaMid = lipgloss.NewStyle().Foreground(ColorMagentaMid)
	StyleBarMagentaDim = lipgloss.NewStyle().Foreground(ColorMagentaDim)

	// Time picker
	StyleTimePickerActive = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	StyleTimePickerInactive = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				Padding(0, 1)

	// Footer & help
	StyleFooter   = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleHelpKey  = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleHelpDesc = lipgloss.NewStyle().Foreground(ColorDimWhite)

	// Reusable
	StyleDimWhite = lipgloss.NewStyle().Foreground(ColorDimWhite)
	StyleCyan     = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleMagenta  = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleDimCyan  = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleCursor   = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleAmber      = lipgloss.NewStyle().Foreground(ColorAmber)
	StyleGlitchLine = lipgloss.NewStyle().Foreground(ColorCyan)

	// Repo overlay tags (kept for compatibility)
	StyleRepoTag = lipgloss.NewStyle().
			Foreground(ColorMagenta).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorRepoTagBorder).
			Padding(0, 1)

	StyleRepoTagActive = lipgloss.NewStyle().
				Foreground(ColorMagenta).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorMagenta).
				Bold(true).
				Padding(0, 1)

	StyleTimePicker = lipgloss.NewStyle().Foreground(ColorDimWhite)
)
