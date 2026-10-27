package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type identityChoice struct {
	id, email, name string
}

type identityMergeState struct {
	open, review, saving       bool
	nameEdited                 bool
	choices                    []identityChoice
	selected                   map[string]bool
	primary, name, query       string
	edit, input, before, error string
	cursor                     int
	pending                    map[string]git.Identity
}

type identitySavedMsg struct {
	identities map[string]git.Identity
	primary    string
	err        error
}

func (m *Model) openIdentityMerge() {
	f := identityMergeState{open: true, selected: make(map[string]bool)}
	selected := m.selectedContributorID()
	for _, author := range stats.AggregateWithOptions(m.local.records, stats.AggregateOptions{BotIdentities: m.options.BotIdentities}) {
		email, _ := strings.CutPrefix(author.ID, "email:")
		if !strings.HasPrefix(author.ID, "email:") {
			email = ""
		}
		if email != "" {
			if _, err := git.NormalizeIdentities(map[string]git.Identity{email: {}}); err != nil {
				email = ""
			}
		}
		f.choices = append(f.choices, identityChoice{id: author.ID, email: email, name: author.Name})
		if author.ID == selected && email != "" {
			f.primary, f.selected[email], f.name = email, true, m.options.Identities[email].Name
		}
	}
	slices.SortFunc(f.choices, func(a, b identityChoice) int {
		if result := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)); result != 0 {
			return result
		}
		return strings.Compare(a.id, b.id)
	})
	for i, choice := range f.choices {
		if choice.id == selected {
			f.cursor = i
		}
	}
	m.identityMerge = f
}

func (f identityMergeState) selectedCount() int {
	count := 0
	for _, selected := range f.selected {
		if selected {
			count++
		}
	}
	return count
}

func (f identityMergeState) visibleChoices() []identityChoice {
	var result []identityChoice
	for _, choice := range f.choices {
		if strings.Contains(strings.ToLower(choice.name+" "+choice.email), strings.ToLower(f.query)) {
			result = append(result, choice)
		}
	}
	return result
}

func (f identityMergeState) reviewLines(width int) []string {
	emails := append([]string{f.primary}, f.pending[f.primary].Aliases...)
	var lines []string
	for i, email := range emails {
		mark := "[x] "
		if i == 0 {
			mark = "[P] "
		}
		wrapped := strings.Split(ansi.Hardwrap(displayText(email), max(1, width-4), true), "\n")
		for j, line := range wrapped {
			if j == 0 {
				lines = append(lines, mark+line)
			} else {
				lines = append(lines, "    "+line)
			}
		}
	}
	return lines
}

func identityMergeModalSize(width, height int, review bool) (panel, page, rowHeight int) {
	panel = max(6, min(92, width-2))
	rowHeight = 1
	if panel < 72 && !review {
		rowHeight = 2
	}
	page = max(1, min(12, (height-12)/rowHeight))
	return
}

