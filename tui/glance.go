package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

const (
	glanceActivity = iota
	glancePeople
	glanceRelated
	glanceSubareas
)

var glanceTabs = []string{"Activity", "People", "Related", "Subareas"}
var overviewSortLabels = []string{"Activity (commits)", "Recent", "Name"}
var changeSortLabels = []string{"Lines +/-", "Commits", "Recent", "Name"}

func (m Model) overviewSortLabel() string {
	mode := m.overviewSort
	labels := overviewSortLabels
	if (m.areaRepoID == "" || m.directory.active) && !m.glance.detailOpen {
		mode = m.repositorySort
		labels = changeSortLabels
	}
	mode = max(0, min(mode, len(labels)-1))
	direction := "↓"
	if labels[mode] == "Name" {
		direction = "↑"
	}
	if m.overviewReverse {
		if direction == "↑" {
			direction = "↓"
		} else {
			direction = "↑"
		}
	}
	return labels[mode] + " " + direction
}

type glanceFrame struct {
	literal, relatedFromLiteral                       bool
	relatedOverlap, relatedPeople                     bool
	areaID, name, path, leafID                        string
	relatedFromID, relatedFromPath, relatedFromLeafID string
	tab                                               int
	queries                                           [4]string
	rows                                              [4]int
	ids                                               [4]string
	personID                                          string
}

type glanceState struct {
	detailOpen      bool
	frame           glanceFrame
	stack           []glanceFrame
	overviewAreaID  string
	overviewQueries [2]string
	searching       bool
}

func (m Model) glanceQuery() string {
	if m.glance.detailOpen {
		return m.glance.frame.queries[m.glance.frame.tab]
	}
	if m.directory.active {
		return m.directory.frame.query
	}
	if m.areaRepoID != "" {
		return m.glance.overviewQueries[1]
	}
	return m.glance.overviewQueries[0]
}
func (m *Model) setGlanceQuery(q string) {
	if m.glance.detailOpen {
		m.glance.frame.queries[m.glance.frame.tab] = q
		return
	}
	if m.directory.active {
		m.directory.frame.query = q
		return
	}
	i := 0
	if m.areaRepoID != "" {
		i = 1
	}
	m.glance.overviewQueries[i] = q
}
func (m Model) glanceRepositories() []repositoryActivity {
	rows := m.activityRepositories()
	q := strings.ToLower(m.glanceQuery())
	if m.overviewReverse {
		slices.Reverse(rows)
	}
	if q == "" {
		return rows
	}
	var out []repositoryActivity
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.repo.Name), q) {
			out = append(out, r)
		}
	}
	return out
}
func (m *Model) openGlanceArea(r repositoryActivity) {
	m.glance.detailOpen = true
	m.glance.overviewAreaID = r.repo.ID
	m.glance.frame = glanceFrame{areaID: r.repo.ID, name: r.repo.Name}
	m.glance.stack = nil
	m.personID = ""
}
func (m *Model) pushGlanceFrame(next glanceFrame) {
	current := m.glance.frame
	current.personID = m.personID
	// Copy-on-append prevents Bubble Tea model copies from mutating earlier state.
	m.glance.stack = append(append([]glanceFrame(nil), m.glance.stack...), current)
	m.glance.frame = next
	m.selectedAreaID = next.areaID
	m.personID = ""
	m.showPaths = false
	m.pathOffset = 0
}
func (m *Model) closeGlanceDetail() {
	m.glance.detailOpen = false
	m.selectedAreaID = m.glance.overviewAreaID
	m.glance.frame = glanceFrame{}
	m.glance.stack = nil
	m.personID = ""
}

