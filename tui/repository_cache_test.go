package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func TestManagedCachePreparationAndOfflineRescan(t *testing.T) {
	var calls []string
	fetchErr := errors.New("network unavailable")
	opts := Options{
		PrepareRepository: func(context.Context, git.Repository) error {
			calls = append(calls, "prepare")
			return fetchErr
		},
		ScanRepository: func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) {
			calls = append(calls, "scan")
			return []git.CommitRecord{{CommitID: "retained"}}, nil
		},
	}
	repo := git.Repository{ID: "org/repo"}
	msg := loadRepoCmd(context.Background(), repo, opts, 4, true)().(RepoLoadedMsg)
	if !reflect.DeepEqual(calls, []string{"prepare", "scan"}) || msg.FetchErr != fetchErr || msg.Err != nil || len(msg.Records) != 1 || msg.Generation != 4 {
		t.Fatalf("managed refresh lost retained history: %#v, calls %v", msg, calls)
	}
	calls = nil
	msg = loadRepoCmd(context.Background(), repo, opts, 5, false)().(RepoLoadedMsg)
	if !reflect.DeepEqual(calls, []string{"scan"}) || msg.FetchErr != nil {
		t.Fatalf("offline rescan prepared the cache: %v", calls)
	}
}

func TestManagedPRInventoryDoesNotRequireSuccessfulGitScan(t *testing.T) {
	m := awarenessFixture()
	m.repositories = []git.Repository{{ID: "org/repo", Name: "org/repo", Path: "/not-cloned"}}
	m.local.repositories = nil
	m.options.PrepareRepository = func(context.Context, git.Repository) error { return nil }
	p := &fakePRProvider{resolveErr: errors.New("must not read a checkout"), result: gh.Result{Repo: "org/repo", Complete: true, PRs: []gh.PullRequest{{Repo: "org/repo", Number: 1, UpdatedAt: time.Now()}}}}
	m.prProvider = p
	cmd := m.startPRRefresh()
	if cmd == nil {
		t.Fatal("Git failure prevented PR discovery")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if p.resolves != 0 || p.fetches != 1 || len(m.allPRs()) != 1 {
		t.Fatalf("managed PR inventory: resolves=%d fetches=%d prs=%d", p.resolves, p.fetches, len(m.allPRs()))
	}
}

func TestRepositoryCacheRemovalRequiresExplicitConfirmation(t *testing.T) {
	m := awarenessFixture()
	m.repositories = []git.Repository{{ID: "org/listed", Name: "org/listed"}}
	orphan := git.Repository{ID: "org/archived", Name: "org/archived"}
	m.cacheInfo = git.CacheInfo{Root: "/cache", Bytes: 1048576, Orphans: []git.Repository{orphan}}
	removed := ""
	m.options.CacheInfo = func() (git.CacheInfo, error) { return git.CacheInfo{Root: "/cache"}, nil }
	m.options.RemoveCachedRepository = func(id string) error { removed = id; return nil }
	m.openRepositoryControls()
	m, _ = applyLocal(t, m, key("d"))
	if m.cacheRemoveConfirm != "" {
		t.Fatal("listed repository offered for deletion")
	}
	m.overlayCursor = 1
	m, _ = applyLocal(t, m, key(" "))
	if m.overlayExcluded[orphan.ID] {
		t.Fatal("orphan toggled into the selected listing")
	}
	m, _ = applyLocal(t, m, key("d"))
	if m.cacheRemoveConfirm != orphan.ID || removed != "" {
		t.Fatal("deletion was not staged")
	}
	m, _ = applyLocal(t, m, key("esc"))
	if m.cacheRemoveConfirm != "" || removed != "" || m.viewMode != ViewRepoOverlay {
		t.Fatal("cancel deleted data or left controls")
	}
	m, _ = applyLocal(t, m, key("d"))
	m.scan.running = true
	next, cmd := m.handleRepositoryControls(key("enter"))
	m = next.(Model)
	if cmd != nil || removed != "" {
		t.Fatal("cache deletion ran during refresh")
	}
	m.scan.running = false
	next, cmd = m.handleRepositoryControls(key("enter"))
	m = next.(Model)
	if cmd == nil || !m.cacheRemoving {
		t.Fatal("confirmation did not start deletion")
	}
	m, _ = applyLocal(t, m, cacheInfoMsg{info: git.CacheInfo{Root: "/old"}})
	if !m.cacheRemoving {
		t.Fatal("late status update canceled removal state")
	}
	m, _ = applyLocal(t, m, cmd())
	if removed != orphan.ID || m.cacheRemoving || m.cacheRemoveConfirm != "" || len(m.cacheInfo.Orphans) != 0 || m.overlayCursor != 0 {
		t.Fatal("confirmed deletion did not reconcile controls")
	}
}

func TestRepositoryCacheRemovalFailureAndModalBounds(t *testing.T) {
	m := awarenessFixture()
	m.cacheInfo = git.CacheInfo{Root: "/cache", Orphans: []git.Repository{{ID: "org/old", Name: "org/old"}}}
	m.options.CacheInfo = func() (git.CacheInfo, error) { return m.cacheInfo, nil }
	m.options.RemoveCachedRepository = func(string) error { return errors.New("permission denied") }
	m.openRepositoryControls()
	m.cacheRemoveConfirm = "org/old"
	for _, width := range []int{40, 80, 160} {
		m.width, m.height = width, 24
		view := m.renderRepoOverlay()
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("modal exceeds %d columns", width)
			}
		}
	}
	next, cmd := m.removeCachedRepository()
	m = next.(Model)
	m, _ = applyLocal(t, m, cmd())
	if !strings.Contains(m.cacheError, "permission denied") || len(m.cacheInfo.Orphans) != 1 {
		t.Fatal("failed deletion hid cache or diagnostic")
	}
}
