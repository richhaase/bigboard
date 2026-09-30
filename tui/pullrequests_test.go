package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type fakePRProvider struct {
	resolves, fetches int
	result            gh.Result
	ctx               context.Context
	repoByPath        map[string]string
	resolveErr        error
}

func (p *fakePRProvider) Resolve(ctx context.Context, path string) (string, error) {
	p.resolves++
	p.ctx = ctx
	if p.resolveErr != nil {
		return "", p.resolveErr
	}
	if p.repoByPath != nil {
		if repo := p.repoByPath[path]; repo != "" {
			return repo, nil
		}
		return "", gh.ErrUnsupportedOrigin
	}
	return "org/repo", nil
}
func (p *fakePRProvider) Fetch(_ context.Context, _ string) gh.Result { p.fetches++; return p.result }
func prFixture() Model {
	m := awarenessFixture()
	pr := gh.PullRequest{Repo: "org/repo", Number: 7, URL: "https://github.com/org/repo/pull/7", Title: "Add feature", Author: "robot[bot]", UpdatedAt: time.Now(), ReviewDecision: "UNKNOWN", CheckState: "PENDING", Mergeable: "UNKNOWN", Files: []gh.File{{Path: "feature/new.go"}, {Path: "ui/new.go"}}, FilesComplete: true, ContextComplete: true, PreviousPathsComplete: true}
	m.prState = prState{repositories: map[string]string{"/api": "org/repo", "/web": "org/repo"}, snapshots: map[string]prSnapshot{"org/repo": {prs: []gh.PullRequest{pr}, checked: time.Now()}}}
	return m
}
func TestPRRefreshAutomaticallySchedulesWithoutOptions(t *testing.T) {
	m := awarenessFixture()
	p := &fakePRProvider{result: gh.Result{Complete: true}}
	m.prProvider = p
	cmd := m.startPRRefresh()
	if cmd == nil {
		t.Fatal("automatic provider not scheduled")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if p.resolves != len(m.loadedRepos) || p.fetches != 1 || m.prState.loading {
		t.Fatal("automatic refresh incomplete")
	}
}
func TestPRRefreshDeduplicatesAndRejectsOldGeneration(t *testing.T) {
	m := prFixture()
	p := &fakePRProvider{result: gh.Result{Repo: "org/repo", Complete: true}}
	m.prProvider = p
	cmd := m.startPRRefresh()
	msg := cmd().(prsLoadedMsg)
	if p.fetches != 1 || p.resolves != 3 {
		t.Fatalf("calls %d %d", p.resolves, p.fetches)
	}
	m.cancelPRRefresh()
	next, _ := m.Update(msg)
	m = next.(Model)
	if len(m.allPRs()) != 1 {
		t.Fatal("obsolete generation applied")
	}
	if p.ctx.Err() == nil {
		t.Fatal("context not canceled")
	}
	cmd = m.startPRRefresh()
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.allPRs()) != 0 {
		t.Fatal("complete empty must clear closed PRs")
	}
}
func TestPRFailurePartialAndOriginFailureRetainEvidence(t *testing.T) {
	m := prFixture()
	now := time.Now()
	old := m.allPRs()[0]
	result := gh.Result{Repo: "org/repo", Complete: false, Err: errors.New("rate limited")}
	m.applyPRResult(prsLoadedMsg{repositories: map[string]string{}, errors: map[string]error{"/api": errors.New("origin unavailable")}, results: map[string]gh.Result{"org/repo": result}, checked: now})
	if len(m.allPRs()) != 1 || m.prState.repositories["/api"] != "org/repo" || !strings.Contains(m.prStatus(), "STALE") {
		t.Fatal("lost stale evidence")
	}
	old.Files = nil
	old.FilesComplete = false
	result.PRs = []gh.PullRequest{old}
	m.applyPRResult(prsLoadedMsg{results: map[string]gh.Result{"org/repo": result}, checked: now})
	if len(m.allPRs()[0].Files) != 2 {
		t.Fatal("partial files silently lost old mappings")
	}
}
func TestPRScopeDedupAndLocalFiltersIndependent(t *testing.T) {
	m := prFixture()
	before, _, _, _ := m.aggregateTotals()
	m.hideBots = true
	m.timeIdx = 0
	m.personID = "email:unknown"
	m.recomputeAuthors()
	if len(m.allPRs()) != 1 {
		t.Fatal("duplicate checkout or local filters changed PRs")
	}
	m.areaRepoID = "/api"
	m.selectedAreaID = "auto:feature"
	if len(m.scopePRs("auto:feature")) != 1 || len(m.scopePRs("auto:ui")) != 1 {
		t.Fatal("multi-area mapping missing")
	}
	areas := m.currentWorkAreas()
	found := false
	for _, a := range areas {
		if a.ID == "auto:feature" {
			found = true
			if len(a.Records) != 0 {
				t.Fatal("fabricated PR commits")
			}
		}
	}
	if !found {
		t.Fatal("PR-only area absent")
	}
	after, _, _, _ := m.aggregateTotals()
	if before != after {
		t.Fatal("PR changed commit statistics")
	}
	m.excludedRepos["/api"] = true
	m.excludedRepos["/web"] = true
	if len(m.allPRs()) != 0 {
		t.Fatal("excluded repos leaked")
	}
}
func TestPROverlayNavigationResizeAndTerminalSafety(t *testing.T) {
	m := prFixture()
	snap := m.prState.snapshots["org/repo"]
	snap.prs[0].Title = "hostile\x1b]52;c;payload\a\n title"
	snap.prs[0].Files = append(snap.prs[0].Files, gh.File{Path: strings.Repeat("long/", 30) + "last.go"})
	m.prState.snapshots["org/repo"] = snap
	m = pressAwareness(m, "p")
	if !m.showPRs {
		t.Fatal("p did not open")
	}
	m = pressAwareness(m, "enter")
	if !m.prDetail {
		t.Fatal("no detail")
	}
	for _, size := range [][2]int{{120, 40}, {40, 18}, {20, 8}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		out := m.View()
		if strings.Contains(out, "\x1b]52") {
			t.Fatal("terminal injection")
		}
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("overflow width %d: %q", size[0], line)
			}
		}
		if len(strings.Split(out, "\n")) > size[1] {
			t.Fatal("height overflow")
		}
	}
	m = pressAwareness(m, "esc")
	if m.prDetail || !m.showPRs {
		t.Fatal("detail escape")
	}
	m = pressAwareness(m, "esc")
	if m.showPRs || m.selectedRepoID != "/api" {
		t.Fatal("list escape lost scope")
	}
}
func TestPRInitialLoadSchedulesOnlyAfterLocalScan(t *testing.T) {
	m := awarenessFixture()
	m.prProvider = &fakePRProvider{}
	m.loading = true
	m.resetPending()
	for i, repo := range m.repositories {
		next, cmd := m.Update(RepoLoadedMsg{Repository: repo, Records: []git.CommitRecord{}})
		m = next.(Model)
		if i == len(m.repositories)-1 && cmd == nil {
			t.Fatal("missing initial PR refresh")
		}
	}
	if m.cancelPRs != nil {
		m.cancelPRs()
	}
}

