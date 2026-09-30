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
