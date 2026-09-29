package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorCyan    = lipgloss.AdaptiveColor{Light: "#006B86", Dark: "#00FFFF"}
	ColorMagenta = lipgloss.AdaptiveColor{Light: "#9C27B0", Dark: "#FF00FF"}
	ColorGreen   = lipgloss.AdaptiveColor{Light: "#1B7F3B", Dark: "#00FF88"}
	ColorAmber   = lipgloss.AdaptiveColor{Light: "#B25900", Dark: "#FFB000"}
	ColorRed     = lipgloss.AdaptiveColor{Light: "#C8002A", Dark: "#FF0040"}

	ColorBannerGrad = [7]lipgloss.AdaptiveColor{
		{Light: "#003D55", Dark: "#00FFFF"},
		{Light: "#00536F", Dark: "#00EEFF"},
		{Light: "#006782", Dark: "#00CCDD"},
		{Light: "#00789A", Dark: "#00AACC"},
		{Light: "#008CB0", Dark: "#0088AA"},
		{Light: "#009AC2", Dark: "#006688"},
		{Light: "#00ACDD", Dark: "#005577"},
	}

	ColorCyanMid = lipgloss.AdaptiveColor{Light: "#3F8CA0", Dark: "#00BBDD"}
	ColorCyanDim = lipgloss.AdaptiveColor{Light: "#7CAEB8", Dark: "#005577"}

	ColorMagentaMid = lipgloss.AdaptiveColor{Light: "#B449C4", Dark: "#CC00CC"}
	ColorMagentaDim = lipgloss.AdaptiveColor{Light: "#D29ED9", Dark: "#660066"}

	ColorDimCyan   = lipgloss.AdaptiveColor{Light: "#5E8590", Dark: "#005566"}
	ColorDimWhite  = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#555555"}
	ColorBrightWht = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E0E0E0"}
	ColorRowEven   = lipgloss.AdaptiveColor{Light: "#F0F2F8", Dark: "#0A0A1A"}
	ColorRowOdd    = lipgloss.AdaptiveColor{Light: "#FAFAFA", Dark: "#070714"}
	ColorRowSelect = lipgloss.AdaptiveColor{Light: "#C5E0EC", Dark: "#0C2030"}

	ColorGold   = lipgloss.AdaptiveColor{Light: "#B8860B", Dark: "#FFD700"}
	ColorSilver = lipgloss.AdaptiveColor{Light: "#6E6E6E", Dark: "#C0C0C0"}
	ColorBronze = lipgloss.AdaptiveColor{Light: "#8B5A2B", Dark: "#CD7F32"}
)

var (
	StyleTitle    = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleSubtitle = lipgloss.NewStyle().Foreground(ColorDimCyan)

	StyleStatBox = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(ColorDimCyan).
			Padding(0, 3).
			Align(lipgloss.Center)

	StyleStatLabel = lipgloss.NewStyle().Foreground(ColorDimCyan)

	StyleTableHeader = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleRowEven     = lipgloss.NewStyle().Background(ColorRowEven)
	StyleRowOdd      = lipgloss.NewStyle().Background(ColorRowOdd)
	StyleRowSelected = lipgloss.NewStyle().Background(ColorRowSelect).Bold(true).Foreground(ColorCyan)

	StyleRank       = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleRankGold   = lipgloss.NewStyle().Foreground(ColorGold).Bold(true)
	StyleRankSilver = lipgloss.NewStyle().Foreground(ColorSilver).Bold(true)
	StyleRankBronze = lipgloss.NewStyle().Foreground(ColorBronze).Bold(true)

	StyleAuthor  = lipgloss.NewStyle().Foreground(ColorBrightWht)
	StyleNumeric = lipgloss.NewStyle().Foreground(ColorGreen)

	StyleBarCyan       = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleBarCyanMid    = lipgloss.NewStyle().Foreground(ColorCyanMid)
	StyleBarCyanDim    = lipgloss.NewStyle().Foreground(ColorCyanDim)
	StyleBarMagenta    = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleBarMagentaMid = lipgloss.NewStyle().Foreground(ColorMagentaMid)
	StyleBarMagentaDim = lipgloss.NewStyle().Foreground(ColorMagentaDim)

	StyleTimePickerActive = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	StyleTimePickerInactive = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				Padding(0, 1)

	StyleFooter   = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleHelpKey  = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleHelpDesc = lipgloss.NewStyle().Foreground(ColorDimWhite)

	StyleDimWhite = lipgloss.NewStyle().Foreground(ColorDimWhite)
	StyleCyan     = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleMagenta  = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleDimCyan  = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleCursor   = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleAmber = lipgloss.NewStyle().Foreground(ColorAmber)
)