func TestPRCompleteInventoryRemovesClosedEvenWithPartialContext(t *testing.T) {
	m := prFixture()
	incoming := m.allPRs()[0]
	incoming.Number = 8
	incoming.FilesComplete = false
	incoming.ContextComplete = false
	m.applyPRResult(prsLoadedMsg{results: map[string]gh.Result{"org/repo": {Repo: "org/repo", Complete: true, PRs: []gh.PullRequest{incoming}}}, checked: time.Now()})
	if len(m.allPRs()) != 1 || m.allPRs()[0].Number != 8 || !m.prState.snapshots["org/repo"].partial {
		t.Fatal("complete inventory retained closed PR")
	}
	m.areaRepoID = "/api"
	if len(m.scopePRs("unknown")) != 1 {
		t.Fatal("partial path scope missing")
	}
}
func TestPRRefreshKeyCancelsSupersededFetch(t *testing.T) {
	m := prFixture()
	m.prProvider = &fakePRProvider{}
	first := m.startPRRefresh()
	generation := m.prGeneration
	m = pressAwareness(m, "R")
	if m.prGeneration <= generation || m.prState.loading {
		t.Fatal("local refresh did not cancel PR generation")
	}
	next, _ := m.Update(first())
	m = next.(Model)
	if !m.loading {
		t.Fatal("old PR message interfered with local refresh")
	}
}

