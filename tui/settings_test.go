package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func settingsTestOptions() *SettingsOptions {
	return &SettingsOptions{
		ConfigPath: "/config/bigboard.json",
		Initial:    SettingsConfig{Organizations: []string{"acme"}, Exclude: []string{"acme/web"}},
		Account: func(context.Context) (SettingsAccount, error) {
			return SettingsAccount{Login: "viewer", Organizations: []string{"acme", "other"}}, nil
		},
		Discover: func(context.Context, SettingsConfig) ([]git.Repository, error) {
			return []git.Repository{{ID: "acme/api", Name: "acme/api"}, {ID: "acme/web", Name: "acme/web"}}, nil
		},
		Save: func(_ context.Context, cfg SettingsConfig, repos []git.Repository) ([]git.Repository, error) {
			var selected []git.Repository
			for _, repo := range repos {
				if !slices.Contains(cfg.Exclude, repo.ID) {
					selected = append(selected, repo)
				}
			}
			return selected, nil
		},
	}
}

func settingsTestModel(t *testing.T, options *SettingsOptions) Model {
	t.Helper()
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "test", DefaultTimeIndex, Options{Settings: options, PRProvider: &fakePRProvider{}})
	m.width, m.height = 100, 30
	t.Cleanup(m.Close)
	return m
}

func settingsTestApplyCommand(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("settings did not schedule its request")
	}
	m, next := applyLocal(t, m, cmd())
	if next != nil {
		t.Fatal("settings response scheduled unrelated work")
	}
	return m
}

func settingsTestOpen(t *testing.T, options *SettingsOptions) Model {
	t.Helper()
	m := settingsTestModel(t, options)
	m, cmd := applyLocal(t, m, keyboardContractKey(","))
	if !m.settings.open || m.settings.required || m.settings.busy != "account" {
		t.Fatal("Settings shortcut did not open account discovery")
	}
	return settingsTestApplyCommand(t, m, cmd)
}

func settingsTestToggleChoice(t *testing.T, m Model, choice string) Model {
	t.Helper()
	m.settings.cursor = slices.Index(m.settings.choices(), choice)
	if m.settings.cursor < 0 {
		t.Fatalf("settings choice %q is unavailable: %v", choice, m.settings.choices())
	}
	return keyboardContractPress(t, m, " ")
}

func TestSettingsStagesOrganizationsAndRepositoriesUntilSave(t *testing.T) {
	options := settingsTestOptions()
	m := settingsTestOpen(t, options)
	before := m
	m = settingsTestToggleChoice(t, m, "acme")
	if m.settings.selectedOrganizations["acme"] || !before.settings.selectedOrganizations["acme"] || !reflect.DeepEqual(options.Initial.Organizations, []string{"acme"}) {
		t.Fatal("organization draft mutated its previous model or saved configuration")
	}
	m = keyboardContractPress(t, m, "enter")
	if m.settings.step != 0 || m.settings.error == "" {
		t.Fatal("empty organization selection advanced")
	}
	m = keyboardContractPress(t, m, " ")
	m, cmd := applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, cmd)
	if m.settings.step != 1 || !m.settings.selectedRepositories["acme/api"] || m.settings.selectedRepositories["acme/web"] {
		t.Fatal("repository selection did not respect saved exclusions")
	}
	before = m
	m = keyboardContractPress(t, m, " ", "enter")
	if m.settings.selectedRepositories["acme/api"] || !before.settings.selectedRepositories["acme/api"] || m.settings.step != 1 || !strings.Contains(m.settings.error, "at least one repository") {
		t.Fatal("repository draft leaked or empty selection advanced")
	}
	m = keyboardContractPress(t, m, "down", " ", "enter")
	if m.settings.step != 2 || !m.settings.selectedRepositories["acme/web"] || !reflect.DeepEqual(options.Initial.Exclude, []string{"acme/web"}) {
		t.Fatal("review saved the draft prematurely")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.settings.step != 1 || !m.settings.selectedRepositories["acme/web"] || m.settings.selectedRepositories["acme/api"] {
		t.Fatal("back discarded repository choices")
	}
	m = keyboardContractPress(t, m, "esc", "esc")
	if m.settings.open || !reflect.DeepEqual(options.Initial.Exclude, []string{"acme/web"}) || len(m.repositories) != 0 {
		t.Fatal("cancel applied staged settings")
	}
}

