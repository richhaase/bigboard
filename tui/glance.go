package tui

import (
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

type glanceFrame struct {
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
	searching, help bool
}

func (m Model) glanceQuery() string {
	if m.glance.detailOpen {
		return m.glance.frame.queries[m.glance.frame.tab]
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
	i := 0
	if m.areaRepoID != "" {
		i = 1
	}
	m.glance.overviewQueries[i] = q
}
func (m Model) glanceRepositories() []repositoryActivity {
	rows := m.activityRepositories()
	q := strings.ToLower(m.glanceQuery())
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
	m.evidenceOffset = 0
	m.awarenessPane = 2
}
func (m *Model) pushGlanceFrame(next glanceFrame) {
	current := m.glance.frame
	current.personID = m.personID
	// Copy-on-append prevents Bubble Tea model copies from mutating earlier state.
	m.glance.stack = append(append([]glanceFrame(nil), m.glance.stack...), current)
	m.glance.frame = next
	m.selectedAreaID = next.areaID
	m.personID = ""
	m.evidenceOffset = 0
	m.showPaths = false
	m.pathOffset = 0
}
func (m *Model) closeGlanceDetail() {
	m.glance.detailOpen = false
	m.selectedAreaID = m.glance.overviewAreaID
	m.glance.frame = glanceFrame{}
	m.glance.stack = nil
	m.personID = ""
	m.awarenessPane = 0
	m.evidenceOffset = 0
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
	if key == "q" || key == "ctrl+c" {
		return m.quit()
	}
	if m.glance.help {
		if key == "?" || key == "esc" || key == "enter" {
			m.glance.help = false
		}
		return m, nil
	}
	if key == "?" {
		m.glance.help = true
		return m, nil
	}
	if m.showPaths {
		switch key {
		case "esc", "enter":
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
		case "up", "k":
			m.pathOffset = max(0, m.pathOffset-1)
		case "down", "j":
			m.pathOffset = min(m.commitInspectorMaxOffset(), m.pathOffset+1)
		case "pgup":
			m.pathOffset = max(0, m.pathOffset-max(1, m.height-8))
		case "pgdown":
			m.pathOffset = min(m.commitInspectorMaxOffset(), m.pathOffset+max(1, m.height-8))
		case "home", "g":
			m.pathOffset = 0
		case "end", "G":
			m.pathOffset = m.commitInspectorMaxOffset()
		}
		return m, nil
	}
	switch key {
	case "p", "P":
		m.showPRs = true
		m.prAll = key == "P"
		m.prDetail = false
		m.prRow = 0
		m.prOffset = 0
		return m, nil
	case "v":
		m.viewMode = ViewAggregate
		return m, nil
	case "R":
		cmd := m.startLocalRefresh()
		return m, cmd
	case "r":
		m.overlayExcluded = make(map[string]bool)
		for k, v := range m.excludedRepos {
			m.overlayExcluded[k] = v
		}
		m.overlayCursor = 0
		m.returnView = ViewAwareness
		m.viewMode = ViewRepoOverlay
		return m, nil
	case "b":
		m.hideBots = !m.hideBots
		m.recomputeAuthors()
		return m, nil
	case "left", "h", "right", "l":
		delta := 1
		if key == "left" || key == "h" {
			delta = -1
		}
		m.timeIdx = max(0, min(len(TimePresets)-1, m.timeIdx+delta))
		m.recomputeAuthors()
		return m, nil
	case "/":
		m.glance.searching = true
		return m, nil
	case "esc":
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
			m.areaRepoID = ""
			m.personID = ""
			return m, nil
		}
		return m.quit()
	}
	if m.glance.detailOpen {
		return m.handleGlanceDetailKey(key), nil
	}
	rows := m.glanceRepositories()
	selected := m.selectedRepository(rows)
	switch key {
	case "s":
		m.overviewSort = (m.overviewSort + 1) % len(overviewSortLabels)
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
				m.areaRepoID = rows[selected].repo.ID
				m.selectedAreaID = ""
				m.glance.overviewQueries[1] = ""
			} else {
				m.openGlanceArea(rows[selected])
			}
		}
	default:
		if next, ok := glanceMove(key, selected, len(rows), max(1, m.height-15)); ok && len(rows) > 0 {
			m.setSelectedScopeID(rows[next].repo.ID)
		}
	}
	return m, nil
}

func glanceMove(key string, current, total, page int) (int, bool) {
	next := current
	switch key {
	case "up", "k":
		next--
	case "down", "j":
		next++
	case "pgup":
		next -= page
	case "pgdown":
		next += page
	case "home", "g":
		next = 0
	case "end", "G":
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
	case "s":
		if f.tab == glanceSubareas {
			m.overviewSort = (m.overviewSort + 1) % len(overviewSortLabels)
		}
		return m
	}
	ids := m.glanceDetailIDs()
	selected := m.glanceSelected(ids)
	if next, ok := glanceMove(key, selected, len(ids), max(1, m.height-10)); ok && len(ids) > 0 {
		f.rows[f.tab] = next
		f.ids[f.tab] = ids[next]
		if f.tab == glanceActivity {
			m.evidenceOffset = next
		}
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
		m.evidenceOffset = 0
	case glanceRelated:
		for _, related := range m.glanceRelatedAreas() {
			if related.area.ID == ids[selected] {
				m.pushGlanceFrame(glanceFrame{areaID: related.area.ID, name: related.area.Name, relatedFromID: f.areaID, relatedFromPath: f.path, relatedFromLeafID: f.leafID, tab: glancePeople})
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
	if f.tab == glanceActivity {
		m.evidenceOffset = i
	}
	return i
}

func commitSelectionID(r git.CommitRecord) string {
	if r.CommitID != "" {
		return r.CommitID
	}
	return r.RepoID + "\x00" + r.Email + "\x00" + r.Author + "\x00" + r.Date.String() + "\x00" + r.Subject
}