func TestPRRefreshPreservesSelectedIdentityAndClosesRemovedDetail(t *testing.T) {
	m := prFixture()
	m.showPRs = true
	m.prAll = true
	m.prDetail = true
	old := m.allPRs()[0]
	newer := old
	newer.Number = 99
	newer.UpdatedAt = old.UpdatedAt.Add(time.Hour)
	m.applyPRResult(prsLoadedMsg{results: map[string]gh.Result{"org/repo": {Complete: true, PRs: []gh.PullRequest{newer, old}}}})
	if !m.prDetail || m.visiblePRs()[m.prRow].Number != 7 {
		t.Fatal("refresh silently changed selected PR")
	}
	m.applyPRResult(prsLoadedMsg{results: map[string]gh.Result{"org/repo": {Complete: true, PRs: []gh.PullRequest{newer}}}})
	if m.prDetail {
		t.Fatal("removed PR detail remained open")
	}
}

func TestPRAutomaticAuthUnavailableKeepsLocalHistoryAndStopsRepeatingRequests(t *testing.T) {
	for _, tc := range []struct {
		err   error
		label string
	}{{gh.ErrAuthentication, "gh not signed in"}, {gh.ErrUnavailable, "gh not installed"}} {
		t.Run(tc.label, func(t *testing.T) {
			m := awarenessFixture()
			for i := range m.loadedRepos {
				m.loadedRepos[i].Path = m.loadedRepos[i].ID
			}
			p := &fakePRProvider{repoByPath: map[string]string{"/api": "org/api", "/web": "org/web"}, result: gh.Result{Err: tc.err}}
			m.prProvider = p
			before := len(m.allRecords)
			cmd := m.startPRRefresh()
			if cmd == nil || m.loading {
				t.Fatal("automatic fetch missing or blocks local history")
			}
			next, _ := m.Update(cmd())
			m = next.(Model)
			if p.fetches != 1 {
				t.Fatalf("retried unavailable GitHub CLI %d times", p.fetches)
			}
			if len(m.allRecords) != before || m.loading || m.prState.loading {
				t.Fatal("local history blocked/changed")
			}
			if !strings.Contains(m.prStatus(), tc.label) || !strings.Contains(m.View(), "Fix login") {
				t.Fatalf("missing useful status/history: %s", m.View())
			}
			m = pressAwareness(m, "v")
			if m.viewMode != ViewAggregate {
				t.Fatal("cannot navigate local stats")
			}
		})
	}
}
func TestPRUnsupportedOriginNeverCallsGitHub(t *testing.T) {
	m := awarenessFixture()
	p := &fakePRProvider{resolveErr: gh.ErrUnsupportedOrigin}
	m.prProvider = p
	next, _ := m.Update(m.startPRRefresh()())
	m = next.(Model)
	if p.fetches != 0 || !strings.Contains(m.prStatus(), "unsupported GitHub origin") {
		t.Fatal("unsupported origin queried or hidden")
	}
}
func TestPRAutomaticInitialLoadAndOverlayRefresh(t *testing.T) {
	m := awarenessFixture()
	m.loading = true
	m.resetPending()
	p := &fakePRProvider{result: gh.Result{Complete: true}}
	m.prProvider = p
	var refresh tea.Cmd
	for _, repo := range m.repositories {
		next, cmd := m.Update(RepoLoadedMsg{Repository: repo})
		m = next.(Model)
		if !m.loading {
			refresh = cmd
		}
	}
	if refresh == nil {
		t.Fatal("final scan did not schedule GitHub")
	}
	next, _ := m.Update(refresh())
	m = next.(Model)
	m = pressAwareness(m, "P")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("overlay refresh not scheduled")
	}
	m.Update(cmd())
	if p.fetches != 2 {
		t.Fatalf("automatic fetches=%d", p.fetches)
	}
}
