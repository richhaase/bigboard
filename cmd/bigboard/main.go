package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
	"github.com/rdh/bigboard/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type excludeFlags []string

func (e *excludeFlags) String() string { return strings.Join(*e, ",") }
func (e *excludeFlags) Set(v string) error {
	*e = append(*e, v)
	return nil
}

func main() {
	sortFlag := flag.String("sort", "total", "Initial sort: commits|added|removed|net|total")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	var excludes excludeFlags
	flag.Var(&excludes, "exclude", "Repo directory name to exclude (repeatable)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("bigboard %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	repoPaths := git.DiscoverRepos(paths)
	if len(repoPaths) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no git repositories found in %v\n", paths)
		os.Exit(1)
	}

	// Build exclusion set from --exclude flags, matching against repo basenames
	excludedRepos := make(map[string]bool)
	for _, rp := range repoPaths {
		base := filepath.Base(rp)
		for _, ex := range excludes {
			if base == ex {
				excludedRepos[base] = true
			}
		}
	}

	initialSort := stats.SortFieldFromString(*sortFlag)
	model := tui.NewModel(repoPaths, initialSort, excludedRepos)

	p := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		p.Send(tui.LoadDataCmd(repoPaths)())
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
