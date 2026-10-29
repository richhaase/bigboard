package gui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsPreservesConfigurationSymlinks(t *testing.T) {
	for _, kind := range []string{"directory", "files"} {
		t.Run(kind, func(t *testing.T) {
			target := t.TempDir()
			analysisData := []byte(`{"organizations":["example"],"since":"30d","sort":"commits"}`)
			settingsData := []byte(`{"mode":"cache","organizations":["example"],"repositories":["example/engine"]}`)
			for name, data := range map[string][]byte{"config.json": analysisData, "desktop.json": settingsData} {
				if err := os.WriteFile(filepath.Join(target, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			directory := filepath.Join(t.TempDir(), "config")
			if kind == "directory" {
				if err := os.Symlink(target, directory); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"config.json", "desktop.json"} {
					if err := os.Symlink(filepath.Join(target, name), filepath.Join(directory, name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			settings, analysis, source, err := loadSettings(filepath.Join(directory, "desktop.json"))
			if err != nil {
				t.Fatal(err)
			}
			if settings.Mode != "cache" || len(settings.Repositories) != 1 || settings.Repositories[0] != "example/engine" {
				t.Fatalf("settings were not loaded through symlinks: %+v", settings)
			}
			if analysis.Since != "30d" || analysis.Sort != "commits" || !bytes.Equal(analysis.source, analysisData) || !bytes.Equal(source, settingsData) {
				t.Fatal("configuration or source bytes changed through symlinks")
			}
		})
	}
}
