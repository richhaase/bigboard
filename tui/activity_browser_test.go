package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func activityBrowserGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	args = append([]string{"-c", "user.name=Test User", "-c", "user.email=test@example.com", "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + os.DevNull}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func activityBrowserFixture(t *testing.T) Model {
	t.Helper()
	m := directoryFixture()
	dir := t.TempDir()
	activityBrowserGit(t, dir, "init", "-b", "main")
	for _, record := range m.local.records {
		for _, change := range record.Changes {
			path := filepath.Join(dir, change.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	activityBrowserGit(t, dir, "add", ".")
	activityBrowserGit(t, dir, "commit", "-m", "initial")
	m.repositories[0].Path = dir
	m.local.revisions = map[string]string{"/mono": activityBrowserGit(t, dir, "rev-parse", "HEAD")}
	m.prProvider = &fakePRProvider{repoByPath: map[string]string{dir: "org/repo"}}
	return m
}

func openActivityBrowser(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.handleKey(key("o"))
	if cmd == nil {
		t.Fatal("missing Activity browser command")
	}
	next, _ = next.(Model).Update(cmd())
	return next.(Model)
}

func TestActivityBrowserURLs(t *testing.T) {
	revision := strings.Repeat("a", 40)
	for _, test := range []struct{ path, kind, want string }{
		{"", "", "https://github.com/org/repo"},
		{"src/name #?%.go", "blob", "https://github.com/org/repo/blob/" + revision + "/src/name%20%23%3F%25.go"},
		{"src/日本語", "tree", "https://github.com/org/repo/tree/" + revision + "/src/%E6%97%A5%E6%9C%AC%E8%AA%9E"},
		{"src/../other", "tree", ""}, {"/src", "tree", ""}, {"src//file", "blob", ""},
		{"src", "commit", ""}, {"", "tree", ""}, {"nul\x00path", "blob", ""},
	} {
		if got := activityBrowserURL("Org/Repo", revision, test.path, test.kind); got != test.want {
			t.Fatalf("%+v: got %q", test, got)
		}
	}
	if activityBrowserURL("../repo", revision, "src", "tree") != "" || activityBrowserURL("org/repo", "HEAD", "src", "tree") != "" {
		t.Fatal("accepted unsafe repository or moving revision")
	}
}

func TestActivityBrowserRepositoryDirectoryAndFile(t *testing.T) {
	m := activityBrowserFixture(t)
	provider := m.prProvider.(*fakePRProvider)
	var opened []string
	m.prBrowserOpener = func(address string) error { opened = append(opened, address); return nil }
	next, cmd := m.handleKey(key("o"))
	if cmd == nil || provider.resolves != 0 || len(opened) != 0 {
		t.Fatal("repository opening must be asynchronous")
	}
	next, _ = next.(Model).Update(cmd())
	m = next.(Model)
	if len(opened) != 1 || opened[0] != "https://github.com/org/repo" || m.areaRepoID != "" {
		t.Fatalf("wrong repository or changed navigation: %v", opened)
	}
	if !strings.Contains(m.View(), "o repo") {
		t.Fatal("repository browser footer missing")
	}
	m = pressAwareness(m, "enter")
	m.directory.frame.selectedID = "dir:apps"
	m = openActivityBrowser(t, m)
	revision := m.local.revisions["/mono"]
	if opened[1] != "https://github.com/org/repo/tree/"+revision+"/apps" || m.directory.frame.path != "" {
		t.Fatalf("opened unselected directory or changed navigation: %v", opened)
	}
	if !strings.Contains(m.View(), "o path") {
		t.Fatal("directory browser footer missing")
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	m = openActivityBrowser(t, m)
	if opened[2] != "https://github.com/org/repo/blob/"+revision+"/apps/web/login.tsx" || provider.fetches != 0 {
		t.Fatalf("wrong file or remote request: %v", opened)
	}
	provider.repoByPath[m.repositories[0].Path] = "org/renamed"
	m = openActivityBrowser(t, m)
	if !strings.Contains(opened[3], "/org/renamed/") || m.activityBrowserError != "" {
		t.Fatal("origin was not revalidated")
	}
}

func TestActivityBrowserRetainsScanRevisionAcrossLocalHeadChangesAndRefresh(t *testing.T) {
	m := activityBrowserFixture(t)
	dir := m.repositories[0].Path
	revision := m.local.revisions["/mono"]
	activityBrowserGit(t, dir, "rm", "-r", "apps")
	activityBrowserGit(t, dir, "commit", "-m", "remove apps")
	var opened []string
	m.prBrowserOpener = func(address string) error { opened = append(opened, address); return nil }
	m = pressAwareness(m, "enter")
	m.directory.frame.selectedID = "dir:apps"
	m = openActivityBrowser(t, m)
	if len(opened) != 1 || opened[0] != "https://github.com/org/repo/tree/"+revision+"/apps" {
		t.Fatal("browser followed moving local HEAD instead of the retained scan")
	}
	m.options.PrepareRepository = func(context.Context, git.Repository) error { return nil }
	m.options.RepositoryRevision = func(context.Context, git.Repository) (string, error) {
		return activityBrowserGit(t, dir, "rev-parse", "HEAD"), nil
	}
	next, refresh := m.handleKey(key("r"))
	m = runRefreshCommands(t, next.(Model), refresh)
	current := activityBrowserGit(t, dir, "rev-parse", "HEAD")
	if m.local.revisions["/mono"] != current {
		t.Fatal("manual refresh did not retain its new scan revision")
	}
	m.directory.frame.selectedID = "dir:apps"
	m = openActivityBrowser(t, m)
	if len(opened) != 1 || !strings.Contains(m.activityBrowserError, "absent or unavailable") || !strings.Contains(ansi.Strip(m.View()), "Could not open Activity:") {
		t.Fatal("deleted directory opened or failure was not visible")
	}
	m.directory.frame.selectedID = "dir:services"
	m = openActivityBrowser(t, m)
	if len(opened) != 2 || opened[1] != "https://github.com/org/repo/tree/"+current+"/services" || m.activityBrowserError != "" {
		t.Fatal("existing path did not open at refreshed revision")
	}
}

func TestActivityBrowserFailuresAndBlockedContexts(t *testing.T) {
	for _, failure := range []string{"origin", "revision", "launcher", "type"} {
		t.Run(failure, func(t *testing.T) {
			m := activityBrowserFixture(t)
			m = pressAwareness(m, "enter")
			provider := m.prProvider.(*fakePRProvider)
			calls := 0
			m.prBrowserOpener = func(string) error { calls++; return errors.New("launcher failed") }
			switch failure {
			case "origin":
				provider.resolveErr = gh.ErrUnsupportedOrigin
			case "revision":
				m.local.revisions = nil
			case "type":
				m.local.records[0].Changes = []git.PathChange{{Path: "services"}}
				m.local.records = m.local.records[:1]
				m.directory.frame.selectedID = "file:services"
			}
			m = openActivityBrowser(t, m)
			if m.activityBrowserError == "" || !strings.Contains(ansi.Strip(m.View()), "Could not open Activity:") || provider.fetches != 0 {
				t.Fatal("browser failure hidden or performed remote request")
			}
			if failure == "launcher" && calls != 1 || failure != "launcher" && calls != 0 {
				t.Fatal("opened an unsupported selection")
			}
		})
	}
	for _, keys := range [][]string{{"tab"}, {"enter", "tab"}, {"?"}, {"f"}, {"i"}, {"e"}, {"c"}, {"enter", "2"}, {"enter", "3"}} {
		m := activityBrowserFixture(t)
		for _, k := range keys {
			m = pressAwareness(m, k)
		}
		m.prBrowserOpener = func(string) error { t.Fatal("blocked context opened browser"); return nil }
		_, cmd := m.handleKey(key("o"))
		if cmd != nil {
			t.Fatalf("blocked context %v produced command", keys)
		}
	}
	m := activityBrowserFixture(t)
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "o")
	if !m.glance.searching || m.glanceQuery() != "o" {
		t.Fatal("o must remain literal during search")
	}
	m = pressAwareness(m, "no matches")
	m = pressAwareness(m, "enter")
	if _, cmd := m.handleKey(key("o")); cmd != nil {
		t.Fatal("empty filtered selection produced a command")
	}
}

func TestActivityBrowserErrorPreservesRankingPagingAndPane(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 20}, {120, 36}} {
		for _, directory := range []bool{false, true} {
			m := boardLayoutFixture()
			m.width, m.height = size[0], size[1]
			if directory {
				m = keyboardContractPress(t, m, "enter")
			}
			m.activityBrowserError = "Could not open Activity: path is absent or unavailable at the scanned revision"
			m = keyboardContractPress(t, m, "home")
			page := m.worklistPageSize()
			m = keyboardContractPress(t, m, "pgdown")
			rows := m.prepareActivityProjection().rows
			selected := m.selectedRepository(rows)
			if directory {
				selected = m.selectedDirectory(rows)
			}
			if selected != min(page, len(rows)-1) {
				t.Fatal("error message displaced a visible ranking row without updating paging")
			}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "Could not open Activity:") || lipgloss.Height(view) != m.height {
				t.Fatalf("error hidden or height changed at %v: %s", size, view)
			}
			if activityContributorsHeight(m.width, m.height) > 0 && !strings.Contains(view, "CONTRIBUTORS") {
				t.Fatal("error message displaced contributor pane")
			}
		}
	}
}

func TestScanRevisionRequiresStableSuccessfulLookup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		calls := 0
		options := Options{
			RepositoryRevision: func(context.Context, git.Repository) (string, error) {
				calls++
				if calls == 1 {
					return "first", nil
				}
				if fail {
					return "", errors.New("revision unavailable")
				}
				return "moved", nil
			},
			ScanRepository: func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) {
				return []git.CommitRecord{{CommitID: "record"}}, nil
			},
		}
		result := loadRepoCmd(t.Context(), git.Repository{}, options, 1, false)().(RepoLoadedMsg)
		if result.Revision != "" || len(result.Records) != 1 || result.Err != nil {
			t.Fatal("uncertain revision was attached to a successful scan")
		}
	}
}
