package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

// runExport collects, filters, aggregates, and writes the contributor table to
// w in the requested format (json|csv|md) without launching the TUI. Per-repo
// scan failures are reported to errw and skipped.
func runExport(w, errw io.Writer, format string, repoPaths []string, excluded map[string]bool, since time.Duration, sortField stats.SortField) error {
	var all []git.CommitRecord
	failed := 0
	for _, p := range repoPaths {
		ref := git.DetectDefaultBranch(p)
		recs, err := git.CollectCommits(p, ref)
		if err != nil {
			fmt.Fprintf(errw, "warning: skipping %s: %v\n", p, err)
			failed++
			continue
		}
		all = append(all, recs...)
	}
	if failed > 0 && failed == len(repoPaths) {
		return fmt.Errorf("all %d repositories failed to scan", failed)
	}
	all = stats.FilterByRepo(all, excluded)
	all = stats.FilterByTime(all, since)
	authors := stats.Aggregate(all)
	stats.Sort(authors, sortField)

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(authors)
	case "csv":
		return exportCSV(w, authors)
	case "md", "markdown":
		return exportMarkdown(w, authors)
	default:
		return fmt.Errorf("invalid --export format %q (want json|csv|md)", format)
	}
}

func exportCSV(w io.Writer, authors []stats.AuthorStats) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	header := []string{"rank", "contributor", "commits", "added", "removed", "net", "total_change", "ai_commits", "ai_percent", "active_days", "first_commit", "last_commit"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for i, a := range authors {
		row := []string{
			strconv.Itoa(i + 1),
			a.Name,
			strconv.Itoa(a.Commits),
			strconv.Itoa(a.Added),
			strconv.Itoa(a.Removed),
			strconv.Itoa(a.Net),
			strconv.Itoa(a.TotalChange),
			strconv.Itoa(a.AICommits),
			strconv.Itoa(a.AIPercent()),
			strconv.Itoa(a.ActiveDays),
			dateOrEmpty(a.FirstCommit),
			dateOrEmpty(a.LastCommit),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return cw.Error()
}

func exportMarkdown(w io.Writer, authors []stats.AuthorStats) error {
	if _, err := fmt.Fprintln(w, "| # | Contributor | Commits | Added | Removed | Net | AI % | Active days |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "|--:|---|--:|--:|--:|--:|--:|--:|"); err != nil {
		return err
	}
	for i, a := range authors {
		if _, err := fmt.Fprintf(w, "| %d | %s | %d | %d | %d | %d | %d%% | %d |\n",
			i+1, mdCell(a.Name), a.Commits, a.Added, a.Removed, a.Net, a.AIPercent(), a.ActiveDays); err != nil {
			return err
		}
	}
	return nil
}

func dateOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
