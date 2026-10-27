package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/stats"
)

type activityContributorPane struct {
	focused bool
	scope   string
	offset  int
}

func activityPaneRail(focused bool) string {
	if focused {
		return StyleCyan.Render("┃ ")
	}
	return worklistRule.Render("│ ")
}

func activityPaneHeading(text string, width int, focused bool) string {
	text = padCells(ansi.Strip(text), width)
	if focused {
		return StyleTitle.Background(ColorRowSelect).Render(text)
	}
	return StyleDimWhite.Render(text)
}

func (m Model) activityContributorKey(row activityRow) string {
	if m.directory.active {
		return row.repositoryID + "\x00" + m.directory.frame.path + "\x00" + row.scopeID
	}
	return row.scopeID
}

func (m *Model) activityContributorSelection() (activityRow, bool) {
	if m.glance.detailOpen || m.showPaths || m.areaRepoID != "" && !m.directory.active {
		return activityRow{}, false
	}
	return m.prepareActivityProjection().contributorSelection(m)
}

func (m *Model) normalizeActivityContributors() {
	if m.viewMode != ViewAwareness || m.showPRs {
		return
	}
	if m.glance.detailOpen || m.showPaths || m.areaRepoID != "" && !m.directory.active {
		m.contributorPane = activityContributorPane{}
		return
	}
	m.normalizeProjectedContributors(m.prepareActivityProjection())
}

func (m *Model) normalizeProjectedContributors(projection activityProjection) {
	if m.viewMode != ViewAwareness || m.showPRs {
		return
	}
	row, ok := projection.contributorSelection(m)
	if !ok {
		m.contributorPane = activityContributorPane{}
		return
	}
	key := m.activityContributorKey(row)
	if key != m.contributorPane.scope {
		m.contributorPane = activityContributorPane{scope: key}
	}
	_, page := m.activityContributorFrame(row, m.width, activityContributorsHeight(m.width, m.height))
	if page == 0 || len(row.people) == 0 {
		m.contributorPane.focused, m.contributorPane.offset = false, 0
		return
	}
	m.contributorPane.offset = max(0, min(m.contributorPane.offset, len(row.people)-page))
}

func (m *Model) handleActivityContributorKey(key string, projection activityProjection) bool {
	m.normalizeProjectedContributors(projection)
	row, ok := projection.contributorSelection(m)
	if !ok {
		return false
	}
	_, page := m.activityContributorFrame(row, m.width, activityContributorsHeight(m.width, m.height))
	if key == "tab" || key == "shift+tab" {
		if page > 0 && len(row.people) > 0 {
			m.contributorPane.focused = !m.contributorPane.focused
		}
		return true
	}
	if !m.contributorPane.focused {
		return false
	}
	maxOffset := max(0, len(row.people)-page)
	switch key {
	case "esc", "left":
		m.contributorPane.focused = false
	case "up":
		m.contributorPane.offset = max(0, m.contributorPane.offset-1)
	case "down":
		m.contributorPane.offset = min(maxOffset, m.contributorPane.offset+1)
	case "pgup":
		m.contributorPane.offset = max(0, m.contributorPane.offset-page)
	case "pgdown":
		m.contributorPane.offset = min(maxOffset, m.contributorPane.offset+page)
	case "home":
		m.contributorPane.offset = 0
	case "end":
		m.contributorPane.offset = maxOffset
	case "enter", "right":
	default:
		return false
	}
	return true
}

func (m Model) activityContributorScope(row activityRow) string {
	if !m.directory.active {
		return displayText(row.label)
	}
	repo, _ := m.areaRepository()
	name := row.label
	if path, ok := strings.CutPrefix(row.scopeID, "dir:"); ok {
		name = path + "/"
	} else if path, ok := strings.CutPrefix(row.scopeID, "file:"); ok {
		name = path
	}
	return displayText(repo.Name + " / " + name)
}

