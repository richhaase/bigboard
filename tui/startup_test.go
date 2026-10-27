package tui

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type startupFixture struct {
	revisions map[string]string
	fetched   map[string]string
	failures  map[string]error
	scans     map[string]int
	fetches   map[string]int
}

func newStartupFixture(t *testing.T, names ...string) (Model, *startupFixture) {
	t.Helper()
	f := &startupFixture{
		revisions: make(map[string]string), fetched: make(map[string]string), failures: make(map[string]error),
		scans: make(map[string]int), fetches: make(map[string]int),
	}
	var repos []git.Repository
	for _, name := range names {
		repos = append(repos, git.Repository{ID: name, Name: name, DefaultBranch: "main"})
	}
	opts := Options{
		PRProvider: &fakePRProvider{},
		RepositoryRevision: func(_ context.Context, repo git.Repository) (string, error) {
			return f.revisions[repo.ID], nil
		},
		PrepareRepository: func(_ context.Context, repo git.Repository) error {
			f.fetches[repo.ID]++
			if err := f.failures[repo.ID]; err != nil {
				return err
			}
			if revision := f.fetched[repo.ID]; revision != "" {
				f.revisions[repo.ID] = revision
			}
			return nil
		},
		ScanRepository: func(_ context.Context, repo git.Repository, _ git.CollectOptions) ([]git.CommitRecord, error) {
			f.scans[repo.ID]++
			return []git.CommitRecord{{RepoID: repo.ID, CommitID: f.revisions[repo.ID]}}, nil
		},
	}
	m := NewModelWithOptions(repos, stats.SortByTotal, nil, "test", DefaultTimeIndex, opts)
	t.Cleanup(m.Close)
	return m, f
}

func finishStartupPhase(t *testing.T, m Model, command tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	generation := m.scan.generation
	queue := []tea.Cmd{command}
	for len(queue) > 0 {
		command, queue = queue[0], queue[1:]
		if command == nil {
			continue
		}
		message := command()
		if batch, ok := message.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		m, command = applyLocal(t, m, message)
		queue = append(queue, command)
		if m.scan.generation != generation || !m.scan.running {
			return m, tea.Batch(queue...)
		}
	}
	t.Fatal("startup phase did not finish")
	return m, nil
}

func TestCachedStartupPublishesBeforeFetchingAndRescansOnlyMovedRepositories(t *testing.T) {
	m, f := newStartupFixture(t, "org/stable", "org/moved", "org/offline", "org/new")
	f.revisions = map[string]string{"org/stable": "same", "org/moved": "old", "org/offline": "retained"}
	f.fetched = map[string]string{"org/moved": "new", "org/new": "cloned"}
	f.failures["org/offline"] = errors.New("network unavailable")
	m, fetch := finishStartupPhase(t, m, m.Init())
	if m.loading || m.err != nil || len(m.local.repositories) != 3 || len(m.local.records) != 3 || len(f.fetches) != 0 {
		t.Fatalf("cached history was not published before any remote work: loading=%v repos=%d records=%d fetches=%v err=%v", m.loading, len(m.local.repositories), len(m.local.records), f.fetches, m.err)
	}
	if len(m.local.failed) != 0 || !reflect.DeepEqual(f.scans, map[string]int{"org/stable": 1, "org/moved": 1, "org/offline": 1}) {
		t.Fatalf("missing cache became an offline failure or was scanned: failures=%v scans=%v", m.local.failed, f.scans)
	}
	stableAt, offlineAt := m.local.scannedAt["org/stable"], m.local.scannedAt["org/offline"]
	oldGeneration := m.scan.generation - 1
	remaining := m.scan.remaining
	m, ignored := applyLocal(t, m, RepoLoadedMsg{Generation: oldGeneration, Repository: m.repositories[0], Records: []git.CommitRecord{{CommitID: "late"}}})
	if ignored != nil || m.scan.remaining != remaining {
		t.Fatal("late offline result entered the fetch phase")
	}
	m = runRefreshCommands(t, m, fetch)
	if m.loading || m.scan.running || len(m.local.repositories) != 4 || len(m.local.records) != 4 {
		t.Fatalf("fetch phase failed to publish complete retained/replacement snapshot: %+v", m.local)
	}
	if !reflect.DeepEqual(f.scans, map[string]int{"org/stable": 1, "org/moved": 2, "org/offline": 1, "org/new": 1}) {
		t.Fatalf("unexpected rescans: %v", f.scans)
	}
	for _, repo := range m.repositories {
		if f.fetches[repo.ID] != 1 {
			t.Fatalf("repository %s fetched %d times", repo.ID, f.fetches[repo.ID])
		}
	}
	if !m.local.staleRepos["org/offline"] || m.local.errors["org/offline"] == "" || !m.local.scannedAt["org/offline"].Equal(offlineAt) || !m.local.scannedAt["org/stable"].Equal(stableAt) {
		t.Fatal("retained offline data lost its scan timestamp or failed-fetch evidence")
	}
	got := make(map[string]string)
	for _, record := range m.local.records {
		got[record.RepoID] = record.CommitID
	}
	if !reflect.DeepEqual(got, map[string]string{"org/stable": "same", "org/moved": "new", "org/offline": "retained", "org/new": "cloned"}) {
		t.Fatalf("wrong published records: %v", got)
	}
}

