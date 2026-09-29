package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
	"github.com/richhaase/bigboard/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) > 7 {
					commit = s.Value[:7]
				} else {
					commit = s.Value
				}
			case "vcs.time":
				date = s.Value
			}
		}
	}
}

func main() {
	versionFlag := flag.Bool("version", false, "Print version and exit")
	exportFlag := flag.Bool("export", false, "Print contributor stats as JSON and exit")
	groupFlag := flag.String("group", "", "Use a named repo group from the config file")
	configFlag := flag.String("config", "", "Config file path (default ~/.config/bigboard/config.json)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("bigboard %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	cfgPath := *configFlag
	explicitCfg := cfgPath != ""
	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}
	cfg, err := loadConfig(cfgPath, explicitCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: reading config %s: %v\n", cfgPath, err)
		os.Exit(1)
	}

	switch strings.ToLower(cfg.Theme) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	case "", "auto":
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid theme %q in config (want auto|light|dark)\n", cfg.Theme)
		os.Exit(1)
	}

	sortStr := cfg.Sort
	if sortStr == "" {
		sortStr = "total"
	}
	initialSort, err := stats.ParseSortField(sortStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	timeIdx, err := timeIndexForSince(cfg.Since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	depth := cfg.Depth
	if depth < 1 {
		depth = 1
	}

	var paths []string
	switch {
	case *groupFlag != "":
		g, ok := cfg.Groups[*groupFlag]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: unknown group %q (config: %s)\n", *groupFlag, cfgPath)
			os.Exit(1)
		}
		paths = g
	case len(flag.Args()) > 0:
		paths = flag.Args()
	case len(cfg.Paths) > 0:
		paths = cfg.Paths
	default:
		paths = []string{"."}
	}
	for i, p := range paths {
		paths[i] = expandHome(p)
	}
	if err := validateScanPaths(paths); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	repoPaths := git.DiscoverReposDepth(paths, depth)
	if len(repoPaths) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no git repositories found in %v\n", paths)
		os.Exit(1)
	}
	repositories := git.NewRepositories(repoPaths)

	excludedRepos, err := buildExcludeSet(repositories, cfg.Exclude)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid exclusion: %v\n", err)
		os.Exit(1)
	}

	if *exportFlag {
		if err := runExportJSON(os.Stdout, os.Stderr, repositories, excludedRepos, initialSort, analysisOptions{
			FuzzyMatching:    cfg.Fuzzy,
			IncludeGenerated: cfg.AllFiles,
			AIIdentities:     cfg.AIIdentities,
			BotIdentities:    cfg.BotIdentities,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	model := tui.NewModelWithOptions(repositories, initialSort, excludedRepos, version, timeIdx, tui.Options{
		FuzzyMatching:    cfg.Fuzzy,
		IncludeGenerated: cfg.AllFiles,
		AIIdentities:     cfg.AIIdentities,
		BotIdentities:    cfg.BotIdentities,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func buildExcludeSet(repositories []git.Repository, patterns []string) (map[string]bool, error) {
	ex := make(map[string]bool)
	for _, pattern := range patterns {
		exact := false
		for _, repo := range repositories {
			if pattern == filepath.Base(repo.Path) || pattern == repo.Name {
				ex[repo.ID] = true
				exact = true
			}
		}
		if _, err := filepath.Match(pattern, ""); err != nil {
			if exact {
				continue
			}
			return nil, fmt.Errorf("pattern %q: %w", pattern, err)
		}
		for _, repo := range repositories {
			if ok, _ := filepath.Match(pattern, filepath.Base(repo.Path)); ok {
				ex[repo.ID] = true
				continue
			}
			if ok, _ := filepath.Match(pattern, repo.Name); ok {
				ex[repo.ID] = true
			}
		}
	}
	return ex, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func validateScanPaths(paths []string) error {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("scan path %q: %w", path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("scan path %q is not a directory", path)
		}
	}
	return nil
}
