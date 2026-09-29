package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
	"github.com/richhaase/bigboard/tui"
)

func TestTimeIndexForSince(t *testing.T) {
	cases := []struct {
		in   string
		want int
		err  bool
	}{
		{"", tui.DefaultTimeIndex, false},
		{"1d", 0, false},
		{"14d", 2, false},
		{"90d", 4, false},
		{"1y", 5, false},
		{"all", len(tui.TimePresets) - 1, false},
		{"ALL", len(tui.TimePresets) - 1, false},
		{" 30d ", 3, false},
		{"6m", 0, true},
		{"21d", 0, true},
		{"nonsense", 0, true},
	}
	for _, tc := range cases {
		got, err := timeIndexForSince(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("timeIndexForSince(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("timeIndexForSince(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("timeIndexForSince(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestBuildExcludeSet(t *testing.T) {
	repos := git.NewRepositories([]string{"/a/api", "/b/web", "/c/vendor-lib", "/d/experiment-1"})
	ex, err := buildExcludeSet(repos, []string{"web", "vendor-*", "experiment-*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range repos {
		want := repo.Name != "api"
		if ex[repo.ID] != want {
			t.Errorf("excluded[%q] = %v, want %v", repo.ID, ex[repo.ID], want)
		}
	}
	if _, err := buildExcludeSet(repos, []string{"["}); err == nil {
		t.Error("invalid glob should return an error")
	}

	literal := git.NewRepositories([]string{"/a/[archive"})
	ex, err = buildExcludeSet(literal, []string{"[archive"})
	if err != nil {
		t.Fatalf("exact repository name was rejected as a glob: %v", err)
	}
	if !ex[literal[0].ID] {
		t.Error("exact repository name containing '[' was not excluded")
	}

	globAndLiteral := git.NewRepositories([]string{"/a/*", "/b/api"})
	ex, err = buildExcludeSet(globAndLiteral, []string{"*"})
	if err != nil {
		t.Fatalf("valid glob matching a literal repository name failed: %v", err)
	}
	for _, repo := range globAndLiteral {
		if !ex[repo.ID] {
			t.Errorf("valid glob did not exclude %q", repo.Name)
		}
	}
}

func TestBuildExcludeSetMatchesUniqueDisplayName(t *testing.T) {
	repos := git.NewRepositories([]string{"/src/org-a/api", "/src/org-b/api"})
	ex, err := buildExcludeSet(repos, []string{"org-a/api"})
	if err != nil {
		t.Fatal(err)
	}
	if !ex[repos[0].ID] {
		t.Error("display name should exclude the matching duplicate")
	}
	if ex[repos[1].ID] {
		t.Error("display name must not exclude the other duplicate")
	}

	ex, err = buildExcludeSet(repos, []string{"org-b/*"})
	if err != nil {
		t.Fatal(err)
	}
	if ex[repos[0].ID] || !ex[repos[1].ID] {
		t.Errorf("display-name glob matched wrong repos: %v", ex)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if cfg, err := loadConfig(missing, false); err != nil || cfg == nil {
		t.Errorf("non-explicit missing config should return empty cfg, no error; got cfg=%v err=%v", cfg, err)
	}
	if _, err := loadConfig(missing, true); err == nil {
		t.Errorf("explicit missing config should error")
	}

	unknown := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"paths":[],"typo":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(unknown, true); err == nil {
		t.Error("unknown config field should return an error")
	}
}

func TestLoadConfigIdentityLists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"ai_identities":["tag@teamsense.com","@agents.example.com"],"bot_identities":["renovate"]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AIIdentities) != 2 || cfg.AIIdentities[0] != "tag@teamsense.com" {
		t.Errorf("ai_identities not loaded: %v", cfg.AIIdentities)
	}
	if len(cfg.BotIdentities) != 1 || cfg.BotIdentities[0] != "renovate" {
		t.Errorf("bot_identities not loaded: %v", cfg.BotIdentities)
	}
}

func TestDiagnosticText(t *testing.T) {
	if got := diagnosticText("repo\x1b\nname"); strings.ContainsAny(got, "\x1b\n") {
		t.Errorf("diagnosticText retained terminal controls: %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandHome("~/src"); got != filepath.Join(home, "src") {
		t.Errorf("expandHome(~/src) = %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome left absolute path alone: %q", got)
	}
}

func TestValidateScanPaths(t *testing.T) {
	if err := validateScanPaths([]string{t.TempDir()}); err != nil {
		t.Fatalf("valid directory: %v", err)
	}
	if err := validateScanPaths([]string{filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("missing scan path should return an error")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateScanPaths([]string{file}); err == nil {
		t.Error("non-directory scan path should return an error")
	}
}

func TestDiscoverReposDepth(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		d := filepath.Join(root, rel)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"git", "init", "-b", "main"}, {"git", "config", "user.email", "x@x.com"}, {"git", "config", "user.name", "X"}} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = d
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s %v", args, out, err)
			}
		}
	}
	mk("org/repoA")
	mk("org/repoB")
	mk("shallow")

	d1 := git.DiscoverReposDepth([]string{root}, 1)
	if len(d1) != 1 { // only "shallow" is one level down; org's repos are two levels
		t.Errorf("depth 1: expected 1 repo, got %d: %v", len(d1), d1)
	}
	d2 := git.DiscoverReposDepth([]string{root}, 2)
	if len(d2) != 3 {
		t.Errorf("depth 2: expected 3 repos, got %d: %v", len(d2), d2)
	}
}

func TestDiscoverReposDepthFollowsSymlinks(t *testing.T) {
	real := t.TempDir()
	repo := filepath.Join(real, "actual")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepoWithCommit(t, repo)

	workspace := t.TempDir()
	link := filepath.Join(workspace, "linked")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	found := git.DiscoverReposDepth([]string{workspace}, 1)
	if len(found) != 1 {
		t.Fatalf("symlinked repo not discovered: %v", found)
	}

	both := git.DiscoverReposDepth([]string{workspace, real}, 1)
	if len(both) != 1 {
		t.Errorf("symlink and real path should deduplicate to one repo: %v", both)
	}
}

func initRepoWithCommit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "ada@x.com"},
		{"git", "config", "user.name", "Ada"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"git", "add", "."}, {"git", "commit", "-m", "init"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
}

func TestRunExportJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir)

	var out, errw bytes.Buffer
	repositories := git.NewRepositories([]string{dir})
	if err := runExportJSON(&out, &errw, repositories, nil, stats.SortByTotal, analysisOptions{}); err != nil {
		t.Fatalf("runExportJSON: %v", err)
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Ada" || authors[0].Added != 3 {
		t.Errorf("unexpected json round-trip: %+v", authors)
	}
	if authors[0].Bot {
		t.Error("human contributor flagged as bot")
	}
	if !strings.Contains(out.String(), `"bot"`) {
		t.Errorf("bot field missing from JSON:\n%s", out.String())
	}
}

