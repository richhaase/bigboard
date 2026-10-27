package tui

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

type cacheInfoMsg struct {
	info    git.CacheInfo
	err     error
	removal bool
}

func (m Model) cacheInfoCmd() tea.Cmd {
	if m.options.CacheInfo == nil {
		return nil
	}
	read := m.options.CacheInfo
	return m.commands.wrap(func() tea.Msg {
		info, err := read()
		return cacheInfoMsg{info: info, err: err}
	})
}

func (m Model) prRepositories() []git.Repository {
	if m.options.PrepareRepository != nil {
		return m.repositories
	}
	return m.local.repositories
}

func (m Model) repositoryControlRows() []git.Repository {
	if m.options.CacheInfo == nil {
		return m.local.repositories
	}
	rows := slices.Clone(m.repositories)
	for _, repo := range m.cacheInfo.Orphans {
		if !slices.ContainsFunc(rows, func(row git.Repository) bool { return row.ID == repo.ID }) {
			rows = append(rows, repo)
		}
	}
	return rows
}

func (m Model) orphanedRepository(id string) bool {
	return slices.ContainsFunc(m.cacheInfo.Orphans, func(repo git.Repository) bool { return repo.ID == id })
}

func (m Model) removeCachedRepository() (tea.Model, tea.Cmd) {
	id, remove, read := m.cacheRemoveConfirm, m.options.RemoveCachedRepository, m.options.CacheInfo
	if id == "" || remove == nil || read == nil || !m.orphanedRepository(id) || m.scan.running {
		m.cacheError = "Only unlisted cached repositories can be removed after refresh finishes."
		return m, nil
	}
	m.cacheRemoving = true
	return m, m.commands.wrap(func() tea.Msg {
		if err := remove(id); err != nil {
			return cacheInfoMsg{info: m.cacheInfo, err: fmt.Errorf("cache removal failed: %w", err), removal: true}
		}
		info, err := read()
		return cacheInfoMsg{info: info, err: err, removal: true}
	})
}
