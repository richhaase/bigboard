package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type settingsState struct {
	open, required        bool
	step, cursor          int
	busy, error, login    string
	personalAccount       string
	personalAccounts      []string
	organizations         []string
	selectedOrganizations map[string]bool
	repositories          []git.Repository
	selectedRepositories  map[string]bool
	query, edit, input    string
	before                string
}

type settingsAccountMsg struct {
	generation uint64
	account    SettingsAccount
	err        error
}

type settingsRepositoriesMsg struct {
	generation   uint64
	repositories []git.Repository
	err          error
}

type settingsSavedMsg struct {
	generation   uint64
	config       SettingsConfig
	repositories []git.Repository
	err          error
}

type settingsReloadMsg struct{ generation uint64 }

func cloneSettingsConfig(cfg SettingsConfig) SettingsConfig {
	return SettingsConfig{PersonalAccount: cfg.PersonalAccount, Organizations: slices.Clone(cfg.Organizations), Exclude: slices.Clone(cfg.Exclude)}
}

func (m *Model) settingsRequest(kind string) {
	if m.cancelSettings != nil {
		m.cancelSettings()
	}
	parent := m.scanContext
	if parent == nil {
		parent = context.Background()
	}
	m.settingsContext, m.cancelSettings = context.WithCancel(parent)
	m.settingsGeneration++
	m.settings.busy, m.settings.error = kind, ""
}

func (m *Model) prepareSettings(required bool) {
	if m.options.Settings == nil {
		return
	}
	m.settings = settingsState{
		open: true, required: required,
		personalAccount:       m.options.Settings.Initial.PersonalAccount,
		organizations:         slices.Clone(m.options.Settings.Initial.Organizations),
		selectedOrganizations: make(map[string]bool), selectedRepositories: make(map[string]bool),
	}
	if m.settings.personalAccount != "" {
		m.settings.personalAccounts = []string{m.settings.personalAccount}
	}
	for _, org := range m.settings.organizations {
		m.settings.selectedOrganizations[org] = true
	}
	m.settingsRequest("account")
	m.settings.error = m.options.Settings.InitialError
}

func (m Model) settingsAccountCmd() tea.Cmd {
	ctx, generation, read := m.settingsContext, m.settingsGeneration, m.options.Settings.Account
	return m.commands.wrap(func() tea.Msg {
		if read == nil {
			return settingsAccountMsg{generation: generation, err: fmt.Errorf("GitHub account lookup is unavailable")}
		}
		account, err := read(ctx)
		return settingsAccountMsg{generation: generation, account: account, err: err}
	})
}

func (m Model) applySettingsAccount(msg settingsAccountMsg) Model {
	if !m.settings.open || msg.generation != m.settingsGeneration || m.settings.busy != "account" {
		return m
	}
	m.settings.busy = ""
	if msg.err != nil {
		m.settings.login, m.settings.error = "", settingsFailure(msg.err)
		return m
	}
	m.settings.login = msg.account.Login
	login := strings.ToLower(msg.account.Login)
	m.settings.personalAccounts = nil
	if login != "" {
		m.settings.personalAccounts = []string{login}
	}
	if saved := m.settings.personalAccount; saved != "" && saved != login {
		m.settings.personalAccounts = append(m.settings.personalAccounts, saved)
	}
	m.settings.organizations = append(slices.Clone(m.settings.organizations), msg.account.Organizations...)
	slices.Sort(m.settings.organizations)
	m.settings.organizations = slices.Compact(m.settings.organizations)
	m.settings.cursor = max(0, min(m.settings.cursor, len(m.settings.choices())-1))
	return m
}

