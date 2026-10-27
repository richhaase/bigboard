package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type remoteProbeState struct {
	context  context.Context
	results  map[string]gh.RepositoryProbe
	requests int
	checked  time.Time
}

type repositoriesProbedMsg struct {
	generation uint64
	state      *remoteProbeState
}

func (m *Model) startRemotePass() tea.Cmd {
	provider, ok := m.prProvider.(interface {
		ProbeAll(context.Context, []string) []gh.RepositoryProbe
	})
	if !ok || m.options.PrepareIfChanged == nil {
		if m.scan.phase == scanStartupFetch {
			return tea.Batch(m.loadCmds(), m.startPRRefresh())
		}
		return m.loadCmds()
	}
	var repositories []string
	for _, repo := range m.repositories {
		if !m.excludedRepos[repo.ID] {
			repositories = append(repositories, repo.ID)
		}
	}
	budget := m.options.PRRequestBudget
	if budget <= 0 {
		budget = gh.MaxRefreshRequests
	}
	ctx := gh.WithRequestBudget(m.scanContext, budget)
	generation := m.scan.generation
	local := m.localScanContext
	return m.commands.wrap(func() tea.Msg {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		stop := context.AfterFunc(local, cancel)
		defer stop()
		results := provider.ProbeAll(bounded, repositories)
		state := &remoteProbeState{context: ctx, results: make(map[string]gh.RepositoryProbe), requests: gh.RequestCount(ctx), checked: time.Now()}
		for i, result := range results {
			if i < len(repositories) {
				state.results[repositories[i]] = result
			}
		}
		return repositoriesProbedMsg{generation: generation, state: state}
	})
}

func (m Model) applyRepositoryProbes(msg repositoriesProbedMsg) (tea.Model, tea.Cmd) {
	if !m.scan.running || msg.generation != m.scan.generation || m.quitting {
		return m, nil
	}
	m.remoteProbe = msg.state
	m.repositories = append([]git.Repository(nil), m.repositories...)
	for i, repo := range m.repositories {
		probe := msg.state.results[repo.ID]
		if probe.Err == nil && probe.Repo == repo.ID && probe.DefaultBranch != "" && probe.HeadOID != "" {
			m.repositories[i].DefaultBranch = probe.DefaultBranch
		}
	}
	if m.scan.phase == scanStartupFetch {
		return m, tea.Batch(m.loadCmds(), m.startPRRefreshWithProbe(msg.state))
	}
	return m, m.loadCmds()
}

func (m Model) probedOptions(repo git.Repository) Options {
	options := m.options
	if m.remoteProbe == nil || options.PrepareIfChanged == nil || m.scan.localOnly {
		return options
	}
	probe, ok := m.remoteProbe.results[repo.ID]
	if !ok || probe.Err != nil || probe.Repo != repo.ID || probe.DefaultBranch == "" || probe.HeadOID == "" {
		return options
	}
	options.PrepareRepository = func(ctx context.Context, repository git.Repository) error {
		return options.PrepareIfChanged(ctx, repository, probe.DefaultBranch, probe.HeadOID)
	}
	return options
}
