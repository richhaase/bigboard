package tui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type relatedArea struct {
	area   stats.WorkArea
	shared int
}

func (m Model) glanceAreaForFrame(f glanceFrame) stats.WorkArea {
	root := stats.WorkArea{ID: f.areaID, Name: f.name}
	for _, area := range m.currentWorkAreas() {
		if area.ID == f.areaID {
			root = area
			break
		}
	}
	if f.path == "" && f.leafID == "" {
		return root
	}
	prefix := f.path
	wanted := "path:" + f.path
	if f.leafID != "" {
		wanted = f.leafID
		if prefix == "" {
			prefix = strings.TrimPrefix(root.ID, "auto:")
		}
	} else {
		prefix = path.Dir(f.path)
		if prefix == "." {
			prefix = ""
		}
	}
	for _, child := range stats.Subareas(root, prefix) {
		if child.ID == wanted {
			return child
		}
	}
	return stats.WorkArea{ID: wanted, Name: f.name}
}
func (m Model) glanceDetailArea() stats.WorkArea { return m.glanceAreaForFrame(m.glance.frame) }

func (m Model) glanceAreaRecords() []git.CommitRecord {
	records := m.glanceDetailArea().Records
	if source := m.glance.frame.relatedFromID; source != "" {
		from := glanceFrame{areaID: source, path: m.glance.frame.relatedFromPath, leafID: m.glance.frame.relatedFromLeafID}
		shared := make(map[string]bool)
		for _, r := range m.glanceAreaForFrame(from).Records {
			shared[stats.IdentityID(r)] = true
		}
		var kept []git.CommitRecord
		for _, r := range records {
			if shared[stats.IdentityID(r)] {
				kept = append(kept, r)
			}
		}
		records = kept
	}
	return records
}
func (m Model) glanceAreaPeople() []stats.AuthorStats {
	people := stats.AggregateWithOptions(m.glanceAreaRecords(), stats.AggregateOptions{BotIdentities: m.options.BotIdentities})
	sortPeople(people)
	return people
}
func (m Model) glancePeopleList() []stats.AuthorStats {
	people := m.glanceAreaPeople()
	q := strings.ToLower(m.glance.frame.queries[glancePeople])
	if q == "" {
		return people
	}
	var out []stats.AuthorStats
	for _, p := range people {
		if strings.Contains(strings.ToLower(p.Name+" "+p.ID), q) {
			out = append(out, p)
		}
	}
	return out
}
func (m Model) glanceEvidence() []git.CommitRecord {
	var result []git.CommitRecord
	q := strings.ToLower(m.glance.frame.queries[glanceActivity])
	for _, r := range stats.UniqueRecords(m.glanceAreaRecords()) {
		if m.personID != "" && stats.IdentityID(r) != m.personID {
			continue
		}
		if q != "" {
			text := r.Subject + " " + r.Author + " " + r.Email + " " + r.CommitID
			for _, p := range r.Changes {
				text += " " + p.Path + " " + p.PreviousPath
			}
			if !strings.Contains(strings.ToLower(text), q) {
				continue
			}
		}
		result = append(result, r)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].Date.Equal(result[j].Date) {
			return result[i].Date.After(result[j].Date)
		}
		return commitSelectionID(result[i]) < commitSelectionID(result[j])
	})
	return result
}
func (m Model) glanceRelatedAreas() []relatedArea {
	source := make(map[string]bool)
	for _, r := range m.glanceDetailArea().Records {
		source[stats.IdentityID(r)] = true
	}
	var result []relatedArea
	q := strings.ToLower(m.glance.frame.queries[glanceRelated])
	for _, area := range m.currentWorkAreas() {
		if area.ID == m.glance.frame.areaID || (q != "" && !strings.Contains(strings.ToLower(area.Name), q)) {
			continue
		}
		shared := make(map[string]bool)
		for _, r := range area.Records {
			id := stats.IdentityID(r)
			if source[id] {
				shared[id] = true
			}
		}
		if len(shared) > 0 {
			result = append(result, relatedArea{area: area, shared: len(shared)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].shared != result[j].shared {
			return result[i].shared > result[j].shared
		}
		if result[i].area.Name != result[j].area.Name {
			return result[i].area.Name < result[j].area.Name
		}
		return result[i].area.ID < result[j].area.ID
	})
	return result
}
func (m Model) glanceSubareas() []stats.WorkArea {
	f := m.glance.frame
	if f.leafID != "" || !strings.HasPrefix(f.areaID, "auto:") {
		return nil
	}
	root := m.glanceAreaForFrame(glanceFrame{areaID: f.areaID, name: f.name})
	prefix := f.path
	if prefix == "" {
		prefix = strings.TrimPrefix(f.areaID, "auto:")
	}
	children := stats.Subareas(root, prefix)
	var activities []repositoryActivity
	byID := make(map[string]stats.WorkArea)
	q := strings.ToLower(f.queries[glanceSubareas])
	for _, child := range children {
		if q != "" && !strings.Contains(strings.ToLower(child.Name), q) {
			continue
		}
		byID[child.ID] = child
		activity := repositoryActivity{repo: git.Repository{ID: child.ID, Name: child.Name}, commits: len(stats.UniqueRecords(child.Records))}
		for _, r := range child.Records {
			if r.Date.After(activity.latest) {
				activity.latest = r.Date
			}
		}
		activities = append(activities, activity)
	}
	sortRepositoryActivities(activities, m.overviewSort)
	var result []stats.WorkArea
	for _, a := range activities {
		result = append(result, byID[a.repo.ID])
	}
	return result
}
func (m Model) glanceDetailIDs() []string {
	var ids []string
	switch m.glance.frame.tab {
	case glanceActivity:
		for _, r := range m.glanceEvidence() {
			ids = append(ids, commitSelectionID(r))
		}
	case glancePeople:
		if m.glance.frame.queries[glancePeople] == "" {
			ids = append(ids, "")
		}
		for _, p := range m.glancePeopleList() {
			ids = append(ids, p.ID)
		}
	case glanceRelated:
		for _, a := range m.glanceRelatedAreas() {
			ids = append(ids, a.area.ID)
		}
	case glanceSubareas:
		for _, a := range m.glanceSubareas() {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// glanceLensLines shares the identity-preserving, scrollable lens between the
// compact view and the wide workspace without reparsing rendered headings.
func (m Model) glanceLensLines(width, height int) []string {
	var lines []string
	areaPeople := m.glanceAreaPeople()
	if m.glance.frame.relatedFromID != "" {
		lines = append(lines, StyleDimWhite.Render("  Shared contributors · associations, not collaboration"))
	}
	if m.glance.frame.tab == glanceSubareas {
		lines = append(lines, m.glanceColumnHeaderFor(width, false))
	}
	ids := m.glanceDetailIDs()
	selected := m.glanceSelected(ids)
	budget := max(1, height-len(lines)-3)
	start := max(0, min(selected-budget/2, len(ids)-budget))
	if m.glance.frame.tab == glanceActivity {
		// At worst each commit adds its own day heading. Keep selection
		// within the viewport after accounting for those rendered lines.
		start = max(0, selected-max(0, (budget-2)/4))
	}
	end := min(len(ids), start+budget)
	switch m.glance.frame.tab {
	case glanceActivity:
		records := m.glanceEvidence()
		// Day headings consume real viewport space; each commit remains one subject-first row.
		lastDay := ""
		used := 0
		end = start
		for i := start; i < len(records) && used < budget; i++ {
			r := records[i]
			day := r.Date.Local().Format("Mon Jan 02, 2006")
			if day != lastDay && used+1 < budget {
				lines = append(lines, StyleDimWhite.Render("  "+day))
				used++
				lastDay = day
			}
			if used >= budget {
				break
			}
			subject := displayText(r.Subject)
			if subject == "" {
				subject = "(no subject)"
			}
			lines = append(lines, awarenessRow("  "+glanceCursor(i == selected)+" "+subject, i == selected, i, width))
			used++
			end = i + 1
		}
		if len(records) == 0 {
			lines = append(lines, "  No matching commits in this area and range.")
		}
	case glancePeople:
		people := m.glancePeopleList()
		labels := make(map[string]string)
		labels[""] = "All contributors"
		for _, p := range people {
			labels[p.ID] = glancePersonLabel(p, areaPeople)
			if p.Bot {
				labels[p.ID] = "[BOT] " + labels[p.ID]
			}
		}
		for i := start; i < end; i++ {
			lines = append(lines, awarenessRow("  "+glanceCursor(i == selected)+" "+labels[ids[i]], i == selected, i, width))
		}
		if len(ids) == 0 {
			lines = append(lines, "  No matching contributors. Esc clears search.")
		}
	case glanceRelated:
		rows := m.glanceRelatedAreas()
		for i := start; i < end; i++ {
			r := rows[i]
			suffix := fmt.Sprintf("%d shared people", r.shared)
			line := glanceCursor(i == selected) + " " + padCells(displayText(r.area.Name), max(5, width-7-ansi.StringWidth(suffix))) + " " + suffix
			lines = append(lines, awarenessRow("  "+line, i == selected, i, width))
		}
		if len(rows) == 0 {
			lines = append(lines, "  No other areas share contributors in this range.")
		}
	case glanceSubareas:
		rows := m.glanceSubareas()
		for i := start; i < end; i++ {
			child := rows[i]
			activity := repositoryActivity{repo: git.Repository{ID: child.ID, Name: child.Name}, commits: len(stats.UniqueRecords(child.Records)), people: stats.AggregateWithOptions(child.Records, stats.AggregateOptions{})}
			for _, r := range child.Records {
				if r.Date.After(activity.latest) {
					activity.latest = r.Date
				}
			}
			lines = append(lines, m.glanceActivityRow(activity, i == selected, i, width, false))
		}
		if len(rows) == 0 {
			empty := "  No deeper path groups in this area and range."
			if strings.HasPrefix(m.glance.frame.areaID, "named:") {
				empty = "  Named area kept as configured; Activity shows its paths."
			}
			lines = append(lines, empty)
		}
	}
	label := strings.ToLower(glanceTabs[m.glance.frame.tab])
	if m.glance.frame.tab == glanceSubareas {
		label += " · " + overviewSortLabels[max(0, min(m.overviewSort, len(overviewSortLabels)-1))] + " · counts overlap"
	}
	if m.glance.frame.tab == glancePeople {
		label = fmt.Sprintf("rows · %d contributors (alphabetical)", len(m.glancePeopleList()))
	}
	position := "0/0"
	if len(ids) > 0 {
		position = fmt.Sprintf("%d–%d/%d", start+1, end, len(ids))
	}
	lines = append(lines, StyleDimWhite.Render("  "+position+" "+label))
	footer := "  ↑↓ select · Enter open · Tab lens · / find · Esc back · ? help"
	if m.glance.searching || m.glanceQuery() != "" {
		footer = "  / " + displayText(m.glanceQuery()) + " · Enter accept · Esc clear"
	}
	lines = append(lines, footer, m.glanceCoverageLine())
	return lines
}

func (m Model) selectedGlanceCommit() (git.CommitRecord, bool) {
	evidence := m.glanceEvidence()
	if len(evidence) == 0 {
		return git.CommitRecord{}, false
	}
	ids := make([]string, len(evidence))
	for i, r := range evidence {
		ids[i] = commitSelectionID(r)
	}
	selectedID := m.glance.frame.ids[glanceActivity]
	index := m.glanceSelected(ids)
	if m.showPaths && selectedID != "" {
		found := false
		for i, id := range ids {
			if id == selectedID {
				index = i
				found = true
				break
			}
		}
		if !found {
			return git.CommitRecord{}, false
		}
	}
	r := evidence[index]
	// The activity lens is area-scoped; the inspector exposes the canonical full
	// commit so paths outside that area and excluded-file evidence stay reachable.
	if repo, ok := m.areaRepository(); ok {
		for _, full := range m.recordsInRepository(repo, m.filteredRecords()) {
			if commitSelectionID(full) == commitSelectionID(r) {
				return full, true
			}
		}
	}
	return r, true
}
func (m Model) commitInspectorLines(width int) []string {
	r, ok := m.selectedGlanceCommit()
	if !ok {
		return []string{"  No matching commit in this range."}
	}
	var lines []string
	appendWrapped := func(s string) { lines = append(lines, wrapGlanceText(displayText(s), max(1, width-4))...) }
	appendWrapped("Subject: " + r.Subject)
	appendWrapped("Author: " + r.Author + " <" + r.Email + ">")
	appendWrapped("Author date: " + r.Date.Local().Format("2006-01-02 15:04:05 -0700 MST"))
	appendWrapped("Commit: " + r.CommitID)
	if repo, ok := m.areaRepository(); ok {
		appendWrapped("Repository: " + repo.Name)
	}
	lines = append(lines, "")
	if r.PathsUnknown {
		appendWrapped("Paths unknown: shallow history boundary")
	}
	if len(r.Changes) == 0 && !r.PathsUnknown {
		appendWrapped("No file changes")
	}
	appendWrapped(fmt.Sprintf("All changed paths (%d)", len(r.Changes)))
	lines = append(lines, wrappedPathLines(r, width)...)
	return lines
}
func (m Model) commitInspectorMaxOffset() int {
	return max(0, len(m.commitInspectorLines(max(1, m.width)))-max(1, m.height-5))
}
func (m Model) renderCommitInspector(width, height int) string {
	lines := nightCompactBanner(width)
	lines = append(lines, RenderSectionHeader("COMMIT · CHANGED PATHS", width), m.glanceScanLine(m.areaRepoID))
	body := m.commitInspectorLines(width)
	budget := max(1, height-len(lines)-2)
	offset := max(0, min(m.pathOffset, max(0, len(body)-budget)))
	lines = append(lines, body[offset:min(len(body), offset+budget)]...)
	lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  Lines %d–%d/%d", offset+1, min(len(body), offset+budget), len(body))), "  ↑↓ scroll · PgUp/PgDn · Enter/Esc back · q quit")
	return fitGlanceLines(lines, width, height)
}
func wrapGlanceText(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var lines []string
	for len(s) > 0 {
		part := cutToWidth(s, width)
		if part == "" {
			break
		}
		lines = append(lines, "  "+part)
		s = s[len(part):]
	}
	return lines
}

// Put canonical identity first for duplicate names so a long display name
// cannot push their distinguishing information out of a narrow People row.
func glancePersonLabel(a stats.AuthorStats, people []stats.AuthorStats) string {
	for _, other := range people {
		if other.Name == a.Name && other.ID != a.ID {
			return displayText(strings.TrimPrefix(a.ID, "email:")) + " · " + displayText(a.Name)
		}
	}
	return displayText(a.Name)
}
