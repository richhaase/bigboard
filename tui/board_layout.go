package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func boardListHeading(label, sort string, summary []string, start, end, total, width int) string {
	inner := max(1, width-4)
	position := fmt.Sprintf("%d–%d of %d", start+1, end, total)
	if total == 0 {
		position = "0 of 0"
	}
	left := label + " · " + sort
	available := max(1, inner-ansi.StringWidth(position)-2)
	if ansi.StringWidth(left) > available {
		left = sort
	}
	for _, part := range summary {
		if ansi.StringWidth(left+" · "+part) <= available {
			left += " · " + part
		}
	}
	return "  " + StyleTitle.Render(padCells(left, available)+"  "+position)
}

func boardColumns(text string, width int) string {
	return "  " + StyleDimWhite.Render(padCells(text, max(1, width-4)))
}

type footerHint struct {
	key   string
	label string
}

type footerHints struct {
	local   []footerHint
	editing bool
}

func searchFooter() footerHints {
	return footerHints{editing: true, local: []footerHint{{"Enter", "accept"}, {"Esc", "cancel"}}}
}

func focusedContributorsFooter() footerHints {
	return footerHints{local: []footerHint{{"Tab", "ranking"}, {"↑↓", "scroll"}, {"f", "filters"}}}
}

func plainFooterGroup(hints []footerHint) string {
	parts := make([]string, len(hints))
	for i, hint := range hints {
		parts[i] = hint.key
		if hint.label != "" {
			parts[i] += " " + hint.label
		}
	}
	return strings.Join(parts, "  ")
}

func fitFooter(footer footerHints, width int) string {
	local := footer.local
	var boards []footerHint
	global := []footerHint{{"?", "help"}}
	if footer.editing {
		global = []footerHint{{"F1", "help"}}
	} else if width >= 160 {
		boards = []footerHint{{"c", "contributors"}, {"a", "activity"}, {"p", "PRs"}}
	} else if width >= 70 {
		boards = []footerHint{{"c/a/p", "boards"}}
	} else {
		boards = []footerHint{{"c/a/p", ""}}
	}
	var chosen []footerHint
	groups := func() []string {
		var out []string
		for _, group := range [][]footerHint{boards, chosen, global} {
			if len(group) > 0 {
				out = append(out, plainFooterGroup(group))
			}
		}
		return out
	}
	fits := func() bool { return ansi.StringWidth(strings.Join(groups(), " │ ")) <= max(1, width-4) }
	if len(local) >= 2 {
		chosen = local[:2]
		if !fits() {
			boards = nil
		}
		chosen = nil
	}
	for _, hint := range local {
		if len(chosen) == 5 {
			break
		}
		chosen = append(chosen, hint)
		if !fits() {
			chosen = chosen[:len(chosen)-1]
		}
	}
	if !footer.editing {
		global = append(global, footerHint{"r", "refresh"})
		if !fits() {
			global = global[:len(global)-1]
		}
	}
	var rendered []string
	for _, group := range [][]footerHint{boards, chosen, global} {
		var hints []string
		for _, hint := range group {
			hints = append(hints, StyleCyan.Render(hint.key)+" "+StyleDimWhite.Render(hint.label))
		}
		if len(hints) > 0 {
			rendered = append(rendered, strings.Join(hints, "  "))
		}
	}
	return strings.Join(rendered, StyleDimCyan.Render(" │ "))
}
