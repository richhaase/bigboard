package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

var prSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9DDBA2"))

type prTableLayout struct{ repo, number, state, author, title, review, checks, updated int }

func resolvePRTable(width int) prTableLayout {
	l := prTableLayout{repo: max(18, width/6), number: 7, state: 5, author: min(24, max(14, width/10)), review: 13, checks: 9, updated: 7}
	l.title = max(1, width-4-2-7-l.repo-l.number-l.state-l.author-l.review-l.checks-l.updated)
	return l
}

func prReviewLabel(state string) string {
	switch state {
	case "APPROVED":
		return "Approved"
	case "REVIEW_REQUIRED":
		return "Needs review"
	case "CHANGES_REQUESTED":
		return "Changes req."
	default:
		return "Unknown"
	}
}

func prCheckLabel(state string) string {
	switch state {
	case "SUCCESS":
		return "Passing"
	case "FAILURE":
		return "Failing"
	case "ERROR":
		return "Error"
	case "PENDING":
		return "Pending"
	case "EXPECTED":
		return "Expected"
	default:
		return "Unknown"
	}
}

func prReviewStyle(state string) lipgloss.Style {
	switch state {
	case "CHANGES_REQUESTED":
		return lipgloss.NewStyle().Foreground(ColorRed)
	case "APPROVED":
		return prSuccessStyle
	case "REVIEW_REQUIRED":
		return StyleCyan
	default:
		return StyleAmber
	}
}

func prCheckStyle(state string) lipgloss.Style {
	if state == "SUCCESS" {
		return prSuccessStyle
	}
	if prCheckLabel(state) == "Unknown" {
		return StyleAmber
	}
	return worklistSignalStyle(state)
}

func (m Model) prUpdatedAge(pr gh.PullRequest) string {
	ref := m.prState.snapshots[pr.Repo].checked
	if ref.IsZero() {
		ref = time.Now()
	}
	return m.lastLookAge(pr.UpdatedAt, activityAge(pr.UpdatedAt, ref))
}

func (m Model) prListRowHeight() int {
	if m.width >= 110 {
		return 1
	}
	if m.width >= 70 {
		return 2
	}
	return 3
}

func (m Model) prListPageSize() int {
	return m.prListCapacity(len(m.prSelectedPreview(m.visiblePRs(), m.width)))
}

func (m Model) prListCapacity(previewRows int) int {
	budget := m.height - screenHeaderRows - screenFooterRows - 2 - previewRows
	if m.prBrowserError != "" {
		budget--
	}
	return max(1, budget/m.prListRowHeight())
}

func prTableColumns(width int) string {
	if width < 70 {
		return boardColumns("  STATE / PR # / TITLE · STATUS / AGE", width)
	}
	if width < 110 {
		return boardColumns("  STATE / PR # / TITLE · REPO / AUTHOR / STATUS / AGE", width)
	}
	l := resolvePRTable(width)
	return boardColumns("  "+padCells("REPOSITORY", l.repo)+" "+padCells("PR #", l.number)+" "+padCells("STATE", l.state)+" "+padCells("AUTHOR", l.author)+" "+padCells("TITLE", l.title)+" "+padCells("REVIEW", l.review)+" "+padCells("CHECKS", l.checks)+" "+rightCells("UPDATED", l.updated), width)
}

func prRepositoryAuthor(repo, author string, width int) string {
	authorWidth := min(ansi.StringWidth(author), max(12, width/3))
	repoWidth := min(ansi.StringWidth(repo), max(1, width-3-authorWidth))
	return padCells(repo, repoWidth) + " · " + padCells(author, max(1, width-3-repoWidth))
}

