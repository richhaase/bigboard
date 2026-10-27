package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func prDetailTime(at time.Time) string {
	if at.IsZero() {
		return "Unknown"
	}
	return at.Local().Format("Jan 02, 2006 15:04 MST")
}

func (m Model) prDetailLines(pr gh.PullRequest, width int) []string {
	inner := max(1, width-4)
	var lines []string
	add := func(text string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(displayText(text), inner, ""), "\n") {
			lines = append(lines, style.Render(line))
		}
	}
	section := func(title string) { lines = append(lines, ""); add(title, StyleTitle) }
	add(pr.Title, StyleAuthor.Bold(true))
	state := "● Open"
	if pr.Draft {
		state = "◇ Draft"
	}
	add(githubIdentity(pr.Author)+" · "+state, StyleDimWhite)
	lines = append(lines, "")
	lines = append(lines, StyleDimWhite.Render(padCells("Review", 10))+prReviewStyle(pr.ReviewDecision).Render(prReviewLabel(pr.ReviewDecision)))
	lines = append(lines, StyleDimWhite.Render(padCells("Checks", 10))+prCheckStyle(pr.CheckState).Render(prCheckLabel(pr.CheckState)))
	conflicts, conflictStyle := "Unknown", StyleAmber
	switch pr.Mergeable {
	case "MERGEABLE":
		conflicts, conflictStyle = "None reported", StyleDimWhite
	case "CONFLICTING":
		conflicts, conflictStyle = "Conflicts reported", lipgloss.NewStyle().Foreground(ColorRed)
	}
	lines = append(lines, StyleDimWhite.Render(padCells("Conflicts", 10))+conflictStyle.Render(conflicts))
	add("Updated   "+prDetailTime(pr.UpdatedAt)+" (PR activity)", StyleDimWhite)
	snapshot := m.prState.snapshots[pr.Repo]
	add("Checked   "+prDetailTime(snapshot.checked), StyleDimWhite)
	add("Checks and conflicts do not establish merge readiness.", StyleDimWhite)
	if snapshot.err != nil || snapshot.partial || !pr.FilesComplete || !pr.ContextComplete || !pr.PreviousPathsComplete {
		section("Coverage")
		if snapshot.err != nil || snapshot.partial {
			add("STALE/PARTIAL snapshot · retained evidence may no longer be current", StyleAmber)
			if !snapshot.lastGood.IsZero() {
				add("Last complete fetch "+prDetailTime(snapshot.lastGood), StyleDimWhite)
			}
			if snapshot.err != nil {
				add(snapshot.err.Error(), StyleAmber)
			}
		}
		if !pr.FilesComplete {
			add("PARTIAL files · other areas may be affected", StyleAmber)
		}
		if !pr.ContextComplete {
			add("PARTIAL review/context data · unknown is not approval", StyleAmber)
		}
		if !pr.PreviousPathsComplete {
			add("Rename origins unavailable · current paths shown", StyleDimWhite)
		}
	}
	section("Reviewers")
	requested := "None"
	if len(pr.RequestedReviewers) > 0 {
		var names []string
		for _, name := range pr.RequestedReviewers {
			names = append(names, githubIdentity(name))
		}
		requested = strings.Join(names, ", ")
	} else if !pr.RequestedReviewersComplete {
		requested = "Unknown"
	}
	if !pr.RequestedReviewersComplete {
		requested += " · partial data"
	}
	add("Requested: "+requested, StyleDimWhite)
	if len(pr.Reviews) == 0 {
		text := "No reviews recorded"
		if !pr.ReviewsComplete {
			text = "Review history unavailable or incomplete"
		}
		add(text, StyleDimWhite)
	}
	for _, review := range pr.Reviews {
		label, style := prReviewLabel(review.State), prReviewStyle(review.State)
		switch review.State {
		case "COMMENT", "COMMENTED":
			label, style = "Commented", StyleDimWhite
		case "DISMISSED":
			label, style = "Dismissed", StyleDimWhite
		case "PENDING":
			label, style = "Pending", StyleAmber
		}
		add(githubIdentity(review.Author)+" · "+label, style)
	}
	if len(pr.Reviews) > 0 && !pr.ReviewsComplete {
		add("PARTIAL review history", StyleAmber)
	}
	section("Affected areas")
	mapped := false
	for _, repo := range m.local.repositories {
		if !m.excludedRepos[repo.ID] && m.prState.repositories[repo.ID] == pr.Repo {
			var names []string
			for _, area := range m.prAreas(repo, pr) {
				names = append(names, area.Name)
			}
			if len(names) > 0 {
				add(repo.Name+": "+strings.Join(names, ", "), StyleAuthor)
				mapped = true
			}
		}
	}
	if !mapped {
		add("No areas mapped from known files", StyleDimWhite)
	}
	section("GitHub link")
	add(pr.URL, StyleDimWhite)
	count := fmt.Sprintf("Changed files · %d", len(pr.Files))
	if !pr.FilesComplete {
		count += " known · PARTIAL"
	}
	section(count)
	if len(pr.Files) == 0 {
		text := "No changed files reported"
		if !pr.FilesComplete {
			text = "Changed files unavailable or incomplete"
		}
		add(text, StyleDimWhite)
	}
	for _, file := range pr.Files {
		path := file.Path
		if !m.options.IncludeGenerated && git.IsGeneratedPath(path) {
			path += " [excluded from local statistics]"
		}
		for _, line := range strings.Split(ansi.Hardwrap(displayText(path), max(1, inner-2), true), "\n") {
			lines = append(lines, "  "+line)
		}
	}
	return lines
}

