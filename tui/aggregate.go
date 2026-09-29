package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/stats"
)

// AggregateView renders the aggregate leaderboard table.
type AggregateView struct{}

// TableState carries the scroll/sort/filter state into RenderTable.
type TableState struct {
	SelectedRow  int
	ScrollOffset int
	VisibleRows  int
	SortField    stats.SortField
	SortAsc      bool
	Width        int
	Searching    bool
	Query        string
}

const (
	wideTableWidth = 96
	midTableWidth  = 78
)

type tableLayout struct {
	nameW            int
	numW             int
	aiW              int
	barW             int
	showAddedRemoved bool
	showBar          bool
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func resolveTableLayout(width int) tableLayout {
	const (
		baseNameW = 22
		numW      = 10
		aiW       = 5
	)
	avail := width - chromeInset
	switch {
	case width >= wideTableWidth:
		fixed := 2 + 2 + 1 + baseNameW + 1 + numW + 1 + numW + 1 + numW + 1 + numW + 1 + aiW + 1
		return tableLayout{
			nameW:            baseNameW,
			numW:             numW,
			aiW:              aiW,
			barW:             clampInt(avail-fixed, 10, 16),
			showAddedRemoved: true,
			showBar:          true,
		}
	case width >= midTableWidth:
		fixed := 2 + 2 + 1 + baseNameW + 1 + numW + 1 + numW + 1 + aiW + 1
		return tableLayout{
			nameW:            baseNameW,
			numW:             numW,
			aiW:              aiW,
			barW:             clampInt(avail-fixed, 8, 16),
			showAddedRemoved: false,
			showBar:          true,
		}
	default:
		nameW := clampInt(avail-(2+2+1+1+numW+1+numW+1+aiW), 12, baseNameW)
		return tableLayout{
			nameW:            nameW,
			numW:             numW,
			aiW:              aiW,
			showAddedRemoved: false,
			showBar:          false,
		}
	}
}

func (l tableLayout) rowWidth() int {
	w := 2 + 2 + 1 + l.nameW + 1 + l.numW
	if l.showAddedRemoved {
		w += 1 + l.numW + 1 + l.numW
	}
	w += 1 + l.numW + 1 + l.aiW
	if l.showBar {
		w += 1 + l.barW
	}
	return w
}

// RenderTable returns a styled leaderboard for the given authors, showing only
// the scroll window [ScrollOffset, ScrollOffset+VisibleRows) plus a status
// footer. Ranks and podium styling follow metric rank even when the sort is
// ascending. Columns adapt to TableState.Width, dropping ADDED/REMOVED then the
// impact bar as the terminal narrows.
func (v AggregateView) RenderTable(authors []stats.AuthorStats, ts TableState) string {
	if len(authors) == 0 {
		if ts.Searching || ts.Query != "" {
			return StyleAmber.Render(fmt.Sprintf("  ◈ NO MATCH for %q — esc to clear filter.", displayText(ts.Query)))
		}
		return StyleAmber.Render("  ◈ NO SIGNAL — no commit data in range. Widen the time range with ←/→.")
	}

	l := resolveTableLayout(ts.Width)

	maxTotal := 0
	for _, a := range authors {
		if a.TotalChange > maxTotal {
			maxTotal = a.TotalChange
		}
	}

	arrow := " ▼"
	if ts.SortAsc {
		arrow = " ▲"
	}
	colCommits, colAdded, colRemoved, colNet, colAI, colImpact := "COMMITS", "ADDED", "REMOVED", "NET", "AI%", "+/- IMPACT"
	switch ts.SortField {
	case stats.SortByCommits:
		colCommits += arrow
	case stats.SortByAdded:
		colAdded += arrow
	case stats.SortByRemoved:
		colRemoved += arrow
	case stats.SortByNet:
		colNet += arrow
	case stats.SortByAI:
		colAI += arrow
	default:
		colImpact += arrow
	}

	var headerCells []string
	headerCells = append(headerCells,
		fmt.Sprintf("  %-2s", "#"),
		fmt.Sprintf("%-*s", l.nameW, "CONTRIBUTOR"),
		fmt.Sprintf("%*s", l.numW, colCommits),
	)
	if l.showAddedRemoved {
		headerCells = append(headerCells,
			fmt.Sprintf("%*s", l.numW, colAdded),
			fmt.Sprintf("%*s", l.numW, colRemoved),
		)
	}
	headerCells = append(headerCells,
		fmt.Sprintf("%*s", l.numW, colNet),
		fmt.Sprintf("%*s", l.aiW, colAI),
	)
	if l.showBar {
		headerCells = append(headerCells, fmt.Sprintf("%-*s", l.barW, colImpact))
	}
	header := StyleTableHeader.Render(strings.Join(headerCells, " "))

	separator := StyleDimCyan.Render("  " + strings.Repeat("━", l.rowWidth()))
	sectionHdr := StyleDimCyan.Render("  " + hrule(ts.Width-chromeInset))

	start := ts.ScrollOffset
	if start < 0 {
		start = 0
	}
	end := start + ts.VisibleRows
	if end > len(authors) {
		end = len(authors)
	}

	var rows []string
	for i := start; i < end; i++ {
		a := authors[i]
		cursor := "  "
		if i == ts.SelectedRow {
			cursor = StyleCursor.Render("▸ ")
		}

		rank := i + 1
		if ts.SortAsc {
			rank = len(authors) - i
		}
		var rankStyle lipgloss.Style
		switch rank {
		case 1:
			rankStyle = StyleRankGold
		case 2:
			rankStyle = StyleRankSilver
		case 3:
			rankStyle = StyleRankBronze
		default:
			rankStyle = StyleRank
		}
		rankStr := rankStyle.Render(fmt.Sprintf("%-2s", fmt.Sprintf("%02d", rank)))

		nameCell := StyleAuthor.Render(padRight(Truncate(a.Name, l.nameW), l.nameW))
		if a.Bot {
			label := " BOT"
			nameFit := l.nameW - lipgloss.Width(label)
			nameCell = StyleAuthor.Render(padRight(Truncate(a.Name, nameFit), nameFit)) + StyleDimCyan.Render(label)
		}

		var cells []string
		cells = append(cells,
			cursor+rankStr,
			nameCell,
			StyleNumeric.Render(fmt.Sprintf("%*s", l.numW, FormatNumber(a.Commits))),
		)
		if l.showAddedRemoved {
			cells = append(cells,
				StyleNumeric.Render(fmt.Sprintf("%*s", l.numW, FormatNumber(a.Added))),
				StyleNumeric.Render(fmt.Sprintf("%*s", l.numW, FormatNumber(a.Removed))),
			)
		}
		cells = append(cells, renderNet(a.Net, l.numW))

		aiCell := strings.Repeat(" ", l.aiW)
		if a.AICommits > 0 {
			label := fmt.Sprintf("%d%%", a.AIPercent())
			if a.AIPercent() == 0 {
				label = "<1%"
			}
			aiCell = StyleAmber.Render(fmt.Sprintf("%*s", l.aiW, label))
		}
		cells = append(cells, aiCell)

		if l.showBar {
			cells = append(cells, RenderImpactBar(a.Added, a.Removed, maxTotal, l.barW))
		}

		line := strings.Join(cells, " ")

		var rowStyle lipgloss.Style
		switch {
		case i == ts.SelectedRow:
			rowStyle = StyleRowSelected
		case i%2 == 0:
			rowStyle = StyleRowEven
		default:
			rowStyle = StyleRowOdd
		}
		rows = append(rows, rowStyle.Render(line))
	}

	parts := []string{sectionHdr, "", header, separator}
	parts = append(parts, rows...)
	parts = append(parts, renderTableFooter(ts, start, end, len(authors)))
	return strings.Join(parts, "\n")
}

func renderTableFooter(ts TableState, start, end, total int) string {
	if ts.Searching {
		return "  " + StyleCyan.Render("/") + StyleAuthor.Render(displayText(ts.Query)) + StyleCursor.Render("▌") +
			StyleDimWhite.Render("   (enter to apply · esc to clear)")
	}
	rng := fmt.Sprintf("showing %d–%d of %d", start+1, end, total)
	if ts.Query != "" {
		return "  " + StyleDimCyan.Render(fmt.Sprintf("filter %q · %s · esc clears", displayText(ts.Query), rng))
	}
	return "  " + StyleDimCyan.Render(rng)
}