func TestCachedStartupColdCacheStaysOnBootUntilCloned(t *testing.T) {
	m, f := newStartupFixture(t, "org/new")
	f.fetched["org/new"] = "cloned"
	m, fetch := finishStartupPhase(t, m, m.Init())
	if !m.loading || m.err != nil || len(f.scans) != 0 || len(f.fetches) != 0 {
		t.Fatalf("cold cache scanned missing history or left boot prematurely: loading=%v scans=%v fetches=%v err=%v", m.loading, f.scans, f.fetches, m.err)
	}
	m = runRefreshCommands(t, m, fetch)
	if m.loading || m.err != nil || f.scans["org/new"] != 1 || f.fetches["org/new"] != 1 || len(m.local.records) != 1 {
		t.Fatal("cold cache did not clone and scan once")
	}
}

func TestCachedStartupEmptyHistoryStillShowsBoardBeforeFetching(t *testing.T) {
	m, f := newStartupFixture(t, "org/empty")
	f.revisions["org/empty"] = "empty-history"
	m.options.ScanRepository = func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) { return nil, nil }
	m, fetch := finishStartupPhase(t, m, m.Init())
	if m.loading || m.err != nil || len(m.local.repositories) != 1 || len(f.fetches) != 0 {
		t.Fatal("valid empty cached history was mistaken for an absent cache")
	}
	m = runRefreshCommands(t, m, fetch)
	if m.err != nil || m.scan.running || f.fetches["org/empty"] != 1 {
		t.Fatal("empty cached repository failed to complete startup")
	}
}

func TestCachedStartupManualRefreshStillScansAfterUnchangedOrFailedFetch(t *testing.T) {
	m, f := newStartupFixture(t, "org/repo")
	f.revisions["org/repo"] = "same"
	m = runRefreshCommands(t, m, m.Init())
	if f.scans["org/repo"] != 1 {
		t.Fatalf("unchanged startup scanned %d times", f.scans["org/repo"])
	}
	m, refresh := applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, refresh)
	if f.scans["org/repo"] != 2 || f.fetches["org/repo"] != 2 {
		t.Fatal("manual refresh skipped the unchanged repository scan")
	}
	f.failures["org/repo"] = errors.New("offline")
	m, refresh = applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, refresh)
	if f.scans["org/repo"] != 3 || f.fetches["org/repo"] != 3 || !m.local.staleRepos["org/repo"] {
		t.Fatal("manual failed fetch no longer scans cached history")
	}
}

