package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const screenHeaderRows = 5
const screenFooterRows = 3

func (m Model) screenHeader(title string, width int) []string {
	left := "  BIGBOARD / " + displayText(title)
	repos := m.repositories
	if len(repos) == 0 {
		repos = m.loadedRepos
	}
	included, loaded := 0, 0
	for _, repo := range repos {
		if !m.excludedRepos[repo.ID] {
			included++
		}
	}
	for _, repo := range m.loadedRepos {
		if !m.excludedRepos[repo.ID] {
			loaded++
		}
	}
	right := fmt.Sprintf("%d repos", included)
	if loaded != included {
		right = fmt.Sprintf("%d/%d repos", loaded, included)
	}
	if m.version != "" {
		right += " · " + displayText(m.version)
	}
	right += "  "
	if width >= 90 {
		left = ansi.Truncate(left, max(1, width-ansi.StringWidth(right)-2), "…")
		left += strings.Repeat(" ", max(2, width-ansi.StringWidth(left)-ansi.StringWidth(right))) + right
	}
	left = worklistBand.Render(worklistLine(ansi.Truncate(left, max(1, width), "…"), width))
	bots := "bots shown"
	if m.hideBots {
		bots = "bots hidden"
	}
	context := "Range: " + m.rangeLabel() + " · t to change · " + bots + " · R refresh"
	if width < 70 {
		context = "Range: " + m.rangeLabel() + " · t to change"
	}
	if m.showPRs {
		context = "All open PRs · independent of local date, person and bot filters"
	}
	pr := strings.TrimPrefix(m.prStatus(), "GitHub")
	pr = strings.TrimPrefix(pr, " PRs")
	pr = strings.TrimSpace(strings.TrimPrefix(pr, " · "))
	pr, _, _ = strings.Cut(pr, " · p ")
	state, rest, _ := strings.Cut(pr, " · ")
	pr = "PRs: " + state + " · all dates"
	if rest != "" {
		pr += " · " + rest
	}
	return []string{left, StyleDimWhite.Render("  " + context), m.worklistSignal("  " + m.screenLocalFreshness("")), m.worklistSignal("  " + pr), "  " + worklistRule.Render(strings.Repeat("─", max(1, width-4)))}
}

func (m Model) screenLocalFreshness(repoID string) string {
	local := "Local: not scanned yet"
	stale := false
	if repoID != "" {
		if at := m.scannedAt[repoID]; !at.IsZero() {
			local = "Local: " + at.Local().Format("Jan 02 15:04 MST")
		}
		stale = m.staleRepos[repoID]
	} else {
		repos := m.repositories
		if len(repos) == 0 {
			repos = m.loadedRepos
		}
		stale = len(m.failedRepos) > 0
		var oldest time.Time
		scanned, included := 0, 0
		for _, repo := range repos {
			if m.excludedRepos[repo.ID] {
				continue
			}
			included++
			stale = stale || m.staleRepos[repo.ID]
			if at := m.scannedAt[repo.ID]; !at.IsZero() {
				scanned++
				if oldest.IsZero() || at.Before(oldest) {
					oldest = at
				}
			}
		}
		if !oldest.IsZero() {
			local = "Local: oldest scan " + oldest.Local().Format("Jan 02 15:04 MST")
		}
		if included > 0 && scanned < included {
			local = fmt.Sprintf("Local: PARTIAL · %d/%d repos scanned", scanned, included)
		}
		if included == 0 {
			local = "Local: no included repositories"
		}
	}
	if stale {
		local = "Local: STALE · " + strings.TrimPrefix(local, "Local: ")
	}
	if m.refreshing {
		local += " · Updating…"
	}
	if repoID == "" && len(m.failedRepos) > 0 {
		local += fmt.Sprintf(" · Update errors: %d · e details", len(m.failedRepos))
	}
	return local
}

func screenFinish(lines []string, footer, coverage string, width, height int) string {
	if height <= 0 {
		height = len(lines) + screenFooterRows
	}
	if height < screenHeaderRows+screenFooterRows+1 {
		return fitGlanceLines([]string{"Resize terminal · Esc back · q quit"}, max(1, width), height)
	}
	budget := height - screenFooterRows
	lines = append([]string(nil), lines[:min(len(lines), budget)]...)
	for len(lines) < budget {
		lines = append(lines, "")
	}
	lines = append(lines, "  "+worklistRule.Render(strings.Repeat("─", max(1, width-4))), StyleCyan.Render("  "+screenFooter(footer, width)), StyleDimWhite.Render("  "+coverage))
	return fitGlanceLines(lines, max(1, width), height)
}

func boardShortcuts(width int) string {
	if width < 70 {
		return "l/w/p"
	}
	if width < 110 {
		return "l/w/p boards"
	}
	return "l leaderboard · w worklist · p PRs"
}

func screenFooter(footer string, width int) string {
	if strings.HasPrefix(footer, "F1 help") {
		return footer
	}
	footer = strings.TrimPrefix(footer, "? help · ")
	return "? help · " + boardShortcuts(width) + " · " + footer
}

func (m Model) statisticsFooter() string {
	if m.viewMode == ViewOperative {
		if m.width < 60 {
			return "? help · ←/Esc back · PgUp/PgDn"
		}
		if m.width < 110 {
			return "? help · ←/Esc back · ↑↓ scroll · PgUp/PgDn"
		}
		return "? help · ←/Esc back · ↑↓ scroll · PgUp/PgDn · t range · R refresh · q quit"
	}
	if m.searching {
		return "F1 help · Enter accept · Esc clear"
	}
	if m.width < 70 {
		return "? help · ↑↓ · → open"
	}
	if m.width < 110 {
		return "? help · ↑↓ select · →/Enter open · / find · s sort"
	}
	return "? help · ↑↓ select · →/Enter open · / find · s/S sort · t range · b bots · R refresh · q quit"
}

func (m Model) screenCoverage() string {
	m.areaRepoID = ""
	coverage := strings.TrimSpace(ansi.Strip(m.glanceCoverageLine()))
	if m.width < 70 {
		bots := "bots shown"
		if m.hideBots {
			bots = "bots hidden"
		}
		coverage = bots + " · " + coverage
	}
	if m.width >= 100 {
		coverage += " · ages at local scan · fetch on launch and R"
	}
	return coverage
}

func (m Model) statisticsBodyBudget() int {
	return max(1, m.height-screenHeaderRows-screenFooterRows)
}
