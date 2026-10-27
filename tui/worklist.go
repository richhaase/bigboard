package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var worklistRule = lipgloss.NewStyle().Foreground(lipgloss.Color("#415665"))

func (m Model) renderWorklist(width, height int) string {
	m.normalizeActivityContributors()
	if m.showPaths {
		return m.renderCommitInspector(width, height)
	}
	if m.glance.detailOpen {
		return m.renderWorklistDetail(width, height)
	}
	if m.directory.active {
		return m.renderDirectoryBoard(width, height)
	}
	rows := m.glanceRepositories()
	selected := m.selectedRepository(rows)
	lines := m.worklistHeader(width)
	wide := width >= 110 && height >= 28
	inner := width - 4
	scope := "REPOSITORIES"
	if _, ok := m.areaRepository(); ok {
		scope = "WORK AREAS"
	}
	listBudget := m.worklistPageSize()
	start := max(0, min(selected-listBudget/2, len(rows)-listBudget))
	end := min(len(rows), start+listBudget)
	var summary []string
	if m.areaRepoID == "" {
		summary = worklistOverviewSummary(rows)
	}
	lines = append(lines, boardListHeading(scope, m.overviewSortLabel(), summary, start, end, len(rows), width))
	if m.areaRepoID == "" {
		lines = append(lines, "  "+m.repositoryOverviewColumns(inner))
	} else if wide {
		lines = append(lines, "  "+m.worklistColumns(inner))
	} else if inner >= 66 {
		a := (inner - 11) / 2
		p := inner - a - 10
		lines = append(lines, boardColumns("  "+padCells("REPO / AREA", a)+" "+padCells("LAST AUTHOR +OTHERS", p)+" "+rightCells("AGE", 6), width))
	} else {
		lines = append(lines, boardColumns("  "+padCells("REPO / AREA", max(1, inner-9))+" "+rightCells("AGE", 6), width))
	}

	maxValue := m.repositoryMeasureMax(rows)
	for i := start; i < end; i++ {
		if m.areaRepoID == "" {
			lines = append(lines, m.worklistRepositoryRow(rows[i], i == selected, inner, i+1, maxValue)...)
		} else {
			lines = append(lines, m.worklistRow(rows[i], i == selected, inner, wide)...)
		}
	}
	if len(rows) == 0 {
		empty := "No matching names. x clears search and board filters."
		if m.glanceQuery() == "" {
			empty = "No local activity or known PRs. t range · i repos"
			if m.areaRepoID != "" {
				empty = "No work-area activity. t range · Esc back"
			}
		}
		lines = append(lines, "  "+empty)
	}

	if m.areaRepoID != "" && wide && len(rows) > 0 {
		lines = append(lines, "  "+worklistRule.Render(strings.Repeat("─", inner)))
		preview := m
		preview.openGlanceArea(rows[selected])
		lines = append(lines, preview.worklistEvidence(rows[selected], inner, height-len(lines)-3, false)...)
	} else if m.areaRepoID != "" && len(rows) > 0 {
		r := rows[selected]
		label := fmt.Sprintf("Selected: %s commits · %d contributors", FormatNumber(r.commits), len(r.people))
		lines = append(lines, StyleDimWhite.Render("  "+label))
	} else if len(rows) > 0 {
		lines = m.appendActivityContributors(lines, rows[selected], width, height)
	}
	return m.worklistFinish(lines, width, height, false)
}

func (m Model) worklistHeader(width int) []string {
	title := "BIGBOARD / Activity"
	if m.glance.detailOpen {
		repo, _ := m.areaRepository()
		title += " / " + displayText(repo.Name) + " / " + displayText(m.glanceDetailArea().Name)
		if width < 70 {
			title = "BIGBOARD / " + displayText(m.glanceDetailArea().Name)
		}
	}
	return m.screenHeader(strings.TrimPrefix(title, "BIGBOARD / "), width)
}

