package tui

import (
	"errors"
	gh "github.com/richhaase/bigboard/github"
	"strings"
	"testing"
)

func TestPRBrowserListDetailAndErrors(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			m := prFixture()
			m.width, m.height = 80, 30
			m.showPRs, m.prAll, m.prDetail = true, true, detail
			calls := 0
			m.prBrowserOpener = func(url string) error {
				calls++
				if url != "https://github.com/org/repo/pull/7" {
					t.Fatalf("wrong URL %q", url)
				}
				if fail {
					return errors.New("launcher failed")
				}
				return nil
			}
			if !strings.Contains(m.View(), "o open PR in browser") {
				t.Fatal("action missing")
			}
			next, cmd := m.handleKey(key("o"))
			m = next.(Model)
			if cmd == nil || calls != 0 {
				t.Fatal("opener must be asynchronous")
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			if calls != 1 || m.prDetail != detail || !m.showPRs {
				t.Fatal("opener altered navigation")
			}
			if strings.Contains(m.View(), "Could not open browser: launcher failed") != fail {
				t.Fatal("error visibility incorrect")
			}
		}
	}
}

func TestPRBrowserRejectsInvalidAndEmpty(t *testing.T) {
	good := gh.PullRequest{Repo: "org/repo", Number: 7, URL: "https://github.com/org/repo/pull/7"}
	for _, url := range []string{"", "http://github.com/org/repo/pull/7", "https://evil.test/org/repo/pull/7", "https://github.com/org/repo/pull/8", "https://github.com/org/repo/pull/7?x=1", "file:///tmp/pr", "https://github.com/org/repo/pull/7;open x"} {
		pr := good
		pr.URL = url
		if prBrowserURL(pr) != "" {
			t.Fatalf("accepted %q", url)
		}
	}
	for _, repo := range []string{"../repo", "org/..", "org/repo/extra", "org/repo%2fissues", "org/repo;bad"} {
		pr := good
		pr.Repo = repo
		pr.URL = "https://github.com/" + repo + "/pull/7"
		if prBrowserURL(pr) != "" {
			t.Fatal("invalid repo accepted")
		}
	}
	m := prFixture()
	m.width, m.height = 80, 30
	m.showPRs, m.prAll = true, true
	m.prState.snapshots = nil
	m.prBrowserOpener = func(string) error { t.Fatal("empty opened browser"); return nil }
	_, cmd := m.handleKey(key("o"))
	if cmd != nil || strings.Contains(m.View(), "o open PR") {
		t.Fatal("empty action")
	}
	m = prFixture()
	m.width, m.height = 80, 30
	m.showPRs, m.prAll = true, true
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[0].URL = "file:///tmp/pr"
	m.prState.snapshots["org/repo"] = snapshot
	next, cmd := m.handleKey(key("o"))
	m = next.(Model)
	if cmd != nil || strings.Contains(m.View(), "o open PR") || !strings.Contains(m.View(), "canonical GitHub URL unavailable") {
		t.Fatal("invalid URL should show error without opening")
	}
}

func TestPRBrowserDoesNotCaptureRelatedShortcut(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "3")
	m.prBrowserOpener = func(string) error { t.Fatal("Related opened browser"); return nil }
	next, cmd := m.handleKey(key("o"))
	m = next.(Model)
	if cmd != nil || m.showPRs || !strings.Contains(m.View(), "CONTRIBUTOR OVERLAP") {
		t.Fatal("Related o changed")
	}
}

func TestBrowserCommandUsesArgumentVector(t *testing.T) {
	url := "https://github.com/org/repo/pull/7"
	for platform, command := range map[string]string{"darwin": "open", "linux": "xdg-open", "windows": "rundll32"} {
		got, args, err := browserCommand(platform, url)
		if err != nil || got != command || args[len(args)-1] != url {
			t.Fatalf("invalid %s launch", platform)
		}
	}
	if _, _, err := browserCommand("unsupported", url); err == nil {
		t.Fatal("missing unsupported error")
	}
}

func TestPRBrowserDetailCanScrollToFinalPath(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := prFixture()
		m.width, m.height = width, 18
		m.showPRs, m.prAll, m.prDetail = true, true, true
		snapshot := m.prState.snapshots["org/repo"]
		for range 40 {
			snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: "extra/path.go"})
		}
		snapshot.prs[0].Files = append(snapshot.prs[0].Files, gh.File{Path: "FINAL-PATH.go"})
		m.prState.snapshots["org/repo"] = snapshot
		m.prBrowserError = "Could not open browser: failed"
		for range 300 {
			next, _ := m.handleKey(key("down"))
			m = next.(Model)
		}
		view := m.View()
		for _, want := range []string{"FINAL-PATH.go", "o open PR in browser", "Could not open browser"} {
			if !strings.Contains(view, want) {
				t.Fatalf("width %d missing %s: %s", width, want, view)
			}
		}
	}
}

func TestPRBrowserOpensSelectedRow(t *testing.T) {
	m := prOnlySelectionFixture()
	m.showPRs, m.prAll = true, true
	m.prRow = 2
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[2].URL = "https://github.com/org/repo/pull/262"
	m.prState.snapshots["org/repo"] = snapshot
	calls := 0
	m.prBrowserOpener = func(url string) error {
		calls++
		if url != "https://github.com/org/repo/pull/262" {
			t.Fatal("wrong selection")
		}
		return nil
	}
	_, cmd := m.handleKey(key("o"))
	if cmd == nil {
		t.Fatal("no launch")
	}
	cmd()
	if calls != 1 {
		t.Fatal("not launched once")
	}
}
