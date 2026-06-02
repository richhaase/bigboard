package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

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
	sinceFlag := flag.String("since", "", "Window for --export: e.g. 30d, 2w, 1y, all (default 14d)")
	groupFlag := flag.String("group", "", "Use a named repo group from the config file")
	depthFlag := flag.Int("depth", 0, "Directory levels to scan for repos (default 1)")
	configFlag := flag.String("config", "", "Config file path (default ~/.config/bigboard/config.json)")
	watchFlag := flag.String("watch", "", "Auto-refresh interval, e.g. 30s or 5m (default off)")
	noAnimFlag := flag.Bool("no-anim", false, "Disable the animated glitch line")
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

	// Resolve settings: an explicitly-set flag wins, else config, else default.
	sortStr := pick(setFlags["sort"], *sortFlag, cfg.Sort, "total")
	themeStr := pick(setFlags["theme"], *themeFlag, cfg.Theme, "auto")
	stats.FuzzyMatching = *fuzzyFlag || (!setFlags["fuzzy"] && cfg.Fuzzy)
	if *allFilesFlag || (!setFlags["all-files"] && cfg.AllFiles) {
		git.FilterGeneratedPaths = false
	}
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
		// Let lipgloss auto-detect from the terminal.
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid theme %q (want auto|light|dark)\n", themeStr)
		os.Exit(1)
	}

	// Resolve scan paths: --group > CLI args > config paths > ".".
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

	repoPaths := git.DiscoverReposDepth(paths, depth)
	if len(repoPaths) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no git repositories found in %v\n", paths)
		os.Exit(1)
	}

	excludePatterns := append(append([]string{}, cfg.Exclude...), excludes...)
	excludedRepos := buildExcludeSet(repoPaths, excludePatterns)
	initialSort := stats.SortFieldFromString(sortStr)

	// Headless export mode: run the pipeline and exit without the TUI.
	if *exportFlag != "" {
		since, err := parseSince(pick(setFlags["since"], *sinceFlag, cfg.Since, "14d"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --since: %v\n", err)
			os.Exit(1)
		}
		if err := runExport(os.Stdout, os.Stderr, strings.ToLower(*exportFlag), repoPaths, excludedRepos, since, initialSort); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var watchInterval time.Duration
	if *watchFlag != "" {
		watchInterval, err = time.ParseDuration(*watchFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --watch interval %q: %v\n", *watchFlag, err)
			os.Exit(1)
		}
	}

	model := tui.NewModel(repoPaths, initialSort, excludedRepos, version, watchInterval, !*noAnimFlag)

	// The model's Init kicks off the concurrent per-repo load.
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// pick resolves a string setting: an explicitly-set flag wins, then a non-empty
// config value, then the default.
func pick(setByFlag bool, flagVal, cfgVal, def string) string {
	if setByFlag {
		return flagVal
	}
	if cfgVal != "" {
		return cfgVal
	}
	return def
}

// buildExcludeSet returns the set of repo basenames matching any exclude
// pattern (exact match or filepath glob).
func buildExcludeSet(repoPaths, patterns []string) map[string]bool {
	ex := make(map[string]bool)
	for _, rp := range repoPaths {
		base := filepath.Base(rp)
		for _, pat := range patterns {
			if pat == base {
				ex[base] = true
				break
			}
			if ok, err := filepath.Match(pat, base); err == nil && ok {
				ex[base] = true
				break
			}
		}
	}
	return ex
}

// expandHome expands a leading ~ to the user's home directory.
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
