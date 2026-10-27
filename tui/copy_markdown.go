package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/stats"
)

const maxClipboardBytes = 1024 * 1024

type clipboardResult struct {
	err  error
	rows int
}

type clipboardCommand struct {
	text string
	out  io.Writer
}

func (c *clipboardCommand) SetStdin(io.Reader)    {}
func (c *clipboardCommand) SetStderr(io.Writer)   {}
func (c *clipboardCommand) SetStdout(w io.Writer) { c.out = w }
func (c *clipboardCommand) Run() error {
	if c.out == nil {
		return fmt.Errorf("terminal output unavailable")
	}
	request := osc52.New(c.text)
	if os.Getenv("TMUX") != "" {
		request = request.Tmux()
	} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		request = request.Screen()
	}
	sequence := request.String()
	n, err := io.WriteString(c.out, sequence)
	if err == nil && n != len(sequence) {
		err = io.ErrShortWrite
	}
	return err
}

func (m Model) canCopyMarkdown() bool {
	if m.settings.open || m.identityMerge.open || m.identityMerge.saving || m.loading || m.err != nil || m.showHelp || m.showScanErrors || m.filters.open || m.helpSearchActive() || m.viewMode == ViewRepoOverlay {
		return false
	}
	if m.showPRs {
		return !m.prUI.detail
	}
	if m.viewMode == ViewAggregate {
		return true
	}
	return m.viewMode == ViewAwareness && !m.showPaths && (!m.glance.detailOpen || m.glance.frame.tab == glanceActivity)
}

func (m Model) copyMarkdown() (tea.Model, tea.Cmd) {
	payload := m.markdownList()
	if len(payload) > maxClipboardBytes {
		m.copyStatus = "Copy too large (1 MiB limit); narrow the filters"
		return m, nil
	}
	m.copyStatus = "Sending Markdown clipboard request…"
	return m, tea.Exec(&clipboardCommand{text: payload}, func(err error) tea.Msg { return clipboardResult{err: err, rows: strings.Count(payload, "\n| ") - 2} })
}

func markdownCell(s string) string {
	s = displayText(ansi.Strip(s))
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "|", "\\|", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]").Replace(s)
}

func markdownRow(b *strings.Builder, cells ...string) {
	b.WriteString("| ")
	for i, cell := range cells {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(markdownCell(cell))
	}
	b.WriteString(" |\n")
}

func markdownHeader(b *strings.Builder, cells ...string) {
	markdownRow(b, cells...)
	for i := range cells {
		cells[i] = "---"
	}
	markdownRow(b, cells...)
}

func markdownTime(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.Local().Format(time.RFC3339)
}

