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
	AI      int
}

// OperativeView renders a contributor detail screen.
type OperativeView struct {
	FuzzyMatching bool
}

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
	now := time.Now()

	sections = append(sections, renderBanner(width)...)
	sections = append(sections, "")

	sections = append(sections, RenderFooter(repoCount, excludedCount, width, ""))
	sections = append(sections, RenderTimePicker(timeIdx))
	sections = append(sections, "")

	sections = append(sections, RenderSectionHeader(fmt.Sprintf("CONTRIBUTOR: %s", strings.ToUpper(authorName)), width))

	authorRecords := filterRecordsByAuthor(records, authorStats, authorName, v.FuzzyMatching)
	if authorStats == nil && len(authorRecords) == 0 {
		sections = append(sections, "")
		sections = append(sections, StyleAmber.Render("  ◈ NO SIGNAL — no commit data in range. Widen the time range with ←/→."))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	if authorStats != nil {
		sections = append(sections, "")
		sections = append(sections, RenderStatBoxes(authorStats.Commits, authorStats.Added, authorStats.Removed, authorStats.AICommits, width))
		sections = append(sections, "")
		sections = append(sections, renderMetricsLine(authorStats))
	}

	if authorStats != nil && len(authorStats.PerRepo) > 0 {
		sections = append(sections, "")
		sections = append(sections, RenderSectionHeader("REPO CONTRIBUTIONS", width))
		sections = append(sections, "")
		sections = append(sections, v.renderRepoBreakdown(authorStats, width))
	}

	if len(authorRecords) > 0 {
		sections = append(sections, "")
		sections = append(sections, RenderSectionHeader("ACTIVITY TIMELINE", width))
		sections = append(sections, "")
		sections = append(sections, v.renderTimeline(authorRecords, width))

		sections = append(sections, "")
		sections = append(sections, RenderSectionHeader("ACTIVITY MATRIX", width))
		sections = append(sections, "")
		sections = append(sections, v.renderHeatmap(authorRecords, width, now))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func heatmapRamp() []struct {
	ch    string
	style lipgloss.Style
} {
	return []struct {
		ch    string
		style lipgloss.Style
	}{
		{"·", StyleDimWhite},
		{"░", StyleBarCyanDim},
		{"▒", StyleBarCyanMid},
		{"▓", StyleCyan},
		{"█", StyleMagenta},
	}
}

func (v OperativeView) renderHeatmap(records []git.CommitRecord, width int, now time.Time) string {
	totals := make(map[string]int)
	maxV := 0
	for _, r := range records {
		k := r.Date.Format("2006-01-02")
		totals[k] += r.Added + r.Removed
		if totals[k] > maxV {
			maxV = totals[k]
		}
	}
	if maxV == 0 {
		maxV = 1
	}

	weeks := width - chromeInset - 6
	if weeks < 12 {
		weeks = 12
	}
	if weeks > 53 {
		weeks = 53
	}

	firstCol := now.AddDate(0, 0, -7*(weeks-1))
	startSunday := firstCol.AddDate(0, 0, -int(firstCol.Weekday()))

	ramp := heatmapRamp()
	weekdays := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

	var rows []string
	for wd := 0; wd < 7; wd++ {
		var b strings.Builder
		b.WriteString("  ")
		b.WriteString(StyleDimWhite.Render(fmt.Sprintf("%-4s", weekdays[wd])))
		for col := 0; col < weeks; col++ {
			cellDate := startSunday.AddDate(0, 0, col*7+wd)
			if cellDate.After(now) {
				b.WriteString(" ")
				continue
			}
			level := 0
			if val := totals[cellDate.Format("2006-01-02")]; val > 0 {
				level = 1 + (val*3)/maxV
				if level > 4 {
					level = 4
				}
			}
			b.WriteString(ramp[level].style.Render(ramp[level].ch))
		}
		rows = append(rows, b.String())
	}

	legend := "  " + StyleDimWhite.Render("less ") +
		ramp[1].style.Render(ramp[1].ch) + ramp[2].style.Render(ramp[2].ch) +
		ramp[3].style.Render(ramp[3].ch) + ramp[4].style.Render(ramp[4].ch) +
		StyleDimWhite.Render(" more")
	rows = append(rows, "", legend)
	return strings.Join(rows, "\n")
}

func (v OperativeView) renderRepoBreakdown(as *stats.AuthorStats, width int) string {
	nameW := 30
	numW := 10

	type repoEntry struct {
		name string
		rc   *stats.RepoContribution
	}
	var entries []repoEntry
	for name, rc := range as.PerRepo {
		entries = append(entries, repoEntry{name, rc})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].rc.TotalChange != entries[j].rc.TotalChange {
			return entries[i].rc.TotalChange > entries[j].rc.TotalChange
		}
		return entries[i].name < entries[j].name
	})

	maxTotal := 0
	for _, e := range entries {
		if e.rc.TotalChange > maxTotal {
			maxTotal = e.rc.TotalChange
		}
	}

	barW := 15
	rowFmt := fmt.Sprintf("  %%-%ds %%%ds %%%ds %%%ds %%%ds  %%s", nameW, numW, numW, numW, numW)

	var rows []string

	header := fmt.Sprintf(rowFmt,
		StyleTableHeader.Render(fmt.Sprintf("%-*s", nameW, "REPO")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "COMMITS")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "ADDED")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "REMOVED")),
		StyleTableHeader.Render(fmt.Sprintf("%*s", numW, "NET")),
		"",
	)
	rows = append(rows, header)
	rows = append(rows, "  "+StyleDimCyan.Render(hrule(width-chromeInset)))

	for i, e := range entries {
		name := StyleMagenta.Render(padRight(Truncate(e.name, nameW), nameW))
		commits := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Commits)))
		added := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Added)))
		removed := StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Removed)))
		net := renderNet(e.rc.Net, numW)
		bar := RenderImpactBar(e.rc.Added, e.rc.Removed, maxTotal, barW)

		row := fmt.Sprintf("  %s %s %s %s %s  %s", name, commits, added, removed, net, bar)
		if e.rc.AICommits > 0 && e.rc.Commits > 0 {
			row += "  " + StyleAmber.Render(fmt.Sprintf("ai %d%%", e.rc.AICommits*100/e.rc.Commits))
		}

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