func (m Model) prDetailModalSize() (int, int) {
	width := max(6, min(96, m.width-2))
	budget := max(1, min(24, m.height-9))
	if m.prBrowserError != "" {
		budget = max(1, budget-1)
	}
	return width, budget
}

func (m Model) prDetailBudget() int {
	_, budget := m.prDetailModalSize()
	return budget
}

func (m Model) prDetailMaxOffset() int {
	prs := m.visiblePRs()
	if len(prs) == 0 {
		return 0
	}
	pr := prs[max(0, min(m.prRow, len(prs)-1))]
	width, budget := m.prDetailModalSize()
	return max(0, len(m.prDetailLines(pr, width))-budget)
}

func (m Model) renderPRDetailModal(background string, pr gh.PullRequest, width, height int) string {
	m.width, m.height = width, height
	panelWidth, page := m.prDetailModalSize()
	inner := panelWidth - 4
	lines := m.prDetailLines(pr, panelWidth)
	offset := max(0, min(m.prOffset, len(lines)-page))
	end := min(len(lines), offset+page)
	heading := fmt.Sprintf("PR #%d · %s", pr.Number, displayText(pr.Repo))
	snapshot := m.prState.snapshots[pr.Repo]
	status, style := "Range: "+m.rangeLabel()+" · PR updated time", StyleDimWhite
	if !pr.ContextComplete || !pr.FilesComplete || !pr.RequestedReviewersComplete || !pr.ReviewsComplete {
		status, style = "PARTIAL context · details below", StyleAmber
	}
	if snapshot.err != nil || snapshot.partial {
		status, style = "STALE/PARTIAL · retained snapshot", StyleAmber
	}
	content := []string{StyleTitle.Render(ansi.Truncate(heading, inner, "…")), style.Render(ansi.Truncate(status, inner, "…")), StyleDimCyan.Render(strings.Repeat("─", inner))}
	content = append(content, lines[offset:end]...)
	for len(content) < min(page, len(lines))+3 {
		content = append(content, "")
	}
	if m.prBrowserError != "" {
		content = append(content, StyleAmber.Render(ansi.Truncate(displayText(m.prBrowserError), inner, "…")))
	}
	position := fmt.Sprintf("%d–%d of %d · ↑↓ / PgUp / PgDn", offset+1, end, len(lines))
	content = append(content, StyleDimWhite.Render(ansi.Truncate(position, inner, "…")))
	controls := "Esc close"
	if prBrowserURL(pr) != "" {
		controls += " · o browser"
	}
	if inner >= 50 {
		controls += " · r refresh"
	}
	controls += " · ? help"
	content = append(content, StyleCyan.Render(controls))
	return renderCenteredModal(background, content, panelWidth, width, height)
}
