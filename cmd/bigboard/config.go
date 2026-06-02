package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the optional persistent configuration, read from a JSON file
// (default ~/.config/bigboard/config.json). Command-line flags override it.
type Config struct {
	Paths    []string            `json:"paths"`     // default scan paths when none given on the CLI
	Exclude  []string            `json:"exclude"`   // default exclude globs (matched against repo basename)
	Sort     string              `json:"sort"`      // default sort field
	Since    string              `json:"since"`     // default window for --export (e.g. "30d", "1y", "all")
	Theme    string              `json:"theme"`     // auto|light|dark
	Fuzzy    bool                `json:"fuzzy"`     // enable fuzzy author merging
	AllFiles bool                `json:"all_files"` // count generated/vendored files
	Depth    int                 `json:"depth"`     // discovery depth (0 = use built-in default)
	Groups   map[string][]string `json:"groups"`    // named repo/path groups selectable via --group
}

// defaultConfigPath returns $XDG_CONFIG_HOME/bigboard/config.json, falling back
// to ~/.config/bigboard/config.json.
func defaultConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "bigboard", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "bigboard", "config.json")
}

// loadConfig reads and parses the config file at path. A missing file is not an
// error — it returns an empty Config. An explicitly-requested path that does not
// exist (explicit==true) is an error.
func loadConfig(path string, explicit bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// parseSince converts a window string into a duration. It accepts Go durations
// (e.g. "48h"), day/week/year suffixes ("30d", "2w", "1y"), and "all"/"0"/""
// for all-time (0).
func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" || s == "0" {
		return 0, nil
	}
	if n, ok := suffixNum(s, "d"); ok {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	if n, ok := suffixNum(s, "w"); ok {
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	if n, ok := suffixNum(s, "y"); ok {
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// suffixNum parses "<int><suffix>" and returns (n, true) on success.
func suffixNum(s, suffix string) (int, bool) {
	if !strings.HasSuffix(s, suffix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, suffix))
	if err != nil {
		return 0, false
	}
	return n, true
}