func TestRunExportJSONFlagsConfiguredBots(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir)

	var out, errw bytes.Buffer
	repositories := git.NewRepositories([]string{dir})
	options := analysisOptions{BotIdentities: []string{"ada@x.com"}}
	if err := runExportJSON(&out, &errw, repositories, nil, stats.SortByTotal, options); err != nil {
		t.Fatal(err)
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || !authors[0].Bot {
		t.Errorf("configured bot identity not flagged in export: %+v", authors)
	}
}

func TestRunExportJSONKeepsDuplicateBasenamesDistinct(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "org-a", "api"),
		filepath.Join(root, "org-b", "api"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWithCommit(t, path)
	}
	repositories := git.NewRepositories(paths)

	var out, errw bytes.Buffer
	if err := runExportJSON(&out, &errw, repositories, nil, stats.SortByTotal, analysisOptions{}); err != nil {
		t.Fatal(err)
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || len(authors[0].PerRepo) != 2 {
		t.Fatalf("duplicate basenames were conflated: %+v", authors)
	}
	for _, repo := range repositories {
		if _, ok := authors[0].PerRepo[repo.Name]; !ok {
			t.Errorf("missing qualified repo %q in %v", repo.Name, authors[0].PerRepo)
		}
	}

	out.Reset()
	if err := runExportJSON(&out, &errw, repositories, map[string]bool{repositories[0].ID: true}, stats.SortByTotal, analysisOptions{}); err != nil {
		t.Fatal(err)
	}
	authors = nil
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || len(authors[0].PerRepo) != 1 {
		t.Fatalf("excluding one duplicate repo affected the wrong scope: %+v", authors)
	}
	if _, ok := authors[0].PerRepo[repositories[1].Name]; !ok {
		t.Errorf("remaining repo %q missing after exclusion", repositories[1].Name)
	}
}

func TestRunExportJSONAllReposFail(t *testing.T) {
	var out, errw bytes.Buffer
	repositories := git.NewRepositories([]string{t.TempDir()})
	if err := runExportJSON(&out, &errw, repositories, nil, stats.SortByTotal, analysisOptions{}); err == nil {
		t.Error("expected error when all repos fail to scan")
	}
	if out.Len() != 0 {
		t.Errorf("no output should be written when all repos fail, got:\n%s", out.String())
	}
	if errw.Len() == 0 {
		t.Error("expected a warning for the failed repository")
	}
}
