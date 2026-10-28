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

func (m Model) prUpdatedReference(pr gh.PullRequest) time.Time {
	ref := m.prState.snapshots[pr.Repo].checked
	if ref.IsZero() {
		ref = time.Now()
	}
	return ref
}

func prInactiveAt(updated, ref time.Time) bool {
	return !updated.IsZero() && ref.Sub(updated) >= 14*24*time.Hour
}

func (m Model) prUpdatedAge(pr gh.PullRequest) string {
	ref := m.prUpdatedReference(pr)
	age := activityAge(pr.UpdatedAt, ref)
	if prInactiveAt(pr.UpdatedAt, ref) {
		age = StyleAmber.Render("!") + age
	}
	return m.lastLookAge(pr.UpdatedAt, age)
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
	if m.prUI.browserError != "" {
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
	authorStyle := StyleDimWhite
	if prOwnedBy(pr, m.prState.login) {
		author = "YOU " + author
		authorStyle = StyleCyan.Bold(true)
	}
	review, checks := prReviewLabel(pr.ReviewDecision), prCheckLabel(pr.CheckState)
	age := m.prUpdatedAge(pr)
	cursor := glanceCursor(selected) + " "
	if !selected {
		cursor = "  "
	}
	var lines []string
	if width >= 110 {
		l := resolvePRTable(width)
		lines = []string{cursor + StyleDimWhite.Render(padCells(repo, l.repo)) + " " + padCells(number, l.number) + " " + StyleDimWhite.Render(padCells(state, l.state)) + " " + authorStyle.Render(padCells(author, l.author)) + " " + titleStyle.Render(padCells(title, l.title)) + " " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, l.review)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, l.checks)) + " " + rightCells(age, l.updated)}
	} else if width >= 70 {
		inner := width - 4
		metaWidth := max(1, inner-2-13-9-7-3)
		metadata := "  " + authorStyle.Render(prRepositoryAuthor(repo, author, metaWidth)) + " " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, 13)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, 9)) + " " + rightCells(age, 7)
		lines = []string{cursor + state + " " + padCells(number, 7) + " " + titleStyle.Render(title), metadata}
	} else {
		metadata := "  " + authorStyle.Render(prRepositoryAuthor(repo, author, width-6))
		status := "  " + prReviewStyle(pr.ReviewDecision).Render(padCells(review, 13)) + " " + prCheckStyle(pr.CheckState).Render(padCells(checks, 9)) + " " + rightCells(age, 7)
		lines = []string{cursor + state + " " + padCells(number, 7) + " " + titleStyle.Render(title), metadata, status}
	}
	for i, line := range lines {
		lines[i] = awarenessRow("  "+line, selected, index, width)
	}
	return lines
}

func (m Model) prRepositoryLabels() map[string]string {
	names := make([]string, 0)
	for _, pr := range m.allPRs() {
		names = append(names, pr.Repo)
	}
	return shortRepositoryLabels(names)
}

func (m Model) prSelectedPreview(prs []gh.PullRequest, width int) []string {
	if len(prs) == 0 || width < 70 || m.height < 24 {
		return nil
	}
	budget := m.prPreviewBudget()
	if budget < 5 {
		return nil
	}
	pr := prs[max(0, min(m.prUI.row, len(prs)-1))]
	if m.prUI.previewFocus {
		return m.prExpandedPreview(pr, width, budget)
	}
	var notices []string
	if m.sinceLastLook(pr.UpdatedAt) {
		notices = append(notices, StyleCyan.Render("  + updated since last look"))
	}
	if prInactiveAt(pr.UpdatedAt, m.prUpdatedReference(pr)) {
		notices = append(notices, StyleAmber.Render("  ! Inactive · no PR update for 14+ days"))
	}
	inner := max(1, width-4)
	var extra []string
	if budget >= 6+len(notices) {
		extra = append(extra, "  "+ansi.Truncate(prPreviewMetadata(pr), inner, "…"))
	}
	if reasons := prAttentionReasons(pr, m.prState.login); len(reasons) > 0 && budget >= 7+len(notices) {
		extra = append(extra, StyleCyan.Render("  "+ansi.Truncate(strings.Join(reasons, " · "), inner, "…")))
	}
	identity := wrapGlanceText(fmt.Sprintf("SELECTED  %s #%d · Tab expand", displayText(pr.Repo), pr.Number), inner)
	identityBudget := max(1, budget-4-len(notices)-len(extra))
	if len(identity) > identityBudget {
		identity = identity[:identityBudget]
		identity[identityBudget-1] = "  " + ansi.Truncate(strings.TrimPrefix(identity[identityBudget-1], "  ")+"…", inner, "…")
	}
	author := githubIdentity(pr.Author) + " · ● Open"
	if pr.Draft {
		author = githubIdentity(pr.Author) + " · ◇ Draft"
	}
	title := wrapGlanceText(displayText(pr.Title), inner)
	titleBudget := max(1, budget-len(identity)-3-len(notices)-len(extra))
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
	lines = append(lines, notices...)
	lines = append(lines, extra...)
	lines = append(lines, StyleDimWhite.Render("  "+author), "  Review: "+prReviewStyle(pr.ReviewDecision).Render(prReviewLabel(pr.ReviewDecision))+" · Checks: "+prCheckStyle(pr.CheckState).Render(prCheckLabel(pr.CheckState)))
	return lines
}

