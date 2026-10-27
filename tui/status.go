package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type dataStatus struct {
	label, compact      string
	warning, refreshing bool
	fetching            bool
}

func (m Model) statusRepositories() []git.Repository {
	repos := m.repositories
	if len(repos) == 0 {
		repos = m.local.repositories
	}
	var included []git.Repository
	for _, repo := range repos {
		if !m.excludedRepos[repo.ID] {
			included = append(included, repo)
		}
	}
	return included
}

func (m Model) localRepositoryFailed(repo git.Repository) bool {
	return m.local.staleRepos[repo.ID] || m.local.errors[repo.ID] != "" || slices.Contains(m.local.failed, repo.Name)
}

func (m Model) gitDataStatus() dataStatus {
	if m.startup.pending && !m.startup.cachedAt.IsZero() {
		return dataStatus{label: "cached", compact: "cached", refreshing: m.scan.running, fetching: m.scan.phase == scanStartupFetch}
	}
	repos := m.statusRepositories()
	scanned, failed := 0, 0
	for _, repo := range repos {
		if !m.local.scannedAt[repo.ID].IsZero() {
			scanned++
		}
		if m.localRepositoryFailed(repo) {
			failed++
		}
	}
	status := dataStatus{label: "scanned", compact: "scanned", refreshing: m.scan.running}
	switch {
	case len(repos) == 0:
		status.label, status.compact = "none tracked", "none"
	case failed > 0:
		status.label, status.compact, status.warning = "STALE", "STALE", true
		if scanned == 0 {
			status.label, status.compact = "FAILED", "FAILED"
		} else if scanned < len(repos) {
			status.label += "/PARTIAL"
		}
	case scanned < len(repos):
		status.label, status.compact, status.warning = "PARTIAL", "PARTIAL", true
		if scanned == 0 {
			status.label, status.compact = "not scanned", "unknown"
		}
	}
	return status
}

func (m Model) prDataStatus() dataStatus {
	eligible, snapshots := 0, 0
	failed, partial := false, false
	for _, repo := range m.statusRepositories() {
		err := m.prState.errors[repo.ID]
		if errors.Is(err, gh.ErrUnsupportedOrigin) {
			continue
		}
		eligible++
		snapshot, exists := m.prState.snapshots[m.prState.repositories[repo.ID]]
		if exists {
			snapshots++
		}
		failed = failed || err != nil || snapshot.err != nil
		partial = partial || !exists || snapshot.partial
	}
	status := dataStatus{label: "checked", compact: "checked", refreshing: m.prState.loading}
	switch {
	case eligible == 0:
		status.label, status.compact = "not applicable", "n/a"
	case failed:
		status.label, status.compact, status.warning = "STALE", "STALE", true
		if snapshots == 0 || len(m.allPRs()) == 0 {
			status.label, status.compact = "unavailable", "FAILED"
		} else if partial {
			status.label += "/PARTIAL"
		}
	case snapshots == 0:
		status.label, status.compact, status.warning = "not checked", "unknown", true
	case partial:
		status.label, status.compact, status.warning = "PARTIAL", "PARTIAL", true
	}
	return status
}

func statusText(status dataStatus, compact bool) string {
	label := status.label
	if compact {
		label = status.compact
	}
	if status.refreshing {
		if compact {
			label += "↻"
		} else if status.fetching {
			label += " · fetching…"
		} else {
			label += " · refreshing"
		}
	}
	return label
}

func (m Model) compactDataStatus(width int) string {
	git, prs := m.gitDataStatus(), m.prDataStatus()
	inner := max(1, width-4)
	progress := ""
	if git.refreshing && len(m.repositories) > 0 {
		progress = fmt.Sprintf(" %d/%d", len(m.repositories)-m.scan.remaining, len(m.repositories))
	}
	gitText, prText := m.statusProgressText(git, false)+progress, m.statusProgressText(prs, false)
	compact := ansi.StringWidth("Git: "+gitText+" · PRs: "+prText+" · e status") > inner
	gitLabel, prLabel, hint := "Git: ", "PRs: ", "e status"
	if compact {
		gitLabel, prLabel, hint = "Git ", "PR ", "e info"
		gitText, prText = m.statusProgressText(git, true)+progress, m.statusProgressText(prs, true)
		if ansi.StringWidth(gitLabel+gitText+" · "+prLabel+prText+" · "+hint) > inner {
			gitText = m.statusProgressText(git, true)
		}
	}
	style := func(status dataStatus, label string) string {
		if status.warning {
			return StyleAmber.Render(label)
		}
		if status.refreshing {
			return StyleCyan.Render(label)
		}
		return StyleDimWhite.Render(label)
	}
	line := style(git, gitLabel+gitText) + StyleDimWhite.Render(" · ") + style(prs, prLabel+prText) + StyleDimWhite.Render(" · ") + StyleCyan.Render(hint)
	return ansi.Truncate(line, inner, "…")
}

