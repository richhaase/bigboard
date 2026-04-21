package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/stats"
)

// AggregateView renders the aggregate leaderboard table.
type AggregateView struct{}

// RenderTable returns a styled leaderboard string for the given authors.
func (v AggregateView) RenderTable(authors []stats.AuthorStats, selectedRow int, sortField stats.SortField, width int) string {
	if len(authors) == 0 {
		return StyleDimWhite.Render("  No commit data found.")
	}

	const (
		nameW = 22
		numW  = 10
		barW  = 20
	)

	// Find max TotalChange for impact bar scaling
	maxTotal := 0
	for _, a := range authors {
		if a.TotalChange > maxTotal {
			maxTotal = a.TotalChange
		}
	}

	colRank := "#"
	colName := "CONTRIBUTOR"
	colCommits := "COMMITS"
	colAdded := "ADDED"
	colRemoved := "REMOVED"
	colNet := "NET"
	colImpact := "+/- IMPACT"

	switch sortField {
	case stats.SortByTotal:
		colImpact += " ▼"
	case stats.SortByCommits:
		colCommits += " ▼"
	case stats.SortByAdded:
		colAdded += " ▼"
	case stats.SortByRemoved:
		colRemoved += " ▼"
	case stats.SortByNet:
		colNet += " ▼"
	}

	header := StyleTableHeader.Render(
		fmt.Sprintf("  %-2s %-*s %*s %*s %*s %*s %-*s",
			colRank,
			nameW, colName,
			numW, colCommits,
			numW, colAdded,
			numW, colRemoved,
			numW, colNet,
			barW, colImpact,
		),
	)

	totalRowWidth := 2 + 2 + 1 + nameW + 1 + numW + 1 + numW + 1 + numW + 1 + numW + 1 + barW
	separator := StyleDimCyan.Render("  " + strings.Repeat("━", totalRowWidth))

	// Section header
	sectionHdr := StyleDimCyan.Render("  " + strings.Repeat("━", width-4))

	// Limit to 20 rows
	limit := len(authors)
	if limit > 20 {
		limit = 20
	}

	var rows []string
	for i := 0; i < limit; i++ {
		a := authors[i]
		rank := fmt.Sprintf("%02d", i+1)
		name := Truncate(a.Name, nameW)
		commits := FormatNumber(a.Commits)
		added := FormatNumber(a.Added)
		removed := FormatNumber(a.Removed)
		net := FormatNumber(a.Net)
		bar := RenderImpactBar(a.Added, a.Removed, maxTotal, barW)

		cursor := "  "
		if i == selectedRow {
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
		rankStr := rankStyle.Render(fmt.Sprintf("%-2s", rank))

		nameStr := StyleAuthor.Render(fmt.Sprintf("%-*s", nameW, name))
		commitsStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, commits))
		addedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, added))
		removedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, removed))

		var netStr string
		if a.Net < 0 {
			netStr = lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf("%*s", numW, net))
		} else {
			netStr = StyleNumeric.Render(fmt.Sprintf("%*s", numW, net))
		}

		barStr := fmt.Sprintf("%-*s", barW, bar)

		line := cursor + rankStr + " " + nameStr + " " + commitsStr + " " + addedStr + " " + removedStr + " " + netStr + " " + barStr

		var rowStyle lipgloss.Style
		switch {
		case i == selectedRow:
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
	return strings.Join(parts, "\n")
}