func (m Model) prPreviewBudget() int {
	available := m.height - screenHeaderRows - screenFooterRows - 2 - 3*m.prListRowHeight()
	if m.prUI.browserError != "" {
		available--
	}
	if m.prUI.previewFocus {
		return min(available, min(24, max(10, (m.height-screenHeaderRows-screenFooterRows-2)/2)))
	}
	return min(10, available)
}

func prPreviewMetadata(pr gh.PullRequest) string {
	reviewers := "none requested"
	if len(pr.RequestedReviewers) > 0 {
		var names []string
		for _, reviewer := range pr.RequestedReviewers {
			names = append(names, githubIdentity(reviewer))
		}
		reviewers = strings.Join(names, ", ")
	}
	if !pr.RequestedReviewersComplete {
		if len(pr.RequestedReviewers) == 0 {
			reviewers = "unknown"
		} else {
			reviewers += " (partial)"
		}
	}
	files := fmt.Sprintf("%d", max(pr.FileCount, len(pr.Files)))
	if !pr.FilesComplete {
		if pr.FileCount == 0 && len(pr.Files) == 0 {
			files = "unknown"
		} else {
			files += " (paths partial)"
		}
	}
	conflicts := "unknown"
	switch pr.Mergeable {
	case "MERGEABLE":
		conflicts = "none reported"
	case "CONFLICTING":
		conflicts = "reported"
	}
	return "Conflicts: " + conflicts + " · Files: " + files + " · Reviewers: " + reviewers
}

func (m Model) prPreviewLineCount() int {
	prs := m.visiblePRs()
	if len(prs) == 0 {
		return 0
	}
	return len(m.prDetailLines(prs[max(0, min(m.prUI.row, len(prs)-1))], m.width))
}

func (m Model) prExpandedPreview(pr gh.PullRequest, width, budget int) []string {
	inner := max(1, width-4)
	content := m.prDetailLines(pr, width)
	page := max(1, budget-2)
	offset := max(0, min(m.prUI.previewOffset, len(content)-page))
	end := min(len(content), offset+page)
	heading := fmt.Sprintf("SELECTED  %s #%d · %d–%d/%d · Tab list", displayText(pr.Repo), pr.Number, offset+1, end, len(content))
	lines := []string{"  " + worklistRule.Render(strings.Repeat("─", inner)), "  " + StyleTitle.Render(ansi.Truncate(heading, inner, "…"))}
	for _, line := range content[offset:end] {
		lines = append(lines, "  "+line)
	}
	return lines
}

func (m Model) prFooter(prs []gh.PullRequest) footerHints {
	if m.prUI.searching {
		return searchFooter()
	}
	var local []footerHint
	if m.prUI.previewFocus && !m.prUI.detail {
		local = append(local, footerHint{"Tab", "list"}, footerHint{"↑↓", "scroll"})
	}
	if m.prUI.detail || m.prContext.active {
		local = append(local, footerHint{"Esc", "back"})
	}
	if m.prUI.detail {
		local = append(local, footerHint{"↑↓", "scroll"})
	}
	local = append(local, footerHint{"f", "filters"})
	if !m.prUI.detail {
		local = append(local, footerHint{"/", "find"}, footerHint{"Enter", "open"})
	}
	if len(prs) > 0 && prBrowserURL(prs[max(0, min(m.prUI.row, len(prs)-1))]) != "" {
		local = append(local, footerHint{"o", "browser"})
	}
	local = append(local, footerHint{"t", "date range"})
	return footerHints{local: local}
}

func (m Model) prListErrors() []string {
	var lines []string
	for _, repo := range m.local.repositories {
		if !m.repoInView(repo.ID) || m.prUI.repoID != "" && repo.ID != m.prUI.repoID {
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
	selected := max(0, min(m.prUI.row, len(prs)-1))
	start := max(0, min(selected-budget/2, len(prs)-budget))
	end := min(len(prs), start+budget)
	var sortHints []string
	if width >= 110 {
		sortHints = []string{"A author", "z sort"}
	}
	lines := []string{boardListHeading("OPEN PRs", m.prSortLabel(), sortHints, start, end, len(prs), width), prTableColumns(width)}
	if len(prs) == 0 {
		empty := "No PRs to display in this snapshot."
		if m.prUI.query != "" || !m.prUI.filters.empty() {
			empty = "No matching PRs. x clears filters and search."
		}
		_, bounded := m.rangeCutoffAt(time.Now())
		if bounded && len(m.allPRs()) > 0 {
			empty = "No matching PRs in range. t changes range; x clears PR filters."
		}
		if len(m.prUI.filters.mine) > 0 && m.prState.login == "" {
			empty = m.prMineStatus()
		}
		lines = append(lines, "  "+empty)
	}
	labels := m.prRepositoryLabels()
	for i := start; i < end; i++ {
		lines = append(lines, m.prTableRow(prs[i], labels[prs[i].Repo], i == selected, i, width)...)
	}
	remaining := m.height - screenHeaderRows - screenFooterRows - len(lines) - len(preview)
	if m.prUI.browserError != "" {
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
	return "PR updated time · Git contributor and bot filters do not apply"
}
