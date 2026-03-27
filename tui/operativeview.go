package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
)

// MonthActivity holds commit stats for a single month.
type MonthActivity struct {
	Month   time.Time
	Commits int
	Added   int
	Removed int
}

// OperativeView renders a contributor detail screen.
type OperativeView struct{}

// RenderOperativeDetail renders the full operative detail view.
func (v OperativeView) RenderOperativeDetail(
	authorName string,
	authorStats *stats.AuthorStats,
	records []git.CommitRecord,
	width int,
) string {
	var sections []string

	// Header
	header := StyleTitle.Render(fmt.Sprintf("⟐ %s ⟐", strings.ToUpper(authorName)))
	subtitle := StyleSubtitle.Render("// OPERATIVE INTELLIGENCE DOSSIER")
	backHint := StyleSubtitle.Render("// [ESC] back")
	glitch := StyleGlitchLine.Render(strings.Repeat("═", width))
	sections = append(sections, lipgloss.JoinVertical(lipgloss.Left, header, subtitle, backHint, glitch))

	// Summary stat boxes
	if authorStats != nil {
		sections = append(sections, "")
		sections = append(sections, RenderStatBoxes(authorStats.Commits, authorStats.Added, authorStats.Removed))
	}

	// Per-repo breakdown table
	if authorStats != nil && len(authorStats.PerRepo) > 0 {
		sections = append(sections, "")
		sections = append(sections, v.renderRepoBreakdown(authorStats, width))
	}

	// Monthly activity timeline
	authorRecords := filterRecordsByAuthor(records, authorName)
	if len(authorRecords) > 0 {
		sections = append(sections, "")
		sections = append(sections, StyleTableHeader.Render("ACTIVITY TIMELINE"))
		sections = append(sections, StyleGlitchLine.Render(strings.Repeat("─", width)))
		sections = append(sections, v.renderTimeline(authorRecords, width))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderRepoBreakdown renders a table of per-repo contributions.
func (v OperativeView) renderRepoBreakdown(as *stats.AuthorStats, width int) string {
	var rows []string

	// Header
	header := fmt.Sprintf("  %-30s %10s %10s %10s %10s",
		StyleTableHeader.Render("REPO"),
		StyleTableHeader.Render("COMMITS"),
		StyleTableHeader.Render("ADDED"),
		StyleTableHeader.Render("REMOVED"),
		StyleTableHeader.Render("NET"),
	)
	rows = append(rows, header)
	rows = append(rows, StyleGlitchLine.Render(strings.Repeat("─", width)))

	// Sort repos by total change descending
	type repoEntry struct {
		name string
		rc   *stats.RepoContribution
	}
	var entries []repoEntry
	for name, rc := range as.PerRepo {
		entries = append(entries, repoEntry{name, rc})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rc.TotalChange > entries[j].rc.TotalChange
	})

	for i, e := range entries {
		repoName := StyleRepoTag.Render(Truncate(e.name, 28))
		commits := StyleNumeric.Render(fmt.Sprintf("%10s", FormatNumber(e.rc.Commits)))
		added := StyleNumeric.Render(fmt.Sprintf("%10s", FormatNumber(e.rc.Added)))
		removed := StyleNumeric.Render(fmt.Sprintf("%10s", FormatNumber(e.rc.Removed)))
		net := StyleNumeric.Render(fmt.Sprintf("%10s", FormatNumber(e.rc.Net)))

		row := fmt.Sprintf("  %s %s %s %s %s", repoName, commits, added, removed, net)

		var rowStyle lipgloss.Style
		if i%2 == 0 {
			rowStyle = StyleRowEven
		} else {
			rowStyle = StyleRowOdd
		}
		rows = append(rows, rowStyle.Render(row))
	}

	return strings.Join(rows, "\n")
}

// renderTimeline renders a monthly activity bar chart.
func (v OperativeView) renderTimeline(records []git.CommitRecord, width int) string {
	months := aggregateByMonth(records)
	if len(months) == 0 {
		return StyleSubtitle.Render("  No activity data")
	}

	// Find max commits for bar scaling
	maxCommits := 0
	for _, m := range months {
		if m.Commits > maxCommits {
			maxCommits = m.Commits
		}
	}

	// Limit to last 12 months
	if len(months) > 12 {
		months = months[len(months)-12:]
	}

	labelW := 10 // "Jan 2026  "
	numW := 6    // " 123 "
	barW := width - labelW - numW - 4
	if barW < 10 {
		barW = 10
	}
	if barW > 60 {
		barW = 60
	}

	var rows []string
	for _, m := range months {
		label := StyleDimWhite.Render(fmt.Sprintf("%-10s", m.Month.Format("Jan 2006")))
		count := StyleNumeric.Render(fmt.Sprintf("%4d ", m.Commits))

		// Bar
		filled := 0
		if maxCommits > 0 {
			filled = m.Commits * barW / maxCommits
		}
		if filled == 0 && m.Commits > 0 {
			filled = 1
		}

		bar := StyleBarCyan.Render(strings.Repeat("▓", filled)) +
			strings.Repeat(" ", barW-filled)

		rows = append(rows, fmt.Sprintf("  %s%s%s", label, count, bar))
	}

	return strings.Join(rows, "\n")
}

// filterRecordsByAuthor returns records matching the given author name
// (using the same fuzzy matching as stats.AreSimilarNames).
func filterRecordsByAuthor(records []git.CommitRecord, authorName string) []git.CommitRecord {
	var result []git.CommitRecord
	for _, r := range records {
		if r.Author == authorName || stats.AreSimilarNames(r.Author, authorName) {
			result = append(result, r)
		}
	}
	return result
}

// aggregateByMonth groups records into monthly buckets, sorted chronologically.
func aggregateByMonth(records []git.CommitRecord) []MonthActivity {
	byMonth := make(map[string]*MonthActivity)

	for _, r := range records {
		key := r.Date.Format("2006-01")
		ma, ok := byMonth[key]
		if !ok {
			y, m, _ := r.Date.Date()
			ma = &MonthActivity{
				Month: time.Date(y, m, 1, 0, 0, 0, 0, time.UTC),
			}
			byMonth[key] = ma
		}
		ma.Commits++
		ma.Added += r.Added
		ma.Removed += r.Removed
	}

	result := make([]MonthActivity, 0, len(byMonth))
	for _, ma := range byMonth {
		result = append(result, *ma)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Month.Before(result[j].Month)
	})

	return result
}
