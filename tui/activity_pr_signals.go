package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	activityPROpenWidth    = 8
	activityPRFailWidth    = 4
	activityPRChangesWidth = 7
	activityPRWideWidth    = activityPROpenWidth + activityPRFailWidth + activityPRChangesWidth + 2
)

type activityPRSignal struct {
	open, failing, changes int
	known, uncertain       bool
}

func (m Model) activityPRSignal(row activityRow) activityPRSignal {
	snapshot, ok := m.prState.snapshots[m.prState.repositories[row.repositoryID]]
	if !ok || !m.repoInView(row.repositoryID) {
		return activityPRSignal{}
	}
	signal := activityPRSignal{known: true, uncertain: snapshot.partial || snapshot.err != nil || m.prState.errors[row.repositoryID] != nil}
	if signal.uncertain && len(snapshot.prs) == 0 && snapshot.lastGood.IsZero() {
		signal.known = false
		return signal
	}
	literal := row.scopeID != row.repositoryID
	for _, pr := range m.prsInViewForRepository(row.repositoryID) {
		if literal {
			if !pr.FilesComplete {
				signal.uncertain = true
			}
			if !m.prTouchesLiteralScope(pr, row.scopeID) {
				continue
			}
		}
		signal.open++
		if pr.CheckState == "FAILURE" || pr.CheckState == "ERROR" {
			signal.failing++
		}
		if pr.ReviewDecision == "CHANGES_REQUESTED" {
			signal.changes++
		}
	}
	return signal
}

func (s activityPRSignal) render(wide bool) string {
	count := func(n int, warning bool) string {
		if !s.known {
			return StyleAmber.Render("?")
		}
		text := FormatNumber(n)
		if s.uncertain {
			return StyleAmber.Render(text + "?")
		}
		if n == 0 {
			if warning {
				return ""
			}
			return StyleDimCyan.Render("—")
		}
		if warning {
			return lipgloss.NewStyle().Foreground(ColorRed).Render(text)
		}
		return text
	}
	if !wide {
		return count(s.open, false)
	}
	return strings.Join([]string{
		rightCells(count(s.open, false), activityPROpenWidth),
		rightCells(count(s.failing, true), activityPRFailWidth),
		rightCells(count(s.changes, true), activityPRChangesWidth),
	}, " ")
}
