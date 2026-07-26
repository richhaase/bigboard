package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("config contains multiple JSON values")
		}
		return nil, err
	}
	return &cfg, nil
}

func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" || s == "0" {
		return 0, nil
	}
	for _, unit := range []struct {
		suffix   string
		duration time.Duration
	}{
		{"d", 24 * time.Hour},
		{"w", 7 * 24 * time.Hour},
		{"y", 365 * 24 * time.Hour},
	} {
		if strings.HasSuffix(s, unit.suffix) {
			return parseUnitDuration(strings.TrimSuffix(s, unit.suffix), unit.duration)
		}
	}
	duration, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if duration < 0 {
		return 0, fmt.Errorf("duration must not be negative")
	}
	return duration, nil
}

func parseUnitDuration(value string, unit time.Duration) (time.Duration, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("duration must not be negative")
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if n > int64(maxDuration/unit) {
		return 0, fmt.Errorf("duration is too large")
	}
	return time.Duration(n) * unit, nil
}
