package tui

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func TestCloseCancelsAndJoinsRunningCommands(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByCommits, nil, "", DefaultTimeIndex, Options{})
	started, stopped := make(chan struct{}), make(chan struct{})
	cmd := m.commands.wrap(func() tea.Msg {
		close(started)
		<-m.scanContext.Done()
		close(stopped)
		return nil
	})
	go cmd()
	<-started
	m.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("Close returned before the command stopped")
	}
	m.Close()
	if got := cmd(); got != nil {
		t.Fatal("closed group dispatched a queued command")
	}
}

type cancelWaitingPRProvider struct{ started, stopped chan struct{} }

func (p *cancelWaitingPRProvider) Resolve(context.Context, string) (string, error) {
	return "org/repo", nil
}
func (p *cancelWaitingPRProvider) Fetch(ctx context.Context, repo string) gh.Result {
	close(p.started)
	<-ctx.Done()
	close(p.stopped)
	return gh.Result{Repo: repo, Err: ctx.Err()}
}

func TestInitialModelCloseAlsoStopsLaterPRRefresh(t *testing.T) {
	p := &cancelWaitingPRProvider{make(chan struct{}), make(chan struct{})}
	original := NewModelWithOptions(nil, stats.SortByCommits, nil, "", DefaultTimeIndex, Options{PRProvider: p})
	updated := original
	updated.loadedRepos = []git.Repository{{ID: "/repo", Path: "/repo"}}
	cmd := updated.startPRRefresh()
	go cmd()
	<-p.started
	original.Close() // main retains the initial model, not Update's latest copy.
	select {
	case <-p.stopped:
	default:
		t.Fatal("PR request outlived model Close")
	}
}

func TestCommandGroupCloseConcurrentDispatch(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByCommits, nil, "", DefaultTimeIndex, Options{})
	var callers sync.WaitGroup
	for range 100 {
		callers.Go(func() { m.commands.wrap(func() tea.Msg { <-m.scanContext.Done(); return nil })() })
	}
	m.Close()
	callers.Wait()
}

func TestExternalBubbleTeaExitThenClose(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByCommits, nil, "", DefaultTimeIndex, Options{})
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); m.Close(); done <- err }()
	p.Send(tea.QuitMsg{})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	if m.scanContext.Err() == nil {
		t.Fatal("scan context remains live")
	}
}
