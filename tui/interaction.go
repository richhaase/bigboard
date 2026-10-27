package tui

import "github.com/charmbracelet/x/ansi"

type modalMinimum struct {
	width, height int
}

var (
	settingsMinimum   = modalMinimum{40, 18}
	identityMinimum   = modalMinimum{36, 16}
	repositoryMinimum = modalMinimum{30, 10}
	filterMinimum     = modalMinimum{40, 18}
)

func (minimum modalMinimum) fits(width, height int) bool {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	return width >= minimum.width && height >= minimum.height
}

func (m Model) modalMinimum() modalMinimum {
	switch {
	case m.settings.open:
		return settingsMinimum
	case m.identityMerge.open:
		return identityMinimum
	case m.filters.open:
		return filterMinimum
	case m.viewMode == ViewRepoOverlay:
		return repositoryMinimum
	default:
		return modalMinimum{}
	}
}

func (m Model) blocksHiddenModalInput(key string) bool {
	if m.showHelp || m.modalMinimum().fits(m.width, m.height) {
		return false
	}
	switch key {
	case "ctrl+c", "esc", "f1":
		return false
	case "?", "q", "left", "a", "c", "p", "l", "w":
		return m.helpSearchActive()
	default:
		return true
	}
}

func inputLine(prefix, input string, width int) string {
	if width <= 0 {
		return ""
	}
	prefix = ansi.Truncate(prefix, max(0, width-2), "")
	room := max(0, width-ansi.StringWidth(prefix)-1)
	input = displayText(input)
	cells := ansi.StringWidth(input)
	if cells > room {
		marker := ""
		if room > 1 {
			marker, room = "…", room-1
		}
		for cut := cells - room; ; cut++ {
			tail := ansi.TruncateLeft(input, cut, "")
			if ansi.StringWidth(tail) <= room {
				input = marker + tail
				break
			}
		}
	}
	return prefix + input + "▌"
}
