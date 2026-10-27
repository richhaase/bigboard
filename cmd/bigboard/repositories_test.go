package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestRepositoryConfigRejectsLegacySchemaWithoutRewriting(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"paths", `{"paths":["~/src"]}`, "legacy paths"},
		{"empty paths", `{"organizations":["example"],"paths":[]}`, "legacy paths"},
		{"null paths", `{"paths":null}`, "legacy paths"},
		{"depth", `{"depth":2}`, "legacy depth"},
		{"zero depth", `{"organizations":["example"],"depth":0}`, "legacy depth"},
		{"null depth", `{"depth":null}`, "legacy depth"},
		{"absolute group path", `{"organizations":["example"],"groups":{"backend":["/src/api"]}}`, "groups.backend"},
		{"home group path", `{"organizations":["example"],"groups":{"backend":["~/api"]}}`, "groups.backend"},
		{"nested group path", `{"organizations":["example"],"groups":{"backend":["src/example/api"]}}`, "groups.backend"},
		{"bare group name", `{"organizations":["example"],"groups":{"backend":["api"]}}`, "groups.backend"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			source := []byte(tc.body)
			if err := os.WriteFile(path, source, 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(path, true); err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), repositoryConfigExample) {
				t.Fatalf("migration diagnostic = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, source) {
				t.Fatal("loading a legacy config changed its contents")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatal("loading a legacy config changed its permissions")
			}
		})
	}
}

func TestRepositoryConfigCanonicalizesOrganizationsAndGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	source := []byte(`{"organizations":[" Example ","Other"],"groups":{"backend":["EXAMPLE/API","Other/Worker"]},"exclude":["archive-*"],"since":"90d"}`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRepositoryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Organizations, []string{"example", "other"}) || !reflect.DeepEqual(cfg.Groups["backend"], []string{"example/api", "other/worker"}) {
		t.Fatalf("repository identities = %+v, %+v", cfg.Organizations, cfg.Groups)
	}
	if !reflect.DeepEqual(cfg.Exclude, []string{"archive-*"}) || cfg.Since != "90d" {
		t.Fatal("repository validation changed unrelated settings")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, source) || !bytes.Equal(cfg.source, source) {
		t.Fatal("canonicalization rewrote the original configuration")
	}
}

func TestRepositoryConfigRejectsInvalidOrganizationScope(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing", Config{}, "organizations is required"},
		{"empty", Config{Organizations: []string{" "}}, "invalid organization"},
		{"path", Config{Organizations: []string{"../example"}}, "invalid organization"},
		{"url", Config{Organizations: []string{"https://github.com/example"}}, "invalid organization"},
		{"repository", Config{Organizations: []string{"example/api"}}, "invalid organization"},
		{"outside organization", Config{Organizations: []string{"example"}, Groups: map[string][]string{"backend": {"example-other/api"}}}, "outside configured organizations"},
		{"group path", Config{Organizations: []string{"example"}, Groups: map[string][]string{"backend": {"/src/api"}}}, "owner/name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRepositoryConfig(&tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSelectRepositoriesUsesExactOrganizationsAndGroupIdentities(t *testing.T) {
	repos := []git.Repository{
		{ID: "example/api", Name: "example/api", Path: "/cache/example/api.git", DefaultBranch: "main"},
		{ID: "example/worker", Name: "example/worker", Path: "/cache/example/worker.git", DefaultBranch: "trunk"},
		{ID: "example-other/api", Name: "example-other/api", Path: "/cache/example-other/api.git", DefaultBranch: "main"},
		{ID: "other/api", Name: "other/api", Path: "/cache/other/api.git", DefaultBranch: "stable"},
	}
	cfg := &Config{Organizations: []string{"example", "other"}, Groups: map[string][]string{
		"backend": {"example/api", "other/api"},
		"empty":   {},
		"missing": {"example/api", "example/missing"},
	}}
	for _, tc := range []struct {
		group string
		want  []git.Repository
	}{
		{"", []git.Repository{repos[0], repos[1], repos[3]}},
		{"backend", []git.Repository{repos[0], repos[3]}},
		{"empty", nil},
	} {
		t.Run(tc.group, func(t *testing.T) {
			got, err := selectRepositories(cfg, tc.group, repos)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selection = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		group string
		want  string
	}{
		{"unknown", `unknown group "unknown"`},
		{"missing", "group repository example/missing is unavailable or archived"},
	} {
		t.Run(tc.group, func(t *testing.T) {
			got, err := selectRepositories(cfg, tc.group, repos)
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("selection = %+v, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCacheExclusionsMatchRepositoryIdentityAndBasename(t *testing.T) {
	repos := []git.Repository{
		{ID: "example/api", Name: "example/api", Path: "/cache/example/api.git"},
		{ID: "other/api", Name: "other/api", Path: "/cache/other/api.git"},
		{ID: "example/worker", Name: "example/worker", Path: "/cache/example/worker.git"},
	}
	for _, tc := range []struct {
		pattern string
		want    map[string]bool
	}{
		{"api", map[string]bool{"example/api": true, "other/api": true}},
		{"example/api", map[string]bool{"example/api": true}},
		{"example/*", map[string]bool{"example/api": true, "example/worker": true}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			got, err := buildExcludeSet(repos, []string{tc.pattern})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("excluded = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
