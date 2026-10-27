package tui

import "time"

type BookmarkOptions struct {
	Time      time.Time
	Selection string
	Path      string
	Error     string
	Save      func(time.Time) error
}

func (m Model) sinceLastLook(at time.Time) bool {
	return m.options.Bookmark != nil && !m.options.Bookmark.Time.IsZero() && at.After(m.options.Bookmark.Time)
}

func (m Model) SaveBookmark() error {
	bookmark := m.options.Bookmark
	if bookmark == nil || bookmark.Save == nil || !m.quitting || m.bookmarkSeenAt.IsZero() {
		return nil
	}
	return bookmark.Save(m.bookmarkSeenAt)
}

func (m Model) bookmarkStatusLines() []string {
	bookmark := m.options.Bookmark
	if bookmark == nil {
		return nil
	}
	lines := []string{StyleTitle.Render("SINCE LAST LOOK"), "Selection: " + displayText(bookmark.Selection)}
	if bookmark.Time.IsZero() {
		lines = append(lines, "No saved bookmark for this selection. Markers will be available on a later launch.")
	} else {
		lines = append(lines, "Last look: "+bookmark.Time.Local().Format("Jan 02 15:04:05 MST"))
	}
	if bookmark.Error != "" {
		lines = append(lines, StyleAmber.Render(displayText(bookmark.Error)))
	}
	if m.bookmarkSeenAt.IsZero() {
		lines = append(lines, "Bookmark will advance on normal exit after a fetched snapshot is available.")
	} else {
		lines = append(lines, "On normal exit, save snapshot time: "+m.bookmarkSeenAt.Local().Format("Jan 02 15:04:05 MST"))
	}
	if bookmark.Path != "" {
		lines = append(lines, "State file: "+displayText(bookmark.Path))
	}
	return append(lines, "")
}
