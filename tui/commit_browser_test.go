package tui

import (
	"errors"
	"strings"
	"testing"

	gh "github.com/richhaase/bigboard/github"
)

func commitBrowserFixture() Model {
	m := monorepoFixture()
	m.width, m.height = 120, 30
	m.repositories[0].Path = "/mono"
	m.local.records[0].CommitID = strings.Repeat("a", 40)
	m.local.records[1].CommitID = strings.Repeat("b", 40)
	m.local.records[1].Subject = "Fix behavior (#123)"
	m.local.records[1].Changes = m.local.records[0].Changes
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "1")
	m = pressAwareness(m, "down")
	return m
}

func TestCommitBrowserURLValidation(t *testing.T) {
	for _, hash := range []string{strings.Repeat("a", 40), strings.Repeat("b", 64)} {
		if got := commitBrowserURL("Org/Repo", hash); got != "https://github.com/org/repo/commit/"+hash {
			t.Fatalf("wrong commit URL: %q", got)
		}
	}
	for _, hash := range []string{"", "abc1234", strings.Repeat("g", 40), strings.Repeat("a", 39), strings.Repeat("a", 40) + "?x=1", strings.Repeat("a", 40) + "\n"} {
		if got := commitBrowserURL("org/repo", hash); got != "" {
			t.Fatalf("accepted invalid commit ID %q", hash)
		}
	}
	for _, repo := range []string{"", "../repo", "org/..", "org/repo/extra", "org/repo%2fother", "https://evil.test/org/repo"} {
		if got := commitBrowserURL(repo, strings.Repeat("a", 40)); got != "" {
			t.Fatalf("accepted invalid repository %q", repo)
		}
	}
}

func TestCommitBrowserSelectedRowAndInspector(t *testing.T) {
	for _, inspector := range []bool{false, true} {
		m := commitBrowserFixture()
		if inspector {
			m = pressAwareness(m, "enter")
		}
		provider := &fakePRProvider{repoByPath: map[string]string{"/mono": "org/repo"}}
		m.prProvider = provider
		calls := 0
		m.prBrowserOpener = func(url string) error {
			calls++
			if want := "https://github.com/org/repo/commit/" + strings.Repeat("b", 40); url != want {
				t.Fatalf("opened %q, want selected commit %q", url, want)
			}
			return nil
		}
		next, cmd := m.handleKey(key("o"))
		m = next.(Model)
		if cmd == nil || calls != 0 || provider.resolves != 0 {
			t.Fatal("commit opening must be asynchronous")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		if calls != 1 || provider.resolves != 1 || provider.fetches != 0 || m.showPaths != inspector || m.showPRs {
			t.Fatal("opening must preserve context and only read the local origin")
		}
		if !strings.Contains(m.View(), "o commit") {
			t.Fatal("commit browser footer action missing")
		}
		found := false
		for _, section := range m.currentHelp().sections {
			for _, binding := range section.bindings {
				found = found || binding.key == "o" && strings.Contains(strings.ToLower(binding.action), "commit")
			}
		}
		if !found {
			t.Fatal("commit browser help missing")
		}
	}
}

func TestCommitBrowserFailuresVisible(t *testing.T) {
	for _, inspector := range []bool{false, true} {
		for _, originFailure := range []bool{false, true} {
			m := commitBrowserFixture()
			if inspector {
				m = pressAwareness(m, "enter")
			}
			provider := &fakePRProvider{}
			if originFailure {
				provider.resolveErr = gh.ErrUnsupportedOrigin
			}
			m.prProvider = provider
			calls := 0
			m.prBrowserOpener = func(string) error {
				calls++
				return errors.New("launcher failed")
			}
			next, cmd := m.handleKey(key("o"))
			if cmd == nil {
				t.Fatal("no origin-resolution command")
			}
			m = next.(Model)
			next, _ = m.Update(cmd())
			m = next.(Model)
			if m.commitBrowserError == "" || !strings.Contains(m.View(), "Could not open commit:") {
				t.Fatal("failure must be visible in the current commit view")
			}
			if originFailure && calls != 0 || !originFailure && calls != 1 || provider.fetches != 0 {
				t.Fatal("origin failure must not open a browser or fetch")
			}
		}
	}
}

func TestCommitBrowserResolvesCurrentOriginEachTime(t *testing.T) {
	m := commitBrowserFixture()
	provider := &fakePRProvider{repoByPath: map[string]string{"/mono": "org/first"}}
	m.prProvider = provider
	var urls []string
	m.prBrowserOpener = func(url string) error { urls = append(urls, url); return nil }
	for _, repo := range []string{"org/first", "org/second"} {
		provider.repoByPath["/mono"] = repo
		next, cmd := m.handleKey(key("o"))
		if cmd == nil {
			t.Fatal("missing command")
		}
		m = next.(Model)
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	if len(urls) != 2 || !strings.Contains(urls[0], "/org/first/") || !strings.Contains(urls[1], "/org/second/") || provider.fetches != 0 {
		t.Fatal("stale origin association used")
	}
}

func TestCommitBrowserLeavesOtherContextsAndSearchAlone(t *testing.T) {
	for _, tab := range []int{glancePeople, glanceRelated, glanceSubareas} {
		m := commitBrowserFixture()
		m.glance.frame.tab = tab
		m.prBrowserOpener = func(string) error { t.Fatal("unrelated context opened browser"); return nil }
		_, cmd := m.handleKey(key("o"))
		if cmd != nil {
			t.Fatal("unrelated context produced browser command")
		}
	}
	m := commitBrowserFixture()
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "o")
	if !m.glance.searching || m.glanceQuery() != "o" {
		t.Fatal("o must remain literal during search")
	}
	m = commitBrowserFixture()
	m.local.records = nil
	_, cmd := m.handleKey(key("o"))
	if cmd != nil {
		t.Fatal("empty commit selection produced command")
	}
}