func (m Model) handleIdentityMergeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.identityMerge
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	if f.edit != "" {
		switch msg.String() {
		case "enter":
			if f.edit == "name" {
				f.name, f.nameEdited = strings.TrimSpace(f.input), true
			}
			f.edit = ""
		case "esc":
			if f.edit == "find" {
				f.query = f.before
			}
			f.edit = ""
		case "backspace":
			if input := []rune(f.input); len(input) > 0 {
				f.input = string(input[:len(input)-1])
			}
		case "ctrl+u":
			f.input = ""
		default:
			if len(msg.Runes) > 0 && len([]rune(f.input))+len(msg.Runes) <= 200 {
				f.input += string(msg.Runes)
			}
		}
		if f.edit == "find" {
			f.query, f.cursor = f.input, 0
		}
		return m, nil
	}
	choices := f.visibleChoices()
	panel, page, _ := identityMergeModalSize(m.width, m.height, f.review)
	total := len(choices)
	if f.review {
		total = len(f.reviewLines(panel - 4))
	}
	f.cursor = max(0, min(f.cursor, total-1))
	if cursor, ok := glanceMove(msg.String(), f.cursor, total, page); ok {
		f.cursor = cursor
		return m, nil
	}
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc", "left":
		if f.review {
			f.review, f.cursor, f.error = false, 0, ""
		} else {
			m.identityMerge = identityMergeState{}
		}
	case "enter":
		if !f.review {
			var emails []string
			for email, selected := range f.selected {
				if selected {
					emails = append(emails, email)
				}
			}
			slices.Sort(emails)
			var pending map[string]git.Identity
			var err error
			if len(emails) == 1 && emails[0] == f.primary {
				pending, err = git.SetIdentityDisplayName(m.options.Identities, f.primary, f.name)
			} else {
				pending, err = git.MergeIdentities(m.options.Identities, f.primary, emails, f.name)
			}
			if err != nil {
				f.error = displayText(err.Error())
				return m, nil
			}
			f.pending, f.review, f.cursor, f.error = pending, true, 0, ""
			return m, nil
		}
		if m.options.SaveIdentities == nil {
			f.error = "Identity saving is unavailable in this session."
			return m, nil
		}
		f.saving, f.error = true, ""
		pending, primary, save := f.pending, f.primary, m.options.SaveIdentities
		return m, m.commands.wrap(func() tea.Msg {
			copy, err := git.NormalizeIdentities(pending)
			if err == nil {
				err = save(copy)
			}
			return identitySavedMsg{identities: pending, primary: primary, err: err}
		})
	case "/", "n":
		if !f.review {
			f.edit, f.input = "find", f.query
			if msg.String() == "n" {
				f.edit, f.input = "name", f.name
			}
			f.before, f.error = f.input, ""
		}
	case " ", "k":
		if f.review || len(choices) == 0 {
			return m, nil
		}
		choice := choices[f.cursor]
		if choice.email == "" {
			f.error = "This contributor has no valid email for saved identity changes."
			return m, nil
		}
		f.selected = maps.Clone(f.selected)
		if msg.String() == "k" || f.primary == "" {
			f.primary, f.selected[choice.email] = choice.email, true
			if !f.nameEdited {
				f.name = m.options.Identities[choice.email].Name
			}
		} else if choice.email == f.primary {
			f.error = "Choose another primary with k before removing this identity."
			return m, nil
		} else {
			f.selected[choice.email] = !f.selected[choice.email]
		}
		f.error = ""
	}
	return m, nil
}

func (m Model) applyIdentitySave(msg identitySavedMsg) (tea.Model, tea.Cmd) {
	if !m.identityMerge.saving {
		return m, nil
	}
	m.identityMerge.saving = false
	if msg.err != nil {
		m.identityMerge.error = "Not saved: " + displayText(msg.err.Error())
		return m, nil
	}
	m.options.Identities = msg.identities
	m.remapContributorSelections(msg.identities)
	m.identityMerge = identityMergeState{}
	m.identityReselect = "email:" + msg.primary
	m.resetPending()
	m.scan.localOnly = true
	if len(m.repositories) == 0 {
		m.finalizeLoad()
	}
	return m, m.loadCmds()
}

func (m *Model) remapContributorSelections(identities map[string]git.Identity) {
	aliases := make(map[string]string)
	for primary, identity := range identities {
		for _, alias := range identity.Aliases {
			aliases["email:"+alias] = "email:" + primary
		}
	}
	remap := func(id string) string {
		if canonical, ok := aliases[id]; ok {
			return canonical
		}
		return id
	}
	people := make(filterValues, 0, len(m.activityPeople))
	for _, person := range m.activityPeople {
		person = remap(person)
		if !slices.Contains(people, person) {
			people = append(people, person)
		}
	}
	m.activityPeople = people
	m.personID, m.activeAuthorID = remap(m.personID), remap(m.activeAuthorID)
	remapFrame := func(frame glanceFrame) glanceFrame {
		frame.personID = remap(frame.personID)
		for i, id := range frame.ids {
			frame.ids[i] = remap(id)
		}
		return frame
	}
	m.glance.frame = remapFrame(m.glance.frame)
	m.glance.stack = slices.Clone(m.glance.stack)
	for i, frame := range m.glance.stack {
		m.glance.stack[i] = remapFrame(frame)
	}
}

