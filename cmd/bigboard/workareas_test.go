package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkAreaConfiguration(t *testing.T) {
	cases := []struct {
		body  string
		valid bool
	}{
		{`{"work_areas":{"mono":[{"name":"Authentication","paths":["services/auth","apps/web/auth"]}]}}`, true},
		{`{"work_areas":{"mono":[{"name":"Bad","paths":["../outside"]}]}}`, false},
		{`{"work_areas":{"mono":[{"name":"A","paths":["src"]},{"name":"B","paths":["src"]}]}}`, false},
		{`{"work_areas":{" ":[]}}`, false},
		{`{"depth":2}`, true},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path, true)
		if (err == nil) != tc.valid {
			t.Fatalf("loadConfig(%s)=%+v,%v", tc.body, cfg, err)
		}
	}
}

func TestGitHubConfigAutomaticUnlessExplicitlyDisabled(t *testing.T) {
	for _, tc := range []struct {
		text    string
		enabled bool
	}{{`{}`, true}, {`null`, true}, {`{"paths":["/src"],"theme":"dark"}`, true}, {`{"github":{}}`, true}, {`{"github":null}`, true}, {`{"github":{"enabled":null}}`, true}, {`{"github":{"enabled":false}}`, false}, {`{"github":{"enabled":true}}`, true}} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(tc.text), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path, true)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != tc.text {
			t.Fatal("loading rewrote existing configuration")
		}
		if cfg.GitHub.Enabled != tc.enabled {
			t.Fatalf("enabled=%t", cfg.GitHub.Enabled)
		}
	}
}

func TestMissingOptionalConfigEnablesAutomaticPRs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	cfg, err := loadConfig(path, false)
	if err != nil || !cfg.GitHub.Enabled {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("loading missing config wrote a config file")
	}
	if _, err := loadConfig(path, true); err == nil {
		t.Fatal("explicit missing config accepted")
	}
}
