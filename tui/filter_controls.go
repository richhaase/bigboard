package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) clearBoardFilters() {
	selectedPR, selectedContributor := m.selectedPRKey(), m.selectedContributorID()
	if m.showPRs {
		m.prUI.filters, m.prUI.query = prFilters{}, ""
		m.selectPR(selectedPR)
		if m.prUI.detail && m.selectedPRKey() != selectedPR {
			m.prUI.detail, m.prUI.offset = false, 0
		}
		return
	}
	if m.viewMode == ViewAwareness {
		m.activityPeople, m.activityHideBots = nil, false
		m.glance.overviewQueries = [2]string{}
		m.glance.frame.queries = [4]string{}
		m.glance.stack = append([]glanceFrame(nil), m.glance.stack...)
		for i := range m.glance.stack {
			m.glance.stack[i].queries = [4]string{}
		}
		m.directory.frame.query = ""
		m.directory.stack = append([]directoryFrame(nil), m.directory.stack...)
		for i := range m.directory.stack {
			m.directory.stack[i].query = ""
		}
		return
	}
	m.filterQuery, m.hideBots = "", false
	m.recomputeAuthors()
	m.selectContributor(selectedContributor)
}

func (m Model) boardBotsHidden() bool {
	if !m.showPRs && m.viewMode == ViewAwareness {
		return m.activityHideBots
	}
	return m.hideBots
}

func (m Model) sharedRepositoryLabel() string {
	repos := "all repos"
	if len(m.sharedRepos) == 1 {
		repos = "1 repo"
		for _, repo := range m.prRepositories() {
			if repo.ID == m.sharedRepos[0] {
				repos = displayText(repo.Name)
				break
			}
		}
	} else if len(m.sharedRepos) > 1 {
		repos = fmt.Sprintf("%d repos", len(m.sharedRepos))
	}
	return repos
}

func (m Model) sharedScopeLabel(width int) string {
	label := "Shared: " + m.rangeLabel() + " · " + m.sharedRepositoryLabel()
	if width >= 90 {
		basis := "commit author date"
		if m.showPRs {
			basis = "PR last updated"
		} else if m.viewMode == ViewAwareness {
			basis = "Git author date · PR updated"
		}
		label += " · " + basis
	}
	return ansi.Truncate(label, max(1, width-4), "…")
}

func (m Model) boardFiltersLabel(width int) string {
	var parts []string
	query, searching := m.filterQuery, m.searching
	if m.showPRs {
		for _, category := range []struct{ key, label string }{{"mine", "Mine"}, {"author", "Author"}, {"review", "Review"}, {"checks", "Checks"}, {"state", "State"}} {
			if values := m.prUI.filters.values(category.key); len(values) > 0 {
				parts = append(parts, category.label+": "+displayText(strings.Join(values, ", ")))
			}
		}
		query, searching = m.prUI.query, m.prUI.searching
	} else {
		if m.boardBotsHidden() {
			parts = append(parts, "bots hidden")
		}
		if m.viewMode == ViewAwareness {
			if len(m.activityPeople) > 0 {
				label := fmt.Sprintf("%d contributors", len(m.activityPeople))
				if len(m.activityPeople) == 1 {
					for _, person := range m.activityPeopleOptions() {
						if person.value == m.activityPeople[0] {
							label = displayText(person.label)
							break
						}
					}
				}
				parts = append(parts, "Person: "+label)
			}
			query, searching = m.glanceQuery(), m.glance.searching
		}
	}
	if searching {
		return inputLine("Find: ", query, max(1, width-4))
	}
	if query != "" {
		parts = append(parts, "Find: "+displayText(query))
	}
	if len(parts) == 0 {
		return "Filters: none · f edit"
	}
	label := "Filters: " + strings.Join(parts, " · ")
	if ansi.StringWidth(label) > max(1, width-4) {
		return ansi.Truncate(fmt.Sprintf("Filters: %d active · f edit · x clear", len(parts)), max(1, width-4), "…")
	}
	return label
}
