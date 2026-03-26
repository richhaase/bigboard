package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rdh/bigboard/stats"
)

// RepoView renders a drill-down view for a single repository.
type RepoView struct{}

// RenderRepoTable renders a leaderboard filtered to a single repo's contributors.
func (v RepoView) RenderRepoTable(repoName string, authors []stats.AuthorStats, selectedRow int, sortField stats.SortField, width int) string {
	// Filter to authors with a contribution in this repo and build repo-specific stats
	var filtered []stats.AuthorStats
	var totalCommits, totalAdded, totalRemoved int

	for _, a := range authors {
		rc, ok := a.PerRepo[repoName]
		if !ok {
			continue
		}
		filtered = append(filtered, stats.AuthorStats{
			Name:        a.Name,
			Commits:     rc.Commits,
			Added:       rc.Added,
			Removed:     rc.Removed,
			Net:         rc.Net,
			TotalChange: rc.TotalChange,
			PerRepo:     a.PerRepo,
		})
		totalCommits += rc.Commits
		totalAdded += rc.Added
		totalRemoved += rc.Removed
	}

	stats.Sort(filtered, sortField)

	// Header
	title := StyleTitle.Render(fmt.Sprintf("⟐ %s ⟐", strings.ToUpper(repoName)))
	subtitle := StyleSubtitle.Render("// [ESC] back to aggregate")
	glitch := StyleGlitchLine.Render(strings.Repeat("═", width))
	header := lipgloss.JoinVertical(lipgloss.Left, title, subtitle, glitch)

	// Stat boxes
	statBoxes := RenderStatBoxes(totalCommits, totalAdded, totalRemoved)

	// Table
	table := AggregateView{}.RenderTable(filtered, selectedRow, sortField, width)

	return lipgloss.JoinVertical(lipgloss.Left, header, statBoxes, table)
}
