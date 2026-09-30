package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// Repository associations describe Git history, not live presence. Contributor
// identities and shared commit attribution come from the same pipeline as stats.
type repositoryActivity struct {
	repo   git.Repository
	people []stats.AuthorStats
	latest time.Time
}

func (m Model) activityRepositories() []repositoryActivity {
	var repositories []repositoryActivity
	records := m.filteredRecords()
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	canonical := make(map[string]git.CommitRecord)
	for _, r := range stats.UniqueRecords(records) {
		if r.CommitID != "" {
			canonical[r.CommitID] = r
		}
	}
	for _, repo := range m.loadedRepos {
		if m.excludedRepos[repo.ID] {
			continue
		}
		repository := repositoryActivity{repo: repo}
		for _, author := range m.authors {
			if author.PerRepo[repo.Name] != nil {
				repository.people = append(repository.people, author)
			}
		}
		for _, record := range records {
			r := record
			if chosen, ok := canonical[record.CommitID]; ok {
				r = chosen
			}
			if record.RepoName == repo.Name && visible[stats.IdentityID(r)] && r.Date.After(repository.latest) {
				repository.latest = r.Date
			}
		}
		sort.Slice(repository.people, func(i, j int) bool {
			if repository.people[i].Name != repository.people[j].Name {
				return repository.people[i].Name < repository.people[j].Name
			}
			return repository.people[i].ID < repository.people[j].ID
		})
		repositories = append(repositories, repository)
	}
	sort.Slice(repositories, func(i, j int) bool {
		if !repositories[i].latest.Equal(repositories[j].latest) {
			return repositories[i].latest.After(repositories[j].latest)
		}
		return repositories[i].repo.Name < repositories[j].repo.Name
	})
	return repositories
}

func (m *Model) selectedRepository(repositories []repositoryActivity) int {
	for i, repository := range repositories {
		if repository.repo.ID == m.selectedRepoID {
			return i
		}
	}
	if len(repositories) > 0 {
		m.selectedRepoID = repositories[0].repo.ID
	}
	return 0
}

// evidence preserves all repository associations while selecting one canonical
// identity per object ID. A copied commit must not become two different people.
func (m Model) evidence(repoName, personID string) []git.CommitRecord {
	records := m.filteredRecords()
	inRepository := make(map[string]bool)
	for _, r := range records {
		if r.RepoName == repoName && r.CommitID != "" {
			inRepository[r.CommitID] = true
		}
	}
	visible := make(map[string]bool)
	for _, a := range m.authors {
		visible[a.ID] = true
	}
	var out []git.CommitRecord
	for _, r := range stats.UniqueRecords(records) {
		if !visible[stats.IdentityID(r)] || (personID != "" && stats.IdentityID(r) != personID) {
			continue
		}
		if repoName != "" && !inRepository[r.CommitID] && (r.CommitID != "" || r.RepoName != repoName) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].CommitID < out[j].CommitID
	})
	return out
}

func (m Model) handleAwarenessKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.normalizeAwarenessFocus()
	repositories := m.activityRepositories()
	ri := m.selectedRepository(repositories)
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "v":
		m.viewMode = ViewAggregate
	case "R":
		if !m.loading {
			m.loading = true
			m.resetPending()
			return m, m.loadCmds()
		}
	case "r":
		m.overlayExcluded = make(map[string]bool)
		for k, v := range m.excludedRepos {
			m.overlayExcluded[k] = v
		}
		m.overlayCursor = 0
		m.returnView = ViewAwareness
		m.viewMode = ViewRepoOverlay
	case "b":
		m.hideBots = !m.hideBots
		m.recomputeAuthors()
		m.personID = ""
		m.evidenceOffset = 0
	case "left", "h", "right", "l":
		delta := 1
		if msg.String() == "left" || msg.String() == "h" {
			delta = -1
		}
		m.timeIdx = max(0, min(len(TimePresets)-1, m.timeIdx+delta))
		m.recomputeAuthors()
		m.evidenceOffset = 0
	case "tab":
		m.awarenessPane = (m.awarenessPane + 1) % 3
	case "shift+tab":
		m.awarenessPane = (m.awarenessPane + 2) % 3
	case "esc":
		if m.personID != "" || m.awarenessPane != 0 {
			m.personID = ""
			m.awarenessPane = 0
			m.evidenceOffset = 0
		} else {
			return m.quit()
		}
	case "enter":
		m.awarenessPane = (m.awarenessPane + 1) % 3
	case "up", "k", "down", "j":
		delta := 1
		if msg.String() == "up" || msg.String() == "k" {
			delta = -1
		}
		if len(repositories) == 0 {
			break
		}
		switch m.awarenessPane {
		case 0:
			ri = max(0, min(len(repositories)-1, ri+delta))
			m.selectedRepoID = repositories[ri].repo.ID
			m.evidenceOffset = 0
		case 1:
			people := repositories[ri].people
			pi := -1
			for i, p := range people {
				if p.ID == m.personID {
					pi = i
				}
			}
			pi = max(-1, min(len(people)-1, pi+delta))
			m.personID = ""
			if pi >= 0 {
				m.personID = people[pi].ID
			}
			m.evidenceOffset = 0
		case 2:
			m.evidenceOffset = max(0, min(len(m.evidence(repositories[ri].repo.Name, m.personID))-1, m.evidenceOffset+delta))
		}
	}
	return m, nil
}

