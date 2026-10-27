package tui

import (
	"context"
	"encoding/hex"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/richhaase/bigboard/github"
)

func commitBrowserURL(repo, commitID string) string {
	canonical, err := gh.CanonicalRepo(repo)
	if err != nil || len(commitID) != 40 && len(commitID) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(commitID); err != nil {
		return ""
	}
	return "https://github.com/" + canonical + "/commit/" + commitID
}

type commitBrowserResult struct{ err error }

func (m Model) openSelectedCommit() (tea.Model, tea.Cmd) {
	commit, ok := m.selectedGlanceCommit()
	if !ok {
		return m, nil
	}
	repo, ok := m.areaRepository()
	if !ok {
		m.commitBrowserError = "Could not open commit: repository unavailable"
		return m, nil
	}
	m.commitBrowserError = ""
	provider := m.prProvider
	if provider == nil {
		provider = defaultPRProvider()
	}
	opener := m.prBrowserOpener
	if opener == nil {
		opener = openPRBrowser
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gh.RequestTimeout)
		defer cancel()
		remote, err := provider.Resolve(ctx, repo.Path)
		if err != nil {
			if errors.Is(err, gh.ErrUnsupportedOrigin) {
				err = errors.New("no supported GitHub origin")
			}
			return commitBrowserResult{err: err}
		}
		url := commitBrowserURL(remote, commit.CommitID)
		if url == "" {
			return commitBrowserResult{err: errors.New("canonical GitHub commit URL unavailable")}
		}
		return commitBrowserResult{err: opener(url)}
	}
}