func (m Model) handleAwarenessKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.normalizeAreaScope()
	if !m.glance.detailOpen {
		m.normalizeAwarenessFocus()
	}
	if m.areaRepoID == "" && m.glance.detailOpen {
		m.closeGlanceDetail()
	}
	if m.glance.searching {
		return m.handleGlanceSearch(msg)
	}
	key := msg.String()
	if key == "v" {
		id := m.areaRepoID
		if id == "" {
			rows := m.glanceRepositories()
			if len(rows) == 0 {
				return m, nil
			}
			id = rows[m.selectedRepository(rows)].repo.ID
		}
		m.showPRs, m.prDetail, m.prRepoPicker = true, false, false
		m.prRepoID, m.prQuery, m.prOffset, m.prRow = id, "", 0, 0
		return m, nil
	}
	if key == "q" || key == "ctrl+c" {
		return m.quit()
	}
	if m.showPaths {
		switch key {
		case "esc", "left":
			m.showPaths = false
			m.pathOffset = 0
		case "tab", "shift+tab":
			m.showPaths = false
			m.pathOffset = 0
			step := 1
			if key == "shift+tab" {
				step = 3
			}
			m.glance.frame.tab = (m.glance.frame.tab + step) % 4
			if m.glance.frame.literal && m.glance.frame.tab == glanceSubareas {
				m.closeGlanceDetail()
			}
		case "up":
			m.pathOffset = max(0, m.pathOffset-1)
		case "down":
			m.pathOffset = min(m.commitInspectorMaxOffset(), m.pathOffset+1)
		case "pgup":
			m.pathOffset = max(0, m.pathOffset-max(1, m.height-8))
		case "pgdown":
			m.pathOffset = min(m.commitInspectorMaxOffset(), m.pathOffset+max(1, m.height-8))
		case "home":
			m.pathOffset = 0
		case "end":
			m.pathOffset = m.commitInspectorMaxOffset()
		}
		return m, nil
	}
	switch key {
	case "b":
		m.hideBots = !m.hideBots
		m.recomputeAuthors()
		return m, nil
	case "t":
		if !m.loading && m.err == nil {
			m.openRangePicker()
		}
		return m, nil
	case "/":
		m.glance.searching = true
		return m, nil
	case "esc", "left":
		if m.glanceQuery() != "" {
			m.setGlanceQuery("")
			return m, nil
		}
		if m.glance.detailOpen {
			if m.personID != "" {
				m.personID = ""
				m.glance.frame.tab = glancePeople
				return m, nil
			}
			n := len(m.glance.stack)
			if n > 0 {
				m.glance.frame = m.glance.stack[n-1]
				m.glance.stack = m.glance.stack[:n-1]
				m.selectedAreaID = m.glance.frame.areaID
				m.personID = m.glance.frame.personID
			} else {
				m.closeGlanceDetail()
			}
			return m, nil
		}
		if m.areaRepoID != "" {
			if m.directory.active {
				m.leaveDirectory()
				return m, nil
			}
			m.areaRepoID = ""
			m.personID = ""
			return m, nil
		}
		return m, nil
	}
	if key == "right" {
		key = "enter"
	}
	if m.glance.detailOpen {
		return m.handleGlanceDetailKey(key), nil
	}
	if m.directory.active {
		return m.handleDirectoryKey(key), nil
	}
	rows := m.glanceRepositories()
	selected := m.selectedRepository(rows)
	switch key {
	case "s":
		if m.areaRepoID == "" {
			m.repositorySort = (m.repositorySort + 1) % len(changeSortLabels)
		} else {
			m.overviewSort = (m.overviewSort + 1) % len(overviewSortLabels)
		}
	case "S":
		m.overviewReverse = !m.overviewReverse
	case "tab", "1", "2", "3", "4":
		if m.areaRepoID != "" && len(rows) > 0 {
			m.openGlanceArea(rows[selected])
			if key >= "1" && key <= "4" {
				m.glance.frame.tab = int(key[0] - '1')
			}
		}
	case "enter":
		if len(rows) > 0 {
			if m.areaRepoID == "" {
				m.openDirectoryRepository(rows[selected].repo.ID)
			} else {
				m.openGlanceArea(rows[selected])
			}
		}
	default:
		if next, ok := glanceMove(key, selected, len(rows), m.worklistPageSize()); ok && len(rows) > 0 {
			m.setSelectedScopeID(rows[next].repo.ID)
		}
	}
	return m, nil
}

func glanceMove(key string, current, total, page int) (int, bool) {
	next := current
	switch key {
	case "up":
		next--
	case "down":
		next++
	case "pgup":
		next -= page
	case "pgdown":
		next += page
	case "home":
		next = 0
	case "end":
		next = total - 1
	default:
		return current, false
	}
	return max(0, min(total-1, next)), true
}

