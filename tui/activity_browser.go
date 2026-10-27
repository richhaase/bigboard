package tui

import (
	"context"
	"errors"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type activityBrowserResult struct{ err error }

func activityBrowserURL(repo, revision, path, kind string) string {
	canonical, err := gh.CanonicalRepo(repo)
	if err != nil {
		return ""
	}
	base := "https://github.com/" + canonical
	if path == "" && kind == "" {
		return base
	}
	if commitBrowserURL(repo, revision) == "" || kind != "tree" && kind != "blob" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, '\x00') {
			return ""
		}
		parts[i] = url.PathEscape(part)
	}
	return base + "/" + kind + "/" + revision + "/" + strings.Join(parts, "/")
}

func (m Model) openSelectedActivityScope(rows []activityRow) (tea.Model, tea.Cmd) {
	if len(rows) == 0 || m.glance.detailOpen || m.contributorPane.focused || m.areaRepoID != "" && !m.directory.active {
		return m, nil
	}
	var selected int
	if m.directory.active {
		selected = m.selectedDirectory(rows)
	} else {
		selected = m.selectedRepository(rows)
	}
	row := rows[selected]
	var repo git.Repository
	for _, candidate := range m.repositories {
		if candidate.ID == row.repositoryID {
			repo = candidate
			break
		}
	}
	if repo.Path == "" {
		m.activityBrowserError = "Could not open Activity: repository unavailable"
		return m, nil
	}
	path, kind := "", ""
	if m.directory.active {
		var prefix string
		prefix, path, _ = strings.Cut(row.scopeID, ":")
		switch prefix {
		case "dir":
			kind = "tree"
		case "file":
			kind = "blob"
		default:
			return m, nil
		}
	}
	revision := m.local.revisions[repo.ID]
	m.activityBrowserError = ""
	provider := m.prProvider
	if provider == nil {
		provider = defaultPRProvider()
	}
	opener := m.prBrowserOpener
	if opener == nil {
		opener = openPRBrowser
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gh.RequestTimeout)
		defer cancel()
		remote, err := provider.Resolve(ctx, repo.Path)
		if err != nil {
			if errors.Is(err, gh.ErrUnsupportedOrigin) {
				err = errors.New("no supported GitHub origin")
			}
			return activityBrowserResult{err: err}
		}
		if kind != "" {
			actual, err := git.PathObjectType(ctx, repo, revision, path)
			if err != nil {
				return activityBrowserResult{err: err}
			}
			if actual != kind {
				return activityBrowserResult{err: errors.New("path has a different type at the scanned revision")}
			}
		}
		address := activityBrowserURL(remote, revision, path, kind)
		if address == "" {
			return activityBrowserResult{err: errors.New("canonical GitHub URL unavailable")}
		}
		return activityBrowserResult{err: opener(address)}
	}
}

func (m Model) activityBrowserHeader(lines []string, width int) []string {
	if m.activityBrowserError != "" {
		lines = append(lines, StyleAmber.Render("  "+padCells(m.activityBrowserError, max(1, width-4))))
	}
	return lines
}
