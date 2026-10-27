package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestRunExportJSONSelectedRepositoryFailures(t *testing.T) {
	// Fixture creation should not invoke a developer's signing agent or hooks.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	healthy := filepath.Join(root, "healthy")
	for _, args := range [][]string{
		{"init", "-b", "main", healthy},
		{"-C", healthy, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + filepath.Join(root, "no-hooks"), "commit", "--allow-empty", "-m", "fixture"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	repos := git.NewRepositories([]string{filepath.Join(root, "missing"), healthy})
	original := append([]git.Repository(nil), repos...)
	for _, tc := range []struct {
		name        string
		excluded    map[string]bool
		wantError   bool
		wantAuthors int
		wantWarning bool
	}{
		{name: "included failure excluded success", excluded: map[string]bool{repos[1].ID: true}, wantError: true, wantWarning: true},
		{name: "included failure excluded success by name", excluded: map[string]bool{repos[1].Name: true}, wantError: true, wantWarning: true},
		{name: "included success excluded failure", excluded: map[string]bool{repos[0].ID: true}, wantAuthors: 1},
		{name: "mixed included success and failure", wantAuthors: 1, wantWarning: true},
		{name: "all excluded", excluded: map[string]bool{repos[0].ID: true, repos[1].ID: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			err := runExportJSON(&out, &diagnostics, repos, tc.excluded, stats.SortByTotal, analysisOptions{})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v; JSON = %q", err, tc.wantError, out.String())
			}
			if tc.wantError {
				if out.Len() != 0 || !strings.Contains(err.Error(), "all 1 repositories failed") {
					t.Fatalf("failed selected scope produced JSON %q or wrong error %v", out.String(), err)
				}
			} else {
				var authors []stats.AuthorStats
				if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
					t.Fatal(err)
				}
				if len(authors) != tc.wantAuthors || (len(authors) > 0 && authors[0].Commits != 1) {
					t.Fatalf("authors = %+v, want %d authors with one commit", authors, tc.wantAuthors)
				}
				if tc.wantAuthors == 0 && strings.TrimSpace(out.String()) != "[]" {
					t.Fatalf("empty selected scope should remain an empty JSON array: %q", out.String())
				}
			}
			if (diagnostics.Len() > 0) != tc.wantWarning {
				t.Fatalf("diagnostics = %q, want warning %v", diagnostics.String(), tc.wantWarning)
			}
			if !reflect.DeepEqual(repos, original) {
				t.Fatal("export mutated the caller's repository list")
			}
		})
	}
}

func TestExportAfterCacheReplacementLeftovers(t *testing.T) {
	source := t.TempDir()
	initRepoWithCommit(t, source)
	cache := &git.RepositoryCache{Root: filepath.Join(t.TempDir(), "cache")}
	repo, err := cache.Repository("example/project", "main")
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(filepath.Dir(repo.Path), ".bigboard-replaced-1", "project.git")
	for _, path := range []string{repo.Path, backup} {
		for _, args := range [][]string{
			{"clone", "--quiet", "--bare", "--no-local", source, path},
			{"--git-dir", path, "config", "remote.origin.url", "https://github.com/example/project.git"},
			{"--git-dir", path, "update-ref", "refs/remotes/origin/main", "refs/heads/main"},
			{"--git-dir", path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"},
		} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s %v", args, out, err)
			}
		}
		if err := os.WriteFile(filepath.Join(path, "bigboard-cache"), []byte(repo.ID+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(filepath.Dir(backup), old, old); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Organizations: []string{"example"}}
	listed, err := discoverCachedRepositories(t.Context(), cfg, cache, &settingsProviderStub{}, true)
	if err != nil {
		t.Fatalf("offline discovery: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(backup)); !os.IsNotExist(err) {
		t.Fatalf("stale replacement backup retained: %v", err)
	}
	repositories, excluded, err := selectIncludedRepositories(cfg, "", listed)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if err := runExportJSON(&out, &diagnostics, repositories, excluded, stats.SortByTotal, analysisOptions{ScanRepository: cache.ScanRepository}); err != nil {
		t.Fatalf("export: %v: %s", err, diagnostics.String())
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0].Name != "Ada" || authors[0].Added != 3 || diagnostics.Len() != 0 {
		t.Fatalf("export authors=%+v diagnostics=%q", authors, diagnostics.String())
	}
}
