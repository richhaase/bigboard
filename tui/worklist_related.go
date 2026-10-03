package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type cochangedArea struct {
	area    stats.WorkArea
	records []git.CommitRecord
}

// Co-change requires an actual object ID in both areas. Matching authors,
// timestamps or subjects are never substitutes for shared commit evidence.
func (m Model) glanceCochangedAreas() []cochangedArea {
	source := make(map[string]bool)
	for _, r := range m.glanceDetailArea().Records {
		if r.CommitID != "" {
			source[r.CommitID] = true
		}
	}
	var result []cochangedArea
	q := strings.ToLower(m.glance.frame.queries[glanceRelated])
	for _, area := range m.currentWorkAreas() {
		if area.ID == m.glance.frame.areaID || (q != "" && !strings.Contains(strings.ToLower(area.Name), q)) {
			continue
		}
		var records []git.CommitRecord
		for _, r := range stats.UniqueRecords(area.Records) {
			if r.CommitID != "" && source[r.CommitID] {
				records = append(records, r)
			}
		}
		if len(records) == 0 {
			continue
		}
		sort.Slice(records, func(i, j int) bool {
			if !records[i].Date.Equal(records[j].Date) {
				return records[i].Date.After(records[j].Date)
			}
			return records[i].CommitID < records[j].CommitID
		})
		result = append(result, cochangedArea{area, records})
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].records) != len(result[j].records) {
			return len(result[i].records) > len(result[j].records)
		}
		if result[i].area.Name != result[j].area.Name {
			return result[i].area.Name < result[j].area.Name
		}
		return result[i].area.ID < result[j].area.ID
	})
	return result
}

func (m Model) commitAreaNames(r git.CommitRecord) []string {
	var names []string
	if m.areaRepoID == "" {
		m.areaRepoID = m.selectedRepoID
	}
	if r.CommitID == "" {
		return []string{"unknown (no commit ID)"}
	}
	for _, a := range m.currentWorkAreas() {
		for _, candidate := range a.Records {
			if candidate.CommitID == r.CommitID {
				names = append(names, displayText(a.Name))
				break
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return []string{"unknown"}
	}
	return names
}

func (m Model) sharedContributorNames(area stats.WorkArea) string {
	source := make(map[string]bool)
	for _, p := range m.glanceAreaPeople() {
		source[p.ID] = true
	}
	people := stats.AggregateWithOptions(area.Records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
	sortPeople(people)
	var names []string
	for _, p := range people {
		if source[p.ID] {
			name := glancePersonLabel(p, people)
			if p.Bot {
				name = "[BOT] " + name
			}
			names = append(names, name)
		}
	}
	return strings.Join(names, " · ")
}

func (m Model) worklistRelatedLines(width, height int) []string {
	if m.glance.frame.relatedOverlap {
		return m.worklistOverlapLines(width, height)
	}
	rows := m.glanceCochangedAreas()
	selected := m.glanceSelected(m.glanceDetailIDs())
	lines := []string{StyleTitle.Render("WHAT CHANGED TOGETHER / same commit"), StyleDimWhite.Render("o contributor overlap")}
	compact := height < 20 || width < 76
	budget := max(1, min(5, height-19))
	if compact {
		budget = 1
	}
	start := max(0, min(selected-budget/2, len(rows)-budget))
	end := min(len(rows), start+budget)
	areaWidth := max(16, width/4)
	if !compact {
		lines = append(lines, StyleDimWhite.Render("  "+padCells("AREA", areaWidth)+" "+padCells("COMMITS TOUCHING BOTH", 23)+" LATEST SHARED COMMIT"))
	}
	for i := start; i < end; i++ {
		r := rows[i]
		line := glanceCursor(i == selected) + " " + padCells(displayText(r.area.Name), areaWidth) + " " + padCells(fmt.Sprint(len(r.records)), 23) + " " + displayText(r.records[0].Subject)
		if compact {
			line = glanceCursor(i == selected) + " " + displayText(r.area.Name) + fmt.Sprintf(" · %d shared commits", len(r.records))
		}
		lines = append(lines, strings.TrimPrefix(awarenessRow("  "+line, i == selected, i, width+4), "  "))
	}
	if len(rows) == 0 {
		lines = append(lines, "No commits touch both this area and another in this range.")
	} else {
		r := rows[selected]
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("%d–%d/%d · →/Enter shared-commit evidence", start+1, end, len(rows))))
		if !compact {
			lines = append(lines, worklistRule.Render(strings.Repeat("─", width)), StyleTitle.Render(displayText(m.glanceDetailArea().Name)+" + "+displayText(r.area.Name)))
			lines = append(lines, m.worklistCommit(r.records[0], m.glanceAreaPeople(), width, false)...)
			if len(r.records) > 1 {
				lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("+%d more shared commits · →/Enter evidence", len(r.records)-1)))
			}
		}
	}
	if !compact {
		lines = append(lines, worklistRule.Render(strings.Repeat("─", width)), StyleTitle.Render("CONTRIBUTOR OVERLAP / identities appearing in both areas"))
		overlap := m.glanceRelatedAreas()
		limit := max(0, height-len(lines)-2)
		for _, r := range overlap[:min(len(overlap), limit)] {
			lines = append(lines, padCells(displayText(r.area.Name), areaWidth+2)+" "+m.sharedContributorNames(r.area))
		}
		if len(overlap) > limit {
			lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("+%d more areas with contributor overlap · o all names", len(overlap)-limit)))
		}
		if len(overlap) == 0 {
			lines = append(lines, "No contributor overlap in this range.")
		}
	}
	for i := range lines {
		lines[i] = "  " + ansi.Truncate(lines[i], width, "…")
	}
	return lines[:min(len(lines), max(0, height))]
}

// The overlap list is independently scrollable, including on small terminals.
// Enter preserves identity-based provenance without claiming shared commits.
func (m Model) worklistOverlapLines(width, height int) []string {
	rows := m.glanceRelatedAreas()
	selected := m.glanceSelected(m.glanceDetailIDs())
	lines := []string{StyleTitle.Render("CONTRIBUTOR OVERLAP · o co-change"), StyleDimWhite.Render("Contributor identities in both areas.")}
	budget := max(1, (height-4)/2)
	start := max(0, min(selected-budget/2, len(rows)-budget))
	end := min(len(rows), start+budget)
	for i := start; i < end; i++ {
		lines = append(lines, strings.TrimPrefix(awarenessRow("  "+glanceCursor(i == selected)+" "+displayText(rows[i].area.Name), i == selected, i, width+4), "  "), "  "+m.sharedContributorNames(rows[i].area))
	}
	if len(rows) == 0 {
		lines = append(lines, "No contributor overlap in this range.")
	}
	lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("%d–%d/%d · Enter names/evidence", min(start+1, len(rows)), end, len(rows))))
	for i := range lines {
		lines[i] = "  " + ansi.Truncate(lines[i], width, "…")
	}
	return lines[:min(len(lines), max(0, height))]
}
