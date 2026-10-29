package gui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type analysisConfig struct {
	PersonalAccount string                  `json:"personal_account"`
	Organizations   []string                `json:"organizations"`
	Exclude         []string                `json:"exclude"`
	AllFiles        bool                    `json:"all_files"`
	AIIdentities    []string                `json:"ai_identities"`
	BotIdentities   []string                `json:"bot_identities"`
	Identities      map[string]git.Identity `json:"identities"`
	PRRequestBudget int                     `json:"pr_request_budget"`
}

func normalizeSettings(settings Settings) (Settings, error) {
	if settings.Mode != "cache" && settings.Mode != "live" {
		return Settings{}, errors.New("choose cached history or live GitHub data")
	}
	owner := func(value string) (string, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", nil
		}
		canonical, err := gh.CanonicalRepo(value + "/repository")
		if err != nil {
			return "", fmt.Errorf("invalid GitHub account or organization %q", value)
		}
		name, _, _ := strings.Cut(canonical, "/")
		return name, nil
	}
	var err error
	settings.PersonalAccount, err = owner(settings.PersonalAccount)
	if err != nil {
		return Settings{}, err
	}
	organizations := []string{}
	for _, value := range settings.Organizations {
		name, err := owner(value)
		if err != nil {
			return Settings{}, err
		}
		if name != "" && !slices.Contains(organizations, name) {
			organizations = append(organizations, name)
		}
	}
	settings.Organizations = organizations
	if settings.Mode == "live" && settings.PersonalAccount == "" && len(settings.Organizations) == 0 {
		return Settings{}, errors.New("add a personal GitHub account or organization for live data")
	}
	repositories := []string{}
	for _, value := range settings.Repositories {
		canonical, err := gh.CanonicalRepo(strings.TrimSpace(value))
		if err != nil {
			return Settings{}, fmt.Errorf("repository %q must use owner/name", value)
		}
		owner, _, _ := strings.Cut(canonical, "/")
		if settings.Mode == "live" && owner != settings.PersonalAccount && !slices.Contains(settings.Organizations, owner) {
			return Settings{}, fmt.Errorf("repository %s is outside the configured accounts", canonical)
		}
		if !slices.Contains(repositories, canonical) {
			repositories = append(repositories, canonical)
		}
	}
	settings.Repositories = repositories
	return settings, nil
}

func loadSettings(path string) (Settings, analysisConfig, []byte, error) {
	analysis := analysisConfig{PRRequestBudget: gh.MaxRefreshRequests}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "config.json"))
	if err == nil {
		if err := json.Unmarshal(data, &analysis); err != nil {
			return Settings{}, analysis, nil, fmt.Errorf("reading existing Bigboard configuration: %w", err)
		}
		analysis.Identities, err = git.NormalizeIdentities(analysis.Identities)
		if err != nil {
			return Settings{}, analysis, nil, err
		}
	} else if !os.IsNotExist(err) {
		return Settings{}, analysis, nil, err
	}
	if analysis.PRRequestBudget < 1 {
		analysis.PRRequestBudget = gh.MaxRefreshRequests
	}
	initial := Settings{Mode: "cache", PersonalAccount: analysis.PersonalAccount, Organizations: analysis.Organizations}
	if initial.PersonalAccount != "" || len(initial.Organizations) > 0 {
		initial.Mode = "live"
	}
	settings := initial
	settings.Organizations = slices.Clone(initial.Organizations)
	data, err = os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return Settings{}, analysis, nil, fmt.Errorf("reading desktop settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return Settings{}, analysis, nil, err
	}
	if settings.Mode == "demo" {
		settings = initial
	}
	settings, err = normalizeSettings(settings)
	return settings, analysis, data, err
}

func saveSettings(path string, settings Settings, expected []byte) ([]byte, error) {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if !bytes.Equal(current, expected) {
		return nil, errors.New("desktop settings changed outside this window; restart Bigboard before saving")
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".desktop-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return nil, err
	}
	return data, nil
}