func (m Model) statusProgressText(status dataStatus, compact bool) string {
	if !status.refreshing {
		return statusText(status, compact)
	}
	if compact {
		return status.compact + m.refreshGlyph()
	}
	action := "refreshing"
	if status.fetching {
		action = "fetching"
	}
	return status.label + " · " + m.refreshGlyph() + " " + action
}

func (m Model) dataStatusLines(width int) []string {
	inner := max(1, width-4)
	var lines []string
	add := func(value string) {
		lines = append(lines, strings.Split(ansi.Wrap(value, inner, ""), "\n")...)
	}
	if m.options.CacheInfo != nil {
		add(StyleTitle.Render("REPOSITORY CACHE"))
		add("Location: " + displayText(m.cacheInfo.Root))
		add(fmt.Sprintf("Size: %.1f MiB · %d repositories no longer listed", float64(m.cacheInfo.Bytes)/(1024*1024), len(m.cacheInfo.Orphans)))
		for _, repo := range m.cacheInfo.Orphans {
			add("  " + displayText(repo.Name) + " · removal available in Repository controls")
		}
		for _, problem := range m.cacheInfo.Problems {
			add(StyleAmber.Render(displayText(problem)))
		}
		if m.cacheError != "" {
			add(StyleAmber.Render(m.cacheError))
		}
		add("")
	}
	add(StyleTitle.Render("GIT HISTORY · " + statusText(m.gitDataStatus(), false)))
	repos := m.statusRepositories()
	scanned, failed := 0, 0
	var oldest time.Time
	for _, repo := range repos {
		if at := m.local.scannedAt[repo.ID]; !at.IsZero() {
			scanned++
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
		}
		if m.localRepositoryFailed(repo) {
			failed++
		}
	}
	add(fmt.Sprintf("%d/%d tracked repositories have scanned history.", scanned, len(repos)))
	if !m.startup.cachedAt.IsZero() {
		add("Cached snapshot committed: " + m.startup.cachedAt.Local().Format("Jan 02 15:04:05 MST"))
	}
	if !m.startup.fetchedAt.IsZero() {
		add("Fetched snapshot committed: " + m.startup.fetchedAt.Local().Format("Jan 02 15:04:05 MST"))
	}
	if !oldest.IsZero() {
		add("Oldest retained scan: " + oldest.Local().Format("Jan 02 15:04:05 MST"))
	}
	if m.scan.running {
		progress := fmt.Sprintf("%d/%d repositories checked", len(m.repositories)-m.scan.remaining, len(m.repositories))
		switch m.scan.phase {
		case scanCached:
			add(StyleCyan.Render("Startup phase 1/2: reading cached history · " + progress))
		case scanStartupFetch:
			if m.startup.cachedAt.IsZero() {
				add(StyleCyan.Render("Startup: cloning/fetching cache · " + progress))
			} else {
				add(StyleCyan.Render("Startup phase 2/2: fetching; rescanning changed repositories · " + progress))
				add("The cached snapshot stays visible until this pass finishes.")
			}
		default:
			add(StyleCyan.Render("Refreshing Git. The previous snapshot stays visible."))
			if m.startup.pending {
				add("Startup fetch will resume after the local rescan.")
			}
		}
	}
	if failed > 0 {
		add(StyleAmber.Render(fmt.Sprintf("%d repository updates failed. Retained history may be stale.", failed)))
	}
	for _, repo := range repos {
		detail := displayText(m.local.errors[repo.ID])
		if !m.localRepositoryFailed(repo) && !m.scanErrorDetails {
			continue
		}
		add(StyleTitle.Render(displayText(repo.Name)))
		if detail != "" {
			add(StyleAmber.Render("  " + scanErrorSummary(detail)))
		} else if m.localRepositoryFailed(repo) {
			add(StyleAmber.Render("  Update failed; retained history is stale."))
		}
		if m.scanErrorDetails {
			at := m.local.scannedAt[repo.ID]
			if at.IsZero() {
				add("  No retained scan.")
			} else {
				add("  Retained scan: " + at.Local().Format("Jan 02 15:04:05 MST"))
			}
			if detail != "" {
				add("  " + detail)
			}
		}
	}
	add("")
	prStatus := m.prDataStatus()
	add(StyleTitle.Render("PULL REQUESTS · " + statusText(prStatus, false)))
	if len(m.allPRs()) == 0 && prStatus.warning {
		add("Open PR count is unknown; no reliable cached inventory.")
	} else {
		add(fmt.Sprintf("%d known open PRs cached across all dates.", len(m.allPRs())))
	}
	missingFiles, missingReviews := 0, 0
	prs := m.allPRs()
	for _, pr := range prs {
		if !pr.FilesComplete {
			missingFiles++
		}
		if !pr.RequestedReviewersComplete || !pr.ReviewsComplete {
			missingReviews++
		}
	}
	if len(prs) > 0 {
		add(fmt.Sprintf("%d/%d PRs have complete current-file lists.", len(prs)-missingFiles, len(prs)))
	}
	if missingFiles > 0 {
		add(StyleAmber.Render(fmt.Sprintf("Areas unknown for %d PRs; directory counts may omit affected paths.", missingFiles)))
	}
	if missingReviews > 0 {
		add(fmt.Sprintf("%d PRs have incomplete reviewer/review details.", missingReviews))
	}
	if m.prState.requestBudget > 0 {
		add(fmt.Sprintf("Last refresh requests: %d/%d · account %d · inventory %d · additional files %d.",
			m.prState.accountRequests+m.prState.inventoryRequests+m.prState.fileRequests, m.prState.requestBudget,
			m.prState.accountRequests, m.prState.inventoryRequests, m.prState.fileRequests))
	}
	add(m.prMineStatus())
	var first, last time.Time
	supported, snapshots, unsupported := 0, 0, 0
	for _, repo := range repos {
		if errors.Is(m.prState.errors[repo.ID], gh.ErrUnsupportedOrigin) {
			unsupported++
			continue
		}
		supported++
		if _, exists := m.prState.snapshots[m.prState.repositories[repo.ID]]; exists {
			snapshots++
		}
		at := m.prState.snapshots[m.prState.repositories[repo.ID]].checked
		if at.IsZero() {
			continue
		}
		if first.IsZero() || at.Before(first) {
			first = at
		}
		if at.After(last) {
			last = at
		}
	}
	add(fmt.Sprintf("%d/%d repositories have PR snapshots.", snapshots, supported))
	if unsupported > 0 {
		add(fmt.Sprintf("%d tracked repositories have unsupported origins.", unsupported))
	}
	if !first.IsZero() {
		add("Oldest check: " + first.Local().Format("Jan 02 15:04:05 MST"))
		if !first.Equal(last) {
			add("Latest check: " + last.Local().Format("Jan 02 15:04:05 MST"))
		}
	}
	if m.prState.loading {
		add(StyleCyan.Render("Refreshing PRs. Retained data may no longer be current."))
	}
	if prStatus.warning {
		add(StyleAmber.Render("Coverage is incomplete or unavailable. Retained PRs may be outdated."))
	}
	for _, repo := range repos {
		err := m.prState.errors[repo.ID]
		snapshot, exists := m.prState.snapshots[m.prState.repositories[repo.ID]]
		if err == nil {
			err = snapshot.err
		}
		unknownAreas := 0
		for _, pr := range snapshot.prs {
			if !pr.FilesComplete {
				unknownAreas++
			}
		}
		if !m.scanErrorDetails && (errors.Is(err, gh.ErrUnsupportedOrigin) || exists && !snapshot.partial && err == nil && snapshot.filesErr == nil && unknownAreas == 0) {
			continue
		}
		state := "inventory complete"
		switch {
		case errors.Is(err, gh.ErrUnsupportedOrigin):
			state = "not applicable"
		case err != nil:
			state = "STALE · " + prFailureReason(err)
		case !exists:
			state = "not checked"
		case snapshot.partial:
			state = "PARTIAL"
		}
		add(StyleTitle.Render(displayText(repo.Name) + " · " + state))
		if unknownAreas > 0 {
			add(fmt.Sprintf("  Areas unknown for %d PRs; current-file lists incomplete.", unknownAreas))
		}
		if !snapshot.checked.IsZero() {
			add("  Last checked: " + snapshot.checked.Local().Format("Jan 02 15:04:05 MST"))
		}
		if !snapshot.lastGood.IsZero() && (snapshot.partial || err != nil) {
			add("  Last complete: " + snapshot.lastGood.Local().Format("Jan 02 15:04:05 MST"))
		}
		if m.scanErrorDetails && err != nil {
			add("  " + displayText(err.Error()))
		}
		if m.scanErrorDetails && snapshot.filesErr != nil {
			add("  File details: " + displayText(snapshot.filesErr.Error()))
		}
	}
	if time.Now().Before(m.prState.nextRefresh) {
		add("PR refresh available after " + m.prState.nextRefresh.Local().Format("15:04:05 MST") + ".")
	}
	return lines
}
