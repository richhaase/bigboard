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
	Paths    []string            `json:"paths"`
	Exclude  []string            `json:"exclude"`
	Sort     string              `json:"sort"`
	Since    string              `json:"since"`
	Theme    string              `json:"theme"`
	Fuzzy    bool                `json:"fuzzy"`
	AllFiles bool                `json:"all_files"`
	Depth    int                 `json:"depth"`
	Groups   map[string][]string `json:"groups"`
}

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
