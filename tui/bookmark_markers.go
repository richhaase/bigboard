package tui

import "time"

func (m Model) lastLookAge(at time.Time, age string) string {
	if m.sinceLastLook(at) {
		return StyleCyan.Render("+") + age
	}
	return age
}

func (m Model) bookmarkMarkerLegend() string {
	if m.options.Bookmark != nil && !m.options.Bookmark.Time.IsZero() {
		return "+ since last look"
	}
	return ""
}
