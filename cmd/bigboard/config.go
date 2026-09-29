package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/richhaase/bigboard/tui"
)

// Config is the optional persistent configuration, read from a JSON file
// (default ~/.config/bigboard/config.json).
type Config struct {
	Paths         []string            `json:"paths"`
	Exclude       []string            `json:"exclude"`
	Sort          string              `json:"sort"`
	Since         string              `json:"since"`
	Theme         string              `json:"theme"`
	Fuzzy         bool                `json:"fuzzy"`
	AllFiles      bool                `json:"all_files"`
	Depth         int                 `json:"depth"`
	Groups        map[string][]string `json:"groups"`
	AIIdentities  []string            `json:"ai_identities"`
	BotIdentities []string            `json:"bot_identities"`
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

func timeIndexForSince(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return tui.DefaultTimeIndex, nil
	}
	labels := make([]string, len(tui.TimePresets))
	for i, p := range tui.TimePresets {
		labels[i] = p.Label
		if strings.EqualFold(p.Label, s) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("invalid since %q (want %s)", s, strings.Join(labels, "|"))
}
