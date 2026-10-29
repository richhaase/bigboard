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
	"github.com/richhaase/bigboard/stats"
)

type analysisConfig struct {
	PersonalAccount string                          `json:"personal_account"`
	Organizations   []string                        `json:"organizations"`
	Exclude         []string                        `json:"exclude"`
	AllFiles        bool                            `json:"all_files"`
	AIIdentities    []string                        `json:"ai_identities"`
	BotIdentities   []string                        `json:"bot_identities"`
	Identities      map[string]git.Identity         `json:"identities"`
	PRRequestBudget int                             `json:"pr_request_budget"`
	Since           string                          `json:"since"`
	Sort            string                          `json:"sort"`
	Groups          map[string][]string             `json:"groups"`
	WorkAreas       map[string][]stats.WorkAreaRule `json:"work_areas"`
	Theme           string                          `json:"theme"`
	Fuzzy           bool                            `json:"fuzzy"`
	GitHub          *struct {
		Enabled bool `json:"enabled"`
	} `json:"github"`
	source []byte
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
	if settings.ExcludedRepositories != nil {
		excluded := []string{}
		for _, id := range settings.ExcludedRepositories {
			canonical, err := gh.CanonicalRepo(strings.TrimSpace(id))
			if err != nil {
				return Settings{}, fmt.Errorf("excluded repository %q must use owner/name", id)
			}
			if !slices.Contains(excluded, canonical) {
				excluded = append(excluded, canonical)
			}
		}
		settings.ExcludedRepositories = excluded
	}
	return settings, nil
}

func loadSettings(path string) (Settings, analysisConfig, []byte, error) {
	analysis := analysisConfig{PRRequestBudget: gh.MaxRefreshRequests, Since: "14d", Sort: "total"}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "config.json"))
	if err == nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
			return Settings{}, analysis, nil, errors.New("Bigboard configuration must be a JSON object")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&analysis); err != nil {
			return Settings{}, analysis, nil, fmt.Errorf("reading existing Bigboard configuration: %w", err)
		}
		analysis.Identities, err = git.NormalizeIdentities(analysis.Identities)
		if err != nil {
			return Settings{}, analysis, nil, err
		}
		analysis.source = slices.Clone(data)
	} else if !os.IsNotExist(err) {
		return Settings{}, analysis, nil, err
	}
	if err := validateAnalysis(&analysis); err != nil {
		return Settings{}, analysis, nil, err
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

func validateAnalysis(cfg *analysisConfig) error {
	cfg.Since = strings.ToLower(strings.TrimSpace(cfg.Since))
	if cfg.Since == "" {
		cfg.Since = "14d"
	}
	if !slices.Contains([]string{"1d", "7d", "14d", "30d", "90d", "1y", "all"}, cfg.Since) {
		return fmt.Errorf("invalid since %q", cfg.Since)
	}
	cfg.Sort = strings.ToLower(strings.TrimSpace(cfg.Sort))
	if cfg.Sort == "" || cfg.Sort == "impact" {
		cfg.Sort = "total"
	}
	if !slices.Contains([]string{"commits", "added", "removed", "net", "ai", "total"}, cfg.Sort) {
		return fmt.Errorf("invalid contributor sort %q", cfg.Sort)
	}
	if cfg.PRRequestBudget < 1 {
		return errors.New("pr_request_budget must be positive")
	}
	for name, members := range cfg.Groups {
		for i, id := range members {
			canonical, err := gh.CanonicalRepo(id)
			if err != nil {
				return fmt.Errorf("groups.%s must contain owner/name repositories", name)
			}
			owner, _, _ := strings.Cut(canonical, "/")
			if !strings.EqualFold(owner, cfg.PersonalAccount) && !slices.ContainsFunc(cfg.Organizations, func(org string) bool { return strings.EqualFold(org, owner) }) {
				return fmt.Errorf("groups.%s contains a repository outside configured sources", name)
			}
			cfg.Groups[name][i] = canonical
		}
	}
	for repo, rules := range cfg.WorkAreas {
		if strings.TrimSpace(repo) == "" {
			return errors.New("work_areas repository cannot be empty")
		}
		if err := stats.ValidateWorkAreaRules(rules); err != nil {
			return fmt.Errorf("work_areas %s: %w", repo, err)
		}
	}
	return nil
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
