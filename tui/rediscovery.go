package tui

import (
	"context"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

type repositoriesRediscoveredMsg struct {
	generation   uint64
	repositories []git.Repository
	excluded     map[string]bool
	err          error
}

func (m *Model) startRediscovery() tea.Cmd {
	rediscover, generation := m.options.Rediscover, m.scan.generation
	ctx := m.localScanContext
	if ctx == nil {
		ctx = context.Background()
	}
	return m.commands.wrap(func() tea.Msg {
		repositories, excluded, err := rediscover(ctx)
		return repositoriesRediscoveredMsg{generation: generation, repositories: repositories, excluded: excluded, err: err}
	})
}

func (m Model) applyRediscovery(msg repositoriesRediscoveredMsg) (tea.Model, tea.Cmd) {
	if !m.scan.running || msg.generation != m.scan.generation || m.quitting {
		return m, nil
	}
	m.discoveryErr = ""
	if msg.err != nil {
		m.discoveryErr = displayText(msg.err.Error())
	} else {
		excluded := normalizeExcluded(msg.repositories, msg.excluded)
		for id := range m.excludedRepos {
			if slices.ContainsFunc(msg.repositories, func(repo git.Repository) bool { return repo.ID == id }) {
				excluded[id] = true
			}
		}
		m.repositories = slices.Clone(msg.repositories)
		m.excludedRepos = excluded
		m.resetPending()
	}
	if len(m.repositories) == 0 {
		m.finalizeLoad()
		return m, m.cacheInfoCmd()
	}
	return m, m.startRemotePass()
}
