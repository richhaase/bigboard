package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/stats"
)

// Night Ops uses terminal cells, not pixel positioning. Each panel owns its
// complete width, including padding, so ANSI styling cannot shift its neighbors.
var (
	nightBase   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F0EBFA")).Background(lipgloss.Color("#100D1C"))
	nightMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ACA0C3"))
	nightLime   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D7FF3F"))
	nightViolet = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB70F4"))
	nightBand   = nightLime.Background(lipgloss.Color("#312142"))
)

func nightLine(s string, width int) string { return padCells(s, width) }
func nightHeading(s string, width int) string {
	return nightBand.Render(nightLine(" [ "+s+" ]", width))
}
func nightPanel(lines []string, width, height int) string {
	out := make([]string, max(0, height))
	for i := range out {
		s := ""
		if i < len(lines) {
			s = lines[i]
		}
		out[i] = nightLine(s, width)
	}
	return strings.Join(out, "\n")
}
func nightPair(left, right []string, width, height int) []string {
	lw := (width - 3) / 2
	joined := lipgloss.JoinHorizontal(lipgloss.Top, nightPanel(left, lw, height), nightPanel([]string{}, 3, height), nightPanel(right, width-3-lw, height))
	return strings.Split(joined, "\n")
}

// The overview keeps its selection in the left inventory. Enter moves focus
// into the existing detail lenses; no second, invisible cursor is introduced.
func (m Model) renderNightOps(width, height int) string {
	inner := width - 4
	side := min(32, max(25, inner/4))
	bodyWidth := inner - side - 3
	bodyHeight := height - 10
	rows := m.glanceRepositories()
	selected := m.selectedRepository(rows)
	scope := "REPOSITORIES"
	if m.areaRepoID != "" {
		scope = "AREAS"
	}
	if m.glance.detailOpen {
		// Detail search belongs to its lens, never to the navigation inventory.
		n := m
		n.glance.detailOpen = false
		rows = n.glanceRepositories()
		selected = -1
		for i, r := range rows {
			if r.repo.ID == m.glance.overviewAreaID {
				selected = i
				break
			}
		}
	}
	sidebar := []string{nightHeading(scope+" / COMMITS", side)}
	snapshotExtra := 1
	if len(m.failedRepos) > 0 {
		snapshotExtra = 2
	}
	listBudget := max(1, bodyHeight-8-snapshotExtra)
	start := max(0, min(selected-listBudget/2, len(rows)-listBudget))
	end := min(len(rows), start+listBudget)
	for i := start; i < end; i++ {
		r := rows[i]
		count := FormatNumber(r.commits)
		cursor := "  "
		if i == selected && !m.glance.detailOpen {
			cursor = "› "
		}
		line := cursor + padCells(displayText(r.repo.Name), side-ansi.StringWidth(count)-3) + " " + count
		if i == selected {
			line = nightBand.Bold(!m.glance.detailOpen).Render(line)
		}
		sidebar = append(sidebar, line)
	}
	if len(rows) == 0 {
		sidebar = append(sidebar, " No matching names")
	}
	for len(sidebar) < listBudget+1 {
		sidebar = append(sidebar, "")
	}
	position := fmt.Sprintf(" %d of %d", end-start, len(rows))
	if m.areaRepoID != "" {
		position += " · counts overlap"
	}
	sortNames := []string{"commits ↓", "recent ↓", "name ↑"}
	sidebar = append(sidebar, nightMuted.Render(position), nightLime.Render(" / find · s "+sortNames[max(0, min(m.overviewSort, len(sortNames)-1))]), nightViolet.Render(strings.Repeat("─", side)), nightLime.Render(" SNAPSHOT"))
	scanID := m.selectedScopeID()
	if m.areaRepoID != "" {
		scanID = m.areaRepoID
	}
	scanLabel := "Local scan"
	if m.staleRepos[scanID] {
		scanLabel = "STALE · last good scan"
	}
	scanned := "not recorded"
	if at := m.scannedAt[scanID]; !at.IsZero() {
		scanned = at.Local().Format("Jan 02 15:04 MST")
	}
	sidebar = append(sidebar, nightMuted.Render(scanLabel), nightMuted.Render(scanned))
	sidebar = append(sidebar, nightMuted.Render(" No remote Git fetch"), nightMuted.Render(" "+m.prStatus()))
	if len(m.failedRepos) > 0 {
		sidebar = append(sidebar, nightLime.Render(fmt.Sprintf(" %d scan failures · R retry", len(m.failedRepos))))
	}
	var body []string
	if len(rows) == 0 && !m.glance.detailOpen {
		body = []string{nightHeading("SELECTED "+strings.TrimSuffix(scope, "S"), bodyWidth), "", "No matching activity in this range.", "←→ range · Esc clear/back · r repositories"}
	} else {
		preview := m
		var name string
		var commits int
		var peopleCount int
		if m.glance.detailOpen {
			name = m.glanceDetailArea().Name
			commits = len(stats.UniqueRecords(m.glanceAreaRecords()))
			peopleCount = len(m.glanceAreaPeople())
		} else {
			r := rows[selected]
			name = r.repo.Name
			commits = r.commits
			peopleCount = len(r.people)
			if m.areaRepoID != "" {
				preview.openGlanceArea(r)
			}
		}
		label := "SELECTED AREA"
		if m.areaRepoID == "" {
			label = "SELECTED REPOSITORY"
		}
		body = append(body, nightMuted.Render(label), nightLime.Bold(true).Render(displayText(name)), nightMuted.Render(fmt.Sprintf("%s commits · %d people · Range: %s", FormatNumber(commits), peopleCount, TimePresets[m.timeIdx].Label)))
		tabs := []string{}
		for i, t := range glanceTabs {
			text := fmt.Sprintf(" %d %s ", i+1, t)
			if i == preview.glance.frame.tab {
				text = nightBand.Bold(true).Render(text)
			} else {
				text = nightMuted.Render(text)
			}
			tabs = append(tabs, text)
		}
		if m.areaRepoID != "" {
			body = append(body, strings.Join(tabs, " "))
		} else {
			body = append(body, nightMuted.Render("Enter opens work areas"))
		}
		if m.glance.detailOpen && m.glance.frame.relatedFromID != "" && m.glance.frame.tab == glanceActivity {
			body = append(body, nightMuted.Render("Shared contributors · associations, not collaboration"))
		}
		body = append(body, nightViolet.Render(strings.Repeat("─", bodyWidth)))
		if m.glance.detailOpen && m.glance.frame.tab != glanceActivity {
			lens := m.glanceLensLines(bodyWidth, bodyHeight-len(body)+2)
			body = append(body, lens[:max(0, len(lens)-2)]...)
		} else {
			body = append(body, preview.nightActivity(bodyWidth, bodyHeight-len(body), m.glance.detailOpen, rows, selected)...)
		}
	}
	joined := lipgloss.JoinHorizontal(lipgloss.Top, nightPanel(sidebar, side, bodyHeight), nightViolet.Render(nightPanel(strings.Split(strings.Repeat(" │ \n", bodyHeight), "\n"), 3, bodyHeight)), nightPanel(body, bodyWidth, bodyHeight))
	repoName := "repository awareness"
	if r, ok := m.areaRepository(); ok {
		repoName = displayText(r.Name)
	}
	bots := "bots shown"
	if m.hideBots {
		bots = "bots hidden"
	}
	header := []string{
		nightLime.Render("╱" + strings.Repeat("─", width-2) + "╲"),
		"  " + nightLime.Render("[ BB ]  READ-ONLY AWARENESS") + nightMuted.Render("  /  NIGHT OPS"),
		"  " + nightLime.Bold(true).Render("╭─╮╷╭─╮ ╭─╮╭─╮╭─╮╭─╮╭╮ "),
		"  " + nightViolet.Render("├─┤││ ┐ ├─┤│ │├─┤├┬╯││ ") + nightMuted.Render(" / "+repoName),
		"  " + nightLime.Render("╰─╯╵╰─╯ ╰─╯╰─╯╵ ╵╵╰╴╰╯ ") + nightMuted.Render(" ["+TimePresets[m.timeIdx].Label+"] · "+bots+" · ←→ range"),
		"  " + nightViolet.Render(strings.Repeat("─", inner)),
	}
	for _, line := range strings.Split(joined, "\n") {
		header = append(header, "  "+line)
	}
	footer := " ↑↓ select  Enter inspect  Tab / 1–4 lens  p PRs  Esc back  ? help"
	if !m.glance.detailOpen && m.areaRepoID == "" {
		footer = " ↑↓ select  Enter areas  / find  s sort  p PRs  v stats  ? help"
	}
	if m.glance.searching || m.glanceQuery() != "" {
		footer = " / " + displayText(m.glanceQuery()) + " · Enter accept · Esc clear"
	}
	header = append(header, "  "+nightBand.Render(nightLine(footer, inner)), "  "+nightMuted.Render(strings.TrimSpace(m.glanceCoverageLine())), nightLime.Render("╲"+strings.Repeat("─", width-2)+"╱"))
	return nightPanel(header, width, height)
}

