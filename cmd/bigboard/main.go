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

type excludeFlags []string

func (e *excludeFlags) String() string { return strings.Join(*e, ",") }
func (e *excludeFlags) Set(v string) error {
	*e = append(*e, v)
	return nil
}

func main() {
	sortFlag := flag.String("sort", "total", "Initial sort: commits|added|removed|net|ai|total")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	themeFlag := flag.String("theme", "auto", "Color theme: auto|light|dark")
	fuzzyFlag := flag.Bool("fuzzy", false, "Enable fuzzy author-name merging (may over-merge similar names)")
	allFilesFlag := flag.Bool("all-files", false, "Count generated/vendored files (disables the default ignore list)")
	exportFlag := flag.String("export", "", "Export the view headlessly and exit: json|csv|md")
	sinceFlag := flag.String("since", "", "Initial time window: e.g. 30d, 2w, 1y, all (default 14d)")
	groupFlag := flag.String("group", "", "Use a named repo group from the config file")
	depthFlag := flag.Int("depth", 0, "Directory levels to scan for repos (default 1)")
	configFlag := flag.String("config", "", "Config file path (default ~/.config/bigboard/config.json)")
	var excludes excludeFlags
	flag.Var(&excludes, "exclude", "Repo basename or glob to exclude (repeatable)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("bigboard %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

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

	sortStr := pick(setFlags["sort"], *sortFlag, cfg.Sort, "total")
	themeStr := pick(setFlags["theme"], *themeFlag, cfg.Theme, "auto")
	fuzzyMatching := *fuzzyFlag || (!setFlags["fuzzy"] && cfg.Fuzzy)
	includeGenerated := *allFilesFlag || (!setFlags["all-files"] && cfg.AllFiles)
	depth := 1
	switch {
	case setFlags["depth"]:
		depth = *depthFlag
	case cfg.Depth > 0:
		depth = cfg.Depth
	}
	if depth < 1 {
		depth = 1
	}

	switch strings.ToLower(themeStr) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	case "", "auto":
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid theme %q (want auto|light|dark)\n", themeStr)
		os.Exit(1)
	}
	initialSort, err := stats.ParseSortField(sortStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	since, err := parseSince(pick(setFlags["since"], *sinceFlag, cfg.Since, "14d"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --since: %v\n", err)
		os.Exit(1)
	}
	exportFormat := strings.ToLower(*exportFlag)
	if exportFormat != "" {
		if err := validateExportFormat(exportFormat); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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

	excludePatterns := append(append([]string{}, cfg.Exclude...), excludes...)
	excludedRepos, err := buildExcludeSet(repositories, excludePatterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid exclusion: %v\n", err)
		os.Exit(1)
	}

	if exportFormat != "" {
		if err := runExportWithOptions(os.Stdout, os.Stderr, exportFormat, repositories, excludedRepos, since, initialSort, analysisOptions{
			FuzzyMatching:    fuzzyMatching,
			IncludeGenerated: includeGenerated,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	timeIdx := tui.DefaultTimeIndex
	if setFlags["since"] || cfg.Since != "" {
		timeIdx = tui.TimePresetIndex(since)
	}
	model := tui.NewModelWithOptions(repositories, initialSort, excludedRepos, version, timeIdx, tui.Options{
		FuzzyMatching:    fuzzyMatching,
		IncludeGenerated: includeGenerated,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func pick(setByFlag bool, flagVal, cfgVal, def string) string {
	if setByFlag {
		return flagVal
	}
	if cfgVal != "" {
		return cfgVal
	}
	return def
}

func buildExcludeSet(repositories []git.Repository, patterns []string) (map[string]bool, error) {
	ex := make(map[string]bool)
	for _, pattern := range patterns {
		exact := false
		for _, repo := range repositories {
			if pattern == filepath.Base(repo.Path) {
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
