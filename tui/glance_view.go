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
	fresh := "not yet"
	if t := m.scannedAt[id]; !t.IsZero() {
		fresh = t.Local().Format("Jan 02 15:04 MST")
	}
	prefix := "Last updated: "
	if m.staleRepos[id] {
		prefix = "STALE · last updated: "
	}
	line := "  " + prefix + fresh
	if m.refreshing {
		line += " · Updating…"
	}
	if len(m.failedRepos) > 0 {
		line += fmt.Sprintf(" · Update errors: %d (e details)", len(m.failedRepos))
	}
	if m.staleRepos[id] || len(m.failedRepos) > 0 {
		return StyleAmber.Render(line)
	}
	return StyleDimWhite.Render(line)
}

// Column widths reserve numeric context before allocating space to names.
func glanceColumns(width int) (name, commits, people, last int) {
	commits, people, last = 8, 7, 12
	if width < 70 {
		commits, people, last = 7, 6, 6
	}
	name = width - 4 - 2 - commits - people - last - 3
	return max(4, name), commits, people, last
}
func (m Model) glanceColumnHeaderFor(width int) string {
	name, c, p, last := glanceColumns(width)
	label := "REPOSITORY"
	if m.areaRepoID != "" {
		label = "AREA"
	}
	lastLabel := "LAST"
	if width >= 70 {
		lastLabel = "LAST (local)"
	}
	line := "  " + padCells(label, name) + " " + rightCells("COMMITS", c) + " " + rightCells("PEOPLE", p) + " " + padCells(lastLabel, last)
	return StyleCyan.Render("  " + line)
}
func (m Model) glanceActivityRow(r repositoryActivity, selected bool, index, width int) string {
	name, c, p, last := glanceColumns(width)
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
	return awarenessRow("  "+line, selected, index, width)
}
func (m Model) renderAwareness() string {
	width, height := max(1, m.width), max(1, m.height)
	if width < 40 || height < 18 {
		return ansi.Truncate("Resize to 40×18 · l stats · q quit", width, "…")
	}
	m.normalizeAreaScope()
	if !m.glance.detailOpen {
		m.normalizeAwarenessFocus()
	}
	if m.areaRepoID == "" && m.glance.detailOpen {
		m.closeGlanceDetail()
	}
	if m.glance.help {
		return m.renderGlanceHelp(width, height)
	}
	return m.renderWorklist(width, height)
}

func (m Model) renderGlanceHelp(width, height int) string {
	lines := worklistCompactBanner(width)
	lines = append(lines, RenderSectionHeader("WORKLIST · HELP", width),
		"  → / Enter  repository → areas → detail → commit",
		"  Local data updates on launch or R; no auto-refresh",
		"  ↑↓  select     PgUp/PgDn  page     Home/End  first/last",
		"  /  search this list  Enter accept  Esc clear",
		"  s  sort areas/repos: Activity (commits), Recent, Name",
		"  Tab / Shift-Tab or 1–4: Activity, People, Related, Subareas",
		"  People: alphabetical identities; Enter filters Activity",
		"  Related: Enter shared commits; o contributor overlap",
		"  Subareas: literal child paths; counts may overlap",
		"  ← / Esc  clear filter, then return; ← stays at root",
		"  t  range: presets or custom days     b  bots",
		"  p  selected parent-area PRs     P  all included PRs",
		"  PRs use all open dates, independent of local filters",
		"  r  included repositories     R  refresh local + PR data",
		"  Local refresh reads cached Git history; never runs git fetch",
		"  l  contributor statistics     q / Ctrl-C  quit",
		"  Git associations show neither ownership nor live presence",
		"  ? / Esc  close help")
	return fitGlanceLines(lines, width, height)
}
