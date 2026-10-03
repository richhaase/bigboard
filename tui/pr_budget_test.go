package tui

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"strings"
	"testing"
	"time"
)

func TestPRQuotaStopsWholeQueueAndRetainsSnapshot(t *testing.T) {
	for _, err := range []error{nil, gh.ErrRateLimited} {
		m := prFixture()
		retry := time.Now().Add(time.Hour)
		p := &fakePRProvider{repoByPath: map[string]string{}, result: gh.Result{Complete: err == nil, Err: err, RetryAt: retry}}
		m.prProvider = p
		m.loadedRepos = nil
		for i := 0; i < 30; i++ {
			path := fmt.Sprintf("/repo%d", i)
			m.loadedRepos = append(m.loadedRepos, git.Repository{ID: path, Path: path})
			p.repoByPath[path] = fmt.Sprintf("org/repo%d", i)
		}
		msg := m.startPRRefresh()().(prsLoadedMsg)
		m.applyPRResult(msg)
		if p.fetches != 1 {
			t.Fatalf("fetches=%d", p.fetches)
		}
		if !m.prState.nextRefresh.Equal(retry) {
			t.Fatal("quota reset not retained")
		}
		if cmd := m.startPRRefresh(); cmd != nil {
			t.Fatal("cooldown ignored")
		}
		if !strings.Contains(m.prStatus(), "R after") {
			t.Fatal("cooldown hidden")
		}
		for _, result := range msg.results {
			if result.Repo != "" && result.Repo != "org/repo0" && !errors.Is(result.Err, gh.ErrRateLimited) {
				t.Fatal("skipped repo claims success")
			}
		}
	}
}
func TestPRFreshSnapshotAndActiveRefreshAreReused(t *testing.T) {
	m := prFixture()
	p := &fakePRProvider{result: gh.Result{Complete: true}}
	m.prProvider = p
	cmd := m.startPRRefresh()
	gen := m.prGeneration
	if next := m.startPRRefresh(); next != nil || m.prGeneration != gen {
		t.Fatal("active work restarted")
	}
	m.applyPRResult(cmd().(prsLoadedMsg))
	if next := m.startPRRefresh(); next != nil || p.fetches != 1 {
		t.Fatal("fresh snapshot re-fetched")
	}
	m.prState.nextRefresh = time.Time{}
	next := m.startPRRefresh()
	if next == nil {
		t.Fatal("expired snapshot not refreshable")
	}
	m.applyPRResult(next().(prsLoadedMsg))
	if p.fetches != 2 {
		t.Fatal(p.fetches)
	}
}
func TestPRExcludedRemotesNeverResolveOrFetch(t *testing.T) {
	m := prFixture()
	p := &fakePRProvider{result: gh.Result{Complete: true}}
	m.prProvider = p
	for _, r := range m.loadedRepos {
		m.excludedRepos[r.ID] = true
	}
	m.startPRRefresh()()
	if p.resolves != 0 || p.fetches != 0 {
		t.Fatalf("excluded work %d %d", p.resolves, p.fetches)
	}
}

// This runner returns real provider envelopes but never executes gh or git.
type budgetRunner struct{ calls int }

func (r *budgetRunner) Run(_ context.Context, cmd string, args ...string) ([]byte, error) {
	if cmd == "git" {
		for i, a := range args {
			if a == "-C" && i+1 < len(args) {
				return []byte("https://github.com/org/" + strings.TrimPrefix(args[i+1], "/") + ".git\n"), nil
			}
		}
	}
	r.calls++
	var name string
	for _, a := range args {
		if strings.HasPrefix(a, "name=") {
			name = strings.TrimPrefix(a, "name=")
		}
	}
	return []byte(fmt.Sprintf(`{"data":{"repository":{"nameWithOwner":"org/%s","pullRequests":{"totalCount":0,"pageInfo":{"hasNextPage":false},"nodes":[]}},"rateLimit":{"remaining":5000}}}`, name)), nil
}
func TestPRGlobalBudgetWithRealProvider(t *testing.T) {
	m := awarenessFixture()
	r := &budgetRunner{}
	m.prProvider = gh.NewProvider(r)
	m.loadedRepos = nil
	for i := 0; i < gh.MaxRefreshRequests+5; i++ {
		path := fmt.Sprintf("/repo%d", i)
		m.loadedRepos = append(m.loadedRepos, git.Repository{ID: path, Path: path})
	}
	msg := m.startPRRefresh()().(prsLoadedMsg)
	if r.calls != gh.MaxRefreshRequests {
		t.Fatal(r.calls)
	}
	partial := 0
	for _, result := range msg.results {
		if errors.Is(result.Err, gh.ErrBudgetExceeded) {
			partial++
			if result.Complete {
				t.Fatal("unfetched inventory complete")
			}
		}
	}
	if partial != 5 {
		t.Fatal(partial)
	}
}
func TestPROverlayNavigationDoesNotFetch(t *testing.T) {
	m := prFixture()
	p := &fakePRProvider{}
	m.prProvider = p
	for _, key := range []string{"p", "enter", "down", "up", "esc", "p"} {
		m.handlePRKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	}
	if p.fetches != 0 {
		t.Fatal("navigation fetched")
	}
}

func TestManualLocalRefreshPreservesFirstRemoteFetch(t *testing.T) {
	for _, view := range []ViewMode{ViewAwareness, ViewAggregate} {
		m := awarenessFixture()
		m.viewMode = view
		p := &fakePRProvider{result: gh.Result{Repo: "org/repo", Complete: true}}
		m.prProvider = p
		first := m.startPRRefresh()
		generation := m.prGeneration
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
		m = next.(Model)
		for _, repo := range m.repositories {
			next, cmd := m.Update(RepoLoadedMsg{Generation: m.scanGeneration, Repository: repo})
			m = next.(Model)
			if m.pendingRemaining == 0 && cmd != nil {
				t.Fatal("first remote fetch replaced")
			}
		}
		if m.prGeneration != generation || !m.prState.loading {
			t.Fatal("remote generation invalidated")
		}
		next, _ = m.Update(first())
		m = next.(Model)
		if p.fetches != 1 || m.prState.loading || len(m.prState.snapshots) == 0 {
			t.Fatal("first snapshot lost")
		}
	}
}
