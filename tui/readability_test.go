package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestShortRepositoryLabelsRetainAmbiguity(t *testing.T) {
	repositories := []string{"one/api", "two/api", "one/client", "one/client", "worker", "one/worker", "/legacy/path"}
	want := map[string]string{"one/api": "one/api", "two/api": "two/api", "one/client": "client", "worker": "worker", "one/worker": "one/worker", "/legacy/path": "/legacy/path"}
	if got := shortRepositoryLabels(repositories); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestActivityLabelsOnlyChangePresentation(t *testing.T) {
	m := awarenessFixture()
	m.width, m.height = 160, 40
	for i, name := range []string{"one/api", "two/api", "one/client"} {
		m.repositories[i].Name = name
	}
	m.repositorySort = 3
	before := m.glanceRepositories()
	labels := m.activityRepositoryLabels()
	m.sharedRepos = filterValues{"/api", "/empty"}
	m.excludedRepos["/web"] = true
	m.setGlanceQuery("one/")
	rows := m.glanceRepositories()
	if len(rows) != 2 || rows[0].scopeID != before[0].scopeID || rows[0].label != "one/api" {
		t.Fatalf("canonical search or sorting changed: %+v", rows)
	}
	if got := m.activityRepositoryLabels(); !reflect.DeepEqual(got, labels) {
		t.Fatalf("filter changed repository labels: %v vs %v", got, labels)
	}
	for _, size := range [][2]int{{40, 18}, {80, 24}, {160, 40}} {
		m.width, m.height = size[0], size[1]
		view := ansi.Strip(m.View())
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Fatalf("%v changed frame height", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("%v overflowed: %s", size, line)
			}
		}
		if !strings.Contains(view, "one/api") || strings.Contains(view, "one/client") || !strings.Contains(view, "client") {
			t.Fatalf("%v lost unique display labels: %s", size, view)
		}
	}
}

func TestDisplayPathsAbbreviateOnlyTheHomeDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "user")
	t.Setenv("HOME", home)
	for _, tc := range []struct{ path, want string }{
		{home, "~"},
		{filepath.Join(home, ".cache", "bigboard"), "~/.cache/bigboard"},
		{home + "-other/cache", home + "-other/cache"},
		{"relative/cache", "relative/cache"},
		{"", ""},
	} {
		if got := displayPath(tc.path); got != tc.want {
			t.Fatalf("displayPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
	m := awarenessFixture()
	m.options.Bookmark = &BookmarkOptions{Path: filepath.Join(home, "state.json")}
	if got := strings.Join(m.bookmarkStatusLines(), "\n"); !strings.Contains(got, "State file: ~/state.json") || m.options.Bookmark.Path != filepath.Join(home, "state.json") {
		t.Fatalf("bookmark path changed or was not abbreviated: %s", got)
	}
}

func TestReadabilityCoverageAndHelpKeepWarningsAndCommands(t *testing.T) {
	m := awarenessFixture()
	m.width, m.height = 80, 30
	m.options.Bookmark = &BookmarkOptions{Time: time.Now().Add(-time.Hour)}
	coverage := m.screenCoverage()
	if strings.Contains(coverage, "fetch on") || strings.Contains(coverage, "since last look") || strings.Contains(coverage, "incomplete/stale") || strings.Contains(coverage, "no unknown") {
		t.Fatalf("healthy coverage repeats instructions or legends: %s", coverage)
	}
	m.local.records[0].LinesUnknown = true
	m.local.records[0].PathsUnknown = true
	if got := m.screenCoverage(); !strings.HasPrefix(got, "Unknown line counts") || !strings.Contains(got, "1 commits with unknown paths") {
		t.Fatalf("coverage lost uncertainty: %s", got)
	}
	for _, prs := range []bool{false, true} {
		m.showPRs = prs
		help := ansi.Strip(strings.Join(m.helpLines(78), "\n"))
		lastCommand := strings.Index(help, "Resume PRs")
		legend := strings.Index(help, "Git history")
		if prs {
			legend = strings.Index(help, "Legend")
		}
		if lastCommand < 0 || legend < lastCommand || !strings.Contains(help, "saved bookmark") {
			t.Fatalf("commands do not precede legends or bookmark legend missing: %s", help)
		}
	}
}
