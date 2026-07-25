package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"", 0, false},
		{"all", 0, false},
		{"0", 0, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"2w", 2 * 7 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		{"48h", 48 * time.Hour, false},
		{"-1d", 0, true},
		{"-1h", 0, true},
		{"1000y", 0, true},
		{"nonsense", 0, true},
	}
	for _, tc := range cases {
		got, err := parseSince(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("parseSince(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseSince(%q) = %v, want %v", tc.in, got, tc.want)
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
}

func TestPick(t *testing.T) {
	if got := pick(true, "flagv", "cfgv", "def"); got != "flagv" {
		t.Errorf("set flag should win, got %q", got)
	}
	if got := pick(false, "flagv", "cfgv", "def"); got != "cfgv" {
		t.Errorf("config should win when flag unset, got %q", got)
	}
	if got := pick(false, "flagv", "", "def"); got != "def" {
		t.Errorf("default should win when flag unset and cfg empty, got %q", got)
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

func TestExportFormats(t *testing.T) {
	authors := []stats.AuthorStats{
		{Name: "Ada", Commits: 5, Added: 100, Removed: 10, Net: 90, TotalChange: 110, AICommits: 1, ActiveDays: 3},
		{Name: "Grace", Commits: 2, Added: 20, Removed: 0, Net: 20, TotalChange: 20},
	}

	var csv bytes.Buffer
	if err := exportCSV(&csv, authors); err != nil {
		t.Fatalf("exportCSV: %v", err)
	}
	if !strings.Contains(csv.String(), "contributor") || !strings.Contains(csv.String(), "Ada") {
		t.Errorf("CSV missing header or data:\n%s", csv.String())
	}

	var md bytes.Buffer
	if err := exportMarkdown(&md, authors); err != nil {
		t.Fatalf("exportMarkdown: %v", err)
	}
	if !strings.Contains(md.String(), "| # |") || !strings.Contains(md.String(), "Grace") {
		t.Errorf("markdown missing header or data:\n%s", md.String())
	}

	var unsafeCSV bytes.Buffer
	if err := exportCSV(&unsafeCSV, []stats.AuthorStats{{Name: "=1+1"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unsafeCSV.String(), "'=1+1") {
		t.Errorf("CSV formula was not neutralized:\n%s", unsafeCSV.String())
	}
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

func TestExportMarkdownEscapesPipes(t *testing.T) {
	authors := []stats.AuthorStats{{Name: "Foo | Bar", Commits: 1}}
	var md bytes.Buffer
	if err := exportMarkdown(&md, authors); err != nil {
		t.Fatalf("exportMarkdown: %v", err)
	}
	out := md.String()
	if strings.Contains(out, "Foo | Bar") {
		t.Errorf("unescaped pipe in name would inject a table column:\n%s", out)
	}
	if !strings.Contains(out, "Foo \\| Bar") {
		t.Errorf("expected escaped pipe:\n%s", out)
	}
	if got := mdCell(`A\|B <tag>`); got != `A\\\|B &lt;tag&gt;` {
		t.Errorf("mdCell adversarial escaping = %q", got)
	}
}

func TestRunExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir)

	for _, format := range []string{"json", "csv", "md"} {
		var out, errw bytes.Buffer
		if err := runExport(&out, &errw, format, []string{dir}, nil, 0, stats.SortByTotal); err != nil {
			t.Fatalf("runExport(%s): %v", format, err)
		}
		if out.Len() == 0 {
			t.Errorf("runExport(%s) produced no output", format)
		}
	}

	var out, errw bytes.Buffer
	if err := runExport(&out, &errw, "json", []string{dir}, nil, 0, stats.SortByTotal); err != nil {
		t.Fatalf("runExport json: %v", err)
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Ada" || authors[0].Added != 3 {
		t.Errorf("unexpected json round-trip: %+v", authors)
	}

	if err := runExport(&out, &errw, "xml", []string{dir}, nil, 0, stats.SortByTotal); err == nil {
		t.Error("invalid format should error")
	}
}

func TestRunExportKeepsDuplicateBasenamesDistinct(t *testing.T) {
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
	if err := runExportWithOptions(&out, &errw, "json", repositories, nil, 0, stats.SortByTotal, analysisOptions{}); err != nil {
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
	if err := runExportWithOptions(&out, &errw, "json", repositories, map[string]bool{repositories[0].ID: true}, 0, stats.SortByTotal, analysisOptions{}); err != nil {
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

func TestRunExportAllReposFail(t *testing.T) {
	var out, errw bytes.Buffer
	notRepo := t.TempDir()
	if err := runExport(&out, &errw, "json", []string{notRepo}, nil, 0, stats.SortByTotal); err == nil {
		t.Error("expected error when all repos fail to scan")
	}
	if out.Len() != 0 {
		t.Errorf("no output should be written when all repos fail, got:\n%s", out.String())
	}
}

func TestRunExportValidatesFormatBeforeScanning(t *testing.T) {
	var out, errw bytes.Buffer
	err := runExport(&out, &errw, "pdf", []string{filepath.Join(t.TempDir(), "missing")}, nil, 0, stats.SortByTotal)
	if err == nil || !strings.Contains(err.Error(), "invalid --export format") {
		t.Fatalf("runExport invalid format error = %v", err)
	}
	if errw.Len() != 0 {
		t.Fatalf("invalid format should not scan repositories: %s", errw.String())
	}
}
