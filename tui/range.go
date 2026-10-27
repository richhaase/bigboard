package tui

import (
	"fmt"
	"time"

	"github.com/richhaase/bigboard/git"
)

// Custom ranges are session-only and bounded well below time.Duration overflow.
const maxCustomRangeDays = 3650

func presetRangeLabel(index int) string {
	if index < 0 || index >= len(TimePresets) {
		index = DefaultTimeIndex
	}
	if TimePresets[index].Duration == 0 {
		return "All time"
	}
	if TimePresets[index].Label == "1y" {
		return "1 year"
	}
	return daysRangeLabel(int(TimePresets[index].Duration / (24 * time.Hour)))
}

func daysRangeLabel(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func (m Model) rangeLabel() string {
	if m.sinceLastLookRange && m.bookmarkRangeAvailable() {
		return "Since last look"
	}
	if m.customRangeDays > 0 {
		return daysRangeLabel(m.customRangeDays)
	}
	return presetRangeLabel(m.timeIdx)
}

func (m Model) rangeDuration() time.Duration {
	if m.customRangeDays > 0 {
		return time.Duration(m.customRangeDays) * 24 * time.Hour
	}
	index := m.timeIdx
	if index < 0 || index >= len(TimePresets) {
		index = DefaultTimeIndex
	}
	return TimePresets[index].Duration
}

func (m Model) bookmarkRangeAvailable() bool {
	return m.options.Bookmark != nil && !m.options.Bookmark.Time.IsZero()
}

func (m Model) rangeCutoffAt(now time.Time) (time.Time, bool) {
	if m.sinceLastLookRange && m.bookmarkRangeAvailable() {
		return m.options.Bookmark.Time, true
	}
	duration := m.rangeDuration()
	return now.Add(-duration), duration != 0
}

func (m Model) filterRangeRecordsAt(records []git.CommitRecord, now time.Time) []git.CommitRecord {
	cutoff, bounded := m.rangeCutoffAt(now)
	if !bounded {
		return records
	}
	filtered := make([]git.CommitRecord, 0, len(records))
	for _, record := range records {
		if record.Date.After(cutoff) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func rangeControl(label string) string {
	return StyleDimWhite.Render("  Range: " + label + " · t to change")
}

func (m *Model) openRangePicker() {
	m.openFilters("range")
}