func TestSettingsSearchManualOrganizationAndLiteralShortcuts(t *testing.T) {
	m := settingsTestOpen(t, settingsTestOptions())
	m = keyboardContractPress(t, m, "/", "OTH", "enter")
	if !reflect.DeepEqual(m.settings.choices(), []string{"other"}) {
		t.Fatalf("organization search did not filter case-insensitively: %v", m.settings.choices())
	}
	m = keyboardContractPress(t, m, "/", "ctrl+u", "missing", "esc")
	if m.settings.query != "OTH" || m.settings.edit != "" {
		t.Fatal("canceled search changed the accepted query")
	}
	before := m
	m = keyboardContractPress(t, m, "n", " New-ORG ", "enter")
	if m.settings.edit != "" || m.settings.query != "" || !m.settings.selectedOrganizations["new-org"] || !slices.Contains(m.settings.organizations, "new-org") || before.settings.selectedOrganizations["new-org"] {
		t.Fatal("manual organization was not canonicalized and staged independently")
	}
	m = keyboardContractPress(t, m, "n", "bad/org", "enter")
	if m.settings.edit != "organization" || m.settings.error == "" || slices.Contains(m.settings.organizations, "bad/org") {
		t.Fatal("invalid organization was accepted")
	}
	m = keyboardContractPress(t, m, "esc", "/", "c", "a", "p", "q", "?")
	if m.settings.input != "capq?" || !m.settings.open || m.quitting || m.showHelp || m.showPRs {
		t.Fatal("search interpreted literal shortcut letters")
	}
	m = keyboardContractPress(t, m, "f1")
	if !m.showHelp {
		t.Fatal("text editor help was unavailable")
	}
	m = keyboardContractPress(t, m, "esc", "esc", "p")
	if m.settings.open || !m.showPRs {
		t.Fatal("board switch did not cancel the settings draft")
	}
}

func TestSettingsAccountAndDiscoveryFailureRetry(t *testing.T) {
	options := settingsTestOptions()
	accountCalls, discoveryCalls := 0, 0
	options.Account = func(context.Context) (SettingsAccount, error) {
		accountCalls++
		if accountCalls == 1 {
			return SettingsAccount{}, gh.ErrAuthentication
		}
		return SettingsAccount{Login: "viewer", Organizations: []string{"acme"}}, nil
	}
	options.Discover = func(_ context.Context, cfg SettingsConfig) ([]git.Repository, error) {
		discoveryCalls++
		if !reflect.DeepEqual(cfg.Organizations, []string{"acme"}) || cfg.PersonalAccount != "" {
			t.Fatalf("discovery config=%+v", cfg)
		}
		if discoveryCalls == 1 {
			return nil, gh.ErrUnavailable
		}
		return []git.Repository{{ID: "acme/api", Name: "acme/api"}}, nil
	}
	m := settingsTestOpen(t, options)
	if !strings.Contains(m.settings.error, "gh auth login") || m.settings.busy != "" || discoveryCalls != 0 {
		t.Fatal("account failure did not provide an actionable retry")
	}
	oldGeneration, oldContext := m.settingsGeneration, m.settingsContext
	m, cmd := applyLocal(t, m, keyboardContractKey("r"))
	if m.settingsGeneration <= oldGeneration || oldContext.Err() == nil {
		t.Fatal("account retry did not invalidate the previous request")
	}
	m = settingsTestApplyCommand(t, m, cmd)
	if accountCalls != 2 || m.settings.login != "viewer" || m.settings.error != "" {
		t.Fatal("successful account retry did not recover")
	}
	m, cmd = applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, cmd)
	if m.settings.step != 1 || !strings.Contains(m.settings.error, "missing") || m.settings.busy != "" {
		t.Fatal("discovery failure did not preserve a retryable repository step")
	}
	m, cmd = applyLocal(t, m, keyboardContractKey("r"))
	m = settingsTestApplyCommand(t, m, cmd)
	if discoveryCalls != 2 || m.settings.error != "" || !m.settings.selectedRepositories["acme/api"] {
		t.Fatal("repository discovery retry did not recover")
	}
}