func (m Model) renderIdentityMerge(background string, width, height int) string {
	f := m.identityMerge
	if width < 36 || height < 16 {
		return renderCenteredModal(background, []string{"Contributor identities", "Resize terminal", "Esc cancel"}, max(6, width-2), width, height)
	}
	panel, page, rowHeight := identityMergeModalSize(width, height, f.review)
	inner := panel - 4
	title, description := "Contributor identities", "Set a display name, or select the same person's emails to merge."
	if f.review {
		title, description = "Review contributor merge", "These emails will become one contributor across every board."
		if f.selectedCount() == 1 {
			title, description = "Review display name", "Name only · email mappings stay unchanged."
		}
	}
	primary := f.primary
	if primary == "" {
		primary = "Choose with k"
	}
	name := f.name
	if f.review {
		name = f.pending[f.primary].Name
	}
	if name == "" {
		name = "Automatic (most frequent)"
		if !f.review && f.selectedCount() > 1 {
			name = "Keep primary's name, otherwise automatic"
		}
	}
	lines := []string{
		StyleTitle.Render(title),
		StyleDimWhite.Render(description),
		StyleCyan.Render("Primary: " + displayText(primary)),
		StyleDimWhite.Render("Name: " + displayText(name)),
		worklistRule.Render(strings.Repeat("─", inner)),
	}
	choices := f.visibleChoices()
	selected := max(0, min(f.cursor, len(choices)-1))
	start := max(0, min(selected-page/2, len(choices)-page))
	end := min(len(choices), start+page)
	position := fmt.Sprintf("%d–%d of %d · [P] primary", min(start+1, len(choices)), end, len(choices))
	if f.review {
		body := f.reviewLines(inner)
		offset := max(0, min(f.cursor, len(body)-page))
		last := min(len(body), offset+page)
		lines = append(lines, body[offset:last]...)
		position = fmt.Sprintf("Lines %d–%d of %d · %d emails", offset+1, last, len(body), len(f.pending[f.primary].Aliases)+1)
	}
	for i := start; !f.review && i < end; i++ {
		choice := choices[i]
		mark, email := "[ ]", choice.email
		if f.selected[email] {
			mark = "[x]"
		}
		if email == f.primary {
			mark = "[P]"
		}
		if email == "" {
			mark, email = "[—]", "No valid email · unavailable"
		}
		cursor := "  "
		if i == selected {
			cursor = "› "
		}
		var row []string
		if rowHeight == 1 {
			row = []string{cursor + mark + " " + padCells(displayText(choice.name), 24) + " " + displayText(email)}
		} else {
			row = []string{cursor + mark + " " + displayText(choice.name), "      " + displayText(email)}
		}
		for _, line := range row {
			line = padCells(line, inner)
			if i == selected {
				line = selectedRowBackground(StyleRowSelected.Render(line))
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < 5+page*rowHeight {
		lines = append(lines, "")
	}
	if f.query != "" && !f.review {
		position = "Find: " + displayText(f.query) + " · " + position
	}
	lines = append(lines, StyleDimWhite.Render(position))
	status := "n sets name · no merge selected"
	if count := f.selectedCount(); count > 1 {
		status = fmt.Sprintf("%d identities selected for merge", count)
	} else if count == 0 {
		status = "Choose a primary identity with k"
	}
	if f.review {
		status = "Confirmation saves this merge for future sessions."
		if f.selectedCount() == 1 {
			status = "Saves this name for future sessions."
		}
	}
	if f.error != "" {
		status = f.error
	}
	lines = append(lines, StyleAmber.Render(status))
	actions, navigation := "Space select · k primary · n name", "/ find · Enter review · Esc cancel · ? help"
	if f.review {
		actions, navigation = "Enter merge and save", "Esc edit choices · ↑↓ scroll · ? help"
		if f.selectedCount() == 1 {
			actions = "Enter save name"
		}
	}
	if f.edit != "" {
		label := "Find"
		if f.edit == "name" {
			label = "Display name"
		}
		actions = label + ": " + displayText(f.input) + "▏"
		navigation = "Enter keep edit · Esc cancel edit · F1 help"
	}
	if f.saving {
		actions, navigation = "Saving contributor merge…", ""
		if f.selectedCount() == 1 {
			actions = "Saving display name…"
		}
	}
	lines = append(lines, StyleTitle.Render(actions), StyleDimWhite.Render(navigation))
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, inner, "…")
	}
	return renderCenteredModal(background, lines, panel, width, height)
}
