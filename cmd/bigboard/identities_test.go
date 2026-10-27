package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestLoadConfigIdentityAliases(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		bad  bool
	}{
		{"normalized", `{"identities":{" CANONICAL@example.com ":{"aliases":[" ALIAS@example.com "],"name":" Canonical Name "}},"fuzzy":true}`, false},
		{"self alias", `{"identities":{"a@example.com":{"aliases":[" A@example.com "]}}}`, true},
		{"duplicate key", `{"identities":{"a@example.com":{}," A@example.com ":{}}}`, true},
		{"duplicate alias", `{"identities":{"a@example.com":{"aliases":["c@example.com"," C@example.com "]}}}`, true},
		{"shared alias", `{"identities":{"a@example.com":{"aliases":["c@example.com"]},"b@example.com":{"aliases":["C@example.com"]}}}`, true},
		{"alias is canonical", `{"identities":{"a@example.com":{"aliases":["b@example.com"]},"b@example.com":{}}}`, true},
		{"empty canonical", `{"identities":{" ":{}}}`, true},
		{"empty alias", `{"identities":{"a@example.com":{"aliases":[""]}}}`, true},
		{"name as email", `{"identities":{"A Person":{}}}`, true},
		{"display address", `{"identities":{"a@example.com":{"aliases":["Someone <b@example.com>"]}}}`, true},
		{"unknown entry field", `{"identities":{"a@example.com":{"alias":["b@example.com"]}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadConfig(path, true)
			if tc.bad {
				if err == nil {
					t.Fatalf("invalid identity configuration accepted: %+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]git.Identity{"canonical@example.com": {Aliases: []string{"alias@example.com"}, Name: "Canonical Name"}}
			if !reflect.DeepEqual(cfg.Identities, want) || !cfg.Fuzzy {
				t.Fatalf("wrong normalized config: %+v", cfg)
			}
		})
	}
}

func TestExportUsesCanonicalIdentityClassification(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir)
	options := analysisOptions{
		Identities:   map[string]git.Identity{"canonical@example.com": {Aliases: []string{"ada@x.com"}, Name: "Canonical Name"}},
		AIIdentities: []string{"canonical@example.com"}, BotIdentities: []string{"canonical@example.com"},
	}
	var out, diagnostics bytes.Buffer
	if err := runExportJSON(&out, &diagnostics, git.NewRepositories([]string{dir}), nil, stats.SortByTotal, options); err != nil {
		t.Fatal(err)
	}
	var authors []stats.AuthorStats
	if err := json.Unmarshal(out.Bytes(), &authors); err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0].Name != "Canonical Name" || authors[0].Commits != 1 || authors[0].Added != 3 || authors[0].AICommits != 1 || !authors[0].Bot {
		t.Fatalf("export did not use canonical identity: %+v", authors)
	}
	if diagnostics.Len() != 0 || strings.Contains(out.String(), "ada@x.com") {
		t.Fatalf("unexpected output or scan failure: %s\n%s", out.String(), diagnostics.String())
	}
}