func (m Model) applySettingsRepositories(msg settingsRepositoriesMsg) Model {
	if !m.settings.open || msg.generation != m.settingsGeneration || m.settings.busy != "repositories" {
		return m
	}
	m.settings.busy = ""
	if msg.err != nil {
		m.settings.error = settingsFailure(msg.err)
		return m
	}
	m.settings.repositories = slices.Clone(msg.repositories)
	slices.SortFunc(m.settings.repositories, func(a, b git.Repository) int {
		if a.PushedAt.IsZero() != b.PushedAt.IsZero() {
			if a.PushedAt.IsZero() {
				return 1
			}
			return -1
		}
		if order := b.PushedAt.Compare(a.PushedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	m.settings.selectedRepositories = maps.Clone(m.settings.selectedRepositories)
	for _, repo := range m.settings.repositories {
		if _, exists := m.settings.selectedRepositories[repo.ID]; !exists {
			m.settings.selectedRepositories[repo.ID] = !settingsExcluded(repo.ID, m.options.Settings.Initial.Exclude)
		}
	}
	m.settings.error = ""
	return m
}

func (m *Model) closeSettings() {
	if m.cancelSettings != nil {
		m.cancelSettings()
	}
	m.settingsGeneration++
	m.settings = settingsState{}
}

func (f settingsState) selectedOrgs() []string {
	var selected []string
	for name, included := range f.selectedOrganizations {
		if included {
			selected = append(selected, name)
		}
	}
	slices.Sort(selected)
	return selected
}

func (f settingsState) choices() []string {
	var choices []string
	for _, account := range f.personalAccounts {
		choices = append(choices, "@"+account)
	}
	choices = append(choices, f.organizations...)
	if f.step == 1 {
		choices = make([]string, len(f.repositories))
		for i, repo := range f.repositories {
			choices[i] = repo.ID
		}
	}
	var visible []string
	for _, value := range choices {
		label := value
		if f.step == 0 {
			label = settingsSourceLabel(value)
		}
		if strings.Contains(strings.ToLower(label), strings.ToLower(f.query)) {
			visible = append(visible, value)
		}
	}
	return visible
}

func settingsSourceLabel(source string) string {
	if strings.HasPrefix(source, "@") {
		return source + " · Personal repositories"
	}
	return source + " · Organization"
}

func (f settingsState) sources() SettingsConfig {
	return SettingsConfig{PersonalAccount: f.personalAccount, Organizations: f.selectedOrgs()}
}

func (f settingsState) sourceCount() int {
	count := len(f.selectedOrgs())
	if f.personalAccount != "" {
		count++
	}
	return count
}

func settingsFailure(err error) string {
	switch {
	case errors.Is(err, gh.ErrUnavailable):
		return "GitHub CLI (gh) is missing. Install gh, run gh auth login, then press r to retry."
	case errors.Is(err, gh.ErrAuthentication):
		return "GitHub sign-in is required. Run gh auth login in another terminal, then press r to retry."
	default:
		return displayText(err.Error()) + " · r retry"
	}
}

func settingsExcluded(id string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == id || pattern == filepath.Base(id) {
			return true
		}
		if matched, _ := filepath.Match(pattern, id); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(id)); matched {
			return true
		}
	}
	return false
}

func (m Model) discoverSettingsRepositories() (tea.Model, tea.Cmd) {
	sources := m.settings.sources()
	if len(sources.Organizations) == 0 && sources.PersonalAccount == "" {
		m.settings.error = "Choose your personal account or at least one organization."
		return m, nil
	}
	m.settings.step, m.settings.cursor, m.settings.query = 1, 0, ""
	m.settings.repositories = nil
	m.settingsRequest("repositories")
	ctx, generation, discover := m.settingsContext, m.settingsGeneration, m.options.Settings.Discover
	return m, m.commands.wrap(func() tea.Msg {
		if discover == nil {
			return settingsRepositoriesMsg{generation: generation, err: fmt.Errorf("repository discovery is unavailable")}
		}
		repos, err := discover(ctx, sources)
		return settingsRepositoriesMsg{generation: generation, repositories: repos, err: err}
	})
}

func (m Model) saveSettings() (tea.Model, tea.Cmd) {
	cfg := m.settings.sources()
	for _, repo := range m.settings.repositories {
		if !m.settings.selectedRepositories[repo.ID] {
			cfg.Exclude = append(cfg.Exclude, repo.ID)
		}
	}
	m.settingsRequest("saving")
	ctx, generation, save := m.settingsContext, m.settingsGeneration, m.options.Settings.Save
	repos := slices.Clone(m.settings.repositories)
	return m, m.commands.wrap(func() tea.Msg {
		if save == nil {
			return settingsSavedMsg{generation: generation, err: fmt.Errorf("settings saving is unavailable")}
		}
		selected, err := save(ctx, cfg, repos)
		return settingsSavedMsg{generation: generation, config: cfg, repositories: selected, err: err}
	})
}

func (m Model) applySavedSettings(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	if !m.settings.open || msg.generation != m.settingsGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.settings.busy, m.settings.error = "", displayText(msg.err.Error())
		return m, nil
	}
	settings := *m.options.Settings
	settings.Initial = cloneSettingsConfig(msg.config)
	settings.Required, settings.InitialError = false, ""
	options := m.options
	options.Settings = &settings
	options.PRProvider = m.prProvider
	if m.cancelScans != nil {
		m.cancelScans()
	}
	if m.cancelLocalScans != nil {
		m.cancelLocalScans()
	}
	if m.cancelPRs != nil {
		m.cancelPRs()
	}
	if m.cancelSettings != nil {
		m.cancelSettings()
	}
	previous := m.commands
	next := NewModelWithOptions(msg.repositories, m.sortField, nil, m.version, m.timeIdx, options)
	next.width, next.height = m.width, m.height
	next.customRangeDays = m.customRangeDays
	next.sinceLastLookRange = m.sinceLastLookRange
	next.scan.generation = m.scan.generation + 1
	next.prGeneration = m.prGeneration + 1
	next.cacheGeneration = m.cacheGeneration + 1
	next.settingsGeneration = m.settingsGeneration + 1
	next.showPRs = m.showPRs
	if m.viewMode == ViewAggregate || m.viewMode == ViewOperative {
		next.viewMode = ViewAggregate
	}
	generation := next.cacheGeneration
	return next, next.commands.wrap(func() tea.Msg {
		previous.wait()
		return settingsReloadMsg{generation: generation}
	})
}

