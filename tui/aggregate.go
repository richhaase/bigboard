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

// RenderTable returns a styled leaderboard for the given authors, showing only
// the scroll window [ScrollOffset, ScrollOffset+VisibleRows) plus a status
// footer. Ranks, the podium styling, and the cursor track absolute position.
func (v AggregateView) RenderTable(authors []stats.AuthorStats, ts TableState) string {
	if len(authors) == 0 {
		if ts.Searching || ts.Query != "" {
			return StyleAmber.Render(fmt.Sprintf("  ◈ NO MATCH for %q — esc to clear filter.", ts.Query))
		}
		return StyleAmber.Render("  ◈ NO SIGNAL — no commit data in range. Widen the time range with ←/→.")
	}

	const (
		nameW = 22
		numW  = 10
		aiW   = 5
		barW  = 16
	)

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
	default: // SortByTotal
		colImpact += arrow
	}

	header := StyleTableHeader.Render(
		fmt.Sprintf("  %-2s %-*s %*s %*s %*s %*s %*s %-*s",
			"#", nameW, "CONTRIBUTOR",
			numW, colCommits, numW, colAdded, numW, colRemoved, numW, colNet,
			aiW, colAI, barW, colImpact,
		),
	)

	totalRowWidth := 2 + 2 + 1 + nameW + 1 + numW + 1 + numW + 1 + numW + 1 + numW + 1 + aiW + 1 + barW
	separator := StyleDimCyan.Render("  " + strings.Repeat("━", totalRowWidth))
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

		var rankStyle lipgloss.Style
		switch i {
		case 0:
			rankStyle = StyleRankGold
		case 1:
			rankStyle = StyleRankSilver
		case 2:
			rankStyle = StyleRankBronze
		default:
			rankStyle = StyleRank
		}
		rankStr := rankStyle.Render(fmt.Sprintf("%-2s", fmt.Sprintf("%02d", i+1)))

		nameStr := StyleAuthor.Render(padRight(Truncate(a.Name, nameW), nameW))
		commitsStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(a.Commits)))
		addedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(a.Added)))
		removedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(a.Removed)))

		var netStr string
		if a.Net < 0 {
			netStr = lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf("%*s", numW, FormatNumber(a.Net)))
		} else {
			netStr = StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(a.Net)))
		}

		aiCell := strings.Repeat(" ", aiW)
		if a.AICommits > 0 {
			aiCell = StyleAmber.Render(fmt.Sprintf("%*s", aiW, fmt.Sprintf("%d%%", a.AIPercent())))
		}

		barStr := fmt.Sprintf("%-*s", barW, RenderImpactBar(a.Added, a.Removed, maxTotal, barW))

		line := cursor + rankStr + " " + nameStr + " " + commitsStr + " " + addedStr + " " + removedStr + " " + netStr + " " + aiCell + " " + barStr

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

// renderTableFooter shows the visible range, an active filter, or the live
// search prompt.
func renderTableFooter(ts TableState, start, end, total int) string {
	if ts.Searching {
		return "  " + StyleCyan.Render("/") + StyleAuthor.Render(ts.Query) + StyleCursor.Render("▌") +
			StyleDimWhite.Render("   (enter to apply · esc to clear)")
	}
	rng := fmt.Sprintf("showing %d–%d of %d", start+1, end, total)
	if ts.Query != "" {
		return "  " + StyleDimCyan.Render(fmt.Sprintf("filter %q · %s · esc clears", ts.Query, rng))
	}
	return "  " + StyleDimCyan.Render(rng)
}