func (m Model) activityContributorFrame(row activityRow, width, height int) ([]string, int) {
	if height < 4 || width < 12 {
		return nil, 0
	}
	inner := width - 4
	wide := width >= 70
	rowHeight := 2
	if wide {
		rowHeight = 1
	}
	nameWidth := min(40, max(18, inner/3))
	columns := strings.Join([]string{padCells("CONTRIBUTOR", nameWidth), rightCells("COMMITS", 7), rightCells("ADDED", 10), rightCells("REMOVED", 10), "LINES +/-"}, " ")
	heading := "CONTRIBUTORS · " + m.activityContributorScope(row)
	focused := m.contributorPane.focused && m.contributorPane.scope == m.activityContributorKey(row)
	lines := []string{worklistRule.Render(strings.Repeat("─", inner))}
	if height >= 7 && ansi.StringWidth(heading) > inner {
		first := cutToWidth(heading, inner)
		lines = append(lines, activityPaneHeading(first, inner, focused), activityPaneHeading(ansi.Truncate(heading[len(first):], inner, "…"), inner, focused))
	} else {
		lines = append(lines, activityPaneHeading(ansi.Truncate(heading, inner, "…"), inner, focused))
	}
	if wide && height >= 5 && len(row.people) > 0 {
		lines = append(lines, StyleDimWhite.Render(columns))
	}
	page := max(0, (height-len(lines)-1)/rowHeight)
	if page == 0 {
		lines = lines[1:]
		page = max(0, (height-len(lines)-1)/rowHeight)
	}
	return lines, page
}

func (m Model) activityContributors(row activityRow, width, height int) []string {
	lines, page := m.activityContributorFrame(row, width, height)
	if len(lines) == 0 {
		return nil
	}
	inner := width - 4
	people := slices.Clone(row.people)
	stats.Sort(people, stats.SortByTotal)
	wide := width >= 70
	nameWidth := min(40, max(18, inner/3))
	barWidth := max(1, inner-nameWidth-31)
	if len(people) == 0 {
		lines = append(lines, StyleDimWhite.Render("No contributors in this scope and range."))
	} else {
		start := 0
		if m.contributorPane.scope == m.activityContributorKey(row) {
			start = max(0, min(m.contributorPane.offset, len(people)-page))
		}
		end := min(len(people), start+page)
		maxLines := 0
		for _, person := range people {
			maxLines = max(maxLines, person.Added+person.Removed)
		}
		for _, person := range people[start:end] {
			name := glancePersonLabel(person, people)
			if person.Bot {
				name = "[BOT] " + name
			}
			commits := repositoryLineLabel("", person.Commits, 0, 7)
			countWidth := 10
			if !wide {
				countWidth = min(10, max(3, (inner-10)/3))
			}
			added := StyleBarCyan.Render(repositoryLineLabel("+", person.Added, person.UnknownLineCommits, countWidth))
			removed := StyleBarMagenta.Render(repositoryLineLabel("−", person.Removed, person.UnknownLineCommits, countWidth))
			if wide {
				lines = append(lines, strings.Join([]string{padCells(name, nameWidth), rightCells(commits, 7), rightCells(added, 10), rightCells(removed, 10), RenderImpactBar(person.Added, person.Removed, maxLines, barWidth)}, " "))
			} else {
				commits += " commits"
				lines = append(lines, padCells(name, max(1, inner-ansi.StringWidth(commits)-1))+" "+commits)
				counts := added + " " + removed
				lines = append(lines, counts+"  "+RenderImpactBar(person.Added, person.Removed, maxLines, max(1, inner-ansi.StringWidth(counts)-2)))
			}
		}
		position := fmt.Sprintf("%d–%d of %d", start+1, end, len(people))
		if m.contributorPane.focused && m.contributorPane.scope == m.activityContributorKey(row) {
			position += " · ↑↓ scroll · Tab ranking"
		} else {
			position += " · Tab contributors"
		}
		lines = append(lines, StyleDimWhite.Render(position))
	}
	focused := m.contributorPane.focused && m.contributorPane.scope == m.activityContributorKey(row)
	for i, line := range lines {
		lines[i] = activityPaneRail(focused) + ansi.Truncate(line, inner, "…")
	}
	return lines[:min(height, len(lines))]
}
