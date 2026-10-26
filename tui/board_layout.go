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

func fitFooter(footer string, width int) string {
	parts := strings.Split(footer, " · ")
	has := func(prefix string) bool {
		for _, part := range parts {
			if strings.HasPrefix(part, prefix) {
				return true
			}
		}
		return false
	}
	help := "? help"
	if has("F1 help") {
		help = "F1 help"
	}
	var local, boards []string
	global := []string{help}
	editing := has("Enter apply") || has("Enter OK") || has("Enter accept")
	if editing {
		for _, part := range parts {
			if strings.HasPrefix(part, "Enter ") || strings.HasPrefix(part, "Esc ") {
				local = append(local, part)
			}
		}
	} else {
		if has("Tab ranking") {
			local = append(local, "Tab ranking", "↑↓ scroll")
		} else {
			if has("←/Esc back") || has("Esc back") {
				local = append(local, "Esc back")
			}
			if has("↑↓ scroll") {
				local = append(local, "↑↓ scroll")
			}
		}
		local = append(local, "f filters")
		if has("Tab contributors") {
			local = append(local, "Tab contributors")
		}
		if has("2 people") {
			local = append(local, "2 people")
		}
		if has("x ") {
			local = append(local, "x clear")
		}
		if has("Tab details") {
			local = append(local, "Tab details")
		}
		if has("/ ") {
			local = append(local, "/ find")
		}
		if has("→") {
			local = append(local, "Enter open")
		}
		if has("o ") {
			local = append(local, "o browser")
		}
		if has("t range") {
			local = append(local, "t date range")
		}
		if has("s/S sort") || has("s sort") {
			local = append(local, "s sort")
		}
		if len(local) < 4 && has("v repo PRs") {
			local = append(local, "v PRs")
		}
		if len(local) < 4 && has("Tab") && !has("Tab ranking") && !has("Tab contributors") {
			local = append(local, "Tab lenses")
		}
	}
	if !has("F1 help") && (!editing || width >= 70) {
		if width >= 160 {
			boards = []string{"c contributors", "a activity", "p PRs"}
		} else if width >= 70 {
			boards = []string{"c/a/p boards"}
		} else {
			boards = []string{"c/a/p"}
		}
	}
	var chosen []string
	groups := func() []string {
		var out []string
		for _, group := range [][]string{boards, chosen, global} {
			if len(group) > 0 {
				out = append(out, strings.Join(group, "  "))
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
	if !editing {
		global = append(global, "r refresh")
		if !fits() {
			global = global[:len(global)-1]
		}
	}
	var rendered []string
	for _, group := range [][]string{boards, chosen, global} {
		var hints []string
		for _, hint := range group {
			key, label, _ := strings.Cut(hint, " ")
			hints = append(hints, StyleCyan.Render(key)+" "+StyleDimWhite.Render(label))
		}
		if len(hints) > 0 {
			rendered = append(rendered, strings.Join(hints, "  "))
		}
	}
	return strings.Join(rendered, StyleDimCyan.Render(" │ "))
}
