package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func testIdentityMapping() map[string]git.Identity {
	return map[string]git.Identity{"canonical@example.com": {Aliases: []string{"alias@example.com"}, Name: "Combined Name"}}
}

func TestIdentitySaverPreservesConfigAndSupportsRepeatedMerges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	source := []byte(`{"paths":["/repositories"],"exclude":["archive"],"sort":"commits","since":"90d","groups":{"team":["/repositories/api"]},"github":{"enabled":false},"fuzzy":true,"ai_identities":["agent@example.com"],"bot_identities":["bot@example.com"],"work_areas":{}}`)
	if err := os.WriteFile(path, source, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	save := newIdentitySaver(path, cfg)
	if before, err := os.ReadFile(path); err != nil || !bytes.Equal(before, source) {
		t.Fatal("creating callback changed configuration")
	}
	mapping := testIdentityMapping()
	for i := range 2 {
		if err := save(mapping); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]any
		if err := json.Unmarshal(source, &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &after); err != nil {
			t.Fatal(err)
		}
		delete(after, "identities")
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("unrelated config changed: %+v", after)
		}
		loaded, err := loadConfig(path, true)
		if err != nil || !reflect.DeepEqual(loaded.Identities, mapping) {
			t.Fatalf("mapping not saved: %+v %v", loaded, err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
			t.Fatal("existing configuration permissions changed")
		}
		if i == 0 {
			mapping, err = git.MergeIdentities(mapping, "canonical@example.com", []string{"canonical@example.com", "another@example.com"}, "")
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
		t.Fatal("temporary configuration file left behind")
	}
}

func TestIdentitySaverCreatesMissingConfigOnConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "bigboard", "config.json")
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatal(err)
	}
	save := newIdentitySaver(path, cfg)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("configuration directory created before save")
	}
	if err := save(testIdentityMapping()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("new config must be private")
	}
	loaded, err := loadConfig(path, true)
	if err != nil || !reflect.DeepEqual(loaded.Identities, testIdentityMapping()) {
		t.Fatalf("new configuration unusable: %+v %v", loaded, err)
	}
}

func TestIdentitySaverCreatesConfigThroughSymlinkedParent(t *testing.T) {
	dir, target := t.TempDir(), t.TempDir()
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "nested", "config.json")
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatal(err)
	}
	save := newIdentitySaver(path, cfg)
	for range 2 {
		if err := save(testIdentityMapping()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "nested", "config.json")); err != nil {
		t.Fatal("configuration did not reach the symlink target")
	}
}

func TestIdentitySaverRejectsExternalChangesAndReadOnlyFiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(string) error
	}{
		{"changed", func(path string) error { return os.WriteFile(path, []byte(`{"since":"1d"}`), 0o600) }},
		{"invalid", func(path string) error { return os.WriteFile(path, []byte(`{"unfinished":`), 0o600) }},
		{"deleted", os.Remove},
		{"read only", func(path string) error { return os.Chmod(path, 0o400) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadConfig(path, true)
			if err != nil {
				t.Fatal(err)
			}
			save := newIdentitySaver(path, cfg)
			if err := tc.change(path); err != nil {
				t.Fatal(err)
			}
			before, beforeErr := os.ReadFile(path)
			if err := save(testIdentityMapping()); err == nil {
				t.Fatal("unsafe save succeeded")
			}
			after, afterErr := os.ReadFile(path)
			if !bytes.Equal(before, after) || (beforeErr == nil) != (afterErr == nil) {
				t.Fatal("failed save changed external configuration")
			}
		})
	}
}

func TestIdentitySaverDoesNotOverwriteNewExternalConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatal(err)
	}
	save := newIdentitySaver(path, cfg)
	external := []byte(`{"since":"1d"}`)
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := save(testIdentityMapping()); err == nil {
		t.Fatal("new external configuration overwritten")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, external) {
		t.Fatal("external configuration changed")
	}
}

func TestIdentitySaverPreservesSymlinkAndRejectsRetargeting(t *testing.T) {
	dir := t.TempDir()
	target, other, path := filepath.Join(dir, "managed.json"), filepath.Join(dir, "other.json"), filepath.Join(dir, "config.json")
	for _, file := range []string{target, other} {
		if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	save := newIdentitySaver(path, cfg)
	if err := save(testIdentityMapping()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(path); err != nil || got != target {
		t.Fatal("saving replaced the configuration symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := save(testIdentityMapping()); err == nil {
		t.Fatal("retargeted configuration symlink accepted")
	}
	if data, err := os.ReadFile(other); err != nil || string(data) != `{}` {
		t.Fatal("retargeted file changed")
	}
}
