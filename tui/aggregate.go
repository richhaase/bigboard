package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rdh/bigboard/stats"
)

// AggregateView renders the aggregate leaderboard table.
type AggregateView struct{}

// RenderTable returns a styled leaderboard string for the given authors.
func (v AggregateView) RenderTable(authors []stats.AuthorStats, selectedRow int, sortField stats.SortField, width int) string {
	if len(authors) == 0 {
		return "No commit data found."
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

	// Column headers with active sort indicator
	colRank := "#"
	colName := "OPERATIVE"
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
	separator := StyleDimCyan.Render(strings.Repeat("─", totalRowWidth))

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

		// Cursor indicator for selected row
		cursor := "  "
		if i == selectedRow {
			cursor = StyleCursor.Render("▸ ")
		}

		rankStr := StyleRank.Render(fmt.Sprintf("%-2s", rank))
		nameStr := StyleAuthor.Render(fmt.Sprintf("%-*s", nameW, name))
		commitsStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, commits))
		addedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, added))
		removedStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, removed))
		netStr := StyleNumeric.Render(fmt.Sprintf("%*s", numW, net))
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

	parts := []string{header, separator}
	parts = append(parts, rows...)
	return strings.Join(parts, "\n")
}
