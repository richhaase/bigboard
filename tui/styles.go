package tui

import "github.com/charmbracelet/lipgloss"

// ── Cyberpunk color palette ──────────────────────────────────────────────

var (
	// Primary neons
	ColorCyan    = lipgloss.Color("#00FFFF")
	ColorMagenta = lipgloss.Color("#FF00FF")
	ColorGreen   = lipgloss.Color("#00FF88")
	ColorAmber   = lipgloss.Color("#FFB000")
	ColorRed     = lipgloss.Color("#FF0040")

	// Banner vertical gradient (top → bottom)
	ColorBannerGrad = [7]lipgloss.Color{
		"#00FFFF", "#00EEFF", "#00CCDD", "#00AACC",
		"#0088AA", "#006688", "#005577",
	}

	// Cyan shades for bars
	ColorCyanMid = lipgloss.Color("#00BBDD")
	ColorCyanDim = lipgloss.Color("#005577")

	// Magenta shades for bars
	ColorMagentaMid = lipgloss.Color("#CC00CC")
	ColorMagentaDim = lipgloss.Color("#660066")

	// Backgrounds & chrome
	ColorBg        = lipgloss.Color("#050510")
	ColorDimCyan   = lipgloss.Color("#005566")
	ColorDimWhite  = lipgloss.Color("#555555")
	ColorBrightWht = lipgloss.Color("#E0E0E0")
	ColorRowEven   = lipgloss.Color("#0A0A1A")
	ColorRowOdd    = lipgloss.Color("#070714")
	ColorRowSelect = lipgloss.Color("#0C2030")

	// Rank podium
	ColorGold   = lipgloss.Color("#FFD700")
	ColorSilver = lipgloss.Color("#C0C0C0")
	ColorBronze = lipgloss.Color("#CD7F32")
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

	StyleGlitchLine = lipgloss.NewStyle().Foreground(ColorCyan)

	// Repo overlay tags (kept for compatibility)
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

	StyleTimePicker = lipgloss.NewStyle().Foreground(ColorDimWhite)
)