func (m Model) prTableRow(pr gh.PullRequest, repoLabel string, selected bool, index, width int) []string {
	number := fmt.Sprintf("#%d", pr.Number)
	title := displayText(pr.Title)
	state, titleStyle := "●", StyleAuthor
	if pr.Draft {
		state, titleStyle = "◇", StyleDimWhite
	}
	repo, author := displayText(repoLabel), githubIdentity(pr.Author)
	review, checks := prReviewLabel(pr.ReviewDecision), prCheckLabel(pr.CheckState)
	age := m.prUpdatedAge(pr)
	cursor := glanceCursor(selected) + " "
	if !selected {
		cursor = "  "
	}
	var lines []string
	if width >= 110 {
		l := resolvePRTable(width)
		lines = []string{cursor + StyleDimWhite.Render(padCells(repo, l.repo)) + " " + padCells(number, l.number) + " " + StyleDimWhite.Render(padCells(state, l.state)) + " " + StyleDimWhite.Render(padCells(author, l.author)) + " " + titleStyle.Render(padCells(title, l.title)) + " " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, l.review)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, l.checks)) + " " + rightCells(age, l.updated)}
	} else if width >= 70 {
		inner := width - 4
		metaWidth := max(1, inner-2-13-9-7-3)
		metadata := "  " + StyleDimWhite.Render(prRepositoryAuthor(repo, author, metaWidth)) + " " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, 13)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, 9)) + " " + rightCells(age, 7)
		lines = []string{cursor + state + " " + padCells(number, 7) + " " + titleStyle.Render(title), metadata}
	} else {
		metadata := "  " + prRepositoryAuthor(repo, author, width-6)
		status := "  " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, 13)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, 9)) + " " + rightCells(age, 7)
		lines = []string{cursor + state + " " + padCells(number, 7) + " " + titleStyle.Render(title), metadata, status}
	}
	for i, line := range lines {
		lines[i] = awarenessRow("  "+line, selected, index, width)
	}
	return lines
}

func (m Model) prRepositoryLabels() map[string]string {
	names := map[string]string{}
	duplicates := map[string]bool{}
	labels := map[string]string{}
	for _, pr := range m.allPRs() {
		_, name, ok := strings.Cut(pr.Repo, "/")
		if !ok {
			name = pr.Repo
		}
		if other, exists := names[name]; exists && other != pr.Repo {
			duplicates[name] = true
		}
		names[name] = pr.Repo
		labels[pr.Repo] = name
	}
	for repo, name := range labels {
		if duplicates[name] {
			labels[repo] = repo
		}
	}
	return labels
}

func (m Model) prSelectedPreview(prs []gh.PullRequest, width int) []string {
	if len(prs) == 0 || width < 70 || m.height < 24 {
		return nil
	}
	budget := m.height - screenHeaderRows - screenFooterRows - 2 - 3*m.prListRowHeight()
	if m.prBrowserError != "" {
		budget--
	}
	budget = min(10, budget)
	if budget < 5 {
		return nil
	}
	pr := prs[max(0, min(m.prRow, len(prs)-1))]
	bookmarkRows := 0
	if m.sinceLastLook(pr.UpdatedAt) {
		bookmarkRows = 1
	}
	inner := max(1, width-4)
	identity := wrapGlanceText(fmt.Sprintf("SELECTED  %s #%d", displayText(pr.Repo), pr.Number), inner)
	identityBudget := max(1, budget-4-bookmarkRows)
	if len(identity) > identityBudget {
		identity = identity[:identityBudget]
		identity[identityBudget-1] = "  " + ansi.Truncate(strings.TrimPrefix(identity[identityBudget-1], "  ")+"…", inner, "…")
	}
	author := githubIdentity(pr.Author) + " · ● Open"
	if pr.Draft {
		author = githubIdentity(pr.Author) + " · ◇ Draft"
	}
	title := wrapGlanceText(displayText(pr.Title), inner)
	titleBudget := max(1, budget-len(identity)-3-bookmarkRows)
	if len(title) > titleBudget {
		title = title[:titleBudget]
		suffix := "… · Enter full detail"
		title[titleBudget-1] = "  " + ansi.Truncate(strings.TrimPrefix(title[titleBudget-1], "  "), max(1, inner-ansi.StringWidth(suffix)), "") + suffix
	}
	lines := []string{"  " + worklistRule.Render(strings.Repeat("─", inner))}
	for _, line := range identity {
		lines = append(lines, StyleTitle.Render(line))
	}
	lines = append(lines, title...)
	if bookmarkRows > 0 {
		lines = append(lines, StyleCyan.Render("  + updated since last look"))
	}
	lines = append(lines, StyleDimWhite.Render("  "+author), "  Review: "+prReviewStyle(pr.ReviewDecision).Render(prReviewLabel(pr.ReviewDecision))+" · Checks: "+prCheckStyle(pr.CheckState).Render(prCheckLabel(pr.CheckState)))
	return lines
}