func TestCachedStartupIdentityRescanResumesPendingFetch(t *testing.T) {
	m, f := newStartupFixture(t, "org/repo")
	f.revisions["org/repo"] = "same"
	m, _ = finishStartupPhase(t, m, m.Init())
	oldGeneration := m.scan.generation
	m.identityMerge.saving = true
	next, rescan := m.applyIdentitySave(identitySavedMsg{primary: "primary@example.com", identities: map[string]git.Identity{"primary@example.com": {Aliases: []string{"alias@example.com"}}}})
	m = next.(Model)
	if !m.scan.localOnly || m.scan.generation == oldGeneration {
		t.Fatal("identity save did not supersede the startup fetch generation")
	}
	remaining := m.scan.remaining
	m, ignored := applyLocal(t, m, RepoLoadedMsg{Generation: oldGeneration, Repository: m.repositories[0]})
	if ignored != nil || m.scan.remaining != remaining {
		t.Fatal("obsolete startup result entered the identity rescan")
	}
	m = runRefreshCommands(t, m, rescan)
	if m.scan.running || m.loading || f.fetches["org/repo"] != 1 || f.scans["org/repo"] != 2 {
		t.Fatalf("identity rescan did not resume startup without another scan: fetches=%v scans=%v running=%v", f.fetches, f.scans, m.scan.running)
	}
}

func TestCachedStartupPublishesEachPhaseAtomically(t *testing.T) {
	m, f := newStartupFixture(t, "org/one", "org/two")
	f.revisions = map[string]string{"org/one": "old-one", "org/two": "old-two"}
	f.fetched = map[string]string{"org/one": "new-one", "org/two": "new-two"}
	m, _ = applyLocal(t, m, m.repoLoadCmd(m.repositories[0])())
	if !m.loading || len(m.local.repositories) != 0 || len(m.local.records) != 0 {
		t.Fatal("unfinished offline phase published partial rows")
	}
	m, _ = applyLocal(t, m, m.repoLoadCmd(m.repositories[1])())
	if m.loading || len(m.local.repositories) != 2 || len(m.local.records) != 2 {
		t.Fatal("completed offline phase did not publish its snapshot")
	}
	before := append([]git.CommitRecord(nil), m.local.records...)
	m, _ = applyLocal(t, m, m.repoLoadCmd(m.repositories[0])())
	if !reflect.DeepEqual(m.local.records, before) || !m.scan.running {
		t.Fatal("unfinished fetch phase changed the displayed offline snapshot")
	}
	m, _ = applyLocal(t, m, m.repoLoadCmd(m.repositories[1])())
	if m.scan.running || reflect.DeepEqual(m.local.records, before) || len(m.local.records) != 2 {
		t.Fatal("completed fetch phase did not replace its snapshot atomically")
	}
}

func TestCachedStartupSettingsReloadStartsFromCacheAndRejectsPreviousGeneration(t *testing.T) {
	m, f := newStartupFixture(t, "org/old")
	f.revisions = map[string]string{"org/old": "old", "org/new": "new"}
	m, _ = finishStartupPhase(t, m, m.Init())
	oldGeneration := m.scan.generation
	m.options.Settings = settingsTestOptions()
	m.settings.open = true
	updated, reload := m.applySavedSettings(settingsSavedMsg{
		generation:   m.settingsGeneration,
		config:       SettingsConfig{Organizations: []string{"org"}},
		repositories: []git.Repository{{ID: "org/new", Name: "org/new", DefaultBranch: "main"}},
	})
	m = updated.(Model)
	t.Cleanup(m.Close)
	m, initialize := applyLocal(t, m, reload())
	remaining := m.scan.remaining
	m, ignored := applyLocal(t, m, RepoLoadedMsg{Generation: oldGeneration, Repository: git.Repository{ID: "org/old"}})
	if ignored != nil || m.scan.remaining != remaining {
		t.Fatal("old settings generation changed the new cached pass")
	}
	m, fetch := finishStartupPhase(t, m, initialize)
	if m.loading || len(m.local.records) != 1 || m.local.records[0].RepoID != "org/new" || len(f.fetches) != 0 {
		t.Fatal("settings reload did not show its selected cached history first")
	}
	m = runRefreshCommands(t, m, fetch)
	if f.fetches["org/new"] != 1 || f.fetches["org/old"] != 0 || m.scan.running {
		t.Fatalf("settings reload fetched the wrong repositories: %v", f.fetches)
	}
}

