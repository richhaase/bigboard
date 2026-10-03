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

func worklistPeople(people []stats.AuthorStats, width, limit int) []string {
	if len(people) == 0 {
		return []string{StyleDimWhite.Render("No contributors in this range")}
	}
	people = append([]stats.AuthorStats(nil), people...)
	sortPeople(people)
	sort.SliceStable(people, func(i, j int) bool { return people[i].Commits > people[j].Commits })
	var out []string
	i := 0
	for len(out) < limit && i < len(people) {
		line := ""
		for i < len(people) {
			label := glancePersonLabel(people[i], people)
			if people[i].Bot {
				label = "[BOT] " + label
			}
			label += " " + FormatNumber(people[i].Commits)
			sep := ""
			if line != "" {
				sep = " · "
			}
			suffix := ""
			if len(out) == limit-1 && i+1 < len(people) {
				suffix = fmt.Sprintf(" · +%d more", len(people)-i-1)
			}
			if ansi.StringWidth(line+sep+label+suffix) > width {
				if line != "" {
					break
				}
				label = ansi.Truncate(label, max(1, width-ansi.StringWidth(suffix)), "…")
			}
			line += sep + label
			i++
		}
		if len(out) == limit-1 && i < len(people) {
			line += fmt.Sprintf(" · +%d more", len(people)-i)
		}
		out = append(out, line)
	}
	return out
}

func (m Model) worklistEvidence(r repositoryActivity, width, height int, focused bool) []string {
	records := m.evidence(m.scopeEvidenceKey(r), "")
	people := r.people
	if focused {
		records = m.glanceEvidence()
		people = m.glanceAreaPeople()
	}
	title := "SELECTED  " + displayText(r.repo.Name)
	if repo, ok := m.areaRepository(); ok {
		title = "SELECTED  " + displayText(repo.Name) + " / " + displayText(r.repo.Name)
	}
	counts := fmt.Sprintf("%s commits · %d contributors", FormatNumber(r.commits), len(people))
	lines := []string{StyleTitle.Render(padCells(title, max(1, width-ansi.StringWidth(counts)-2)) + "  " + counts)}
	if focused {
		lines = nil
	}
	for _, line := range worklistPeople(people, width-14, 1) {
		lines = append(lines, "Contributors: "+line)
	}
	lines = append(lines, "", StyleDimWhite.Bold(true).Render("RECENT COMMITS / author time "+time.Now().Local().Format("MST")))
	slots := max(1, min(3, (height-len(lines)-6)/3))
	selected := 0
	if focused {
		selected = m.glanceSelected(m.glanceDetailIDs())
	}
	start := max(0, min(selected-slots/2, len(records)-slots))
	end := min(len(records), start+slots)
	for i := start; i < end; i++ {
		lines = append(lines, m.worklistCommit(records[i], people, width, focused && i == selected)...)
	}
	if len(records) == 0 {
		lines = append(lines, "No matching commits in this area and range.")
	}
	if len(records) > end-start {
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("+%d more commits · →/Enter Activity · 2 People · 3 Related", len(records)-(end-start))))
	}
	// The header owns shared availability. Only retained, scoped PR evidence
	// earns space here; the complete inventory remains available via p.
	prs := m.scopePRs(r.repo.ID)
	if len(prs) > 0 && len(lines)+6 < height {
		prTitle := "OPEN PRs / all dates · local filters do not apply"
		if m.glance.frame.path != "" || m.glance.frame.leafID != "" {
			prTitle = "PARENT AREA PRs / all dates · local filters do not apply"
		}
		lines = append(lines, StyleDimWhite.Bold(true).Render(prTitle), m.worklistSignal(m.prGlanceSignal(r.repo.ID)))
		pr := prs[0]
		lines = append(lines, fmt.Sprintf("#%d %s · p full inventory", pr.Number, displayText(pr.Title)))
	}
	lines = append(lines, "")
	lines = append(lines, collaborationEvidenceLines()...)
	for i := range lines {
		lines[i] = "  " + ansi.Truncate(lines[i], width, "…")
	}
	return lines[:min(len(lines), max(0, height))]
}

func (m Model) worklistCommit(r git.CommitRecord, people []stats.AuthorStats, width int, selected bool) []string {
	author := displayText(r.Author)
	for _, p := range people {
		if p.ID == stats.IdentityID(r) {
			author = glancePersonLabel(p, people)
			break
		}
	}
	subject := displayText(r.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	hash := displayText(r.CommitID[:min(7, len(r.CommitID))])
	cursor := "  "
	if selected {
		cursor = "› "
	}
	stamp := r.Date.Local().Format("Jan 02 15:04")
	lines := []string{cursor + subject, "  " + author + " · " + stamp + " · " + hash, "  Changed areas: " + strings.Join(m.commitAreaNames(r), " · ")}
	for i, line := range lines {
		style := lipgloss.NewStyle()
		if selected && line != "" {
			style = style.Background(ColorRowSelect)
		}
		lines[i] = style.Render(padCells(line, width))
	}
	return lines
}

func (m Model) renderWorklistDetail(width, height int) string {
	lines := m.worklistHeader(width)
	tabs := make([]string, len(glanceTabs))
	for i, label := range glanceTabs {
		tabs[i] = fmt.Sprintf("%d %s", i+1, label)
		if width < 70 {
			tabs[i] = label
		}
		if i == m.glance.frame.tab {
			tabs[i] = "[" + tabs[i] + "]"
		}
	}
	lines = append(lines, StyleCyan.Render("  "+strings.Join(tabs, "  ")))
	area := m.glanceDetailArea()
	people := m.glanceAreaPeople()
	lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  %s commits · %d contributors", FormatNumber(len(stats.UniqueRecords(m.glanceAreaRecords()))), len(people))))
	if m.personID != "" {
		label := displayText(strings.TrimPrefix(m.personID, "email:"))
		for _, p := range people {
			if p.ID == m.personID {
				label = glancePersonLabel(p, people)
			}
		}
		lines = append(lines, StyleCyan.Render("  Activity: "+ansi.Truncate(label, max(1, width-25), "…")+" · Esc clears"))
	}
	if m.glance.frame.relatedFromID != "" {
		label := "Commits touching both areas · not collaboration"
		if m.glance.frame.relatedPeople {
			label = "Shared contributors · may have worked independently"
		}
		lines = append(lines, StyleDimWhite.Render("  "+label))
	}
	switch m.glance.frame.tab {
	case glanceActivity:
		r := repositoryActivity{repo: git.Repository{ID: m.glance.frame.areaID, Name: area.Name}, people: people, commits: len(stats.UniqueRecords(m.glanceAreaRecords()))}
		lines = append(lines, m.worklistEvidence(r, width-4, height-len(lines)-3, true)...)
	case glanceRelated:
		lines = append(lines, m.worklistRelatedLines(width-4, height-len(lines)-3)...)
	default:
		lens := m.glanceLensLines(width, height-len(lines))
		// Shared lens owns its row viewport; Worklist owns the common footer.
		lines = append(lines, lens[:max(0, len(lens)-2)]...)
	}
	return m.worklistFinish(lines, width, height, true)
}