func (m Model) nightActivity(width, height int, focused bool, rows []repositoryActivity, selected int) []string {
	var people = m.glanceAreaPeople()
	if m.areaRepoID == "" {
		people = rows[selected].people
	}
	people = append([]stats.AuthorStats(nil), people...)
	sortPeople(people)
	sort.SliceStable(people, func(i, j int) bool { return people[i].Commits > people[j].Commits })
	half := (width - 3) / 2
	topHeight := min(8, max(4, height/2))
	left := []string{nightHeading("WHO WORKED HERE", half)}
	if len(people) == 0 {
		left = append(left, nightMuted.Render("No contributors in this range"))
	}
	limit := min(len(people), topHeight-2)
	for i := 0; i < limit; i++ {
		p := people[i]
		name := glancePersonLabel(p, people)
		if p.Bot {
			name = "[BOT] " + name
		}
		suffix := FormatNumber(p.Commits)
		barWidth := min(10, max(2, half/5))
		bars := max(1, p.Commits*barWidth/max(1, people[0].Commits))
		left = append(left, padCells(name, half-barWidth-len(suffix)-3)+" "+nightLime.Render(padCells(strings.Repeat("━", bars), barWidth))+" "+suffix)
	}
	if limit < len(people) {
		left = append(left, nightMuted.Render(fmt.Sprintf("+%d more · 2 People", len(people)-limit)))
	}
	right := []string{nightHeading("RELATED AREAS", width-3-half), nightMuted.Render("Shared Git contributors")}
	if m.areaRepoID != "" {
		related := m.glanceRelatedAreas()
		for i := 0; i < min(len(related), topHeight-2); i++ {
			r := related[i]
			count := fmt.Sprint(r.shared)
			right = append(right, padCells(displayText(r.area.Name), width-3-half-len(count)-1)+" "+nightLime.Render(count))
		}
		if len(related) == 0 {
			right = append(right, nightMuted.Render("No shared contributors"))
		}
	} else {
		right = []string{nightHeading("BUSIEST AREAS", width-3-half), strings.TrimSpace(m.glanceBusiestAreas(rows[selected].repo.ID, width-3-half))}
	}
	out := nightPair(left, right, width, topHeight)
	out = append(out, nightViolet.Render(strings.Repeat("─", width)))
	lower := height - len(out)
	scope := m.glance.frame.areaID
	records := m.glanceEvidence()
	if m.areaRepoID == "" {
		scope = rows[selected].repo.ID
		records = m.evidence(m.scopeEvidenceKey(rows[selected]), "")
	}
	left = []string{nightHeading("RECENT COMMITS", half)}
	if focused && m.personID != "" {
		left = append(left, nightLime.Render("Activity: "+displayText(strings.TrimPrefix(m.personID, "email:"))+" · Esc clears"))
	}
	selectedCommit := 0
	if focused {
		selectedCommit = m.glanceSelected(m.glanceDetailIDs())
	}
	slots := max(1, (lower-len(left))/3)
	start := max(0, selectedCommit-slots/2)
	start = min(start, max(0, len(records)-slots))
	for i := start; i < min(len(records), start+slots); i++ {
		r := records[i]
		subject := displayText(r.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		cursor := "  "
		if focused && i == selectedCommit {
			cursor = "› "
		}
		line := nightLine(cursor+subject, half)
		if focused && i == selectedCommit {
			line = nightBand.Render(line)
		}
		left = append(left, line, nightMuted.Render("  "+r.Date.Local().Format("Jan 02 15:04")+" · "+displayText(r.Author)), nightMuted.Render("  "+displayText(r.CommitID[:min(7, len(r.CommitID))])))
	}
	if len(records) == 0 {
		left = append(left, "No matching commits in this area and range.")
	}
	prHeading := "OPEN PRs / p"
	if m.glance.frame.path != "" || m.glance.frame.leafID != "" {
		prHeading = "PARENT AREA PRs / p"
	}
	right = []string{nightHeading(prHeading, width-3-half), nightMuted.Render(m.prGlanceSignal(scope))}
	statusLines := wrapGlanceText(m.prStatus(), max(1, width-3-half))
	for _, line := range statusLines[:min(2, len(statusLines), max(0, lower-4))] {
		right = append(right, nightMuted.Render(strings.TrimSpace(line)))
	}
	for _, pr := range m.scopePRs(scope) {
		cardLines := 3
		if lower < 9 {
			cardLines = 1
		}
		if len(right)+cardLines > lower-1 {
			break
		}
		title := fmt.Sprintf("#%d %s", pr.Number, displayText(pr.Title))
		if pr.Draft {
			title = "DRAFT " + title
		}
		right = append(right, title)
		if cardLines > 1 {
			right = append(right, nightMuted.Render(githubIdentity(pr.Author)+" · checks "+displayText(pr.CheckState)), nightMuted.Render(displayText(pr.ReviewDecision)))
		}
	}
	if lower > 2 {
		for len(right) < lower-1 {
			right = append(right, "")
		}
		right = append(right, nightMuted.Render("All dates · unfiltered"))
	}
	return append(out, nightPair(left, right, width, max(0, lower))...)
}

func nightCompactBanner(width int) []string {
	return []string{nightBand.Render(nightLine("  [ BB ]  BIG BOARD / NIGHT OPS", max(1, width)))}
}

// Restore the canvas after nested Lipgloss spans reset their SGR attributes.
// Derive escapes from the active renderer so NO_COLOR / ASCII stays respected.
func nightCanvas(content string, width, height int) string {
	content = nightPanel(strings.Split(content, "\n"), width, height)
	prefix, suffix, _ := strings.Cut(nightBase.Render("X"), "X")
	if prefix == "" {
		return content
	}
	content = strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+prefix)
	return prefix + content + suffix
}
