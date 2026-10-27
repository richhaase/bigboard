package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

type scanPhase int

const (
	scanDefault scanPhase = iota
	scanCached
	scanStartupFetch
)

type startupProgress struct {
	pending   bool
	cachedAt  time.Time
	fetchedAt time.Time
}

func (m Model) startupRepoCmd(ctx context.Context, repo git.Repository) tea.Cmd {
	phase, generation := m.scan.phase, m.scan.generation
	options := m.options
	excluded := m.excludedRepos[repo.ID]
	previousRevision := m.local.revisions[repo.ID]
	usable := previousRevision != "" && !m.localRepositoryFailed(repo)
	return func() tea.Msg {
		result := RepoLoadedMsg{Generation: generation, Repository: repo}
		if phase == scanStartupFetch && !excluded {
			prepare := options.PrepareRepository
			if prepare == nil {
				prepare = git.FetchRepository
			}
			if err := prepare(ctx, repo); err != nil {
				result.FetchErr, result.Retained = err, true
				return result
			}
		}
		revision, err := options.RepositoryRevision(ctx, repo)
		if err != nil {
			result.Err = err
			return result
		}
		if phase == scanCached && revision == "" {
			result.Skipped = true
			return result
		}
		if phase == scanStartupFetch && usable && previousRevision == revision {
			result.Retained = true
			return result
		}
		result = loadRepoCmd(ctx, repo, options, generation, false)().(RepoLoadedMsg)
		result.Revision = revision
		return result
	}
}

func (m *Model) startStartupFetch() tea.Cmd {
	m.resetPending()
	m.scan.phase = scanStartupFetch
	return m.loadCmds()
}

func (m Model) finishCachedStartup() (tea.Model, tea.Cmd) {
	for _, outcome := range m.scan.outcomes {
		if !outcome.result.Skipped && outcome.result.Err == nil {
			m.finalizeLoad()
			break
		}
	}
	cmd := tea.Batch(m.startStartupFetch(), m.startPRRefresh(), m.cacheInfoCmd())
	return m, cmd
}