func (m Model) markdownList() string {
	var b strings.Builder
	title, sortLabel := "Activity", m.overviewSortLabel()
	if m.showPRs {
		title, sortLabel = "Pull requests", m.prSortLabel()
	} else if m.viewMode == ViewAggregate {
		title, sortLabel = "Contributors", stats.SortFieldLabel(m.sortField)+" descending"
		if m.sortAsc {
			sortLabel = stats.SortFieldLabel(m.sortField) + " ascending"
		}
	} else if m.glance.detailOpen {
		title, sortLabel = "Activity · Commits", "Newest first"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	metadata := []string{m.sharedScopeLabel(1 << 20), m.boardFiltersLabel(1 << 20), "Sort: " + sortLabel}
	var repos []string
	for _, repo := range m.statusRepositories() {
		if m.repoInView(repo.ID) {
			repos = append(repos, repo.Name)
		}
	}
	metadata = append(metadata, "Viewed repositories: "+strings.Join(repos, ", "))
	if m.showPRs && m.prUI.repoID != "" {
		metadata = append(metadata, "PR repository: "+m.prState.repositories[m.prUI.repoID])
	} else if !m.showPRs && m.viewMode == ViewAwareness {
		if repo, ok := m.areaRepository(); ok {
			metadata = append(metadata, "Repository: "+repo.Name)
		}
		if m.directory.active {
			metadata = append(metadata, "Directory: /"+m.directory.frame.path)
		}
		if m.glance.detailOpen {
			metadata = append(metadata, "Scope: "+m.glance.frame.name+" ("+m.glance.frame.areaID+")")
			if m.glance.frame.relatedFromID != "" {
				metadata = append(metadata, "Related evidence from: "+m.glance.frame.relatedFromID)
			}
		}
		if len(m.activityPeople) > 0 {
			metadata = append(metadata, "Contributor identities: "+strings.Join(m.activityPeople, ", "))
		}
		if m.personID != "" {
			metadata = append(metadata, "Focused contributor: "+m.personID)
		}
	}
	metadata = append(metadata, m.screenLocalFreshness(""), "PRs: "+statusText(m.prDataStatus(), false), "Rows: entire filtered list, including rows outside the viewport", "? marks unknown line counts or incomplete/stale PR evidence; overlapping directory/area counts are not additive")
	for _, line := range metadata {
		fmt.Fprintf(&b, "%s\n\n", markdownCell(line))
	}
	switch {
	case m.showPRs:
		markdownHeader(&b, "Repository", "PR", "State", "Author", "Title", "Review", "Checks", "Updated", "Evidence", "URL")
		for _, pr := range m.visiblePRs() {
			state, evidence := "Open", ""
			if pr.Draft {
				state = "Draft"
			}
			snapshot := m.prState.snapshots[pr.Repo]
			if snapshot.partial || !pr.ContextComplete || !pr.FilesComplete {
				evidence = "PARTIAL ?"
			}
			if snapshot.err != nil {
				evidence += " STALE"
			}
			markdownRow(&b, pr.Repo, fmt.Sprint(pr.Number), state, pr.Author, pr.Title, prReviewLabel(pr.ReviewDecision), prCheckLabel(pr.CheckState), markdownTime(pr.UpdatedAt), strings.TrimSpace(evidence), pr.URL)
		}
	case m.viewMode == ViewAggregate:
		markdownHeader(&b, "Rank", "Contributor", "Identity", "Bot", "Commits", "Added", "Removed", "Net", "AI%")
		for i, author := range m.displayedAuthors() {
			markdownRow(&b, fmt.Sprint(i+1), author.Name, author.ID, fmt.Sprint(author.Bot), fmt.Sprint(author.Commits), formatLineCount(author.Added, author.UnknownLineCommits), formatLineCount(author.Removed, author.UnknownLineCommits), formatLineCount(author.Net, author.UnknownLineCommits), fmt.Sprint(author.AIPercent()))
		}
	case m.glance.detailOpen:
		markdownHeader(&b, "Commit", "Subject", "Author", "Identity", "Date", "Added", "Removed")
		for _, record := range m.glanceEvidence() {
			unknown := 0
			if record.LinesUnknown {
				unknown = 1
			}
			markdownRow(&b, record.CommitID, record.Subject, record.Author, stats.IdentityID(record), markdownTime(record.Date), formatLineCount(record.Added, unknown), formatLineCount(record.Removed, unknown))
		}
	default:
		markdownHeader(&b, "Rank", "Repository / area / directory", "Commits", "People", "Added", "Removed", "Open PRs", "Failing", "Changes requested", "Latest", "Status")
		rows := m.glanceRepositories()
		if m.directory.active {
			rows = m.directoryRows()
		}
		for i, row := range rows {
			added, removed, unknown := repositoryLineTotals(row)
			signal := m.activityPRSignal(row)
			count := func(n int) string {
				if !signal.known {
					return "?"
				}
				if signal.uncertain {
					return fmt.Sprint(n) + "?"
				}
				return fmt.Sprint(n)
			}
			status := ""
			if m.local.staleRepos[row.repositoryID] {
				status = "STALE"
			}
			markdownRow(&b, fmt.Sprint(i+1), row.label, fmt.Sprint(row.commits), fmt.Sprint(len(row.people)), formatLineCount(added, unknown), formatLineCount(removed, unknown), count(signal.open), count(signal.failing), count(signal.changes), markdownTime(row.latest), status)
		}
	}
	return b.String()
}

func (m Model) copyCoverage(coverage string) string {
	if m.copyStatus != "" {
		return m.copyStatus
	}
	return coverage
}
