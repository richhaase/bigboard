package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func mineFixture() Model {
	m := prFixture()
	m.width, m.height, m.showPRs = 120, 36, true
	m.prState.login = "Viewer"
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs = nil
	for i, author := range []string{"viewer", "other", "other", "VIEWER"} {
		pr := gh.PullRequest{Repo: "org/repo", Number: i + 1, Title: "Change", Author: author, UpdatedAt: time.Now(), CheckState: "SUCCESS", RequestedReviewersComplete: true}
		switch i {
		case 1:
			pr.RequestedReviewers = []string{"VIEWER"}
		case 2:
			pr.RequestedReviewers = []string{"org/viewer"}
		case 3:
			pr.CheckState = "FAILURE"
		}
		snapshot.prs = append(snapshot.prs, pr)
	}
	m.prState.snapshots["org/repo"] = snapshot
	return m
}

func TestPRMineMatchesAccountWithORAndOtherFacetsWithAND(t *testing.T) {
	m := mineFixture()
	m.prUI.filters.mine = filterValues{prMineAuthor}
	if got := m.visiblePRs(); len(got) != 2 {
		t.Fatalf("authored by account: %+v", got)
	}
	m.prUI.filters.mine = filterValues{prMineReview}
	if got := m.visiblePRs(); len(got) != 1 || got[0].Number != 2 {
		t.Fatalf("user review request must exclude team requests: %+v", got)
	}
	m.prUI.filters.mine = filterValues{prMineAuthor, prMineReview}
	if got := m.visiblePRs(); len(got) != 3 {
		t.Fatalf("Mine choices should combine with OR: %+v", got)
	}
	m.prUI.filters.checks = filterValues{"Passing"}
	if got := m.visiblePRs(); len(got) != 2 {
		t.Fatalf("other facets should combine with AND: %+v", got)
	}
	m.prUI.query = "#2"
	if got := m.visiblePRs(); len(got) != 1 || got[0].Number != 2 {
		t.Fatalf("Mine ignored search: %+v", got)
	}
}

func TestPRMineModalStagesSearchesCancelsAndClears(t *testing.T) {
	m := mineFixture()
	m = chooseFilterOption(t, m, "mine", prMineAuthor)
	if !m.prUI.filters.empty() || len(m.visiblePRs()) != 4 {
		t.Fatal("staging Mine mutated applied filters")
	}
	m = keyboardContractPress(t, m, "/", "requested", "enter")
	if options := m.filterOptions(); len(options) != 1 || options[0].value != prMineReview {
		t.Fatalf("Mine options search: %+v", options)
	}
	m = keyboardContractPress(t, m, " ", "enter")
	if len(m.visiblePRs()) != 3 || len(m.prUI.filters.mine) != 2 || !strings.Contains(ansi.Strip(m.View()), "Mine:") {
		t.Fatal("Mine did not apply all staged choices")
	}
	m = chooseFilterOption(t, m, "mine", prMineAuthor)
	m = keyboardContractPress(t, m, "esc")
	if !slices.Equal(m.prUI.filters.mine, filterValues{prMineAuthor, prMineReview}) {
		t.Fatal("cancel mutated applied Mine choices")
	}
	m = chooseFilterOption(t, m, "mine", "")
	m = keyboardContractPress(t, m, "enter")
	if !m.prUI.filters.empty() || len(m.visiblePRs()) != 4 {
		t.Fatal("All did not clear Mine")
	}
	for _, reset := range []string{"x", "modal"} {
		m.prUI.filters.mine, m.prUI.query = filterValues{prMineAuthor}, "Change"
		if reset == "x" {
			m = keyboardContractPress(t, m, "x")
		} else {
			m = keyboardContractPress(t, m, "f", "x", "enter")
		}
		if !m.prUI.filters.empty() || m.prUI.query != "" || len(m.visiblePRs()) != 4 {
			t.Fatalf("%s did not clear Mine and search", reset)
		}
	}
}

func TestPRMineSurvivesBoardAndContextNavigationWithoutApplyingDraft(t *testing.T) {
	m := mineFixture()
	m.prUI.filters.mine = filterValues{prMineAuthor}
	m = chooseFilterOption(t, m, "mine", prMineReview)
	m = keyboardContractPress(t, m, "a", "p")
	if m.filters.open || !slices.Equal(m.prUI.filters.mine, filterValues{prMineAuthor}) || len(m.visiblePRs()) != 2 {
		t.Fatal("board navigation lost Mine or applied its draft")
	}
	m.openContextPRs("/api")
	if !m.prUI.filters.empty() || len(m.visiblePRs()) != 4 {
		t.Fatal("contextual PRs inherited saved Mine choices")
	}
	m.closeContextPRs(false)
	if !slices.Equal(m.prUI.filters.mine, filterValues{prMineAuthor}) || len(m.visiblePRs()) != 2 {
		t.Fatal("context return lost saved Mine choices")
	}
	m.openFilters("mine")
	m.filters.prs.mine[0] = prMineReview
	if m.prUI.filters.mine[0] != prMineAuthor {
		t.Fatal("Mine draft aliases applied state")
	}
}

