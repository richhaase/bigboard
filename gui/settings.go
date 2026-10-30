package gui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

type analysisConfig struct {
	Mode                 string                          `json:"mode"`
	Repositories         []string                        `json:"repositories"`
	ExcludedRepositories []string                        `json:"excluded_repositories"`
	PersonalAccount      string                          `json:"personal_account"`
	Organizations        []string                        `json:"organizations"`
	Exclude              []string                        `json:"exclude"`
	AllFiles             bool                            `json:"all_files"`
	AIIdentities         []string                        `json:"ai_identities"`
	BotIdentities        []string                        `json:"bot_identities"`
	Identities           map[string]git.Identity         `json:"identities"`
	PRRequestBudget      int                             `json:"pr_request_budget"`
	Since                string                          `json:"since"`
	Sort                 string                          `json:"sort"`
	Groups               map[string][]string             `json:"groups"`
	WorkAreas            map[string][]stats.WorkAreaRule `json:"work_areas"`

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

	repositories := []string{}
	for _, value := range settings.Repositories {
		canonical, err := gh.CanonicalRepo(strings.TrimSpace(value))
		if err != nil {
			return Settings{}, fmt.Errorf("repository %q must use owner/name", value)
		}
		owner, _, _ := strings.Cut(canonical, "/")
		if settings.Mode == "live" && (settings.PersonalAccount != "" || len(settings.Organizations) > 0) && owner != settings.PersonalAccount && !slices.Contains(settings.Organizations, owner) {
			return Settings{}, fmt.Errorf("repository %s is outside the configured accounts", canonical)
		}
		if !slices.Contains(repositories, canonical) {
			repositories = append(repositories, canonical)
		}
	}
	settings.Repositories = repositories
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
	return settings, nil
}

func loadSettings(path string) (Settings, analysisConfig, error) {
	analysis := analysisConfig{PRRequestBudget: gh.MaxRefreshRequests, Since: "14d", Sort: "total"}
	data, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), "config.json")
	hasMode := false
	if err == nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
			return Settings{}, analysis, errors.New("configuration must be a JSON object")
		}
		_, hasMode = fields["mode"]
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&analysis); err != nil {
			return Settings{}, analysis, fmt.Errorf("reading Bigboard configuration: %w", err)
		}
		analysis.Identities, err = git.NormalizeIdentities(analysis.Identities)
		if err != nil {
			return Settings{}, analysis, err
		}
		analysis.source = slices.Clone(data)
	} else if !os.IsNotExist(err) {
		return Settings{}, analysis, err
	}
	if err := validateAnalysis(&analysis); err != nil {
		return Settings{}, analysis, err
	}
	settings := Settings{Mode: analysis.Mode, PersonalAccount: analysis.PersonalAccount, Organizations: slices.Clone(analysis.Organizations), Repositories: slices.Clone(analysis.Repositories), ExcludedRepositories: slices.Clone(analysis.ExcludedRepositories)}
	if !hasMode {
		settings.Mode = "live"
	}
	settings, err = normalizeSettings(settings)
	return settings, analysis, err
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
