package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	if m.prRepoPicker {
		return m.handlePRFilterSearch(msg)
	}
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
	options := m.prFilterOptions()
	_, page := m.prFilterModalSize(m.width, m.height)
	if row, ok := glanceMove(msg.String(), m.prRepoRow, len(options), page); ok {
		m.prRepoRow = row
		return m, nil
	}
	switch msg.String() {
	case "esc", "left":
		m.prRepoPicker, m.prFilterSearch = false, false
	case "f", "u", "v", "k", "d":
		kinds := map[string]string{"f": "repo", "u": "author", "v": "review", "k": "checks", "d": "state"}
		m.openPRFilter(kinds[msg.String()])
	case "tab", "shift+tab":
		index := 0
		for i, kind := range prFilterKinds {
			if kind == m.prFilterKind {
				index = i
				break
			}
		}
		step := 1
		if msg.String() == "shift+tab" {
			step = -1
		}
		m.openPRFilter(prFilterKinds[(index+step+len(prFilterKinds))%len(prFilterKinds)])
	case "/":
		m.prFilterSearch = true
	case "x":
		m.clearPRFilters()
	case "enter", "right":
		if len(options) == 0 {
			return m, nil
		}
		selected := m.selectedPRKey()
		value := options[max(0, min(m.prRepoRow, len(options)-1))].value
		switch m.prFilterKind {
		case "author":
			m.prFilters.author = value
		case "review":
			m.prFilters.review = value
		case "checks":
			m.prFilters.checks = value
		case "state":
			m.prFilters.state = value
		default:
			m.prRepoID = value
		}
		m.prRepoPicker, m.prFilterSearch, m.prDetail, m.prOffset = false, false, false, 0
		m.selectPR(selected)
	}
	return m, nil
}

func (m Model) prFilterTabLines(width int) []string {
	keys := []string{"f", "u", "v", "k", "d"}
	labels := []string{"Repo", "Author", "Review", "Checks", "State"}
	kind := m.prFilterKind
	if kind == "" {
		kind = "repo"
	}
	var lines []string
	line := ""
	for i, tab := range prFilterKinds {
		label := keys[i] + " " + labels[i]
		style := StyleDimWhite
		if tab == kind {
			label = "[" + label + "]"
			style = StyleTitle.Background(ColorRowSelect)
		}
		gap := ""
		if line != "" {
			gap = "  "
		}
		if ansi.StringWidth(line+gap+label) > width && line != "" {
			lines = append(lines, line)
			line = ""
			gap = ""
		}
		line += gap + style.Render(label)
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (m Model) prFilterModalSize(width, height int) (int, int) {
	panelWidth := max(6, min(76, width-2))
	page := max(1, min(12, height-len(m.prFilterTabLines(panelWidth-4))-10))
	return panelWidth, page
}

func (m Model) renderPRScopePicker(background string, width, height int) string {
	if width < 30 || height < 12 {
		return fitGlanceLines([]string{"PR filters", "Resize to choose a filter", "Esc cancels"}, width, height)
	}
	panelWidth, page := m.prFilterModalSize(width, height)
	inner := panelWidth - 4
	name := map[string]string{"repo": "Repository", "author": "Author", "review": "Review status", "checks": "Checks", "state": "Draft / open"}[m.prFilterKind]
	if name == "" {
		name = "Repository"
	}
	content := []string{StyleTitle.Render("PR filters · " + name)}
	content = append(content, m.prFilterTabLines(inner)...)
	query := displayText(m.prFilterQuery)
	if query == "" && !m.prFilterSearch {
		query = "Find options"
	}
	cursor := ""
	if m.prFilterSearch {
		cursor = "▌"
	}
	field := " / " + ansi.Truncate(query, max(1, inner-3-ansi.StringWidth(cursor)), "…") + cursor
	content = append(content, lipgloss.NewStyle().Foreground(ColorDimWhite).Background(ColorRowEven).Render(padCells(field, inner)))
	content = append(content, StyleDimCyan.Render(strings.Repeat("─", inner)))
	options := m.prFilterOptions()
	selected := max(0, min(m.prRepoRow, len(options)-1))
	start := max(0, min(selected-page/2, len(options)-page))
	end := min(len(options), start+page)
	for i := start; i < end; i++ {
		cursor := "  "
		if i == selected {
			cursor = "› "
		}
		applied := " "
		if options[i].value == m.prFilterValue() {
			applied = "✓"
		}
		row := cursor + padCells(options[i].label, max(1, inner-4)) + " " + applied
		style := lipgloss.NewStyle().Foreground(ColorBrightWht)
		if i == selected {
			style = style.Background(ColorRowSelect).Foreground(ColorCyan).Bold(true)
		}
		content = append(content, style.Render(padCells(row, inner)))
	}
	if len(options) == 0 {
		content = append(content, StyleDimWhite.Render("No matches. / edits search."))
	}
	shown := max(1, end-start)
	for ; shown < page; shown++ {
		content = append(content, "")
	}
	position := fmt.Sprintf("%d–%d of %d", start+1, end, len(options))
	if len(options) == 0 {
		position = "0 options"
	}
	content = append(content, StyleDimWhite.Render(position+" · ✓ applied"))
	controls, hints := "Enter apply · Esc cancel", "↑↓ select · Tab category · / find · ? help"
	if inner < 50 {
		hints = "↑↓ · Tab · / find · ? help"
	}
	if m.prFilterSearch {
		controls, hints = "Enter accept · Esc clear", "Type to find · F1 help"
	}
	content = append(content, StyleCyan.Render(controls), StyleDimWhite.Render(hints))
	return renderCenteredModal(background, content, panelWidth, width, height)
}