func (m Model) prFooter(prs []gh.PullRequest) footerHints {
	if m.prSearching {
		return searchFooter()
	}
	var local []footerHint
	if m.prDetail || m.prContext.active {
		local = append(local, footerHint{"Esc", "back"})
	}
	if m.prDetail {
		local = append(local, footerHint{"↑↓", "scroll"})
	}
	local = append(local, footerHint{"f", "filters"})
	if !m.prDetail {
		local = append(local, footerHint{"/", "find"}, footerHint{"Enter", "open"})
	}
	if len(prs) > 0 && prBrowserURL(prs[max(0, min(m.prRow, len(prs)-1))]) != "" {
		local = append(local, footerHint{"o", "browser"})
	}
	local = append(local, footerHint{"t", "date range"})
	return footerHints{local: local}
}

func (m Model) prListErrors() []string {
	var lines []string
	for _, repo := range m.local.repositories {
		if !m.repoInView(repo.ID) || m.prRepoID != "" && repo.ID != m.prRepoID {
			continue
		}
		err := m.prState.errors[repo.ID]
		if err == nil {
			err = m.prState.snapshots[m.prState.repositories[repo.ID]].err
		}
		if err != nil {
			lines = append(lines, "  "+displayText(repo.Name)+": "+displayText(err.Error()))
		}
	}
	return lines
}

func (m Model) renderPRBoardRows(prs []gh.PullRequest, width int) []string {
	preview := m.prSelectedPreview(prs, width)
	budget := m.prListCapacity(len(preview))
	selected := max(0, min(m.prRow, len(prs)-1))
	start := max(0, min(selected-budget/2, len(prs)-budget))
	end := min(len(prs), start+budget)
	lines := []string{boardListHeading("OPEN PRs", m.prSortLabel(), nil, start, end, len(prs), width), prTableColumns(width)}
	if len(prs) == 0 {
		empty := "No PRs to display in this snapshot."
		if m.prQuery != "" || !m.prFilters.empty() {
			empty = "No matching PRs. x clears filters and search."
		}
		_, bounded := m.rangeCutoffAt(time.Now())
		if bounded && len(m.allPRs()) > 0 {
			empty = "No matching PRs in range. t changes range; x clears PR filters."
		}
		if len(m.prFilters.mine) > 0 && m.prState.login == "" {
			empty = m.prMineStatus()
		}
		lines = append(lines, "  "+empty)
	}
	labels := m.prRepositoryLabels()
	for i := start; i < end; i++ {
		lines = append(lines, m.prTableRow(prs[i], labels[prs[i].Repo], i == selected, i, width)...)
	}
	remaining := m.height - screenHeaderRows - screenFooterRows - len(lines) - len(preview)
	if m.prBrowserError != "" {
		remaining--
	}
	remaining = max(0, remaining)
	if remaining > 0 {
		errors := m.prListErrors()
		count := min(remaining, len(errors))
		lines = append(lines, errors[:count]...)
		remaining -= count
	}
	if len(preview) > 0 {
		for range remaining {
			lines = append(lines, "")
		}
		lines = append(lines, preview...)
	}
	return lines
}

func (m Model) prBoardCoverage() string {
	legend := ""
	if marker := m.bookmarkMarkerLegend(); marker != "" {
		legend = marker + " · "
	}
	return legend + "PR updated time · " + m.rangeLabel() + " · " + m.prScopeLabel() + " · ◇ draft · ● open · contributor and bot filters do not apply"
}
