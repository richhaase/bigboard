package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
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
	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}
	cfg, err := loadConfig(cfgPath, *exportFlag)
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

	if len(flag.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "Error: positional repository paths are no longer supported; configure a personal account or organizations and select a --group")
		os.Exit(1)
	}
	setupRequired := len(cfg.Organizations) == 0 && cfg.PersonalAccount == "" && !*exportFlag
	if !setupRequired {
		if err := validateRepositoryConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	cache, err := git.NewRepositoryCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	provider := gh.NewProvider(nil)
	var listed, repositories []git.Repository
	var excludedRepos map[string]bool
	var setupError string
	if !setupRequired {
		if !*exportFlag {
			fmt.Fprintln(os.Stderr, "Discovering GitHub repositories…")
		}
		listed, err = discoverCachedRepositories(context.Background(), cfg, cache, provider, *exportFlag)
		if err != nil {
			if *exportFlag {
				fmt.Fprintf(os.Stderr, "Error: repository discovery: %v\n", err)
				os.Exit(1)
			}
			setupRequired = true
			setupError = fmt.Sprintf("Repository discovery failed: %v", err)
		} else {
			repositories, excludedRepos, err = selectIncludedRepositories(cfg, *groupFlag, listed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	}

	if *exportFlag {
		if len(repositories) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no cached repositories for this selection; launch BigBoard online first")
			os.Exit(1)
		}
		if err := runExportJSON(os.Stdout, os.Stderr, repositories, excludedRepos, initialSort, analysisOptions{
			ScanRepository:   cache.ScanRepository,
			IncludeGenerated: cfg.AllFiles,
			AIIdentities:     cfg.AIIdentities,
			BotIdentities:    cfg.BotIdentities,
			Identities:       cfg.Identities,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	store := newConfigStore(cfgPath, cfg)
	inventory := &repositoryInventory{}
	inventory.replace(listed)
	settings := newSettingsOptions(cfgPath, cfg, *groupFlag, provider, cache, store, inventory)
	settings.Required = setupRequired
	settings.InitialError = setupError

	model := tui.NewModelWithOptions(repositories, initialSort, excludedRepos, version, timeIdx, tui.Options{
		Bookmark:               newBookmarkOptions(*groupFlag),
		PrepareRepository:      cache.Refresh,
		RepositoryRevision:     cache.Revision,
		ScanRepository:         cache.ScanRepository,
		CacheInfo:              func() (git.CacheInfo, error) { return cache.Info(inventory.snapshot()) },
		RemoveCachedRepository: cache.Remove,
		PRRequestBudget:        cfg.PRRequestBudget,
		FuzzyMatching:          cfg.Fuzzy,
		IncludeGenerated:       cfg.AllFiles,
		AIIdentities:           cfg.AIIdentities,
		BotIdentities:          cfg.BotIdentities,
		Identities:             cfg.Identities,
		SaveIdentities:         store.saveIdentities,
		Settings:               settings,
		WorkAreas:              cfg.WorkAreas,
	})

	if err := runTUI(model, tea.WithAltScreen()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Always join background commands, including when Bubble Tea consumes a signal
// or reports a terminal error without delivering a message to the model.
func runTUI(model tui.Model, options ...tea.ProgramOption) error {
	defer model.Close()
	p := tea.NewProgram(model, options...)
	final, err := p.Run()
	if current, ok := final.(tui.Model); ok {
		current.Close()
		if err == nil {
			if saveErr := current.SaveBookmark(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not save last-look bookmark: %v\n", saveErr)
			}
		}
	}
	return err
}

func buildExcludeSet(repositories []git.Repository, patterns []string) (map[string]bool, error) {
	ex := make(map[string]bool)
	for _, pattern := range patterns {
		exact := false
		for _, repo := range repositories {
			if pattern == repositoryBasename(repo) || pattern == repo.Name {
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
			if ok, _ := filepath.Match(pattern, repositoryBasename(repo)); ok {
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

func repositoryBasename(repo git.Repository) string {
	if !filepath.IsAbs(repo.ID) && strings.Contains(repo.ID, "/") {
		return filepath.Base(repo.ID)
	}
	return filepath.Base(repo.Path)
}