func TestSettingsSaveFailureRetainsDraftAndBlocksDepartureWhileSaving(t *testing.T) {
	options := settingsTestOptions()
	saves := 0
	options.Save = func(_ context.Context, cfg SettingsConfig, repos []git.Repository) ([]git.Repository, error) {
		saves++
		if !reflect.DeepEqual(cfg, SettingsConfig{Organizations: []string{"acme"}, Exclude: []string{"acme/api"}}) || len(repos) != 2 {
			t.Fatalf("save lost draft: cfg=%+v repos=%v", cfg, repos)
		}
		return nil, errors.New("configuration changed elsewhere")
	}
	m := settingsTestOpen(t, options)
	m, cmd := applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, cmd)
	m = keyboardContractPress(t, m, " ", "down", " ", "enter")
	if saves != 0 || m.settings.step != 2 {
		t.Fatal("repository step saved without confirmation")
	}
	m, cmd = applyLocal(t, m, keyboardContractKey("enter"))
	if cmd == nil || m.settings.busy != "saving" || saves != 0 {
		t.Fatal("review confirmation did not schedule save")
	}
	m = keyboardContractPress(t, m, "esc", "p", "q", ",")
	if !m.settings.open || m.settings.step != 2 || m.quitting || m.showPRs {
		t.Fatal("pending save allowed departure")
	}
	m = settingsTestApplyCommand(t, m, cmd)
	if saves != 1 || m.settings.busy != "" || m.settings.step != 2 || !strings.Contains(m.settings.error, "changed elsewhere") || !m.settings.selectedRepositories["acme/web"] || !reflect.DeepEqual(options.Initial.Exclude, []string{"acme/web"}) {
		t.Fatal("failed save changed persistent settings or discarded the draft")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.settings.step != 1 || !m.settings.selectedRepositories["acme/web"] {
		t.Fatal("save failure trapped the review screen")
	}
}

func TestSettingsIgnoresObsoleteRequestsAfterBackAndCancel(t *testing.T) {
	m := settingsTestOpen(t, settingsTestOptions())
	m, cmd := applyLocal(t, m, keyboardContractKey("enter"))
	message := cmd()
	requestContext := m.settingsContext
	m = keyboardContractPress(t, m, "esc")
	if requestContext.Err() == nil || m.settings.step != 0 {
		t.Fatal("back did not cancel repository discovery")
	}
	m = settingsTestApplyCommand(t, m, func() tea.Msg { return message })
	if m.settings.step != 0 || len(m.settings.repositories) != 0 {
		t.Fatal("obsolete repository response changed the organization step")
	}
	oldGeneration := m.settingsGeneration
	m = keyboardContractPress(t, m, "esc")
	for _, message := range []tea.Msg{
		settingsAccountMsg{generation: oldGeneration, account: SettingsAccount{Login: "obsolete"}},
		settingsRepositoriesMsg{generation: oldGeneration, repositories: []git.Repository{{ID: "old/repo"}}},
		settingsSavedMsg{generation: oldGeneration, config: SettingsConfig{Organizations: []string{"old"}}},
		settingsReloadMsg{generation: oldGeneration},
	} {
		m, cmd = applyLocal(t, m, message)
		if cmd != nil || m.settings.open || len(m.repositories) != 0 || !reflect.DeepEqual(m.options.Settings.Initial.Organizations, []string{"acme"}) {
			t.Fatalf("obsolete %T changed closed settings", message)
		}
	}
}

