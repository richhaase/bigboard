package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var worklistRule = lipgloss.NewStyle().Foreground(lipgloss.Color("#415665"))

// Worklist is one scan across scope, last author, concrete work and age. Narrow
// terminals replace the overview with detail instead of squeezing two panels.
func (m Model) renderWorklist(width, height int) string {
	if m.showPaths {
		return m.renderCommitInspector(width, height)
	}
	if m.glance.detailOpen {
		return m.renderWorklistDetail(width, height)
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
	lines = append(lines, boardListHeading(scope, m.overviewSortLabel(), nil, start, end, len(rows), width))
	if wide {
		lines = append(lines, "  "+m.worklistColumns(inner))
	} else if inner >= 66 {
		a := (inner - 11) / 2
		p := inner - a - 10
		lines = append(lines, boardColumns("  "+padCells("REPO / AREA", a)+" "+padCells("LAST AUTHOR +OTHERS", p)+" "+rightCells("AGE", 6), width))
	} else {
		lines = append(lines, boardColumns("  "+padCells("REPO / AREA", max(1, inner-9))+" "+rightCells("AGE", 6), width))
	}

	for i := start; i < end; i++ {
		lines = append(lines, m.worklistRow(rows[i], i == selected, inner, wide)...)
	}
	if len(rows) == 0 {
		empty := "No matching names. Esc clears search."
		if m.glanceQuery() == "" {
			empty = "No local activity or known PRs. t range · i repos"
			if m.areaRepoID != "" {
				empty = "No work-area activity. t range · Esc back"
			}
		}
		lines = append(lines, "  "+empty)
	}

	if wide && len(rows) > 0 {
		lines = append(lines, "  "+worklistRule.Render(strings.Repeat("─", inner)))
		preview := m
		if m.areaRepoID != "" {
			preview.openGlanceArea(rows[selected])
		}
		lines = append(lines, preview.worklistEvidence(rows[selected], inner, height-len(lines)-3, false)...)
	} else if len(rows) > 0 {
		r := rows[selected]
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  Selected: %s commits · %d contributors", FormatNumber(r.commits), len(r.people))))
	}
	return m.worklistFinish(lines, width, height, false)
}

func (m Model) worklistHeader(width int) []string {
	title := "BIGBOARD / Worklist"
	if m.glance.detailOpen {
		repo, _ := m.areaRepository()
		title += " / " + displayText(repo.Name) + " / " + displayText(m.glanceDetailArea().Name)
		if width < 70 {
			title = "BIGBOARD / " + displayText(m.glanceDetailArea().Name)
		}
	}
	return m.screenHeader(strings.TrimPrefix(title, "BIGBOARD / "), width)
}

// checked is an attempt time, not freshness of retained evidence. Explicitly
// distinguish it from lastGood and keep unavailable/partial ahead of timestamps.
func (m Model) worklistPRFreshness(id string) string {
	s, ok := m.prState.snapshots[m.prState.repositories[id]]
	if !ok {
		if err := m.prState.errors[id]; err != nil {
			return "PRs: unavailable · all dates · " + prFailureReason(err) + " · p details"
		}
		if m.prState.loading {
			return "PRs: refreshing · all dates · no snapshot yet"
		}
		return "PRs: unknown · all dates · p details"
	}
	state := "complete"
	if s.partial {
		state = "PARTIAL"
	}
	if s.err != nil || m.prState.errors[id] != nil {
		state = "STALE"
		if s.partial {
			state += "/PARTIAL"
		}
		if len(s.prs) == 0 && s.lastGood.IsZero() {
			state = "unavailable"
		}
	}
	line := "PRs: " + state + " · all dates"
	if s.err != nil {
		line += " · " + prFailureReason(s.err)
	}
	if !s.checked.IsZero() {
		line += " · checked " + s.checked.Local().Format("15:04 MST")
	}
	if (s.partial || s.err != nil || m.prState.errors[id] != nil) && !s.lastGood.IsZero() {
		line += " · last complete " + s.lastGood.Local().Format("Jan 02 15:04")
	}
	if m.prState.loading {
		line += " · refreshing"
	}
	return line
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
func worklistLastAuthor(r repositoryActivity) string {
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
func worklistAuthorCell(r repositoryActivity, width int) string {
	suffix := ""
	if len(r.people) > 1 {
		suffix = fmt.Sprintf(" +%d", len(r.people)-1)
	}
	return padCells(ansi.Truncate(worklistLastAuthor(r), max(1, width-ansi.StringWidth(suffix)), "…")+suffix, width)
}
func (m Model) worklistAge(at time.Time, repoID string) string {
	return activityAge(at, m.scannedAt[repoID])
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
func (m Model) worklistRow(r repositoryActivity, selected bool, width int, wide bool) []string {
	name := displayText(r.repo.Name)
	id := r.repo.ID
	if m.areaRepoID != "" {
		id = m.areaRepoID
	}
	if m.staleRepos[id] {
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
		if len(m.scopePRs(r.repo.ID)) > 0 {
			signal = m.worklistSignal(m.worklistPRSignal(r.repo.ID))
		}
		line := cursor + padCells(name, a) + " " + worklistAuthorCell(r, p) + " " + padCells(subject, s)
		if pr > 0 {
			line += " " + padCells(signal, pr)
		}
		lines = []string{line + " " + padCells(m.worklistAge(r.latest, id), t)}
	} else if width >= 66 {
		a := (width - 11) / 2
		p := width - a - 10
		if len(m.scopePRs(r.repo.ID)) > 0 {
			subject = padCells(subject, width-33) + " " + padCells(m.worklistSignal(m.worklistPRSignal(r.repo.ID)), 30)
		}
		lines = []string{cursor + padCells(name, a) + " " + worklistAuthorCell(r, p) + " " + rightCells(m.worklistAge(r.latest, id), 6), "  " + padCells(subject, width-2)}
	} else {
		lines = []string{cursor + padCells(name, width-9) + " " + rightCells(m.worklistAge(r.latest, id), 6), "  " + worklistAuthorCell(r, width-2), "  " + padCells(subject, width-2)}
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
	footer := "↑↓ select · →/Enter open · / find · s/S sort · t range · b bots · v repo PRs · r refresh · q quit"
	if detail {
		footer = "↑↓ select · →/Enter evidence · Tab/1–4 lens · ←/Esc back · t range · b bots · v repo PRs · r refresh"
	}
	if width >= 70 && width < 110 {
		footer = "v repo PRs · / find · ↑↓ select · → open · r refresh"
		if detail {
			footer = "←/Esc back · v repo PRs · Tab lens · → evidence"
		}
	}
	if width < 70 {
		footer = "↑↓ select · → open"
		if detail || m.areaRepoID != "" {
			footer = "←/Esc back · ↑↓ select"
		}
	}
	if m.glance.searching {
		footer = "F1 help · Enter accept · Esc clear"
	} else {
		footer = "? help · " + footer
	}

	return screenFinish(lines, footer, m.screenCoverage(), width, height)
}

func (m Model) worklistPageSize() int {
	if m.width >= 110 && m.height >= 28 {
		return max(3, (m.height-19)/2)
	}
	rowHeight := 3
	if m.width-4 >= 66 {
		rowHeight = 2
	}
	return max(1, (m.height-screenHeaderRows-screenFooterRows-3)/rowHeight)
}
