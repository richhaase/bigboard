package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type analysisOptions struct {
	IncludeGenerated bool
	AIIdentities     []string
	BotIdentities    []string
}

const maxConcurrentExportScans = 8

func runExportJSON(w, errw io.Writer, repositories []git.Repository, excluded map[string]bool, sortField stats.SortField, options analysisOptions) error {
	// Excluded repositories are outside the export's scope: they must neither
	// trigger diagnostics nor make an otherwise failed export appear successful.
	selected := make([]git.Repository, 0, len(repositories))
	for _, repo := range repositories {
		if !excluded[repo.ID] && !excluded[repo.Name] {
			selected = append(selected, repo)
		}
	}
	repositories = selected
	type scanResult struct {
		records []git.CommitRecord
		err     error
	}
	results := make([]scanResult, len(repositories))
	sem := make(chan struct{}, maxConcurrentExportScans)
	var wg sync.WaitGroup
	for i, repo := range repositories {
		wg.Add(1)
		go func(i int, repo git.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			records, err := git.ScanRepository(context.Background(), repo, git.CollectOptions{
				IncludeGenerated: options.IncludeGenerated,
				AIIdentities:     options.AIIdentities,
			})
			results[i] = scanResult{records: records, err: err}
		}(i, repo)
	}
	wg.Wait()

	var all []git.CommitRecord
	failed := 0
	for i, repo := range repositories {
		if results[i].err != nil {
			fmt.Fprintf(errw, "warning: skipping %s: %s\n", diagnosticText(repo.Path), diagnosticText(results[i].err.Error()))
			failed++
			continue
		}
		all = append(all, results[i].records...)
		for _, record := range results[i].records {
			if record.LinesUnknown {
				fmt.Fprintf(errw, "warning: %s has shallow history; boundary line counts are unknown\n", diagnosticText(repo.Path))
				break
			}
		}
	}
	if failed > 0 && failed == len(repositories) {
		return fmt.Errorf("all %d repositories failed to scan", failed)
	}
	authors := stats.AggregateWithOptions(all, stats.AggregateOptions{
		BotIdentities: options.BotIdentities,
	})
	stats.Sort(authors, sortField)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(authors)
}

func diagnosticText(s string) string {
	quoted := strconv.QuoteToGraphic(s)
	return quoted[1 : len(quoted)-1]
}
