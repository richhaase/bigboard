package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorCyan    = lipgloss.AdaptiveColor{Light: "#D7FF3F", Dark: "#D7FF3F"}
	ColorMagenta = lipgloss.AdaptiveColor{Light: "#BB70F4", Dark: "#BB70F4"}
	ColorGreen   = lipgloss.AdaptiveColor{Light: "#D7FF3F", Dark: "#D7FF3F"}
	ColorAmber   = lipgloss.AdaptiveColor{Light: "#FFB000", Dark: "#FFB000"}
	ColorRed     = lipgloss.AdaptiveColor{Light: "#FF0040", Dark: "#FF0040"}

	ColorBannerGrad = [7]lipgloss.AdaptiveColor{
		{Light: "#D7FF3F", Dark: "#D7FF3F"},
		{Light: "#00EEFF", Dark: "#00EEFF"},
		{Light: "#00CCDD", Dark: "#00CCDD"},
		{Light: "#00AACC", Dark: "#00AACC"},
		{Light: "#0088AA", Dark: "#0088AA"},
		{Light: "#006688", Dark: "#006688"},
		{Light: "#705182", Dark: "#705182"},
	}

	ColorCyanMid = lipgloss.AdaptiveColor{Light: "#B4D635", Dark: "#B4D635"}
	ColorCyanDim = lipgloss.AdaptiveColor{Light: "#705182", Dark: "#705182"}

	ColorMagentaMid = lipgloss.AdaptiveColor{Light: "#A759DD", Dark: "#A759DD"}
	ColorMagentaDim = lipgloss.AdaptiveColor{Light: "#633E80", Dark: "#633E80"}

	ColorDimCyan   = lipgloss.AdaptiveColor{Light: "#856898", Dark: "#856898"}
	ColorDimWhite  = lipgloss.AdaptiveColor{Light: "#ACA0C3", Dark: "#ACA0C3"}
	ColorBrightWht = lipgloss.AdaptiveColor{Light: "#F0EBFA", Dark: "#F0EBFA"}
	ColorRowEven   = lipgloss.AdaptiveColor{Light: "#100D1C", Dark: "#100D1C"}
	ColorRowOdd    = lipgloss.AdaptiveColor{Light: "#141021", Dark: "#141021"}
	ColorRowSelect = lipgloss.AdaptiveColor{Light: "#312142", Dark: "#312142"}

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
