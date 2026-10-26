package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type repositoryOverviewLayout struct {
	name, commits, people, added, removed, bar, age int
}

func repositoryOverviewWidths(width int) repositoryOverviewLayout {
	l := repositoryOverviewLayout{name: max(1, width-5), commits: 7, people: 6, added: 10, removed: 10, age: 7}
	if width >= 96 {
		l.name = min(36, max(20, width/4))
		l.bar = max(1, width-5-l.name-l.commits-l.people-l.added-l.removed-l.age-6)
	} else if width >= 66 {
		l.name = width - 5 - l.commits - l.people - l.age - 3
	}
	return l
}

func (m Model) repositoryOverviewColumns(width int) string {
	l := repositoryOverviewWidths(width)
	label := "REPOSITORY"
	if m.directory.active {
		label = "DIRECTORY / FILE"
	}
	cells := []string{"  #  " + padCells(label, l.name)}
	if width >= 66 {
		cells = append(cells, rightCells("COMMITS", l.commits), rightCells("PEOPLE", l.people))
		if width >= 96 {
			cells = append(cells, StyleBarCyan.Render(rightCells("ADDED", l.added)), StyleBarMagenta.Render(rightCells("REMOVED", l.removed)), padCells("LINES +/-", l.bar))
		}
		cells = append(cells, rightCells("LATEST", l.age))
	}
	return StyleDimWhite.Render(strings.Join(cells, " "))
}

func repositoryLineTotals(r repositoryActivity) (added, removed, unknown int) {
	for _, person := range r.people {
		added += person.Added
		removed += person.Removed
		unknown += person.UnknownLineCommits
	}
	return
}

func repositoryLineLabel(prefix string, value, unknown, width int) string {
	text := prefix + formatLineCount(value, unknown)
	if ansi.StringWidth(text) <= width {
		return text
	}
	scaled, unit := float64(value), -1
	for scaled >= 1000 && unit < 5 {
		scaled /= 1000
		unit++
	}
	if unit >= 0 {
		text = fmt.Sprintf("%s%.1f%c", prefix, scaled, "KMBTPE"[unit])
	}
	if unknown > 0 {
		return ansi.Truncate(strings.TrimSuffix(text, "?"), max(1, width-1), "…") + "?"
	}
	return ansi.Truncate(text, width, "…")
}

func (m Model) repositoryMeasureMax(rows []repositoryActivity) int {
	maxValue := 0
	for _, r := range rows {
		added, removed, _ := repositoryLineTotals(r)
		value := added + removed
		maxValue = max(maxValue, value)
	}
	return maxValue
}

func (m Model) worklistRepositoryRow(r repositoryActivity, selected bool, width, rank, maxValue int) []string {
	name := displayText(r.repo.Name)
	repoID := r.repo.ID
	if m.directory.active {
		repoID = m.areaRepoID
	}
	if m.staleRepos[repoID] && !m.directory.active {
		name = "[STALE] " + name
	}
	age := m.worklistAge(r.latest, repoID)
	added, removed, unknown := repositoryLineTotals(r)
	countWidth := 10
	if width < 96 {
		countWidth = max(2, (width-13)/2)
	}
	addedText := StyleBarCyan.Render(repositoryLineLabel("+", added, unknown, countWidth))
	removedText := StyleBarMagenta.Render(repositoryLineLabel("−", removed, unknown, countWidth))
	bar := func(barWidth int) string {
		return RenderImpactBar(added, removed, maxValue, barWidth)
	}
	cursor := "  "
	if selected {
		cursor = "› "
	}
	l := repositoryOverviewWidths(width)
	prefix := cursor + StyleDimWhite.Render(fmt.Sprintf("%2d ", rank))
	cells := []string{prefix + padCells(name, l.name)}
	var lines []string
	if width >= 66 {
		cells = append(cells, rightCells(FormatNumber(r.commits), l.commits), rightCells(FormatNumber(len(r.people)), l.people))
		if width >= 96 {
			cells = append(cells, rightCells(addedText, l.added), rightCells(removedText, l.removed), bar(l.bar))
		}
		cells = append(cells, rightCells(age, l.age))
		lines = append(lines, strings.Join(cells, " "))
	} else {
		lines = append(lines, cells[0], fmt.Sprintf("     %s commits · %d people · %s", FormatNumber(r.commits), len(r.people), age))
	}
	if width < 96 {
		counts := addedText + " " + removedText
		countWidth := min(width-12, lipgloss.Width(counts))
		lines = append(lines, "     "+padCells(counts, countWidth)+"  "+bar(max(1, width-countWidth-7)))
	}
	for i, line := range lines {
		line = padCells(line, width)
		if selected && !m.contributorPane.focused {
			line = selectedRowBackground(lipgloss.NewStyle().Background(ColorRowSelect).Render(line))
		}
		lines[i] = "  " + line
	}
	return lines
}

func worklistOverviewSummary(rows []repositoryActivity) []string {
	active := 0
	for _, row := range rows {
		if row.commits > 0 {
			active++
		}
	}
	parts := []string{fmt.Sprintf("%d active", active)}
	if quiet := len(rows) - active; quiet > 0 {
		parts = append(parts, fmt.Sprintf("%d quiet", quiet))
	}
	return []string{strings.Join(parts, " · ")}
}
