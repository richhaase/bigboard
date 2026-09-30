package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func awarenessRow(line string, selected bool, index, width int) string {
	style := StyleRowEven
	if index%2 != 0 {
		style = StyleRowOdd
	}
	if selected {
		style = StyleRowSelected
	}
	return "  " + style.Width(max(1, width-4)).Render(ansi.Truncate(strings.TrimPrefix(line, "  "), max(1, width-4), "…"))
}
func glanceCursor(selected bool) string {
	if selected {
		return "▸"
	}
	return " "
}
func padCells(s string, width int) string {
	s = ansi.Truncate(s, max(1, width), "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
func rightCells(s string, width int) string {
	s = ansi.Truncate(s, max(1, width), "…")
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(s))) + s
}
func fitGlanceLines(lines []string, width, height int) string {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m Model) glanceScopeRecords() []git.CommitRecord {
	records := m.filteredRecords()
	if repo, ok := m.areaRepository(); ok {
		records = m.recordsInRepository(repo, records)
	}
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	var kept []git.CommitRecord
	for _, r := range stats.UniqueRecords(records) {
		if visible[stats.IdentityID(r)] {
			kept = append(kept, r)
		}
	}
	return kept
}
func (m Model) awarenessSummary(scopeCount int) string {
	records := m.glanceScopeRecords()
	people := make(map[string]bool)
	for _, r := range records {
		people[stats.IdentityID(r)] = true
	}
	scope := "repositories"
	if m.areaRepoID != "" {
		scope = "areas"
	}
	bots := ""
	if m.hideBots {
		bots = " · bots hidden"
	}
	return StyleDimWhite.Render(fmt.Sprintf("  Range: %s%s · %s commits · %d people · %d %s", TimePresets[m.timeIdx].Label, bots, FormatNumber(len(records)), len(people), scopeCount, scope))
}
func (m Model) glanceCoverageLine() string {
	shallow := false
	for _, r := range m.allRecords {
		if r.LinesUnknown && !m.excludedRepos[r.RepoID] && (m.areaRepoID == "" || r.RepoID == m.areaRepoID) {
			shallow = true
			break
		}
	}
	unknown := 0
	for _, r := range m.glanceScopeRecords() {
		if r.PathsUnknown {
			unknown++
		}
	}
	sample := "author dates"
	if shallow {
		sample += " · shallow sample"
	}
	if unknown > 0 {
		sample += fmt.Sprintf(" · %d commits with unknown paths", unknown)
	} else {
		sample += " · no unknown paths in range"
	}
	return StyleDimWhite.Render("  " + sample)
}
func (m Model) glanceScanLine(scope string) string {
	id := scope
	if m.areaRepoID != "" {
		id = m.areaRepoID
	}
	fresh := "not recorded"
	if t := m.scannedAt[id]; !t.IsZero() {
		fresh = t.Local().Format("Jan 02 15:04 MST")
	}
	prefix := "Local scan "
	if m.staleRepos[id] {
		prefix = "STALE · last good scan "
	}
	line := "  " + prefix + fresh
	if len(m.failedRepos) > 0 {
		line += fmt.Sprintf(" · %d scan failures (R retry)", len(m.failedRepos))
	}
	if m.staleRepos[id] || len(m.failedRepos) > 0 {
		return StyleAmber.Render(line)
	}
	return StyleDimWhite.Render(line)
}

