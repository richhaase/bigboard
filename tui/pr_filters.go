package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

type prFilters struct {
	author, review, checks, state string
}

type prFilterOption struct {
	value, label string
}

var prFilterKinds = []string{"repo", "author", "review", "checks", "state"}

func (f prFilters) matches(pr gh.PullRequest) bool {
	author := strings.ToLower(pr.Author)
	if author == "" {
		author = "?"
	}
	return (f.author == "" || f.author == author) &&
		(f.review == "" || f.review == prReviewLabel(pr.ReviewDecision)) &&
		(f.checks == "" || f.checks == prCheckLabel(pr.CheckState)) &&
		(f.state == "" || f.state == "draft" && pr.Draft || f.state == "open" && !pr.Draft)
}

func (m Model) prFilterValue() string {
	switch m.prFilterKind {
	case "author":
		return m.prFilters.author
	case "review":
		return m.prFilters.review
	case "checks":
		return m.prFilters.checks
	case "state":
		return m.prFilters.state
	default:
		return m.prRepoID
	}
}

func (m Model) prFilterOptions() []prFilterOption {
	options := []prFilterOption{{"", "All"}}
	switch m.prFilterKind {
	case "author":
		options[0].label = "All authors"
		authors := map[string]string{}
		for _, pr := range m.allPRs() {
			value := strings.ToLower(pr.Author)
			if value == "" {
				value = "?"
			}
			authors[value] = githubIdentity(value)
			if value == "?" {
				authors[value] = "Unknown author"
			}
		}
		if active := m.prFilters.author; active != "" && authors[active] == "" {
			authors[active] = githubIdentity(active)
		}
		for value, label := range authors {
			options = append(options, prFilterOption{value, label})
		}
		sort.Slice(options[1:], func(i, j int) bool { return options[i+1].value < options[j+1].value })
	case "review":
		options[0].label = "All review states"
		for _, label := range []string{"Needs review", "Approved", "Changes req.", "Unknown"} {
			options = append(options, prFilterOption{label, label})
		}
	case "checks":
		options[0].label = "All check states"
		for _, label := range []string{"Failing", "Error", "Pending", "Expected", "Passing", "Unknown"} {
			options = append(options, prFilterOption{label, label})
		}
	case "state":
		options[0].label = "All open PRs (including drafts)"
		options = append(options, prFilterOption{"open", "● Open, excluding drafts"}, prFilterOption{"draft", "◇ Drafts only"})
	default:
		options[0].label = "All included repositories"
		for _, repo := range m.prScopeRepositories() {
			options = append(options, prFilterOption{repo.ID, displayText(repo.Name)})
		}
	}
	if m.prFilterQuery == "" {
		return options
	}
	filtered := options[:0]
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.label), strings.ToLower(m.prFilterQuery)) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func (m *Model) openPRFilter(kind string) {
	m.prRepoPicker, m.prFilterKind = true, kind
	m.prFilterQuery, m.prFilterSearch, m.prRepoRow = "", false, 0
	for i, option := range m.prFilterOptions() {
		if option.value == m.prFilterValue() {
			m.prRepoRow = i
			break
		}
	}
}

func (m *Model) clearPRFilters() {
	selected := m.selectedPRKey()
	m.prFilters, m.prRepoID, m.prQuery = prFilters{}, "", ""
	m.prRepoPicker, m.prFilterSearch, m.prDetail, m.prOffset = false, false, false, 0
	m.selectPR(selected)
}

func (m Model) handlePRFilterSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		m.prFilterSearch = false
	case "esc":
		m.prFilterSearch, m.prFilterQuery = false, ""
	case "backspace":
		if runes := []rune(m.prFilterQuery); len(runes) > 0 {
			m.prFilterQuery = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.prFilterQuery = ""
	default:
		m.prFilterQuery += string(msg.Runes)
	}
	m.prRepoRow = 0
	return m, nil
}

func (m Model) prFilterContext(scope string, width int) string {
	parts := []string{"Repo: " + scope}
	if m.prFilters.author != "" {
		label := githubIdentity(m.prFilters.author)
		if m.prFilters.author == "?" {
			label = "Unknown"
		}
		parts = append(parts, "Author: "+label)
	}
	if m.prFilters.review != "" {
		parts = append(parts, "Review: "+m.prFilters.review)
	}
	if m.prFilters.checks != "" {
		parts = append(parts, "Checks: "+m.prFilters.checks)
	}
	if m.prFilters.state != "" {
		parts = append(parts, "State: "+m.prFilters.state)
	}
	if len(parts) == 1 {
		return "All open PRs · Scope: " + scope + " · all dates"
	}
	if width < 70 {
		count := len(parts) - 1
		if m.prRepoID != "" {
			count++
		}
		return fmt.Sprintf("%d filters · f/a/v/c/d edit", count)
	}
	partWidth := max(1, (width-4-3*(len(parts)-1))/len(parts))
	for i, part := range parts {
		parts[i] = ansi.Truncate(part, partWidth, "…")
	}
	return strings.Join(parts, " · ")
}
