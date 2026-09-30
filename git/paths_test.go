package git

import "testing"

func TestIsGeneratedPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"src/app.go", false},
		{"vendor/lib.go", true},
		{"nested/node_modules/package/index.js", true},
		{"mybuild/index.js", false},
		{"build/index.js", true},
		{"assets/site.min.js", true},
		{"assets/site.js", false},
		{"go.sum", true},
		{"nested/package-lock.json", true},
		{"vendor/naïve\nfile.go", true},
		{"src/{old.go => new.go}", false},
	} {
		if got := IsGeneratedPath(tc.path); got != tc.want {
			t.Errorf("IsGeneratedPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsGeneratedPathIgnoresLegacyGlobalSettings(t *testing.T) {
	oldFilter, oldDirs, oldGlobs := FilterGeneratedPaths, IgnoredDirs, IgnoredFileGlobs
	t.Cleanup(func() {
		FilterGeneratedPaths, IgnoredDirs, IgnoredFileGlobs = oldFilter, oldDirs, oldGlobs
	})
	FilterGeneratedPaths = false
	IgnoredDirs = []string{"src"}
	IgnoredFileGlobs = []string{"*.go"}
	if !IsGeneratedPath("vendor/file.go") || !IsGeneratedPath("go.sum") {
		t.Fatal("default generated exclusions changed with legacy settings")
	}
	if IsGeneratedPath("src/app.go") {
		t.Fatal("legacy custom exclusions leaked into the default classifier")
	}
}