func TestSettingsRequiredDefersDataWorkUntilSaveAndReload(t *testing.T) {
	settings := settingsTestOptions()
	settings.Required = true
	prepares, scans, cacheReads := 0, 0, 0
	provider := &fakePRProvider{}
	m := NewModelWithOptions([]git.Repository{{ID: "old/repo", Name: "old/repo"}}, stats.SortByTotal, nil, "test", DefaultTimeIndex, Options{
		Settings: settings, PRProvider: provider,
		PrepareRepository: func(context.Context, git.Repository) error { prepares++; return nil },
		ScanRepository: func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) {
			scans++
			return nil, nil
		},
		CacheInfo: func() (git.CacheInfo, error) { cacheReads++; return git.CacheInfo{}, nil },
	})
	defer m.Close()
	if !m.settings.open || !m.settings.required {
		t.Fatal("required setup did not open at startup")
	}
	m = settingsTestApplyCommand(t, m, m.Init())
	m = keyboardContractPress(t, m, "c", "a", "p", "f", "i", "e", "x", ",")
	if !m.settings.open || !m.settings.required || prepares != 0 || scans != 0 || cacheReads != 0 || provider.fetches != 0 || provider.logins != 0 {
		t.Fatal("required setup allowed data work or board navigation")
	}
	m, cmd := applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, cmd)
	m = keyboardContractPress(t, m, "enter")
	m, cmd = applyLocal(t, m, keyboardContractKey("enter"))
	oldScan, oldPR, oldCache := m.scan.generation, m.prGeneration, m.cacheGeneration
	m, reload := applyLocal(t, m, cmd())
	defer m.Close()
	if reload == nil || m.settings.open || m.options.Settings.Required || len(m.repositories) != 1 || m.repositories[0].ID != "acme/api" || m.scan.generation <= oldScan || m.prGeneration <= oldPR || m.cacheGeneration <= oldCache {
		t.Fatal("saved setup did not replace repositories and advance data generations")
	}
	if prepares != 0 || scans != 0 || cacheReads != 0 || provider.fetches != 0 {
		t.Fatal("saved settings started work before old commands were joined")
	}
	for _, message := range []tea.Msg{
		RepoLoadedMsg{Generation: oldScan, Repository: git.Repository{ID: "old/repo"}, Records: []git.CommitRecord{{CommitID: "old"}}},
		prsLoadedMsg{generation: oldPR},
		cacheInfoMsg{generation: oldCache, info: git.CacheInfo{Root: "old-cache"}},
	} {
		m, cmd = applyLocal(t, m, message)
		if cmd != nil || len(m.local.records) != 0 || m.cacheInfo.Root != "" {
			t.Fatalf("obsolete %T changed reloaded data", message)
		}
	}
	m, cmd = applyLocal(t, m, reload())
	if cmd == nil {
		t.Fatal("joined reload did not start data initialization")
	}
	var execute func(tea.Cmd)
	execute = func(command tea.Cmd) {
		if command == nil {
			return
		}
		if batch, ok := command().(tea.BatchMsg); ok {
			for _, child := range batch {
				execute(child)
			}
		}
	}
	execute(cmd)
	if prepares != 1 || scans != 1 || cacheReads != 1 {
		t.Fatalf("reload did not initialize selected repositories: prepares=%d scans=%d cache=%d", prepares, scans, cacheReads)
	}
}

func TestSettingsReloadWaitsForOldCommands(t *testing.T) {
	m := settingsTestOpen(t, settingsTestOptions())
	started, release, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	oldGroup := m.commands
	worker := oldGroup.wrap(func() tea.Msg {
		close(started)
		<-release
		return nil
	})
	go func() { defer close(workerDone); worker() }()
	<-started
	m, reload := applyLocal(t, m, settingsSavedMsg{generation: m.settingsGeneration, config: SettingsConfig{Organizations: []string{"acme"}}})
	defer m.Close()
	if reload == nil {
		close(release)
		t.Fatal("save did not schedule joined reload")
	}
	reloaded := make(chan tea.Msg, 1)
	go func() { reloaded <- reload() }()
	select {
	case <-reloaded:
		close(release)
		t.Fatal("reload finished while an old command was still running")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case message := <-reloaded:
		if _, ok := message.(settingsReloadMsg); !ok {
			t.Fatalf("unexpected reload result %T", message)
		}
	case <-time.After(time.Second):
		t.Fatal("reload did not finish after old commands exited")
	}
	<-workerDone
	called := false
	oldGroup.wrap(func() tea.Msg { called = true; return nil })()
	if called {
		t.Fatal("joined command group allowed an obsolete queued command to start")
	}
}