func (m Model) worklistColumnWidths(width int) (area, author, subject, prs, age int) {
	area = max(18, width/5)
	author = max(18, width/6)
	age = 6
	if len(m.allPRs()) > 0 {
		prs = 30
	}
	gaps := 5
	if prs > 0 {
		gaps++
	}
	subject = max(1, width-area-author-prs-age-gaps)
	return
}
func (m Model) worklistColumns(width int) string {
	a, p, s, pr, t := m.worklistColumnWidths(width)
	columns := "  " + padCells("REPO / AREA", a) + " " + padCells("LAST AUTHOR +OTHERS", p) + " " + padCells("LATEST COMMIT", s)
	if pr > 0 {
		columns += " " + padCells("PRs", pr)
	}
	return StyleDimWhite.Render(columns + " " + padCells("AGE", t))
}
func worklistLastAuthor(r activityRow) string {
	if len(r.people) == 0 {
		return "—"
	}
	for _, p := range r.people {
		if p.ID == r.authorID {
			label := glancePersonLabel(p, r.people)
			if p.Bot {
				label = "[BOT] " + label
			}
			return label
		}
	}
	return "—"
}
func worklistAuthorCell(r activityRow, width int) string {
	suffix := ""
	if len(r.people) > 1 {
		suffix = fmt.Sprintf(" +%d", len(r.people)-1)
	}
	return padCells(ansi.Truncate(worklistLastAuthor(r), max(1, width-ansi.StringWidth(suffix)), "…")+suffix, width)
}
func (m Model) worklistAge(at time.Time, repoID string) string {
	return activityAge(at, m.local.scannedAt[repoID])
}

