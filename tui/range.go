package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Custom ranges are session-only and bounded well below time.Duration overflow.
const maxCustomRangeDays = 3650

type rangePickerState struct {
	open, custom, replace, overflow bool
	cursor                          int
	input, error                    string
}

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

func rangeControl(label string) string {
	return StyleDimWhite.Render("  Range: " + label + " · t to change")
}

func (m *Model) openRangePicker() {
	cursor := m.timeIdx
	if m.customRangeDays > 0 {
		cursor = len(TimePresets)
	}
	m.rangePicker = rangePickerState{open: true, cursor: cursor}
}

func (m Model) handleRangeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker := &m.rangePicker
	key := msg.String()
	if key == "ctrl+c" {
		return m.quit()
	}
	if key == "esc" {
		m.rangePicker = rangePickerState{}
		return m, nil
	}
	if picker.custom {
		switch key {
		case "enter":
			if picker.overflow {
				picker.error = "Too many digits; clear or edit the value"
				return m, nil
			}
			days, err := strconv.Atoi(picker.input)
			if err != nil || days < 1 || days > maxCustomRangeDays {
				picker.error = fmt.Sprintf("Enter a whole number from 1 to %d", maxCustomRangeDays)
				return m, nil
			}
			m.customRangeDays = days
			m.applyRange()
		case "backspace":
			if picker.replace {
				picker.input = ""
			} else if len(picker.input) > 0 {
				picker.input = picker.input[:len(picker.input)-1]
			}
			picker.replace, picker.overflow, picker.error = false, false, ""
		case "ctrl+u":
			picker.input, picker.error, picker.replace, picker.overflow = "", "", false, false
		default:
			if len(msg.Runes) > 0 {
				input := string(msg.Runes)
				if strings.IndexFunc(input, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
					picker.error = "Use digits only"
					return m, nil
				}
				if picker.replace {
					picker.input = ""
				}
				picker.replace, picker.error = false, ""
				// Never apply the truncated prefix of an oversized paste.
				combined := picker.input + input
				picker.overflow = picker.overflow || len(combined) > 6
				picker.input = combined[:min(6, len(combined))]
				if picker.overflow {
					picker.error = "Too many digits; clear or edit the value"
				}
			}
		}
		return m, nil
	}
	switch key {
	case "up":
		picker.cursor = max(0, picker.cursor-1)
	case "down":
		picker.cursor = min(len(TimePresets), picker.cursor+1)
	case "home":
		picker.cursor = 0
	case "end", "c":
		picker.cursor = len(TimePresets)
	case "enter":
		if picker.cursor == len(TimePresets) {
			days := m.customRangeDays
			if days == 0 {
				days = int(TimePresets[m.timeIdx].Duration / (24 * time.Hour))
			}
			if days == 0 {
				days = 14
			}
			picker.custom, picker.replace = true, true
			picker.input = strconv.Itoa(days)
		} else {
			m.timeIdx, m.customRangeDays = picker.cursor, 0
			m.applyRange()
		}
	}
	return m, nil
}

func (m *Model) applyRange() {
	m.rangePicker = rangePickerState{}
	m.statDetailOffset = 0
	personID := m.personID
	m.recomputeAuthors()
	// Filtering is not navigation. Keep a vanished person's intent so detail
	// shows no matching evidence instead of silently broadening to everybody.
	m.personID = personID
}

func (m Model) renderRangePicker() string {
	width, height := max(1, m.width), max(1, m.height)
	if width < 40 || height < 18 {
		return fitGlanceLines([]string{"Resize to 40×18 · Esc cancel"}, width, height)
	}
	lines := []string{StyleTitle.Render("  LOCAL TIME RANGE"), "  Current: " + m.rangeLabel(), "  In-memory filter · no scan or fetch", ""}
	if m.rangePicker.custom {
		input, hint := m.rangePicker.input, "  Type days · Backspace edit"
		if m.rangePicker.overflow {
			input += "…"
		}
		if m.rangePicker.replace {
			hint = "  Type to replace · Backspace edit"
		}
		lines = append(lines, "  Custom range", fmt.Sprintf("  Days (1–%d): [%s]", maxCustomRangeDays, input), "", hint)
		if m.rangePicker.error != "" {
			lines = append(lines, StyleAmber.Render("  "+m.rangePicker.error))
		}
	} else {
		for i := 0; i <= len(TimePresets); i++ {
			label := "Custom days…"
			if i < len(TimePresets) {
				label = presetRangeLabel(i)
			}
			lines = append(lines, awarenessRow(glanceCursor(i == m.rangePicker.cursor)+" "+label, i == m.rangePicker.cursor, i, width))
		}
		lines = append(lines, "", "  ↑↓ choose · c custom")
	}
	lines = append(lines, "  Enter apply · Esc cancel")
	return fitGlanceLines(lines, width, height)
}
