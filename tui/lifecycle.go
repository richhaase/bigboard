package tui

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// commandGroup joins commands already running, while preventing queued Bubble
// Tea commands from starting after shutdown. Models share it across Update copies.
type commandGroup struct {
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

func (g *commandGroup) wrap(cmd tea.Cmd) tea.Cmd {
	if g == nil || cmd == nil {
		return cmd
	}
	return func() tea.Msg {
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			return nil
		}
		g.running.Add(1)
		g.mu.Unlock()
		defer g.running.Done()
		return cmd()
	}
}

func (g *commandGroup) wait() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	g.running.Wait()
}

// Close cancels and joins background work on every program exit, including
// signals and terminal errors that Bubble Tea handles without calling Update.
// Call it after Program.Run returns, before exiting the process.
func (m Model) Close() {
	if m.cancelScans != nil {
		m.cancelScans()
	}
	if m.cancelPRs != nil {
		m.cancelPRs()
	}
	m.commands.wait()
}
