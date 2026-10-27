package tui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
)

func TestMarkdownCopyScopeFiltersAndUnknown(t *testing.T) {
	m := awarenessFixture()
	m.width, m.height = 40, 10
	m.sharedRepos = filterValues{"/api"}
	m.activityPeople = filterValues{"email:ada@x"}
	m.activityHideBots = true
	m.local.records[0].LinesUnknown = true
	m.local.staleRepos["/api"] = true
	m.glance.overviewQueries[0] = "api"
	got := m.markdownList()
	for _, want := range []string{"# Activity", "Shared: All time", "Viewed repositories: api", "Find: api", "bots hidden", "email:ada@x", "STALE", "PARTIAL", "| 1 | api | 1 | 1 | ? | ? |", "entire filtered list"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "| web |") || strings.Contains(got, "| empty |") {
		t.Fatal("repository filter lost")
	}
}

func TestMarkdownCopyContributorsAndCommits(t *testing.T) {
	m := awarenessFixture()
	m.viewMode = ViewAggregate
	m.filterQuery = "Ada"
	m.recomputeAuthors()
	got := m.markdownList()
	if !strings.Contains(got, "| 1 | Ada | email:ada@x |") || strings.Contains(got, "Grace") {
		t.Fatalf("contributor search lost: %s", got)
	}
	m = monorepoFixture()
	m.local.records[0].Subject = "Full | title `with` [link](x) <html>\nnext\x1b[31m"
	m.openDirectoryRepository("/mono")
	m.directory.frame.path = "services"
	if !strings.Contains(m.markdownList(), "| auth/ |") || strings.Contains(m.markdownList(), "| apps/ |") {
		t.Fatal("directory scope lost")
	}
	m.openDirectoryDetails("dir:services/auth", "services/auth", glanceActivity)
	m.personID = "email:ada@x"
	m.glance.frame.queries[glanceActivity] = "Full"
	got = m.markdownList()
	for _, want := range []string{"Activity · Commits", "Repository: mono", "dir:services/auth", "Focused contributor: email:ada@x", "Full \\| title \\`with\\` \\[link\\](x) &lt;html&gt; next", "| auth |"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") || strings.Contains(got, "Reconcile invoices") {
		t.Fatal("unfiltered or terminal control data copied")
	}
}

func TestMarkdownCopyPRRangeFiltersAndQualifiers(t *testing.T) {
	m := prFixture()
	m.showPRs = true
	m.prUI.filters.author = filterValues{"robot[bot]"}
	m.prUI.query = "feature"
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.partial = true
	snapshot.err = errors.New("offline")
	old := snapshot.prs[0]
	old.Number, old.Title, old.UpdatedAt = 8, "Old feature", time.Now().Add(-90*24*time.Hour)
	snapshot.prs = append(snapshot.prs, old)
	m.prState.snapshots["org/repo"] = snapshot
	m.timeIdx = 1
	got := m.markdownList()
	for _, want := range []string{"PR last updated", "Find: feature", "PARTIAL ? STALE", "| org/repo | 7 |", "https://github.com/org/repo/pull/7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Old feature") {
		t.Fatal("PR range ignored")
	}
	m.prUI.filters.author = filterValues{"someone-else"}
	if strings.Contains(m.markdownList(), "| org/repo | 7 |") {
		t.Fatal("PR author filter ignored")
	}
}

func TestMarkdownCopyContextsAndStatus(t *testing.T) {
	for _, context := range []string{"search", "help", "filters", "detail", "people", "loading", "inspector", "identity", "saving"} {
		t.Run(context, func(t *testing.T) {
			m := awarenessFixture()
			switch context {
			case "search":
				m.glance.searching = true
			case "help":
				m.showHelp = true
			case "filters":
				m.openFilters("")
			case "detail":
				m.viewMode = ViewOperative
			case "people":
				m.glance.detailOpen, m.glance.frame.tab = true, glancePeople
			case "loading":
				m.loading = true
			case "inspector":
				m.showPaths = true
			case "identity":
				m.identityMerge.open = true
			case "saving":
				m.identityMerge.open, m.identityMerge.saving = true, true
			}
			if m.canCopyMarkdown() {
				t.Fatal("unsupported context enabled")
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
			if cmd != nil || next.(Model).copyStatus != "" {
				t.Fatal("copy dispatched from unsupported context")
			}
			if context == "search" && next.(Model).glanceQuery() != "y" {
				t.Fatal("search did not retain literal y")
			}
		})
	}
	m := awarenessFixture()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil || !strings.Contains(next.(Model).copyStatus, "Sending") {
		t.Fatal("copy not dispatched")
	}
	next, _ = m.Update(clipboardResult{rows: 3})
	if !strings.Contains(next.(Model).View(), "3 rows as Markdown") {
		t.Fatal("missing copy status")
	}
	next, _ = m.Update(clipboardResult{err: io.ErrClosedPipe})
	if !strings.Contains(next.(Model).copyStatus, "failed") {
		t.Fatal("missing write error")
	}
	m.local.records = []git.CommitRecord{{RepoID: "/api", RepoName: "api", Author: strings.Repeat("a", maxClipboardBytes), Email: "a@x"}}
	m.viewMode = ViewAggregate
	m.recomputeAuthors()
	next, cmd = m.copyMarkdown()
	if cmd != nil || !strings.Contains(next.(Model).copyStatus, "too large") {
		t.Fatal("oversize copy not rejected")
	}
}

func TestClipboardCommandUsesInjectedOutput(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm")
	var output bytes.Buffer
	command := clipboardCommand{text: "# Markdown\n\n| table |"}
	command.SetStdout(&output)
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(command.text)) + "\x07"
	if output.String() != want {
		t.Fatalf("unexpected clipboard sequence: %q", output.String())
	}
	t.Setenv("TMUX", "test")
	output.Reset()
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "\x1bPtmux;") {
		t.Fatal("tmux passthrough absent")
	}
}
