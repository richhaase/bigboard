package main

import (
	"os"
	"path/filepath"
	"testing"

	gh "github.com/richhaase/bigboard/github"
)

func TestPRRequestBudgetConfig(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        int
		invalid     bool
	}{
		{name: "default", input: `{}`, want: gh.MaxRefreshRequests},
		{name: "configured", input: `{"pr_request_budget":256}`, want: 256},
		{name: "minimum", input: `{"pr_request_budget":1}`, want: 1},
		{name: "zero", input: `{"pr_request_budget":0}`, invalid: true},
		{name: "negative", input: `{"pr_request_budget":-1}`, invalid: true},
		{name: "fractional", input: `{"pr_request_budget":1.5}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadConfig(path, true)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid budget accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PRRequestBudget != tc.want {
				t.Fatalf("budget = %d, want %d", cfg.PRRequestBudget, tc.want)
			}
		})
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	cfg, err := loadConfig(missing, false)
	if err != nil || cfg.PRRequestBudget != gh.MaxRefreshRequests {
		t.Fatalf("missing implicit config: %+v, %v", cfg, err)
	}
}
