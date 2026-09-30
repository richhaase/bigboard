package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestObsoleteGitHubSettingIsIgnored(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"github":{}}`, `{"github":null}`, `{"github":{"enabled":null}}`, `{"github":{"enabled":false}}`, `{"github":{"enabled":true}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path, true)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "github") {
			t.Fatal("obsolete switch retained in Config")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != body {
			t.Fatal("loading rewrote configuration")
		}
	}
}

func TestMissingOptionalConfigEnablesAutomaticPRs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	cfg, err := loadConfig(path, false)
	if err != nil || cfg == nil {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("loading missing config wrote a config file")
	}
	if _, err := loadConfig(path, true); err == nil {
		t.Fatal("explicit missing config accepted")
	}
}
