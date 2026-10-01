package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorCyan    = lipgloss.AdaptiveColor{Light: "#9FD0FF", Dark: "#9FD0FF"}
	ColorMagenta = lipgloss.AdaptiveColor{Light: "#B7C9E2", Dark: "#B7C9E2"}
	ColorGreen   = lipgloss.AdaptiveColor{Light: "#9FD0FF", Dark: "#9FD0FF"}
	ColorAmber   = lipgloss.AdaptiveColor{Light: "#FFD184", Dark: "#FFD184"}
	ColorRed     = lipgloss.AdaptiveColor{Light: "#FFA7A0", Dark: "#FFA7A0"}

	ColorBannerGrad = [7]lipgloss.AdaptiveColor{
		{Light: "#9FD0FF", Dark: "#9FD0FF"},
		{Light: "#00EEFF", Dark: "#00EEFF"},
		{Light: "#00CCDD", Dark: "#00CCDD"},
		{Light: "#00AACC", Dark: "#00AACC"},
		{Light: "#0088AA", Dark: "#0088AA"},
		{Light: "#006688", Dark: "#006688"},
		{Light: "#627B8E", Dark: "#627B8E"},
	}

	ColorCyanMid = lipgloss.AdaptiveColor{Light: "#84B8DA", Dark: "#84B8DA"}
	ColorCyanDim = lipgloss.AdaptiveColor{Light: "#627B8E", Dark: "#627B8E"}

	ColorMagentaMid = lipgloss.AdaptiveColor{Light: "#91ACC4", Dark: "#91ACC4"}
	ColorMagentaDim = lipgloss.AdaptiveColor{Light: "#58748B", Dark: "#58748B"}

	ColorDimCyan   = lipgloss.AdaptiveColor{Light: "#91A7B9", Dark: "#91A7B9"}
	ColorDimWhite  = lipgloss.AdaptiveColor{Light: "#B3C2D0", Dark: "#B3C2D0"}
	ColorBrightWht = lipgloss.AdaptiveColor{Light: "#EFF4FA", Dark: "#EFF4FA"}
	ColorRowEven   = lipgloss.AdaptiveColor{Light: "#0F1922", Dark: "#0F1922"}
	ColorRowOdd    = lipgloss.AdaptiveColor{Light: "#0F1922", Dark: "#0F1922"}
	ColorRowSelect = lipgloss.AdaptiveColor{Light: "#24445D", Dark: "#24445D"}

	ColorGold   = lipgloss.AdaptiveColor{Light: "#FFD700", Dark: "#FFD700"}
	ColorSilver = lipgloss.AdaptiveColor{Light: "#C0C0C0", Dark: "#C0C0C0"}
	ColorBronze = lipgloss.AdaptiveColor{Light: "#CD7F32", Dark: "#CD7F32"}
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
