package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var refreshFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type refreshAnimation struct {
	ready    bool
	frame    int
	sequence uint64
	pending  uint64
}

type refreshTickMsg struct{ sequence uint64 }

func (m Model) refreshBusy() bool {
	return !m.quitting && (m.scanContext == nil || m.scanContext.Err() == nil) &&
		(!m.settings.open || !m.settings.required) && (m.scan.running || m.prState.loading)
}

func (m Model) refreshGlyph() string {
	return refreshFrames[m.refreshAnimation.frame%len(refreshFrames)]
}

func (m *Model) refreshTickCmd() tea.Cmd {
	if !m.refreshAnimation.ready || m.refreshAnimation.pending != 0 || !m.refreshBusy() {
		return nil
	}
	m.refreshAnimation.sequence++
	m.refreshAnimation.pending = m.refreshAnimation.sequence
	sequence := m.refreshAnimation.sequence
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return refreshTickMsg{sequence: sequence}
	})
}

func (m Model) updateRefreshTick(msg refreshTickMsg) (tea.Model, tea.Cmd) {
	if msg.sequence == 0 || msg.sequence != m.refreshAnimation.pending {
		return m, nil
	}
	m.refreshAnimation.pending = 0
	if !m.refreshBusy() {
		return m, nil
	}
	m.refreshAnimation.frame = (m.refreshAnimation.frame + 1) % len(refreshFrames)
	cmd := m.refreshTickCmd()
	return m, cmd
}
