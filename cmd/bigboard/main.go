package main

import (
	"flag"
	"fmt"
	"os"

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

func main() {
	sortFlag := flag.String("sort", "total", "Initial sort: commits|added|removed|net|total")
	versionFlag := flag.Bool("version", false, "Print version and exit")
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

	initialSort := stats.SortFieldFromString(*sortFlag)
	model := tui.NewModel(repoPaths, initialSort)

	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start data loading after the program starts
	go func() {
		p.Send(tui.LoadDataCmd(repoPaths)())
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