func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.settings
	key := msg.String()
	if key == "ctrl+c" {
		return m.quit()
	}
	if f.busy == "saving" {
		return m, nil
	}
	if f.edit != "" {
		switch key {
		case "esc":
			if f.edit == "find" {
				f.query = f.before
			}
			f.edit, f.error = "", ""
		case "enter":
			if f.edit == "organization" {
				name, err := gh.CanonicalRepo(strings.TrimSpace(f.input) + "/repository")
				if err != nil {
					f.error = "Enter an organization name, such as example-org."
					return m, nil
				}
				org, _, _ := strings.Cut(name, "/")
				f.organizations = slices.Clone(f.organizations)
				if !slices.Contains(f.organizations, org) {
					f.organizations = append(f.organizations, org)
					slices.Sort(f.organizations)
				}
				f.selectedOrganizations = maps.Clone(f.selectedOrganizations)
				f.selectedOrganizations[org] = true
				f.query = ""
				f.cursor = slices.Index(f.choices(), org)
			}
			f.edit, f.error = "", ""
		case "backspace":
			if runes := []rune(f.input); len(runes) > 0 {
				f.input = string(runes[:len(runes)-1])
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
	if !f.required && (key == "c" || key == "a" || key == "p" || key == "l" || key == "w") {
		m.closeSettings()
		return m.switchBoard(key), nil
	}
	if key == "q" {
		return m.quit()
	}
	if key == "esc" || key == "left" {
		if f.step > 0 {
			if m.cancelSettings != nil {
				m.cancelSettings()
			}
			m.settingsGeneration++
			f.step--
			f.cursor, f.query, f.busy, f.error = 0, "", "", ""
		} else if f.required {
			return m.quit()
		} else {
			m.closeSettings()
		}
		return m, nil
	}
	if f.busy != "" {
		return m, nil
	}
	choices := f.choices()
	_, page := settingsModalSize(m.width, m.height, f.step)
	if f.step < 2 {
		if cursor, ok := glanceMove(key, f.cursor, len(choices), page); ok {
			f.cursor = cursor
			return m, nil
		}
	}
	switch key {
	case "r":
		if f.step == 1 {
			return m.discoverSettingsRepositories()
		}
		if f.step == 0 {
			m.settingsRequest("account")
			return m, m.settingsAccountCmd()
		}
	case "n":
		if f.step == 0 {
			f.edit, f.input, f.error = "organization", "", ""
		}
	case "/":
		if f.step < 2 {
			f.edit, f.before, f.input = "find", f.query, f.query
		}
	case " ":
		if f.step < 2 && f.cursor >= 0 && f.cursor < len(choices) {
			id := choices[f.cursor]
			if f.step == 0 {
				if account, personal := strings.CutPrefix(id, "@"); personal {
					if f.personalAccount == account {
						f.personalAccount = ""
					} else {
						f.personalAccount = account
					}
				} else {
					f.selectedOrganizations = maps.Clone(f.selectedOrganizations)
					f.selectedOrganizations[id] = !f.selectedOrganizations[id]
				}
			} else {
				f.selectedRepositories = maps.Clone(f.selectedRepositories)
				f.selectedRepositories[id] = !f.selectedRepositories[id]
			}
			f.error = ""
		}
	case "A":
		if f.step == 1 {
			f.selectedRepositories = maps.Clone(f.selectedRepositories)
			all := true
			for _, id := range choices {
				all = all && f.selectedRepositories[id]
			}
			for _, id := range choices {
				f.selectedRepositories[id] = !all
			}
		}
	case "enter":
		switch f.step {
		case 0:
			return m.discoverSettingsRepositories()
		case 1:
			selected := 0
			for _, repo := range f.repositories {
				if f.selectedRepositories[repo.ID] {
					selected++
				}
			}
			if selected == 0 || f.error != "" {
				if selected == 0 && f.error == "" {
					f.error = "Choose at least one repository before continuing."
				}
				return m, nil
			}
			f.step, f.cursor, f.query = 2, 0, ""
		case 2:
			return m.saveSettings()
		}
	}
	return m, nil
}

func settingsModalSize(width, height, step int) (int, int) {
	reserved := 16
	if step == 1 {
		reserved++
	}
	return max(6, min(88, width-2)), max(1, min(14, height-reserved))
}

func settingsPushAge(at, now time.Time) string {
	age := activityAge(at, now)
	if at.IsZero() || at.After(now) {
		return age
	}
	return age + " ago"
}

func (m Model) settingsHelp() screenHelp {
	if m.settings.edit != "" {
		return screenHelp{title: "Settings text", sections: []helpSection{{"Edit", []helpBinding{
			{"Type / Backspace", "Edit text; shortcut letters stay literal"}, {"Ctrl+U", "Clear text"}, {"Enter", "Accept edit"}, {"Esc", "Cancel edit"}, {"F1", "Show help while typing"}, {"Ctrl+C", "Quit BigBoard"},
		}}}}
	}
	if m.settings.busy == "saving" {
		return screenHelp{title: "Saving settings", sections: []helpSection{{"Saving", []helpBinding{{"Ctrl+C", "Quit BigBoard"}}}}}
	}
	back := "Previous step; cancel without saving from Sources"
	if m.settings.required {
		back = "Previous step; quit setup from Sources"
	}
	var bindings []helpBinding
	if m.settings.busy == "" {
		if m.settings.step < 2 {
			bindings = append(bindings, helpBinding{"↑ / ↓", "Select an option"}, helpBinding{"PgUp / PgDn", "Move a page"}, helpBinding{"Home / End", "First / last option"}, helpBinding{"Space", "Stage selection"}, helpBinding{"/", "Find options"})
			if m.settings.step == 0 {
				bindings = append(bindings, helpBinding{"n", "Add an organization by name"}, helpBinding{"r", "Retry GitHub access"})
			} else {
				bindings = append(bindings, helpBinding{"A", "Select / deselect all visible repositories"}, helpBinding{"r", "Retry repository discovery"})
			}
			bindings = append(bindings, helpBinding{"Enter", "Continue to the next step"})
		} else {
			bindings = append(bindings, helpBinding{"Enter", "Save and reload the selected repositories"})
		}
	}
	bindings = append(bindings, helpBinding{"Esc / ←", back}, helpBinding{"q / Ctrl+C", "Quit BigBoard"})
	if !m.settings.required {
		bindings = append(bindings, helpBinding{"a / c / p", "Cancel settings and switch boards"})
	}
	return screenHelp{title: "Setup and settings", sections: []helpSection{{"Choose and review", bindings}}, legends: []helpSection{{"Saved configuration", []helpBinding{{"Sources", "Choose your personal account, organizations, or both"}, {"Last push", "GitHub push activity across branches; newest first, unknown dates (—) last"}, {"Save", "Applies repository sources and exclusions to future sessions, then reloads the current board"}, {"GitHub access", "Personal sources include owned public and private repositories accessible to the signed-in GitHub CLI account"}, {"Local data", "Caches selected repositories; developer checkouts are untouched"}}}}}
}

func (m Model) renderSettings(background string, width, height int) string {
	f := m.settings
	if !settingsMinimum.fits(width, height) {
		return renderCenteredModal(background, []string{"BigBoard setup / settings", "Resize to 40 columns × 18 rows", "Esc back / cancel · Ctrl+C quit"}, max(6, width-2), width, height)
	}
	panel, page := settingsModalSize(width, height, f.step)
	inner := panel - 4
	title := "Settings"
	if f.required {
		title = "Welcome to BigBoard"
	}
	steps := []string{"1 Sources", "2 Repositories", "3 Review"}
	for i := range steps {
		if i == f.step {
			steps[i] = "[" + steps[i] + "]"
		}
	}
	stepLine := strings.Join(steps, "  ")
	if ansi.StringWidth(stepLine) > inner {
		stepLine = fmt.Sprintf("Step %d of 3 · %s", f.step+1, []string{"Sources", "Repositories", "Review"}[f.step])
	}
	account := "GitHub: checking access…"
	if f.login != "" {
		account = "GitHub: @" + displayText(f.login)
	} else if f.busy != "account" {
		account = "GitHub: account unavailable · r retry"
	}
	lines := []string{StyleTitle.Render(title), StyleCyan.Render(stepLine), StyleDimWhite.Render(account), worklistRule.Render(strings.Repeat("─", inner))}
	selected := f.sourceCount()
	if f.step == 1 || f.step == 2 {
		selected = 0
		for _, repo := range f.repositories {
			if f.selectedRepositories[repo.ID] {
				selected++
			}
		}
	}
	if f.step < 2 {
		description := "Choose one or more sources."
		if f.step == 1 {
			description = "Choose repositories to track across sessions."
		}
		lines = append(lines, description)
		prompt := "/ Find"
		if f.edit == "organization" {
			prompt = inputLine("Organization: ", f.input, inner)
		} else if f.query != "" || f.edit == "find" {
			prompt = "/ " + displayText(f.query)
			if f.edit == "find" {
				prompt = inputLine("/ ", f.input, inner)
			}
		}
		lines = append(lines, StyleDimWhite.Render(prompt))
		const pushWidth = 11
		pushedAt := make(map[string]time.Time, len(f.repositories))
		if f.step == 1 {
			lines = append(lines, StyleDimWhite.Render(padCells("      REPOSITORY", inner-pushWidth-2)+"  LAST PUSH ↓"))
			for _, repo := range f.repositories {
				pushedAt[repo.ID] = repo.PushedAt
			}
		}
		listStart := len(lines)
		now := time.Now()
		choices := f.choices()
		cursor := max(0, min(f.cursor, len(choices)-1))
		start := max(0, min(cursor-page/2, len(choices)-page))
		end := min(len(choices), start+page)
		for i := start; i < end; i++ {
			chosen := f.selectedOrganizations[choices[i]]
			label := choices[i]
			if f.step == 0 {
				label = settingsSourceLabel(label)
				if account, personal := strings.CutPrefix(choices[i], "@"); personal {
					chosen = f.personalAccount == account
				}
			}
			if f.step == 1 {
				chosen = f.selectedRepositories[choices[i]]
			}
			mark := "[ ] "
			if chosen {
				mark = "[x] "
			}
			prefix := "  "
			if i == cursor {
				prefix = "› "
			}
			row := padCells(prefix+mark+displayText(label), inner)
			if f.step == 1 {
				row = padCells(prefix+mark+displayText(label), inner-pushWidth-2) + "  " + rightCells(settingsPushAge(pushedAt[choices[i]], now), pushWidth)
			}
			if i == cursor {
				row = selectedRowBackground(StyleRowSelected.Render(row))
			}
			lines = append(lines, row)
		}
		if len(choices) == 0 {
			empty := "No matches. Clear the search to see all choices."
			if f.query == "" && f.step == 0 {
				empty = "No sources listed. Retry access or press n to add an org."
			} else if f.query == "" {
				empty = "No repositories found. Back to change sources."
			}
			if f.busy != "" {
				empty = "Loading…"
			}
			lines = append(lines, StyleDimWhite.Render(empty))
		}
		for len(lines) < listStart+page {
			lines = append(lines, "")
		}
		lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("%d–%d of %d · %d selected", min(start+1, len(choices)), end, len(choices), selected)))
	} else {
		lines = append(lines, "Save these choices for future sessions:", StyleTitle.Render(fmt.Sprintf("Sources %d · Repositories %d", f.sourceCount(), selected)))
		sourceNames := f.selectedOrgs()
		if f.personalAccount != "" {
			sourceNames = append([]string{"@" + f.personalAccount + " (personal)"}, sourceNames...)
		}
		orgLines := strings.Split(ansi.Wrap(displayText(strings.Join(sourceNames, ", ")), inner, ""), "\n")
		limit := max(1, min(3, height-19))
		if len(orgLines) > limit {
			orgLines = orgLines[:limit]
			orgLines[limit-1] = ansi.Truncate(orgLines[limit-1], inner-1, "") + "…"
		}
		lines = append(lines, orgLines...)
		lines = append(lines, "", "Save and refresh the selected repos.")
		if m.options.Settings.Group != "" {
			lines = append(lines, "This session uses group: "+displayText(m.options.Settings.Group))
		}
	}
	if f.error != "" {
		errorLines := strings.Split(ansi.Wrap(f.error, inner, ""), "\n")
		for _, line := range errorLines[:min(3, len(errorLines))] {
			lines = append(lines, StyleAmber.Render(line))
		}
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, worklistRule.Render(strings.Repeat("─", inner)))
	action := "Space select · Enter next"
	var hints string
	switch f.step {
	case 0:
		hints = "n add org · / find · r retry · ? help"
		back := "Esc cancel"
		if f.required {
			back = "Esc quit"
		}
		if inner >= 40 {
			action += " · " + back
		} else {
			hints = "n add · " + back + " · ? help"
		}
	case 1:
		hints = "A all/none · / find · Esc back · ? help"
		if inner < 40 {
			hints = "A all/none · Esc back · ? help"
		}
	default:
		action, hints = "Enter save & reload", "Esc back · ? help"
	}
	if f.edit != "" {
		action, hints = "Enter accept · Esc cancel edit", "Type to edit · Ctrl+U clear · F1 help"
	}
	if f.busy != "" {
		action = map[string]string{"account": "Checking GitHub access…", "repositories": "Loading repositories…", "saving": "Saving configuration…"}[f.busy]
	}
	lines = append(lines, StyleTitle.Render(action), StyleDimWhite.Render(hints))
	if len(lines)+2 < height {
		lines = append(lines, StyleDimWhite.Render("Config: "+displayText(m.options.Settings.ConfigPath)))
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, inner, "…")
	}
	return renderCenteredModal(background, lines, panel, width, height)
}
