package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func resolveTableLayout(width int, authors ...stats.AuthorStats) tableLayout {
	const (
		baseNameW = 22
		aiW       = 5
	)
	numW := 10
	for _, author := range authors {
		values := []string{FormatNumber(author.Commits), formatLineCount(author.Net, author.UnknownLineCommits)}
		if width >= wideTableWidth {
			values = append(values, formatLineCount(author.Added, author.UnknownLineCommits), formatLineCount(author.Removed, author.UnknownLineCommits))
		}
		for _, value := range values {
			numW = max(numW, len(value))
		}
	}
	avail := width - chromeInset
	switch {
	case width >= wideTableWidth:
		nameW := max(baseNameW, width/5)
		numW = max(numW, width/9)
		fixed := 2 + 2 + 1 + nameW + 1 + numW + 1 + numW + 1 + numW + 1 + numW + 1 + aiW + 1
		return tableLayout{
			nameW:            nameW,
			numW:             numW,
			aiW:              aiW,
			barW:             max(10, avail-fixed),
			showAddedRemoved: true,
			showBar:          true,
		}
	case width >= midTableWidth:
		fixed := 2 + 2 + 1 + baseNameW + 1 + numW + 1 + numW + 1 + aiW + 1
		extra := max(0, avail-fixed-8)
		return tableLayout{
			nameW:            baseNameW + extra/2,
			numW:             numW,
			aiW:              aiW,
			barW:             8 + extra - extra/2,
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

func (v AggregateView) RenderTable(authors []stats.AuthorStats, ts TableState) string {

	l := resolveTableLayout(ts.Width, authors...)

	maxTotal := 0
	for _, a := range authors {
		maxTotal = max(maxTotal, a.TotalChange)
	}

	arrow := " ↓"
	if ts.SortAsc {
		arrow = " ↑"
	}
	colCommits, colAdded, colRemoved, colNet, colAI, colImpact := "COMMITS", "ADDED", "REMOVED", "NET", "AI%", "LINES +/-"
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
	header := boardColumns(strings.Join(headerCells, " "), ts.Width)
	if ts.Width > 0 && ts.Width < 60 {
		header = boardColumns("  # CONTRIBUTOR / COMMITS · NET / AI", ts.Width)
	}
	if len(authors) == 0 {
		if ts.Searching || ts.Query != "" {
			return header + "\n" + StyleAmber.Render(fmt.Sprintf("  ◈ NO MATCH for %q — x to clear board filters.", displayText(ts.Query)))
		}
		return header + "\n" + StyleAmber.Render("  ◈ NO SIGNAL — no commit data in range. Press t to change the time range.")
	}

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
			cursor = StyleCursor.Render("› ")
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
				StyleNumeric.Render(fmt.Sprintf("%*s", l.numW, formatLineCount(a.Added, a.UnknownLineCommits))),
				StyleNumeric.Render(fmt.Sprintf("%*s", l.numW, formatLineCount(a.Removed, a.UnknownLineCommits))),
			)
		}
		cells = append(cells, renderKnownNet(a.Net, l.numW, a.UnknownLineCommits))

		aiCell := strings.Repeat(" ", l.aiW)
		if a.AICommits > 0 {
			label := authorAIShare(a)
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
		physical := []string{line}
		if ts.Width > 0 && ts.Width < 60 {
			inner := max(1, ts.Width-4)
			commits := FormatNumber(a.Commits)
			nameWidth := max(1, inner-6-max(7, ansi.StringWidth(commits)))
			name := a.Name
			if a.Bot {
				name = ansi.Truncate(displayText(name), max(1, nameWidth-4), "…") + " BOT"
			}
			first := cursor + rankStr + " " + StyleAuthor.Render(padCells(displayText(name), nameWidth)) + " " + StyleNumeric.Render(rightCells(commits, max(7, ansi.StringWidth(commits))))
			second := "     NET " + renderKnownNet(a.Net, 0, a.UnknownLineCommits) + " · AI " + authorAIShare(a)
			physical = append([]string{first}, strings.Split(ansi.Hardwrap(second, inner, true), "\n")...)
		}
		for _, physicalLine := range physical {
			line = rowStyle.Render(padCells(physicalLine, max(l.rowWidth(), ansi.StringWidth(physicalLine))))
			if ts.Width > 0 && ts.Width < 60 {
				line = rowStyle.Render(padCells(physicalLine, max(1, ts.Width-4)))
			}
			if i == ts.SelectedRow {
				line = selectedRowBackground(line)
			}
			rows = append(rows, "  "+line)
		}
	}

	parts := []string{header}
	parts = append(parts, rows...)
	return strings.Join(parts, "\n")
}

func aggregateRowHeight(width int, authors []stats.AuthorStats) int {
	if width > 0 && width < 60 {
		height := 2
		for _, a := range authors {
			line := fmt.Sprintf("     NET %s · AI %s", formatLineCount(a.Net, a.UnknownLineCommits), authorAIShare(a))
			height = max(height, 1+(ansi.StringWidth(line)+max(1, width-4)-1)/max(1, width-4))
		}
		return height
	}
	return max(1, (resolveTableLayout(width, authors...).rowWidth()+2+max(1, width)-1)/max(1, width))
}

func authorAIShare(a stats.AuthorStats) string {
	if a.AICommits > 0 && a.AIPercent() == 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", a.AIPercent())
}