func TestCachedStartupFetchRespectsExclusionsAndDoesNotRepeatPRRefresh(t *testing.T) {
	m, f := newStartupFixture(t, "org/included", "org/excluded")
	f.revisions = map[string]string{"org/included": "included", "org/excluded": "excluded"}
	m.excludedRepos["org/excluded"] = true
	m, commands := finishStartupPhase(t, m, m.Init())
	var loaded []RepoLoadedMsg
	var execute func(tea.Cmd)
	execute = func(command tea.Cmd) {
		if command == nil {
			return
		}
		switch message := command().(type) {
		case tea.BatchMsg:
			for _, child := range message {
				execute(child)
			}
		case RepoLoadedMsg:
			loaded = append(loaded, message)
		case prsLoadedMsg:
			m, _ = applyLocal(t, m, message)
		default:
			t.Fatalf("unexpected startup command result %T", message)
		}
	}
	execute(commands)
	provider := m.prProvider.(*fakePRProvider)
	if provider.logins != 1 || provider.fetches != 1 || f.fetches["org/included"] != 1 || f.fetches["org/excluded"] != 0 {
		t.Fatalf("startup ignored tracking or duplicated PR requests: PRs=%d Git=%v", provider.fetches, f.fetches)
	}
	m.prState.nextRefresh = time.Time{}
	for _, result := range loaded {
		var command tea.Cmd
		m, command = applyLocal(t, m, result)
		if command != nil {
			t.Fatal("finishing the fetch phase scheduled another PR refresh")
		}
	}
	if m.scan.running || provider.logins != 1 {
		t.Fatal("startup did not finish cleanly after its one PR refresh")
	}
}

func TestCachedStartupColdFailureCompletesWithRetryableError(t *testing.T) {
	m, f := newStartupFixture(t, "org/new")
	f.failures["org/new"] = errors.New("network unavailable")
	m = runRefreshCommands(t, m, m.Init())
	if m.loading || m.scan.running || m.err == nil || len(m.local.records) != 0 || !m.local.staleRepos["org/new"] {
		t.Fatal("failed cold startup stayed loading or claimed usable history")
	}
	delete(f.failures, "org/new")
	f.fetched["org/new"] = "recovered"
	m, refresh := applyLocal(t, m, key("r"))
	m = runRefreshCommands(t, m, refresh)
	if m.err != nil || len(m.local.records) != 1 || m.local.staleRepos["org/new"] {
		t.Fatal("manual refresh could not recover a failed cold startup")
	}
}

func TestCachedStartupOriginalModelCloseCancelsBackgroundFetch(t *testing.T) {
	m, f := newStartupFixture(t, "org/repo")
	f.revisions["org/repo"] = "cached"
	started := make(chan struct{})
	m.options.PrepareRepository = func(ctx context.Context, _ git.Repository) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	original := m
	m, _ = finishStartupPhase(t, m, m.Init())
	result := make(chan tea.Msg, 1)
	command := m.repoLoadCmd(m.repositories[0])
	go func() { result <- command() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background fetch did not start")
	}
	closed := make(chan struct{})
	go func() { original.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("closing the original model did not cancel and join the background fetch")
	}
	message := (<-result).(RepoLoadedMsg)
	if !errors.Is(message.FetchErr, context.Canceled) {
		t.Fatalf("canceled fetch lost its cancellation: %v", message.FetchErr)
	}
}
