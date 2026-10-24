package tui

import (
	"fmt"
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
		m.prSearching, m.prQuery = false, ""
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

func (m Model) handlePRScopeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	repos := m.prScopeRepositories()
	if row, ok := glanceMove(msg.String(), m.prRepoRow, len(repos)+1, max(1, m.height-screenHeaderRows-screenFooterRows-1)); ok {
		m.prRepoRow = row
		return m, nil
	}
	switch msg.String() {
	case "esc", "left", "f":
		m.prRepoPicker = false
	case "enter", "right":
		selected := m.selectedPRKey()
		m.prRepoID = ""
		if m.prRepoRow > 0 && m.prRepoRow <= len(repos) {
			m.prRepoID = repos[m.prRepoRow-1].ID
		}
		m.prRepoPicker, m.prDetail, m.prOffset = false, false, 0
		m.selectPR(selected)
	}
	return m, nil
}

func (m Model) renderPRScopePicker(width, height int) string {
	lines := m.screenHeader("Pull requests / Repository scope", width)
	lines = append(lines, "  Choose a repository · inclusion settings stay unchanged")
	repos := m.prScopeRepositories()
	labels := []string{"All included repositories"}
	for _, repo := range repos {
		labels = append(labels, displayText(repo.Name))
	}
	budget := max(1, height-screenHeaderRows-screenFooterRows-1)
	selected := max(0, min(m.prRepoRow, len(labels)-1))
	start := max(0, min(selected-budget/2, len(labels)-budget))
	for i := start; i < min(len(labels), start+budget); i++ {
		lines = append(lines, awarenessRow(glanceCursor(i == selected)+" "+labels[i], i == selected, i, width))
	}
	help := "? help · Enter apply · Esc cancel · ↑↓ select"
	if width < 70 {
		help = "? help · Enter OK · Esc cancel"
	}
	return screenFinish(lines, help, fmt.Sprintf("%d/%d · repository scope only", selected+1, len(labels)), width, height)
}