func (m Model) handleGlanceSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		m.glance.searching = false
	case "esc":
		m.glance.searching = false
		m.setGlanceQuery("")
	case "backspace":
		q := []rune(m.glanceQuery())
		if len(q) > 0 {
			m.setGlanceQuery(string(q[:len(q)-1]))
		}
	default:
		if len(msg.Runes) > 0 {
			m.setGlanceQuery(m.glanceQuery() + string(msg.Runes))
		}
	}
	return m, nil
}

func (m Model) handleGlanceDetailKey(key string) Model {
	f := &m.glance.frame
	if f.literal && (key == "4" || key == "tab" && f.tab == glanceRelated || key == "shift+tab" && f.tab == glanceActivity) {
		m.closeGlanceDetail()
		return m
	}
	switch key {
	case "tab":
		f.tab = (f.tab + 1) % 4
		return m
	case "shift+tab":
		f.tab = (f.tab + 3) % 4
		return m
	case "1", "2", "3", "4":
		f.tab = int(key[0] - '1')
		return m
	case "m":
		if f.tab == glanceRelated {
			f.relatedOverlap = !f.relatedOverlap
			f.ids[glanceRelated] = ""
			f.rows[glanceRelated] = 0
		}
		return m
	case "s", "S":
		if f.tab == glanceSubareas {
			if key == "s" {
				m.overviewSort = (m.overviewSort + 1) % len(overviewSortLabels)
			} else {
				m.overviewReverse = !m.overviewReverse
			}
		}
		return m
	}
	ids := m.glanceDetailIDs()
	selected := m.glanceSelected(ids)
	page := max(1, m.height-10)
	if f.tab == glanceActivity {
		page = m.worklistActivityPageSize()
	}
	if next, ok := glanceMove(key, selected, len(ids), page); ok && len(ids) > 0 {
		f.rows[f.tab] = next
		f.ids[f.tab] = ids[next]
		return m
	}
	if key != "enter" || len(ids) == 0 {
		return m
	}
	switch f.tab {
	case glanceActivity:
		m.showPaths = true
		m.pathOffset = 0
	case glancePeople:
		m.personID = ids[selected]
		f.tab = glanceActivity
		f.rows[glanceActivity] = 0
		f.ids[glanceActivity] = ""
	case glanceRelated:
		if f.relatedOverlap {
			for _, related := range m.glanceRelatedAreas() {
				if related.area.ID == ids[selected] {
					m.pushGlanceFrame(glanceFrame{areaID: related.area.ID, name: related.area.Name, relatedFromID: f.areaID, relatedFromPath: f.path, relatedFromLeafID: f.leafID, relatedFromLiteral: f.literal, relatedPeople: true, tab: glancePeople})
					break
				}
			}
			break
		}
		for _, related := range m.glanceCochangedAreas() {
			if related.area.ID == ids[selected] {
				m.pushGlanceFrame(glanceFrame{areaID: related.area.ID, name: related.area.Name, relatedFromID: f.areaID, relatedFromPath: f.path, relatedFromLeafID: f.leafID, relatedFromLiteral: f.literal, tab: glanceActivity})
				break
			}
		}
	case glanceSubareas:
		for _, child := range m.glanceSubareas() {
			if child.ID == ids[selected] {
				next := glanceFrame{areaID: f.areaID, name: child.Name, path: f.path, leafID: child.ID}
				if strings.HasPrefix(child.ID, "path:") {
					next.path = strings.TrimPrefix(child.ID, "path:")
					next.leafID = ""
				}
				m.pushGlanceFrame(next)
				break
			}
		}
	}
	return m
}

func (m *Model) glanceSelected(ids []string) int {
	f := &m.glance.frame
	for i, id := range ids {
		if id == f.ids[f.tab] && id != "" {
			f.rows[f.tab] = i
			return i
		}
	}
	i := max(0, min(len(ids)-1, f.rows[f.tab]))
	f.rows[f.tab] = i
	if len(ids) > 0 {
		f.ids[f.tab] = ids[i]
	}
	return i
}

func commitSelectionID(r git.CommitRecord) string {
	if r.CommitID != "" {
		return r.CommitID
	}
	return r.RepoID + "\x00" + r.Email + "\x00" + r.Author + "\x00" + r.Date.String() + "\x00" + r.Subject
}
