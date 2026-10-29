package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

type prUIState struct {
	filters       prFilters
	repoID        string
	query         string
	searchBefore  string
	searching     bool
	filterKind    string
	filterQuery   string
	sort          int
	reverse       bool
	row           int
	detail        bool
	offset        int
	browserError  string
	previewFocus  bool
	previewOffset int
	sortOpen      bool
	sortRow       int
}

func (s prUIState) clone() prUIState {
	s.filters = s.filters.clone()
	return s
}

var prSortLabels = []string{"Updated", "Repository", "Author", "Title", "Attention"}

func (m Model) selectedPRKey() string {
	prs := m.visiblePRs()
	if m.prUI.row >= 0 && m.prUI.row < len(prs) {
		return prs[m.prUI.row].Key()
	}
	return ""
}

func (m *Model) selectPR(key string) {
	prs := m.visiblePRs()
	m.prUI.row = max(0, min(m.prUI.row, len(prs)-1))
	for i, pr := range prs {
		if key != "" && pr.Key() == key {
			m.prUI.row = i
			break
		}
	}
	if m.selectedPRKey() != key {
		m.prUI.previewOffset = 0
	}
}

func (m *Model) togglePRMine(value string) {
	selected := m.selectedPRKey()
	if len(m.prUI.filters.mine) == 1 && m.prUI.filters.mine[0] == value {
		m.prUI.filters.mine = nil
	} else {
		m.prUI.filters.mine = filterValues{value}
	}
	m.selectPR(selected)
	if m.prUI.detail && m.selectedPRKey() != selected {
		m.prUI.detail, m.prUI.offset = false, 0
	}
}

func (m Model) prScopeLabel() string {
	if m.prUI.repoID != "" {
		for _, repo := range m.local.repositories {
			if repo.ID == m.prUI.repoID {
				return displayText(repo.Name)
			}
		}
		return "repository unavailable"
	}
	if len(m.sharedRepos) > 0 {
		return m.sharedRepositoryLabel()
	}
	return "all included repositories"
}

func (m Model) prSortLabel() string {
	direction := "A–Z"
	if m.prUI.reverse {
		direction = "Z–A"
	}
	switch m.prUI.sort {
	case 0:
		direction = "newest first"
		if m.prUI.reverse {
			direction = "oldest first"
		}
	case 4:
		direction = "priority first"
		if m.prUI.reverse {
			direction = "priority last"
		}
	}
	return prSortLabels[m.prUI.sort] + " · " + direction
}

func (m Model) prScopeRepositories() []git.Repository {
	var repos []git.Repository
	for _, repo := range m.local.repositories {
		if !m.excludedRepos[repo.ID] {
			repos = append(repos, repo)
		}
	}
	sort.Slice(repos, func(i, j int) bool {
		if repos[i].Name != repos[j].Name {
			return repos[i].Name < repos[j].Name
		}
		return repos[i].ID < repos[j].ID
	})
	return repos
}

func (m Model) handlePRSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	selected := m.selectedPRKey()
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		m.prUI.searching = false
	case "esc":
		m.prUI.searching, m.prUI.query = false, m.prUI.searchBefore
	case "ctrl+u":
		m.prUI.query = ""
	case "backspace":
		if runes := []rune(m.prUI.query); len(runes) > 0 {
			m.prUI.query = string(runes[:len(runes)-1])
		}
	default:
		m.prUI.query += string(msg.Runes)
	}
	m.selectPR(selected)
	return m, nil
}