func activityAge(at, ref time.Time) string {
	if at.IsZero() {
		return "—"
	}
	if ref.IsZero() {
		return at.Local().Format("Jan02")
	}

	if at.After(ref) {
		return "future"
	}
	d := ref.Sub(at)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
func worklistSignalStyle(text string) lipgloss.Style {
	if strings.Contains(text, "failing") || strings.Contains(text, "FAILURE") || strings.Contains(text, "ERROR") {
		return lipgloss.NewStyle().Foreground(ColorRed)
	}
	for _, s := range []string{"STALE", "PARTIAL", "UNKNOWN", "unknown", "unavailable", "pending", "PENDING", "EXPECTED"} {
		if strings.Contains(text, s) {
			return StyleAmber
		}
	}
	return StyleDimWhite
}
func (m Model) worklistSignal(text string) string {
	return worklistSignalStyle(text).Render(text)
}
func (m Model) worklistRow(r activityRow, selected bool, width int, wide bool) []string {
	name := displayText(r.label)
	if m.local.staleRepos[r.repositoryID] {
		name = "[STALE] " + name
	}
	subject := displayText(r.subject)
	if subject == "" {
		subject = "(no subject)"
	}
	if r.commits == 0 {
		subject = "No local commits"
	}
	cursor := "  "
	if selected {
		cursor = "› "
	}
	var lines []string
	if wide {
		a, p, s, pr, t := m.worklistColumnWidths(width)
		signal := ""
		if len(m.scopePRs(r.scopeID)) > 0 {
			signal = m.worklistSignal(m.worklistPRSignal(r.scopeID))
		}
		line := cursor + padCells(name, a) + " " + worklistAuthorCell(r, p) + " " + padCells(subject, s)
		if pr > 0 {
			line += " " + padCells(signal, pr)
		}
		lines = []string{line + " " + padCells(m.worklistAge(r.latest, r.repositoryID), t)}
	} else if width >= 66 {
		a := (width - 11) / 2
		p := width - a - 10
		if len(m.scopePRs(r.scopeID)) > 0 {
			subject = padCells(subject, width-33) + " " + padCells(m.worklistSignal(m.worklistPRSignal(r.scopeID)), 30)
		}
		lines = []string{cursor + padCells(name, a) + " " + worklistAuthorCell(r, p) + " " + rightCells(m.worklistAge(r.latest, r.repositoryID), 6), "  " + padCells(subject, width-2)}
	} else {
		lines = []string{cursor + padCells(name, width-9) + " " + rightCells(m.worklistAge(r.latest, r.repositoryID), 6), "  " + worklistAuthorCell(r, width-2), "  " + padCells(subject, width-2)}
	}
	for i, line := range lines {
		style := lipgloss.NewStyle()
		if selected {
			style = style.Background(ColorRowSelect)
		}
		line = style.Render(padCells(line, width))
		if selected {
			line = selectedRowBackground(line)
		}
		lines[i] = "  " + line
	}

	return lines
}

// Compact signal preserves the evidence qualifier before compressing counts.
func (m Model) worklistPRSignal(scope string) string {
	signal := m.prGlanceSignal(scope)
	signal = strings.ReplaceAll(signal, "PRs unknown · unavailable/partial", "? unavailable/partial")
	signal = strings.ReplaceAll(signal, "PRs unknown", "? unknown")
	signal = strings.ReplaceAll(signal, " known open", " known")
	signal = strings.ReplaceAll(signal, " checks unknown", " ? checks")
	return signal
}

func (m Model) worklistFinish(lines []string, width, height int, detail bool) string {
	return screenFinish(lines, m.worklistFooter(width, height, detail), m.screenCoverage(), width, height)
}

func (m Model) worklistFooter(width, height int, detail bool) footerHints {
	if m.glance.searching {
		return searchFooter()
	}
	pane := !detail && m.areaRepoID == "" && activityContributorsHeight(width, height) > 0
	if pane && m.contributorPane.focused {
		return focusedContributorsFooter()
	}
	var local []footerHint
	if detail || width < 70 && m.areaRepoID != "" {
		local = append(local, footerHint{"Esc", "back"})
	}
	local = append(local, footerHint{"f", "filters"})
	if pane {
		local = append(local, footerHint{"Tab", "contributors"})
	}
	if width < 70 {
		if !detail && m.areaRepoID == "" {
			local = append(local, footerHint{"Enter", "open"})
		}
	} else if detail {
		local = append(local, footerHint{"Enter", "open"})
		if width < 110 {
			local = append(local, footerHint{"v", "PRs"})
		} else {
			local = append(local, footerHint{"t", "date range"})
		}
	} else {
		local = append(local, footerHint{"/", "find"}, footerHint{"Enter", "open"})
		if width >= 110 {
			local = append(local, footerHint{"t", "date range"})
		}
		local = append(local, footerHint{"s", "sort"})
	}
	return footerHints{local: local}
}

func (m Model) worklistPageSize() int {
	if m.areaRepoID == "" || m.directory.active {
		return max(1, (m.height-screenHeaderRows-screenFooterRows-2-activityContributorsHeight(m.width, m.height))/activityRankingRowHeight(m.width))
	}
	if m.width >= 110 && m.height >= 28 {
		return max(3, (m.height-19)/2)
	}
	rowHeight := 3
	if m.width-4 >= 66 {
		rowHeight = 2
	}
	return max(1, (m.height-screenHeaderRows-screenFooterRows-3)/rowHeight)
}

func activityRankingRowHeight(width int) int {
	if width-4 >= 96 {
		return 1
	}
	if width-4 >= 66 {
		return 2
	}
	return 3
}

func activityContributorsHeight(width, height int) int {
	available := height - screenHeaderRows - screenFooterRows - 2
	panel := min(9, available/2, available-3*activityRankingRowHeight(width))
	if panel < 4 {
		return 0
	}
	return panel
}

func (m Model) appendActivityContributors(lines []string, row activityRow, width, height int) []string {
	budget := activityContributorsHeight(width, height)
	if budget == 0 {
		return lines
	}
	start := height - screenFooterRows - budget
	for len(lines) < start {
		lines = append(lines, "")
	}
	focused := !m.contributorPane.focused
	for i := screenHeaderRows; i < len(lines); i++ {
		text := ansi.Cut(lines[i], 2, width-2)
		if i == screenHeaderRows {
			text = activityPaneHeading(text, width-4, focused)
		}
		lines[i] = activityPaneRail(focused) + text
	}
	return append(lines, m.activityContributors(row, width, budget)...)
}
