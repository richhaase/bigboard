package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
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
	timeIdx int,
	repoCount int,
	excludedCount int,
) string {
	var sections []string

	// Header banner
	if width >= 82 {
		for i, line := range bannerLines {
			style := lipgloss.NewStyle().Foreground(ColorBannerGrad[i])
			sections = append(sections, "  "+style.Render(line))
		}
		sections = append(sections, "")
	}

	// Status line + time picker
	sections = append(sections, RenderFooter(repoCount, excludedCount, width, ""))
	sections = append(sections, RenderTimePicker(timeIdx))
	sections = append(sections, "")

	// Operative name as section header
	sections = append(sections, RenderSectionHeader(fmt.Sprintf("CONTRIBUTOR: %s", strings.ToUpper(authorName)), width))

	// Summary stat boxes
	if authorStats != nil {
		sections = append(sections, "")
		sections = append(sections, RenderStatBoxes(authorStats.Commits, authorStats.Added, authorStats.Removed))
	}

	// Per-repo breakdown table
	if authorStats != nil && len(authorStats.PerRepo) > 0 {
		sections = append(sections, "")
		sections = append(sections, RenderSectionHeader("REPO CONTRIBUTIONS", width))
		sections = append(sections, "")
		sections = append(sections, v.renderRepoBreakdown(authorStats, width))
	}

	// Monthly activity timeline
	authorRecords := filterRecordsByAuthor(records, authorName)
	if len(authorRecords) > 0 {
		sections = append(sections, "")
		sections = append(sections, RenderSectionHeader("ACTIVITY TIMELINE", width))
		sections = append(sections, "")
		sections = append(sections, v.renderTimeline(authorRecords, width))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderRepoBreakdown renders a compact table of per-repo contributions.
func (v OperativeView) renderRepoBreakdown(as *stats.AuthorStats, width int) string {
	nameW := 30
	numW := 10

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

	// Find max total for impact bar
	maxTotal := 0
	for _, e := range entries {
		if e.rc.TotalChange > maxTotal {
			maxTotal = e.rc.TotalChange
		}
	}

	barW := 15
	rowFmt := fmt.Sprintf("  %%-%ds %%%ds %%%ds %%%ds %%%ds  %%s", nameW, numW, numW, numW, numW)

	var rows []string

	// Header
	header := fmt.Sprintf(rowFmt,
		StyleTableHeader.Render(fmt.Sprintf("%-*s", nameW, "REPO")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "COMMITS")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "ADDED")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "REMOVED")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "NET")),
		"",
	)
	rows = append(rows, header)
	rows = append(rows, "  "+StyleDimCyan.Render(strings.Repeat("━", width-4)))

	for i, e := range entries {
		name := StyleMagenta.Render(fmt.Sprintf("%-*s", nameW, Truncate(e.name, nameW)))
		commits := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Commits)))
		added := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Added)))
		removed := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Removed)))
		net := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Net)))
		bar := RenderImpactBar(e.rc.Added, e.rc.Removed, maxTotal, barW)

		row := fmt.Sprintf("  %s %s %s %s %s  %s", name, commits, added, removed, net, bar)

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

	// Limit to last 12 months
	if len(months) > 12 {
		months = months[len(months)-12:]
	}

	// Find max total change for bar scaling
	maxTotal := 0
	for _, m := range months {
		total := m.Added + m.Removed
		if total > maxTotal {
			maxTotal = total
		}
	}

	labelW := 10 // "Jan 2026  "
	numW := 6    // " 123 "
	barW := width - labelW - numW - 6
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
		bar := RenderImpactBar(m.Added, m.Removed, maxTotal, barW)

		rows = append(rows, fmt.Sprintf("  %s%s%s", label, count, bar))
	}

	return strings.Join(rows, "\n")
}

// filterRecordsByAuthor returns records matching the given author name.
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
