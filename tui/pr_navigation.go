package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

var prSortLabels = []string{"Updated", "Repository", "Author", "Title"}

func (m Model) selectedPRKey() string {
	prs := m.visiblePRs()
	if m.prRow >= 0 && m.prRow < len(prs) {
		return prs[m.prRow].Key()
	}
	return ""
}

func (m *Model) selectPR(key string) {
	prs := m.visiblePRs()
	m.prRow = max(0, min(m.prRow, len(prs)-1))
	for i, pr := range prs {
		if key != "" && pr.Key() == key {
			m.prRow = i
			break
		}
	}
}

func (m Model) prScopeLabel() string {
	if m.prRepoID != "" {
		for _, repo := range m.loadedRepos {
			if repo.ID == m.prRepoID {
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
	direction := "↑"
	if m.prSort == 0 {
		direction = "↓"
	}
	if m.prReverse {
		if direction == "↑" {
			direction = "↓"
		} else {
			direction = "↑"
		}
	}
	return prSortLabels[m.prSort] + " " + direction
}

func (m Model) prScopeRepositories() []git.Repository {
	var repos []git.Repository
	for _, repo := range m.loadedRepos {
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
		m.prSearching = false
	case "esc":
		m.prSearching, m.prQuery = false, m.prSearchBefore
	case "ctrl+u":
		m.prQuery = ""
	case "backspace":
		if runes := []rune(m.prQuery); len(runes) > 0 {
			m.prQuery = string(runes[:len(runes)-1])
		}
	default:
		m.prQuery += string(msg.Runes)
	}
	m.selectPR(selected)
	return m, nil
}
