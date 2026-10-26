package tui

import (
	"fmt"
	"time"
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

func rangeControl(label string) string {
	return StyleDimWhite.Render("  Range: " + label + " · t to change")
}

func (m *Model) openRangePicker() {
	m.openFilters("range")
}
