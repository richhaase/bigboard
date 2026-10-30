package gui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestUnifiedConfigurationLoadsSettingsAndInfersOnlyAbsentMode(t *testing.T) {
	for _, tc := range []struct {
		name, data, mode string
		valid            bool
	}{
		{"empty", `{}`, "cache", true},
		{"organization", `{"organizations":["Example"]}`, "live", true},
		{"personal", `{"personal_account":"Person"}`, "live", true},
		{"explicit cache", `{"mode":"cache","organizations":["Example"]}`, "cache", true},
		{"explicit empty", `{"mode":"","organizations":["Example"]}`, "", false},
		{"null mode", `{"mode":null}`, "", false},
		{"synthetic", `{"mode":"demo"}`, "", false},
		{"live without sources", `{"mode":"live"}`, "", false},
		{"explicit includes", `{"organizations":["example"],"repositories":["EXAMPLE/engine"]}`, "live", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			settings, _, err := loadSettings(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
			if tc.valid && (settings.Mode != tc.mode || settings.ExcludedRepositories == nil) {
				t.Fatalf("mode or normalized exclusions differ: %+v", settings)
			}
			if tc.name == "explicit includes" && !slices.Equal(settings.Repositories, []string{"example/engine"}) {
				t.Fatalf("explicit include was not canonicalized: %+v", settings)
			}
		})
	}
}

func TestSettingsSaveUsesUnifiedConfigurationAndPreservesAdvancedFields(t *testing.T) {
	s, _, provider := testService(t, "cache")
	original := []byte(`{"organizations":["example"],"sort":"commits","since":"30d","exclude":["vendor-*"],"all_files":true,"ai_identities":["assistant@example.test"],"bot_identities":["robot@example.test"],"identities":{"person@example.test":{"name":"Configured Person"}},"pr_request_budget":42,"groups":{"core":["outside/engine"]},"work_areas":{"example/engine":[{"name":"Core","paths":["src"]}]},"theme":"dark","fuzzy":true,"github":{"enabled":true}}`)
	if err := os.WriteFile(s.path, original, 0640); err != nil {
		t.Fatal(err)
	}
	_, analysis, err := loadSettings(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.analysis = analysis
	before := s.Snapshot()
	if _, err := s.SaveSettings(Settings{Mode: "live", Repositories: []string{"invalid"}}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	unchanged, _ := os.ReadFile(s.path)
	if !bytes.Equal(unchanged, original) || !reflect.DeepEqual(before, s.Snapshot()) || provider.calls != 0 {
		t.Fatal("validation failure changed configuration, snapshot, or remote state")
	}
	settings := Settings{Mode: "cache", PersonalAccount: " Person ", Organizations: []string{" Example ", "example"}, Repositories: []string{"EXAMPLE/engine"}, ExcludedRepositories: []string{"EXAMPLE/skipped"}}
	if _, err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	loaded, saved, err := loadSettings(s.path)
	if err != nil || loaded.PersonalAccount != "person" || !slices.Equal(loaded.Organizations, []string{"example"}) || !slices.Equal(loaded.Repositories, []string{"example/engine"}) || !slices.Equal(loaded.ExcludedRepositories, []string{"example/skipped"}) || loaded.Mode != "cache" {
		t.Fatalf("settings did not round-trip through config.json: %+v, err=%v", loaded, err)
	}
	if saved.Mode != "cache" || s.analysis.PersonalAccount != "person" || !slices.Equal(s.analysis.Organizations, []string{"example"}) {
		t.Fatal("saved settings did not update in-memory configuration")
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var originalFields, savedFields map[string]json.RawMessage
	if err := json.Unmarshal(original, &originalFields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &savedFields); err != nil {
		t.Fatal(err)
	}
	for key, expected := range originalFields {
		if key == "organizations" {
			continue
		}
		var a, b any
		if json.Unmarshal(expected, &a) != nil || json.Unmarshal(savedFields[key], &b) != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("advanced field %s was changed or lost", key)
		}
	}
	if info, err := os.Stat(s.path); err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("configuration permissions changed: %v", err)
	}
}

func TestSettingsAndIdentitySavesShareExternalEditState(t *testing.T) {
	for _, identityFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "settings first", true: "identity first"}[identityFirst], func(t *testing.T) {
			s, _, _ := testService(t, "cache")
			original := []byte(`{"organizations":["example"],"groups":{"core":["outside/engine"]},"since":"30d"}`)
			if err := os.WriteFile(s.path, original, 0600); err != nil {
				t.Fatal(err)
			}
			_, analysis, err := loadSettings(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.analysis = analysis
			settings := Settings{Mode: "cache", Organizations: []string{"example"}, Repositories: []string{"example/engine"}, ExcludedRepositories: []string{"example/skipped"}}
			saveSettings := func() {
				t.Helper()
				if _, err := s.SaveSettings(settings); err != nil {
					t.Fatal(err)
				}
			}
			saveIdentity := func(name string) {
				t.Helper()
				if _, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Name: name}); err != nil {
					t.Fatal(err)
				}
			}
			if identityFirst {
				saveIdentity("First")
			}
			saveSettings()
			saveIdentity("Renamed")
			settings.ExcludedRepositories = []string{"example/another"}
			saveSettings()
			loaded, analysis, err := loadSettings(s.path)
			if err != nil || !slices.Equal(loaded.ExcludedRepositories, settings.ExcludedRepositories) || analysis.Identities["person@example.test"].Name != "Renamed" || analysis.Since != "30d" || !slices.Equal(analysis.Groups["core"], []string{"outside/engine"}) {
				t.Fatalf("interleaved saves lost configuration: %+v %+v, err=%v", loaded, analysis, err)
			}
			external := []byte(`{"organizations":["other"],"since":"7d"}`)
			if err := os.WriteFile(s.path, external, 0600); err != nil {
				t.Fatal(err)
			}
			before := s.Snapshot()
			if _, err := s.SaveSettings(settings); err == nil || !strings.Contains(err.Error(), "outside") {
				t.Fatalf("settings overwrote external configuration: %v", err)
			}
			if _, err := s.SaveIdentity(IdentityRequest{PrimaryEmail: "person@example.test", Name: "Rejected"}); err == nil || !strings.Contains(err.Error(), "outside") {
				t.Fatalf("identity overwrote external configuration: %v", err)
			}
			data, _ := os.ReadFile(s.path)
			if !bytes.Equal(data, external) || !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected save changed file or snapshot")
			}
		})
	}
}

