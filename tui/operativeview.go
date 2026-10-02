package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// MonthActivity holds commit stats for a single month.
type MonthActivity struct {
	Month              time.Time
	Commits            int
	Added              int
	Removed            int
	AI                 int
	UnknownLineCommits int
}

// OperativeView renders a contributor detail screen.
type OperativeView struct {
	FuzzyMatching bool
	RangeLabel    string
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
	label := v.RangeLabel
	if label == "" {
		label = presetRangeLabel(timeIdx)
	}
	sections = append(sections, rangeControl(label))
	sections = append(sections, "")

	sections = append(sections, RenderSectionHeader(fmt.Sprintf("CONTRIBUTOR: %s", strings.ToUpper(authorName)), width))

	authorRecords := filterRecordsByAuthor(records, authorStats, authorName, v.FuzzyMatching)
	if authorStats == nil && len(authorRecords) == 0 {
		sections = append(sections, "")
		sections = append(sections, StyleAmber.Render("  ◈ NO SIGNAL — no commit data in range. Press t to change the time range."))
		return wrapStatistics(strings.Join(sections, "\n"), width)
	}

	if authorStats != nil {
		sections = append(sections, "")
		sections = append(sections, renderContributorSummary(authorStats, width))
		sections = append(sections, "")
		sections = append(sections, renderMetricsLine(authorStats))
		if authorStats.UnknownLineCommits > 0 {
			sections = append(sections, StyleAmber.Render("  ? Line totals are incomplete (shallow history)."))
		}
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

	return wrapStatistics(strings.Join(sections, "\n"), width)
}

// Detail must expose every total even when the glance summary becomes compact.
func renderContributorSummary(as *stats.AuthorStats, width int) string {
	if width <= 0 || width >= midTableWidth {
		return renderStatBoxes(as.Commits, as.Added, as.Removed, as.AICommits, width, as.UnknownLineCommits)
	}
	return wrapStatistics(strings.Join([]string{
		"  COMMITS " + StyleNumeric.Render(FormatNumber(as.Commits)),
		"  ADDED   " + StyleNumeric.Render(formatLineCount(as.Added, as.UnknownLineCommits)),
		"  REMOVED " + StyleNumeric.Render(formatLineCount(as.Removed, as.UnknownLineCommits)),
		"  NET     " + renderKnownNet(as.Net, 0, as.UnknownLineCommits),
		"  AI CO-AUTHORED " + StyleAmber.Render(fmt.Sprintf("%s (%d)", percentLabel(as.AICommits, as.Commits), as.AICommits)),
	}, "\n"), width)
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
	now = now.In(time.Local)
	unknown := make(map[string]bool)
	totals := make(map[string]int)
	maxV := 0
	for _, r := range records {
		k := r.Date.In(time.Local).Format("2006-01-02")
		if r.LinesUnknown {
			unknown[k] = true
		} else {
			totals[k] += r.Added + r.Removed
		}
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
			if unknown[cellDate.Format("2006-01-02")] {
				b.WriteString(StyleAmber.Render("?"))
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

	// Retain the table when every cell fits. Use terminal-cell widths rather
	// than bytes/runes, and include numeric qualifiers and the optional AI share.
	nameW, numW, aiW := 30, 10, 0
	for _, e := range entries {
		nameW = max(nameW, ansi.StringWidth(displayText(e.name)))
		for _, value := range []string{FormatNumber(e.rc.Commits), formatLineCount(e.rc.Added, e.rc.UnknownLineCommits), formatLineCount(e.rc.Removed, e.rc.UnknownLineCommits), ansi.Strip(renderKnownNet(e.rc.Net, 0, e.rc.UnknownLineCommits))} {
			numW = max(numW, ansi.StringWidth(value))
		}
		if e.rc.AICommits > 0 && e.rc.Commits > 0 {
			aiW = max(aiW, 2+ansi.StringWidth("ai "+percentLabel(e.rc.AICommits, e.rc.Commits)))
		}
	}
	const barW = 15
	tableWidth := 2 + nameW + 4*(1+numW) + 2 + barW + aiW
	narrow := width > 0 && tableWidth > width
	var rows []string
	if !narrow {
		header := "  " + StyleTableHeader.Render(padRight("REPO", nameW))
		for _, label := range []string{"COMMITS", "ADDED", "REMOVED", "NET"} {
			header += " " + StyleTableHeader.Render(fmt.Sprintf("%*s", numW, label))
		}
		rows = append(rows, header, "  "+StyleDimCyan.Render(hrule(max(0, width-chromeInset))))
	}
	for i, e := range entries {
		var row string
		if narrow {
			cells := []string{
				"  " + StyleMagenta.Render(displayText(e.name)),
				"  COMMITS " + StyleNumeric.Render(FormatNumber(e.rc.Commits)),
				"  ADDED   " + StyleNumeric.Render(formatLineCount(e.rc.Added, e.rc.UnknownLineCommits)),
				"  REMOVED " + StyleNumeric.Render(formatLineCount(e.rc.Removed, e.rc.UnknownLineCommits)),
				"  NET     " + renderKnownNet(e.rc.Net, 0, e.rc.UnknownLineCommits),
				"  AI      " + StyleAmber.Render(percentLabel(e.rc.AICommits, e.rc.Commits)),
			}
			if i > 0 {
				rows = append(rows, "")
			}
			row = wrapStatistics(strings.Join(cells, "\n"), width)
		} else {
			row = fmt.Sprintf("  %s %s %s %s %s  %s",
				StyleMagenta.Render(padRight(displayText(e.name), nameW)),
				StyleNumeric.Render(fmt.Sprintf("%*s", numW, FormatNumber(e.rc.Commits))),
				StyleNumeric.Render(fmt.Sprintf("%*s", numW, formatLineCount(e.rc.Added, e.rc.UnknownLineCommits))),
				StyleNumeric.Render(fmt.Sprintf("%*s", numW, formatLineCount(e.rc.Removed, e.rc.UnknownLineCommits))),
				renderKnownNet(e.rc.Net, numW, e.rc.UnknownLineCommits),
				RenderImpactBar(e.rc.Added, e.rc.Removed, maxTotal, barW))
			if e.rc.AICommits > 0 && e.rc.Commits > 0 {
				row += "  " + StyleAmber.Render("ai "+percentLabel(e.rc.AICommits, e.rc.Commits))
			}
		}
		rowStyle := StyleRowEven
		if i%2 != 0 {
			rowStyle = StyleRowOdd
		}
		rows = append(rows, rowStyle.Render(row))
	}
	return strings.Join(rows, "\n")
}

// Wrap before the viewport slices physical rows: clipping at the canvas cannot
// recover hidden values, and wrapping after paging would make offsets incorrect.
func wrapStatistics(content string, width int) string {
	if width <= 0 {
		return content
	}
	return ansi.Hardwrap(content, width, true)
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

	var rows []string
	for _, m := range months {
		label := StyleDimWhite.Render(fmt.Sprintf("%-10s", m.Month.Format("Jan 2006")))
		count := StyleNumeric.Render(fmt.Sprintf("%4d ", m.Commits))
		suffix := ""
		if m.UnknownLineCommits > 0 {
			suffix += StyleAmber.Render(" ?")
		}
		if m.AI > 0 {
			suffix += " " + StyleAmber.Render(fmt.Sprintf("◆%d", m.AI))
		}
		prefix := "  " + label + count
		barW := 60
		if width > 0 {
			barW = max(0, min(barW, width-ansi.StringWidth(prefix)-ansi.StringWidth(suffix)))
		}
		bar := ""
		if barW > 0 {
			bar = RenderImpactBar(m.Added, m.Removed, maxTotal, barW)
		}
		rows = append(rows, wrapStatistics(prefix+bar+suffix, width))
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
		parts = append(parts, "AI "+percentLabel(as.AICommits, as.Commits))
	}
	return "  " + StyleDimCyan.Render(strings.Join(parts, "  ·  "))
}

func percentLabel(part, whole int) string {
	if whole <= 0 {
		return "0%"
	}
	pct := part * 100 / whole
	if pct == 0 && part > 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", pct)
}

func filterRecordsByAuthor(records []git.CommitRecord, as *stats.AuthorStats, authorName string, fuzzyMatching bool) []git.CommitRecord {
	var result []git.CommitRecord
	for _, r := range stats.UniqueRecords(records) {
		match := false
		if as != nil && as.ID != "" {
			match = stats.IdentityID(r) == as.ID
		} else if as != nil && len(as.Aliases) > 0 {
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
		date := r.Date.In(time.Local)
		key := date.Format("2006-01")
		ma, ok := byMonth[key]
		if !ok {
			y, m, _ := date.Date()
			ma = &MonthActivity{
				// Use UTC as a marker for the local calendar month: local midnight
				// may fall in a DST gap and normalize into the previous month.
				Month: time.Date(y, m, 1, 0, 0, 0, 0, time.UTC),
			}
			byMonth[key] = ma
		}
		ma.Commits++
		if r.LinesUnknown {
			ma.UnknownLineCommits++
		} else {
			ma.Added += r.Added
			ma.Removed += r.Removed
		}
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

	return fillMonthGaps(result)
}

func fillMonthGaps(months []MonthActivity) []MonthActivity {
	if len(months) < 2 {
		return months
	}
	filled := make([]MonthActivity, 0, len(months))
	filled = append(filled, months[0])
	for _, ma := range months[1:] {
		prev := filled[len(filled)-1].Month
		for cur := prev.AddDate(0, 1, 0); cur.Before(ma.Month); cur = cur.AddDate(0, 1, 0) {
			filled = append(filled, MonthActivity{Month: cur})
		}
		filled = append(filled, ma)
	}
	return filled
}
