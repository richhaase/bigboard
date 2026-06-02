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
	repos := []string{"/a/api", "/b/web", "/c/vendor-lib", "/d/experiment-1"}
	ex := buildExcludeSet(repos, []string{"web", "vendor-*", "experiment-*"})
	for _, want := range []string{"web", "vendor-lib", "experiment-1"} {
		if !ex[want] {
			t.Errorf("expected %q excluded", want)
		}
	}
	if ex["api"] {
		t.Errorf("api should not be excluded")
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