func TestUnifiedSettingsPreserveConfigurationSymlinks(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _ := testService(t, "cache")
			target := t.TempDir()
			targetPath := filepath.Join(target, "config.json")
			original := []byte(`{"mode":"cache","organizations":["example"],"since":"30d","sort":"commits"}`)
			if err := os.WriteFile(targetPath, original, 0640); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(t.TempDir(), "config")
			link := directory
			if kind == "file" {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(directory, "config.json")
			}
			linkTarget := target
			if kind == "file" {
				linkTarget = targetPath
			}
			if err := os.Symlink(linkTarget, link); err != nil {
				t.Fatal(err)
			}
			s.path = filepath.Join(directory, "config.json")
			settings, analysis, err := loadSettings(s.path)
			if err != nil || analysis.Since != "30d" || !bytes.Equal(analysis.source, original) {
				t.Fatalf("configuration symlink did not load: %v", err)
			}
			s.analysis = analysis
			settings.Repositories = []string{"example/engine"}
			if _, err := s.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("save replaced the configuration symlink")
			}
			_, loaded, err := loadSettings(targetPath)
			if err != nil || loaded.Since != "30d" || loaded.Sort != "commits" || !slices.Equal(loaded.Repositories, settings.Repositories) {
				t.Fatalf("save did not preserve advanced fields through symlink: %+v %v", loaded, err)
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			other := t.TempDir()
			otherPath := filepath.Join(other, "config.json")
			if err := os.WriteFile(otherPath, original, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "file" {
				other = otherPath
			}
			if err := os.Symlink(other, link); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveSettings(settings); err == nil || !strings.Contains(err.Error(), "path changed") {
				t.Fatalf("retargeted configuration symlink accepted: %v", err)
			}
		})
	}
}

func TestNewServiceUsesExistingXDGConfigurationLocation(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "home default", true: "custom XDG"}[custom], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := filepath.Join(home, ".config")
			if custom {
				root = t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", root)
			}
			path := filepath.Join(root, "bigboard", "config.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte(`{"organizations":["example"],"since":"30d"}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := NewService()
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.path != path || s.Snapshot().Mode != "live" || s.Snapshot().Preferences.Since != "30d" {
				t.Fatalf("wrong config location or startup settings: %s %+v", s.path, s.Snapshot())
			}
			s.cache = &fakeCache{repositories: []git.Repository{{ID: "example/engine", DefaultBranch: "main"}}}
			s.provider = &fakeProvider{}
			if _, err := s.SaveSettings(Settings{Mode: "cache", Organizations: []string{"example"}, Repositories: []string{"example/engine"}}); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := loadSettings(path)
			if err != nil || loaded.Mode != "cache" {
				t.Fatalf("save missed the existing XDG config file: %+v %v", loaded, err)
			}
		})
	}
}