// Column widths reserve numeric context before allocating space to names.
func glanceColumns(width int, withPR bool) (name, commits, people, last, pr int) {
	commits, people, last = 8, 7, 12
	if width < 70 {
		commits, people, last = 7, 6, 6
	}
	if withPR && width >= 100 {
		pr = 23
	}
	name = width - 4 - 2 - commits - people - last - 3
	if pr > 0 {
		name -= pr + 1
	}
	return max(4, name), commits, people, last, pr
}
func (m Model) glanceColumnHeader(width int) string { return m.glanceColumnHeaderFor(width, true) }
func (m Model) glanceColumnHeaderFor(width int, withPR bool) string {
	name, c, p, last, pr := glanceColumns(width, withPR)
	label := "REPOSITORY"
	if m.areaRepoID != "" {
		label = "AREA"
	}
	lastLabel := "LAST"
	if width >= 70 {
		lastLabel = "LAST (local)"
	}
	line := "  " + padCells(label, name) + " " + rightCells("COMMITS", c) + " " + rightCells("PEOPLE", p) + " " + padCells(lastLabel, last)
	if pr > 0 {
		line += " " + padCells("OPEN PRs · all dates", pr)
	}
	return StyleCyan.Render("  " + line)
}
func (m Model) glanceActivityRow(r repositoryActivity, selected bool, index, width int, withPR bool) string {
	name, c, p, last, pr := glanceColumns(width, withPR)
	recent := "quiet"
	if !r.latest.IsZero() {
		recent = r.latest.Local().Format("Jan 02 15:04")
		if last < 12 {
			recent = r.latest.Local().Format("Jan 02")
			if r.latest.Local().Format("2006-01-02") == time.Now().Local().Format("2006-01-02") {
				recent = r.latest.Local().Format("15:04")
			}
		}
	}
	line := glanceCursor(selected) + " " + padCells(displayText(r.repo.Name), name) + " " + rightCells(FormatNumber(r.commits), c) + " " + rightCells(FormatNumber(len(r.people)), p) + " " + padCells(recent, last)
	if pr > 0 {
		line += " " + padCells(m.prGlanceSignal(r.repo.ID), pr)
	}
	return awarenessRow("  "+line, selected, index, width)
}
func (m Model) renderAwareness() string {
	width, height := max(1, m.width), max(1, m.height)
	if width < 40 || height < 18 {
		return ansi.Truncate("Resize to 40×18 · v stats · q quit", width, "…")
	}
	m.normalizeAreaScope()
	m.normalizeAwarenessFocus()
	if m.areaRepoID == "" && m.glance.detailOpen {
		m.closeGlanceDetail()
	}
	if m.glance.help {
		return m.renderGlanceHelp(width, height)
	}
	if m.glance.detailOpen {
		return m.renderGlanceDetail(width, height)
	}
	all := m.activityRepositories()
	rows := m.glanceRepositories()
	selected := m.selectedRepository(rows)
	bannerWidth := width
	if height < 30 {
		bannerWidth = min(width, bannerMinWidth-1)
	}
	lines := renderBanner(bannerWidth)
	breadcrumb := "  REPOSITORIES"
	if repo, ok := m.areaRepository(); ok {
		breadcrumb += " › " + displayText(repo.Name) + " › WORK AREAS"
	}
	lines = append(lines, StyleSubtitle.Render(breadcrumb), m.awarenessSummary(len(all)))
	selectedID := m.selectedScopeID()
	lines = append(lines, m.glanceScanLine(selectedID), StyleDimWhite.Render("  "+m.prStatus()), StyleDimCyan.Render("  "+hrule(width-chromeInset)), m.glanceColumnHeader(width))
	previewRows := 5
	if height < 30 {
		previewRows = 4
	}
	if m.areaRepoID == "" && height < 30 {
		previewRows++
	}
	budget := max(1, height-len(lines)-previewRows-3)
	start := max(0, min(selected-budget/2, len(rows)-budget))
	end := min(len(rows), start+budget)
	for i := start; i < end; i++ {
		lines = append(lines, m.glanceActivityRow(rows[i], i == selected, i, width, true))
	}
	if len(rows) == 0 {
		empty := "  No included, readable repositories. r include · R retry."
		if m.areaRepoID != "" {
			empty = "  No work-area activity in this range. ←→ range · Esc back."
		}
		if m.glanceQuery() != "" {
			empty = "  No matching names. Esc clears search."
		}
		lines = append(lines, empty)
	}
	position := "0/0"
	if len(rows) > 0 {
		position = fmt.Sprintf("%d–%d/%d", start+1, end, len(rows))
	}
	sortLabel := overviewSortLabels[max(0, min(m.overviewSort, len(overviewSortLabels)-1))]
	qualifier := ""
	if m.areaRepoID != "" {
		qualifier = " · counts overlap"
	}
	lines = append(lines, StyleDimWhite.Render("  "+position+" · "+sortLabel+qualifier))
	if len(rows) > 0 {
		r := rows[selected]
		lines = append(lines, StyleDimCyan.Render("  "+hrule(width-chromeInset)), StyleCyan.Render(fmt.Sprintf("  %s · %s commits · %d people", displayText(r.repo.Name), FormatNumber(r.commits), len(r.people))))
		evidence := m.evidence(m.scopeEvidenceKey(r), "")
		count := 2
		if height < 30 {
			count = 1
		}
		for i := 0; i < count; i++ {
			subject := ""
			if i < len(evidence) {
				subject = evidence[i].Subject
				if subject == "" {
					subject = "(no subject)"
				}
				subject = "  Latest: " + displayText(subject)
			} else if i == 0 {
				subject = "  No local commits in this range"
			}
			lines = append(lines, StyleDimWhite.Render(subject))
		}
		if m.areaRepoID == "" {
			if height >= 30 {
				lines[len(lines)-1] = m.glanceBusiestAreas(r.repo.ID, width)
			} else {
				lines = append(lines, m.glanceBusiestAreas(r.repo.ID, width))
			}
		}
		lines = append(lines, StyleDimWhite.Render("  PRs: "+m.prGlanceSignal(r.repo.ID)+" · p opens all-date scope"))
	}
	footer := "  ↑↓ select · Enter open · / find · s sort · ←→ range · Esc back · ? help"
	if m.glance.searching || m.glanceQuery() != "" {
		footer = "  / " + displayText(m.glanceQuery()) + " · Enter accept · Esc clear"
	}
	lines = append(lines, footer, m.glanceCoverageLine())
	return fitGlanceLines(lines, width, height)
}
func (m Model) renderGlanceHelp(width, height int) string {
	lines := renderBanner(min(width, bannerMinWidth-1))
	lines = append(lines, RenderSectionHeader("GLANCE BOARD · HELP", width),
		"  Enter  repository → areas → detail → full commit",
		"  ↑↓ / j k  select     PgUp/PgDn  page     g/G  first/last",
		"  /  search this list  Enter accept  Esc clear",
		"  s  sort areas/repos: Activity (commits), Recent, Name",
		"  Tab / Shift-Tab or 1–4: Activity, People, Related, Subareas",
		"  People: alphabetical identities; Enter filters Activity",
		"  Related: exact shared people; Enter reveals them",
		"  Subareas: literal child paths; counts may overlap",
		"  Esc  clear filter, then return with selection preserved",
		"  ←→ / h l  time range     b  show/hide bots",
		"  p  selected parent-area PRs     P  all included PRs",
		"  PRs use all open dates, independent of local filters",
		"  r  included repositories     R  refresh local + PR data",
		"  v  contributor statistics     q / Ctrl-C  quit",
		"  Git associations show neither ownership nor live presence",
		"  ? / Esc  close help")
	return fitGlanceLines(lines, width, height)
}

// Repository previews reveal concentration without expanding a second list.
func (m Model) glanceBusiestAreas(repoID string, width int) string {
	m.areaRepoID = repoID
	areas := m.activityAreas()
	sortRepositoryActivities(areas, 0)
	var parts []string
	const prefix = "  Busiest areas: "
	itemWidth := max(10, (width-ansi.StringWidth(prefix)-2)/2)
	for _, area := range areas {
		if area.commits == 0 || len(parts) == 2 {
			break
		}
		count := " " + FormatNumber(area.commits)
		name := ansi.Truncate(displayText(area.repo.Name), max(1, itemWidth-ansi.StringWidth(count)), "…")
		parts = append(parts, name+count)
	}
	if len(parts) == 0 {
		parts = append(parts, "no local commits in range")
	}
	return StyleDimWhite.Render(prefix + strings.Join(parts, ", "))
}
