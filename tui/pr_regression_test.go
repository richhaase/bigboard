package tui

import (
	"context"
	"errors"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"strings"
	"testing"
	"time"
)

func TestUnsupportedRepositoriesDoNotSpoilFreshPRStatus(t *testing.T) {
	for _, count := range []int{0, 1} {
		m := awarenessFixture()
		prs := []gh.PullRequest{}
		if count > 0 {
			prs = append(prs, gh.PullRequest{Repo: "org/repo", Number: 1, FilesComplete: true, ContextComplete: true})
		}
		p := &fakePRProvider{repoByPath: map[string]string{"/github": "org/repo"}, result: gh.Result{Repo: "org/repo", PRs: prs, Complete: true}}
		m.loadedRepos = []git.Repository{{ID: "github", Path: "/github"}, {ID: "local", Path: "/local"}, {ID: "gitlab", Path: "/gitlab"}}
		m.prProvider = p
		next, _ := m.Update(m.startPRRefresh()())
		m = next.(Model)
		status := m.prStatus()
		if p.fetches != 1 || strings.Contains(status, "STALE") || strings.Contains(status, "unavailable") || strings.Contains(status, "PARTIAL") || !strings.Contains(status, "known open PRs") {
			t.Fatalf("fetches=%d status=%s", p.fetches, status)
		}
	}
}

func TestUnsupportedOriginClearsObsoleteAssociation(t *testing.T) {
	m := prFixture()
	m.loadedRepos = m.loadedRepos[:1]
	m.prProvider = &fakePRProvider{resolveErr: gh.ErrUnsupportedOrigin}
	next, _ := m.Update(m.startPRRefresh()())
	m = next.(Model)
	if len(m.allPRs()) != 0 || strings.Contains(m.prStatus(), "STALE") || strings.Contains(m.prStatus(), "unavailable") || !strings.Contains(m.prStatus(), "not applicable") {
		t.Fatalf("obsolete GitHub data retained: %s", m.prStatus())
	}
}

type renamedPRProvider struct {
	result  gh.Result
	fetched []string
}

func (p *renamedPRProvider) Resolve(context.Context, string) (string, error) { return "org/old", nil }
func (p *renamedPRProvider) Fetch(_ context.Context, repo string) gh.Result {
	p.fetched = append(p.fetched, repo)
	result := p.result
	if result.Repo == "" {
		result.Repo = repo
	}
	return result
}

func TestRenamedRepositoryRetainsSnapshotAcrossFailedRefresh(t *testing.T) {
	m := awarenessFixture()
	m.loadedRepos = m.loadedRepos[:1]
	p := &renamedPRProvider{result: gh.Result{Repo: "org/new", Complete: true, PRs: []gh.PullRequest{{Repo: "org/new", Number: 7, FilesComplete: true, ContextComplete: true}}}}
	m.prProvider = p
	refresh := func() {
		t.Helper()
		m.prState.nextRefresh = time.Time{}
		next, _ := m.Update(m.startPRRefresh()())
		m = next.(Model)
	}
	refresh()
	good := m.prState.snapshots["org/new"].lastGood
	p.result = gh.Result{Err: gh.ErrUnavailable}
	refresh()
	if len(p.fetched) != 2 || p.fetched[1] != "org/new" || len(m.allPRs()) != 1 || m.prState.repositories["/api"] != "org/new" || !m.prState.snapshots["org/new"].lastGood.Equal(good) || !strings.Contains(m.prStatus(), "STALE") {
		t.Fatalf("renamed snapshot lost: fetches=%v state=%+v status=%s", p.fetched, m.prState, m.prStatus())
	}
	p.result = gh.Result{Repo: "org/new", Complete: true}
	refresh()
	if len(m.allPRs()) != 0 || strings.Contains(m.prStatus(), "STALE") {
		t.Fatal("successful empty refresh retained obsolete PR")
	}
}

func TestPRRefreshWithoutFetchDoesNotStartCooldown(t *testing.T) {
	for _, mode := range []string{"empty", "excluded", "unsupported", "lookup-error"} {
		t.Run(mode, func(t *testing.T) {
			m := awarenessFixture()
			p := &fakePRProvider{result: gh.Result{Repo: "org/repo", Complete: true}}
			switch mode {
			case "empty":
				m.loadedRepos = nil
			case "excluded":
				for _, repo := range m.loadedRepos {
					m.excludedRepos[repo.ID] = true
				}
			case "unsupported":
				p.resolveErr = gh.ErrUnsupportedOrigin
			case "lookup-error":
				p.resolveErr = errors.New("lookup failed")
			}
			m.prProvider = p
			cmd := m.startPRRefresh()
			if cmd == nil || m.startPRRefresh() != nil {
				t.Fatal("in-flight refresh did not coalesce")
			}
			next, _ := m.Update(cmd())
			m = next.(Model)
			if p.fetches != 0 || !m.prState.nextRefresh.IsZero() {
				t.Fatal("zero fetch refresh imposed cooldown")
			}
			m.loadedRepos = []git.Repository{{ID: "/api"}}
			m.excludedRepos = map[string]bool{}
			p.resolveErr = nil
			cmd = m.startPRRefresh()
			if cmd == nil {
				t.Fatal("next refresh blocked")
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			if p.fetches != 1 || !m.prState.nextRefresh.After(time.Now()) || m.startPRRefresh() != nil {
				t.Fatal("fetched refresh did not impose cooldown")
			}
		})
	}
}