func paneLabel(label string, active bool, width int) string {
	if active {
		label = "▸ " + label
	}
	return RenderSectionHeader(label, width)
}

func awarenessRow(line string, selected bool, index, width int) string {
	style := StyleRowEven
	if index%2 != 0 {
		style = StyleRowOdd
	}
	if selected {
		style = StyleRowSelected
	}
	return style.Width(width).Render(ansi.Truncate(line, width, "…"))
}

func (m Model) awarenessSummary(repositoryCount int) string {
	connected := 0
	for _, a := range m.authors {
		if len(a.PerRepo) > 1 {
			connected++
		}
	}
	commits, _, _, _ := m.aggregateTotals()
	box := func(value int, label string, color lipgloss.TerminalColor) string {
		return StyleStatBox.Render(lipgloss.JoinVertical(lipgloss.Center, lipgloss.NewStyle().Foreground(color).Bold(true).Render(FormatNumber(value)), StyleStatLabel.Render(label)))
	}
	return "  " + lipgloss.JoinHorizontal(lipgloss.Top, box(repositoryCount, "REPOSITORIES", ColorCyan), "  ", box(len(m.authors), "CONTRIBUTORS", ColorGreen), "  ", box(connected, "CROSS-REPO", ColorMagenta), "  ", box(commits, "COMMITS", ColorCyan))
}

