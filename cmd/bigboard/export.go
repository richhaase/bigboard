package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type analysisOptions struct {
	FuzzyMatching    bool
	IncludeGenerated bool
}

func runExport(w, errw io.Writer, format string, repoPaths []string, excluded map[string]bool, since time.Duration, sortField stats.SortField) error {
	repositories := git.NewRepositories(repoPaths)
	normalizedExcluded := make(map[string]bool)
	for key, value := range excluded {
		if value {
			normalizedExcluded[key] = true
		}
	}
	for _, repo := range repositories {
		if excluded[repo.Name] || excluded[filepath.Base(repo.Path)] {
			normalizedExcluded[repo.ID] = true
		}
	}
	for _, repo := range repositories {
		if repo.Name != repo.ID {
			delete(normalizedExcluded, repo.Name)
		}
		if base := filepath.Base(repo.Path); base != repo.ID {
			delete(normalizedExcluded, base)
		}
	}
	return runExportWithOptions(w, errw, format, repositories, normalizedExcluded, since, sortField, analysisOptions{
		FuzzyMatching:    stats.FuzzyMatching,
		IncludeGenerated: !git.FilterGeneratedPaths,
	})
}

func runExportWithOptions(w, errw io.Writer, format string, repositories []git.Repository, excluded map[string]bool, since time.Duration, sortField stats.SortField, options analysisOptions) error {
	if err := validateExportFormat(format); err != nil {
		return err
	}

	var all []git.CommitRecord
	failed := 0
	for _, repo := range repositories {
		recs, err := git.ScanRepository(context.Background(), repo, git.CollectOptions{
			IncludeGenerated: options.IncludeGenerated,
		})
		if err != nil {
			fmt.Fprintf(errw, "warning: skipping %s: %s\n", diagnosticText(repo.Path), diagnosticText(err.Error()))
			failed++
			continue
		}
		all = append(all, recs...)
	}
	if failed > 0 && failed == len(repositories) {
		return fmt.Errorf("all %d repositories failed to scan", failed)
	}
	all = stats.FilterByRepo(all, excluded)
	all = stats.FilterByTime(all, since)
	authors := stats.AggregateWithOptions(all, stats.AggregateOptions{
		FuzzyMatching: options.FuzzyMatching,
	})
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
		return fmt.Errorf("unsupported export format %q", format)
	}
}

func diagnosticText(s string) string {
	quoted := strconv.QuoteToGraphic(s)
	return quoted[1 : len(quoted)-1]
}

func validateExportFormat(format string) error {
	switch format {
	case "json", "csv", "md", "markdown":
		return nil
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
			spreadsheetCell(a.Name),
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

func spreadsheetCell(s string) string {
	trimmed := strings.TrimLeft(s, " \t")
	if trimmed == "" {
		return s
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + s
	default:
		return s
	}
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
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "|", "\\|")
}