func TestSettingsReopenBeforeReloadStillStartsRepositoryLoading(t *testing.T) {
	m := settingsTestOpen(t, settingsTestOptions())
	prepares, scans := 0, 0
	m.options.PrepareRepository = func(context.Context, git.Repository) error { prepares++; return nil }
	m.options.ScanRepository = func(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error) {
		scans++
		return nil, nil
	}
	m, cmd := applyLocal(t, m, keyboardContractKey("enter"))
	m = settingsTestApplyCommand(t, m, cmd)
	m = keyboardContractPress(t, m, "enter")
	m, cmd = applyLocal(t, m, keyboardContractKey("enter"))
	m, reload := applyLocal(t, m, cmd())
	defer m.Close()
	if reload == nil {
		t.Fatal("save did not schedule a reload")
	}
	generation := m.settingsGeneration
	m, account := applyLocal(t, m, keyboardContractKey(","))
	if !m.settings.open || account == nil || m.settingsGeneration <= generation {
		t.Fatal("settings did not reopen while reload was pending")
	}
	m, initialize := applyLocal(t, m, reload())
	if initialize == nil || !m.settings.open {
		t.Fatal("reopening settings discarded the pending repository reload")
	}
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
		}
	}
	execute(initialize)
	if prepares != 1 || scans != 1 || len(loaded) != 1 || loaded[0].Repository.ID != "acme/api" || loaded[0].Generation != m.scan.generation {
		t.Fatalf("saved repositories did not load after settings reopened: prepares=%d scans=%d results=%+v", prepares, scans, loaded)
	}
}

func TestSettingsModalFitsWithLongErrorsAndManyOrganizations(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}} {
		for _, step := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%dx%d step %d", size[0], size[1], step), func(t *testing.T) {
				options := settingsTestOptions()
				options.Group = "core"
				m := settingsTestOpen(t, options)
				m.width, m.height = size[0], size[1]
				m.settings.step = step
				m.settings.error = strings.Repeat("Configuration changed elsewhere; retry after reviewing your choices. ", 5)
				m.settings.organizations = nil
				for i := range 40 {
					org := fmt.Sprintf("example-organization-%02d", i)
					m.settings.organizations = append(m.settings.organizations, org)
					m.settings.selectedOrganizations[org] = true
					id := org + "/repository"
					m.settings.repositories = append(m.settings.repositories, git.Repository{ID: id, Name: id})
					m.settings.selectedRepositories[id] = true
				}
				m.settings.cursor = 39
				view := ansi.Strip(m.View())
				lines := strings.Split(view, "\n")
				if len(lines) != m.height || !strings.Contains(view, "Settings") || !strings.Contains(view, "Configuration changed") {
					t.Fatalf("settings modal lost its frame or error: %s", view)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("settings modal exceeded %d columns: %s", m.width, line)
					}
				}
				if step == 2 && (!strings.Contains(view, "Enter save & reload") || !strings.Contains(view, "Esc back")) {
					t.Fatalf("review clipped its save or back controls: %s", view)
				}
			})
		}
	}
}

func TestSettingsSavePreservesDefaultPRProvider(t *testing.T) {
	m := NewModelWithOptions(nil, stats.SortByTotal, nil, "test", DefaultTimeIndex, Options{Settings: settingsTestOptions()})
	defer m.Close()
	provider := m.prProvider
	if m.options.PRProvider != nil || provider == nil {
		t.Fatal("fixture did not create the default PR provider")
	}
	m.prepareSettings(false)
	m, reload := applyLocal(t, m, settingsSavedMsg{
		generation: m.settingsGeneration,
		config:     SettingsConfig{Organizations: []string{"acme"}},
	})
	defer m.Close()
	if reload == nil || m.prProvider != provider || m.options.PRProvider != provider {
		t.Fatal("saving settings replaced the default PR provider and its retained quota state")
	}
}