func (m Model) renderAwareness() string {
	width := max(1, m.width)
	height := max(1, m.height)
	if width < 40 || height < 18 {
		return ansi.Truncate("Resize to 40×18 · v stats · q quit", width, "…")
	}
	compact := height < 26
	m.normalizeAwarenessFocus()
	repositories := m.activityRepositories()
	ri := m.selectedRepository(repositories)
	timePicker := RenderTimePicker(m.timeIdx)
	if width < 70 {
		timePicker = "  Range: " + TimePresets[m.timeIdx].Label + " · ←→ change"
	}
	lines := renderBanner(min(width, bannerMinWidth-1))
	if height >= 30 {
		lines = strings.Split(RenderHeader(width, len(m.loadedRepos), m.excludedRepoCount(), m.version), "\n")
	}
	lines = append(lines, StyleSubtitle.Render("  WORK RELATIONSHIPS · Local Git history · author dates"), StyleDimWhite.Render("  Remote status unknown (no fetch) · Shared history, not live presence"), timePicker)
	if height >= 38 && width >= 95 {
		lines = append(lines, strings.Split(m.awarenessSummary(len(repositories)), "\n")...)
	}
	if len(m.failedRepos) > 0 {
		lines = append(lines, StyleAmber.Render("  Scan failures: "+displayText(strings.Join(m.failedRepos, ", "))+" · last-good data retained where available"))
	}
	if notice := m.historyNotice(); notice != "" {
		lines = append(lines, notice)
	}
	if len(repositories) == 0 {
		lines = append(lines, "", "  No included, readable repositories. Press r to include repositories or R to retry.")
	} else {
		repository := repositories[ri]
		repoHeading := "REPOSITORIES"
		if width < 70 {
			repoHeading = "REPOS"
		}
		lines = append(lines, paneLabel(fmt.Sprintf("%s  %d/%d · ◆ connected to focused contributor", repoHeading, ri+1, len(repositories)), m.awarenessPane == 0, width))
		// Reserve space for people, evidence and controls even on short terminals.
		repoRows := max(1, min(6, (height-len(lines)-11)/2))
		if compact && m.awarenessPane != 0 {
			repoRows = 1
		}
		start := max(0, min(ri-repoRows/2, len(repositories)-repoRows))
		for i := start; i < min(len(repositories), start+repoRows); i++ {
			r := repositories[i]
			marker := " "
			if i == ri {
				marker = "▸"
			}
			linked := " "
			for _, p := range r.people {
				if p.ID == m.personID {
					linked = "◆"
				}
			}
			names := make([]string, 0, len(r.people))
			for _, p := range r.people {
				names = append(names, displayText(p.Name))
			}
			summary := strings.Join(names, ", ")
			if summary == "" {
				summary = "no commits in range"
			}
			recency := "quiet"
			if !r.latest.IsZero() {
				recency = r.latest.Local().Format("2006-01-02 15:04")
			}
			line := fmt.Sprintf(" %s%s %s · %s · %s", marker, linked, displayText(r.repo.Name), recency, summary)
			lines = append(lines, awarenessRow(line, i == ri, i, width))
		}
		fresh := "not recorded"
		if t := m.scannedAt[repository.repo.ID]; !t.IsZero() {
			fresh = t.Local().Format("Jan 02 15:04:05 MST")
		}
		status := "scanned "
		if m.staleRepos[repository.repo.ID] {
			status = "STALE · last good scan "
		}
		lines = append(lines, StyleDimWhite.Render("  "+displayText(repository.repo.Name)+" · "+status+fresh))
		focusName := "all contributors"
		for _, a := range m.authors {
			if a.ID == m.personID {
				focusName = m.personLabel(a)
			}
		}
		lines = append(lines, paneLabel("PEOPLE  · "+displayText(focusName), m.awarenessPane == 1, width))
		people := []string{"all"}
		selected := 0
		if m.personID != "" {
			selected = -1
		}
		for i, p := range repository.people {
			name := m.personLabel(p)
			if p.Bot {
				name += " [BOT]"
			}
			if p.ID == m.personID {
				selected = i + 1
			}
			people = append(people, name)
		}
		if selected == -1 {
			lines = append(lines, "  Focus has no commits in this repository · Esc clears")
		}
		// Window the contributor list around focus; every identity is reachable.
		peopleStart := max(0, selected-1)
		peopleRows := 3
		if compact && m.awarenessPane != 1 {
			peopleStart = max(0, selected)
			peopleRows = 1
		}
		for i := peopleStart; i < min(len(people), peopleStart+peopleRows); i++ {
			marker := " "
			if i == selected {
				marker = "▸"
			}
			lines = append(lines, awarenessRow("  "+marker+" "+people[i], i == selected, i, width))
		}
		links := []string{}
		if m.personID != "" {
			for _, r := range repositories {
				for _, p := range r.people {
					if p.ID == m.personID {
						links = append(links, displayText(r.repo.Name))
						break
					}
				}
			}
		}
		if m.personID == "" {
			for _, p := range repository.people {
				other := []string{}
				for _, r := range repositories {
					if r.repo.ID != repository.repo.ID {
						for _, rp := range r.people {
							if rp.ID == p.ID {
								other = append(other, displayText(r.repo.Name))
								break
							}
						}
					}
				}
				if len(other) > 0 {
					links = append(links, m.personLabel(p)+" → "+strings.Join(other, ", "))
				}
			}
		}
		if len(links) > 0 {
			connectionHeading := "Connected repositories: "
			if width < 70 {
				connectionHeading = "Connected repos: "
			}
			lines = append(lines, StyleCyan.Render("  "+connectionHeading+strings.Join(links, " ↔ ")))
		}
		evidence := m.evidence(repository.repo.Name, m.personID)
		offset := max(0, min(m.evidenceOffset, len(evidence)-1))
		lines = append(lines, paneLabel(fmt.Sprintf("EVIDENCE  · %d commits · %s", len(evidence), displayText(repository.repo.Name)), m.awarenessPane == 2, width))
		budget := max(0, height-len(lines)-3)
		if width < 70 {
			budget /= 2
		}
		if len(evidence) == 0 {
			lines = append(lines, "  No matching commits in this repository and time range.")
		}
		for i := offset; i < min(len(evidence), offset+budget); i++ {
			r := evidence[i]
			oid := r.CommitID
			if len(oid) > 8 {
				oid = oid[:8]
			}
			subject := r.Subject
			if subject == "" {
				subject = "(no subject)"
			}
			if width < 70 {
				lines = append(lines, "  "+displayText(subject), fmt.Sprintf("    %s %s · %s", r.Date.Local().Format("2006-01-02"), displayText(oid), displayText(r.Author)))
			} else {
				lines = append(lines, fmt.Sprintf("  %s %s · %s · %s", r.Date.Local().Format("2006-01-02 15:04"), displayText(oid), displayText(r.Author), displayText(subject)))
			}
		}
		if len(evidence) > budget {
			lines = append(lines, StyleDimWhite.Render(fmt.Sprintf("  Evidence %d–%d/%d · Tab to evidence, ↑/↓ to scroll", offset+1, min(len(evidence), offset+budget), len(evidence))))
		}
	}
	help := "  Tab pane · ↑↓ select · ←→ range · Esc clear · v stats · r repos · b bots · R refresh · q quit"
	if width < 80 {
		help = "Tab pane ↑↓ move v stats R scan q quit"
	}
	lines = append(lines, help)
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	if len(lines) > height {
		lines = append(lines[:max(0, height-1)], Truncate("  Enlarge terminal · v stats · q quit", width))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) normalizeAwarenessFocus() {
	if m.personID == "" {
		return
	}
	for _, a := range m.authors {
		if a.ID == m.personID {
			return
		}
	}
	m.personID = ""
	m.evidenceOffset = 0
}

func (m Model) personLabel(a stats.AuthorStats) string {
	label := displayText(a.Name)
	for _, other := range m.authors {
		if other.Name == a.Name && other.ID != a.ID {
			return label + " (" + displayText(strings.TrimPrefix(a.ID, "email:")) + ")"
		}
	}
	return label
}
