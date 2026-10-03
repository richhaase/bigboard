package tui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/richhaase/bigboard/github"
)

var prBrowserRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Only the canonical URL matching the retained PR identity is actionable.
func prBrowserURL(pr gh.PullRequest) string {
	if pr.Number <= 0 || !prBrowserRepo.MatchString(pr.Repo) {
		return ""
	}
	for _, part := range strings.Split(pr.Repo, "/") {
		if part == "." || part == ".." {
			return ""
		}
	}
	canonical := fmt.Sprintf("https://github.com/%s/pull/%d", pr.Repo, pr.Number)
	if pr.URL != canonical {
		return ""
	}
	return canonical
}

type prBrowserResult struct{ err error }

func (m Model) openSelectedPR() (tea.Model, tea.Cmd) {
	prs := m.visiblePRs()
	if len(prs) == 0 {
		return m, nil
	}
	url := prBrowserURL(prs[max(0, min(m.prRow, len(prs)-1))])
	if url == "" {
		m.prBrowserError = "Cannot open PR: canonical GitHub URL unavailable"
		return m, nil
	}
	m.prBrowserError = ""
	opener := m.prBrowserOpener
	if opener == nil {
		opener = openPRBrowser
	}
	return m, func() tea.Msg { return prBrowserResult{err: opener(url)} }
}

func browserCommand(platform, url string) (string, []string, error) {
	switch platform {
	case "darwin":
		return "open", []string{url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	default:
		return "", nil, fmt.Errorf("browser opening unsupported on %s", platform)
	}
}

func openPRBrowser(url string) error {
	command, args, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, command, args...).Run()
}
