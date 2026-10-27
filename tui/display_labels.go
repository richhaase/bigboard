package tui

import (
	"os"
	"path/filepath"
	"strings"
)

func shortRepositoryLabels(repositories []string) map[string]string {
	labels := make(map[string]string, len(repositories))
	owners := make(map[string]string, len(repositories))
	ambiguous := make(map[string]bool)
	for _, repository := range repositories {
		name := repository
		owner, short, ok := strings.Cut(repository, "/")
		if ok && owner != "" && short != "" && !strings.Contains(short, "/") {
			name = short
		}
		if other, exists := owners[name]; exists && other != repository {
			ambiguous[name] = true
		}
		owners[name] = repository
		labels[repository] = name
	}
	for repository, name := range labels {
		if ambiguous[name] {
			labels[repository] = repository
		}
	}
	return labels
}

func (m Model) activityRepositoryLabels() map[string]string {
	names := make([]string, 0, len(m.repositories)+len(m.local.repositories))
	for _, repo := range m.repositories {
		names = append(names, repo.Name)
	}
	for _, repo := range m.local.repositories {
		names = append(names, repo.Name)
	}
	return shortRepositoryLabels(names)
}

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && filepath.IsAbs(home) && filepath.IsAbs(path) {
		home = filepath.Clean(home)
		if path == home {
			path = "~"
		} else if relative, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~" + string(filepath.Separator) + relative
		}
	}
	return displayText(path)
}
