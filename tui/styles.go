package tui

import "github.com/charmbracelet/lipgloss"

// Cyberpunk color palette
var (
	ColorBg        = lipgloss.Color("#050510")
	ColorCyan      = lipgloss.Color("#00FFFF")
	ColorCyanMid   = lipgloss.Color("#00BBDD")
	ColorMagenta   = lipgloss.Color("#FF00FF")
	ColorMagentaDm = lipgloss.Color("#AA00AA")
	ColorGreen     = lipgloss.Color("#00FF88")
	ColorYellow    = lipgloss.Color("#FFFF00")
	ColorDimCyan   = lipgloss.Color("#005566")
	ColorDimWhite  = lipgloss.Color("#666666")
	ColorBrightWht = lipgloss.Color("#E0E0E0")
	ColorRowEven   = lipgloss.Color("#0A0A18")
	ColorRowOdd    = lipgloss.Color("#080814")
	ColorRowSelect = lipgloss.Color("#102030")
)

// Layout styles
var (
	StyleApp = lipgloss.NewStyle().Background(ColorBg)

	StyleTitle = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleSubtitle = lipgloss.NewStyle().Foreground(ColorDimCyan)

	StyleGlitchLine = lipgloss.NewStyle().Foreground(ColorCyan)

	StyleStatBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimCyan).
			Padding(0, 2).
			Align(lipgloss.Center)

	StyleStatValue = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleStatLabel = lipgloss.NewStyle().Foreground(ColorDimCyan)

	StyleRepoTag = lipgloss.NewStyle().
			Foreground(ColorMagenta).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#550055")).
			Padding(0, 1)

	StyleRepoTagActive = lipgloss.NewStyle().
				Foreground(ColorMagenta).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorMagenta).
				Bold(true).
				Padding(0, 1)

	StyleTableHeader = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	StyleRowEven     = lipgloss.NewStyle().Background(ColorRowEven)
	StyleRowOdd      = lipgloss.NewStyle().Background(ColorRowOdd)
	StyleRowSelected = lipgloss.NewStyle().Background(ColorRowSelect).Bold(true).Foreground(ColorCyan)

	StyleRank    = lipgloss.NewStyle().Foreground(ColorDimCyan)
	StyleAuthor  = lipgloss.NewStyle().Foreground(ColorBrightWht)
	StyleNumeric = lipgloss.NewStyle().Foreground(ColorGreen)

	StyleTimePicker = lipgloss.NewStyle().Foreground(ColorDimWhite)

	StyleTimePickerActive = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorCyan).
				Padding(0, 1)

	StyleTimePickerInactive = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				Padding(0, 1)

	StyleFooter = lipgloss.NewStyle().Foreground(ColorDimCyan)

	StyleHelpKey  = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleHelpDesc = lipgloss.NewStyle().Foreground(ColorDimWhite)

	StyleBarCyan      = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleBarCyanMid   = lipgloss.NewStyle().Foreground(ColorCyanMid)
	StyleBarMagenta   = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleBarMagentaDm = lipgloss.NewStyle().Foreground(ColorMagentaDm)

	StyleDimWhite = lipgloss.NewStyle().Foreground(ColorDimWhite)
	StyleCyan     = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleMagenta  = lipgloss.NewStyle().Foreground(ColorMagenta)

	StyleCursor  = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleDimCyan = lipgloss.NewStyle().Foreground(ColorDimCyan)
)