func (v OperativeView) renderTimeline(records []git.CommitRecord, width int) string {
	months := aggregateByMonth(records)
	if len(months) == 0 {
		return StyleSubtitle.Render("  No activity data")
	}

	if len(months) > 12 {
		months = months[len(months)-12:]
	}

	maxTotal := 0
	for _, m := range months {
		total := m.Added + m.Removed
		if total > maxTotal {
			maxTotal = total
		}
	}

	labelW := 10
	numW := 6
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

		row := fmt.Sprintf("  %s%s%s", label, count, bar)
		if m.AI > 0 {
			row += " " + StyleAmber.Render(fmt.Sprintf("◆%d", m.AI))
		}
		rows = append(rows, row)
	}

	return strings.Join(rows, "\n")
}

func renderMetricsLine(as *stats.AuthorStats) string {
	var parts []string
	if as.ActiveDays > 0 {
		parts = append(parts, fmt.Sprintf("ACTIVE %d days", as.ActiveDays))
	}
	if !as.FirstCommit.IsZero() {
		parts = append(parts,
			"FIRST "+as.FirstCommit.Format("2006-01-02"),
			"LAST "+as.LastCommit.Format("2006-01-02"))
	}
	parts = append(parts, fmt.Sprintf("CHURN %.2f", as.ChurnRatio()))
	if as.AICommits > 0 {
		parts = append(parts, fmt.Sprintf("AI %d%%", as.AIPercent()))
	}
	return "  " + StyleDimCyan.Render(strings.Join(parts, "  ·  "))
}

func filterRecordsByAuthor(records []git.CommitRecord, as *stats.AuthorStats, authorName string, fuzzyMatching bool) []git.CommitRecord {
	var result []git.CommitRecord
	for _, r := range records {
		match := false
		if as != nil && len(as.Aliases) > 0 {
			match = as.Aliases[r.Author]
		} else {
			match = stats.NamesMatchWithOptions(r.Author, authorName, stats.AggregateOptions{
				FuzzyMatching: fuzzyMatching,
			})
		}
		if match {
			result = append(result, r)
		}
	}
	return result
}

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
		if r.AIAssisted {
			ma.AI++
		}
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