func TestPRMineUnavailableHasReasonAndNeverFallsBackToAllPRs(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{nil, "login not loaded"}, {gh.ErrAuthentication, "gh not signed in"},
		{gh.ErrUnavailable, "gh not installed"}, {gh.ErrResponse, "login not returned"},
		{gh.ErrRateLimited, "rate limited"}, {context.DeadlineExceeded, "login request timed out"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			m := mineFixture()
			m.prState.login, m.prState.loginErr = "", tc.err
			m.prUI.filters.mine = filterValues{prMineAuthor, prMineReview}
			if len(m.visiblePRs()) != 0 || !strings.Contains(ansi.Strip(m.View()), "Mine unavailable: "+tc.reason) {
				t.Fatal("missing account broadened results or hid the reason")
			}
			m.openFilters("mine")
			if !strings.Contains(ansi.Strip(m.View()), tc.reason) {
				t.Fatal("Mine modal hid the account failure")
			}
			m = keyboardContractPress(t, m, "esc", "x")
			if len(m.visiblePRs()) != 4 {
				t.Fatal("missing account suppressed ordinary PR results")
			}
		})
	}
}

func TestPRMineLoginRefreshesOnceAndClearsAccountAfterFailure(t *testing.T) {
	m := mineFixture()
	p := &fakePRProvider{login: "viewer", result: gh.Result{Repo: "org/repo", Complete: true, PRs: m.allPRs()}}
	m.prProvider = p
	m.prUI.filters.mine = filterValues{prMineAuthor}
	cmd := m.startPRRefresh()
	if m.startPRRefresh() != nil {
		t.Fatal("active refresh was not coalesced")
	}
	m.applyPRResult(cmd().(prsLoadedMsg))
	if p.logins != 1 || m.prState.login != "viewer" || len(m.visiblePRs()) != 2 || m.startPRRefresh() != nil {
		t.Fatal("account was not resolved once per deduplicated refresh")
	}
	m.prState.nextRefresh = time.Time{}
	p.login = "other"
	m.applyPRResult(m.startPRRefresh()().(prsLoadedMsg))
	if p.logins != 2 || m.prState.login != "other" || len(m.visiblePRs()) != 2 || m.visiblePRs()[0].Author != "other" {
		t.Fatal("fresh account did not replace previous Mine identity")
	}
	m.prState.nextRefresh = time.Time{}
	p.loginErr = gh.ErrAuthentication
	previousFetches := p.fetches
	m.applyPRResult(m.startPRRefresh()().(prsLoadedMsg))
	if p.logins != 3 || p.fetches != previousFetches || m.prState.login != "" || !errors.Is(m.prState.loginErr, gh.ErrAuthentication) || len(m.visiblePRs()) != 0 || len(m.allPRs()) != 4 {
		t.Fatal("authentication failure retained account, repeated requests or lost stale inventory")
	}
}

func TestPRMineDoesNotResolveLoginWithoutSupportedIncludedRepositories(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		m := mineFixture()
		p := &fakePRProvider{resolveErr: gh.ErrUnsupportedOrigin}
		m.prProvider = p
		if excluded {
			for _, repo := range m.local.repositories {
				m.excludedRepos[repo.ID] = true
			}
		}
		m.startPRRefresh()()
		if p.logins != 0 || p.fetches != 0 {
			t.Fatal("account lookup ran without a supported included repository")
		}
	}
}

func TestPRMineFollowsValidAccountSwitchBetweenRefreshes(t *testing.T) {
	m := mineFixture()
	p := &fakePRProvider{login: "viewer", result: gh.Result{Repo: "org/repo", Complete: true, PRs: m.allPRs()}}
	m.prProvider = p
	m.prUI.filters.mine = filterValues{prMineAuthor}
	m.applyPRResult(m.startPRRefresh()().(prsLoadedMsg))
	if m.prState.login != "viewer" || len(m.visiblePRs()) != 2 || m.visiblePRs()[0].Author == "other" {
		t.Fatal("first refresh did not use the signed-in account")
	}
	p.login = "other"
	m.prState.nextRefresh = time.Time{}
	m.applyPRResult(m.startPRRefresh()().(prsLoadedMsg))
	if p.logins != 2 || m.prState.login != "other" || m.prState.loginErr != nil || len(m.visiblePRs()) != 2 {
		t.Fatalf("account switch was not revalidated: logins=%d login=%q", p.logins, m.prState.login)
	}
	for _, pr := range m.visiblePRs() {
		if pr.Author != "other" {
			t.Fatalf("Mine kept the previous account: %+v", pr)
		}
	}
	if !strings.Contains(m.prMineStatus(), "other") {
		t.Fatalf("Mine status = %q", m.prMineStatus())
	}
}
