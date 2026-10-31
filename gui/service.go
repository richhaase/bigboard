package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

type repositoryCache interface {
	CachedRepositories() ([]git.Repository, error)
	Repository(string, string) (git.Repository, error)
	Refresh(context.Context, git.Repository) error
	Revision(context.Context, git.Repository) (string, error)
	ScanRepository(context.Context, git.Repository, git.CollectOptions) ([]git.CommitRecord, error)
}

type provider interface {
	DiscoverRepositories(context.Context, []string) ([]gh.DiscoveredRepository, error)
	DiscoverPersonalRepositories(context.Context, string) ([]gh.DiscoveredRepository, error)
	CurrentLogin(context.Context) (string, error)
	FetchAll(context.Context, []gh.RepositoryRequest) []gh.Result
}

type Service struct {
	accountID        string
	authorized       map[string]git.Repository
	authorizedOwners map[string]string
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	cache            repositoryCache
	provider         provider
	cachePath        string
	path             string
	analysis         analysisConfig
	snapshot         Snapshot
	busy             bool
	now              func() time.Time
	workers          sync.WaitGroup
	listed           []git.Repository
	inventoryReady   bool
	group            string
	sessionExcluded  map[string]bool
	prResults        map[string]gh.Result
	configStore      *configStore
	bookmarkPath     string
	bookmarkError    string
	bookmarks        map[string]time.Time
	eligible         map[string]time.Time
}

func NewService() (*Service, error) {
	return newService("")
}

func newService(accountID string) (*Service, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("configuration directory must be absolute")
	}
	path := filepath.Join(root, "bigboard", "config.json")
	settings, analysis, err := loadSettings(path)
	if err != nil {
		return nil, err
	}
	cache, err := git.NewRepositoryCache()
	if err != nil {
		return nil, err
	}
	if accountID != "" {
		cache.Root = filepath.Join(filepath.Dir(cache.Root), "accounts", accountID, "repos")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{accountID: accountID, ctx: ctx, cancel: cancel, cache: cache, provider: gh.NewProvider(nil), cachePath: cache.Root, path: path, analysis: analysis, now: time.Now}
	s.configStore = newConfigStore(path, analysis.source)
	s.loadBookmarks()
	s.snapshot = emptySnapshot(settings, cache.Root)
	s.snapshot.Status.Phase = "initial"
	s.snapshot.Status.Message = "Loading your repository history…"
	s.decorateLocked(&s.snapshot)
	return s, nil
}

func emptySnapshot(settings Settings, cachePath string) Snapshot {
	return Snapshot{Mode: settings.Mode, Settings: settings, Repositories: []Repository{}, Commits: []Commit{}, PullRequests: []PR{}, Status: Status{Phase: "ready", Errors: []string{}, CachePath: cachePath}}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.workers.Wait()
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decorateLocked(&s.snapshot)
	return s.restrictAccess(s.snapshot)
}

func (s *Service) SnapshotStatus() SnapshotStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := SnapshotStatus{GeneratedAt: s.snapshot.GeneratedAt, Phase: s.snapshot.Status.Phase, Message: s.snapshot.Status.Message}
	for _, repo := range s.snapshot.Repositories {
		if s.accountAllows(repo.ID) {
			result.Repositories++
		}
	}
	for _, commit := range s.snapshot.Commits {
		if s.accountAllows(commit.Repo) {
			result.Commits++
		}
	}
	for _, pr := range s.snapshot.PullRequests {
		if s.accountAllows(pr.Repo) {
			result.PullRequests++
		}
	}
	return result
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := snapshot
	result.Repositories = slices.Clone(snapshot.Repositories)
	result.Commits = slices.Clone(snapshot.Commits)
	for i := range result.Commits {
		result.Commits[i].Files = slices.Clone(snapshot.Commits[i].Files)
	}
	result.PullRequests = slices.Clone(snapshot.PullRequests)
	for i := range result.PullRequests {
		result.PullRequests[i].Files = slices.Clone(snapshot.PullRequests[i].Files)
		result.PullRequests[i].Reviewers = slices.Clone(snapshot.PullRequests[i].Reviewers)
		result.PullRequests[i].Reviews = slices.Clone(snapshot.PullRequests[i].Reviews)
	}
	result.Settings.Organizations = slices.Clone(snapshot.Settings.Organizations)
	result.Settings.Repositories = slices.Clone(snapshot.Settings.Repositories)
	result.Settings.ExcludedRepositories = slices.Clone(snapshot.Settings.ExcludedRepositories)
	result.Status.Errors = slices.Clone(snapshot.Status.Errors)
	result.Groups = make(map[string][]string, len(snapshot.Groups))
	for name, repos := range snapshot.Groups {
		result.Groups[name] = slices.Clone(repos)
	}
	result.WorkAreas = make(map[string][]stats.WorkAreaRule, len(snapshot.WorkAreas))
	for repo, rules := range snapshot.WorkAreas {
		result.WorkAreas[repo] = slices.Clone(rules)
		for i := range result.WorkAreas[repo] {
			result.WorkAreas[repo][i].Paths = slices.Clone(rules[i].Paths)
		}
	}
	return result
}

func (s *Service) Refresh() (Snapshot, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("a refresh is already in progress")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	if s.accountID != "" && len(s.snapshot.Settings.Repositories) == 0 {
		s.snapshot.Status.Phase = "setup"
		s.snapshot.Status.Message = "Choose repositories available to your GitHub account."
		result := s.restrictAccess(s.snapshot)
		s.mu.Unlock()
		return result, nil
	}
	s.busy = true
	s.workers.Add(1)
	previous := cloneSnapshot(s.snapshot)
	s.snapshot.Status.Phase = "refreshing"
	s.snapshot.Status.Message = "Loading repository evidence…"
	s.mu.Unlock()
	return s.refresh(previous)
}

func (s *Service) SaveSettings(settings Settings) (Snapshot, error) {
	settings, err := normalizeSettings(settings)
	if err != nil {
		return Snapshot{}, err
	}
	if len(settings.Repositories) == 0 {
		return Snapshot{}, errors.New("select at least one repository before saving")
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("wait for the current refresh before saving settings")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	if s.accountID != "" {
		for _, id := range settings.Repositories {
			if !s.accountAllows(id) {
				s.mu.Unlock()
				return Snapshot{}, errors.New("choose repositories available to your GitHub sign-in")
			}
		}
		for _, id := range s.analysis.ExcludedRepositories {
			if !s.accountAllows(id) && !slices.Contains(settings.ExcludedRepositories, id) {
				settings.ExcludedRepositories = append(settings.ExcludedRepositories, id)
			}
		}
	}
	if s.configStore == nil {
		s.configStore = newConfigStore(s.path, s.analysis.source)
	}
	if err := s.configStore.saveSettings(s.ctx, settings); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	s.analysis.Mode = settings.Mode
	s.analysis.PersonalAccount = settings.PersonalAccount
	s.analysis.Organizations = slices.Clone(settings.Organizations)
	s.analysis.Repositories = slices.Clone(settings.Repositories)
	s.analysis.ExcludedRepositories = slices.Clone(settings.ExcludedRepositories)
	s.sessionExcluded = nil
	s.eligible = make(map[string]time.Time)
	s.inventoryReady = false
	s.listed = nil
	s.prResults = nil
	previous := cloneSnapshot(s.snapshot)
	if previous.Mode != settings.Mode {
		previous = emptySnapshot(settings, s.cachePath)
	}
	if s.accountID != "" {
		settings = s.accessibleSettings(settings)
	}
	previous.Settings = settings
	previous = s.retainSelected(previous)
	s.snapshot = cloneSnapshot(previous)
	s.snapshot.Status.Phase = "refreshing"
	s.snapshot.Status.Message = "Loading repository evidence…"
	s.busy = true
	s.workers.Add(1)
	s.mu.Unlock()
	return s.refresh(previous)
}

func (s *Service) Discover(settings Settings) ([]Repository, error) {
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.busy {
		s.mu.Unlock()
		return nil, errors.New("wait for the current operation before discovering repositories")
	}
	s.busy = true
	s.workers.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock(); s.workers.Done() }()
	if s.accountID != "" && settings.Mode == "live" {
		if err := s.checkAccess(s.ctx); err != nil {
			return nil, err
		}
	}
	selected := settings.Repositories
	settings.Repositories = nil
	settings, err := normalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	settings.Repositories = selected
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	repositories, err := s.discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	result := []Repository{}
	for _, repo := range repositories {
		result = append(result, Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch, PushedAt: timestamp(repo.PushedAt), Included: s.configuredIncluded(settings, repo.ID)})
	}
	slices.SortFunc(result, func(a, b Repository) int {
		if order := strings.Compare(b.PushedAt, a.PushedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result, nil
}

func (s *Service) refresh(previous Snapshot) (Snapshot, error) {
	defer s.workers.Done()
	if previous.Settings.Mode == "live" && previous.GeneratedAt == "" {
		cached := s.refreshRepositories(previous, true, false)
		cached.Status.Phase = "refreshing"
		cached.Status.Message = "Loading GitHub repositories…"
		if len(cached.Commits) > 0 {
			cached.Status.Message = "Cached history loaded; refreshing GitHub…"
		}
		s.mu.Lock()
		s.decorateLocked(&cached)
		s.snapshot = cloneSnapshot(cached)
		s.mu.Unlock()
		previous = cached
	}
	if s.accountID != "" && previous.Settings.Mode == "live" {
		if err := s.checkAccess(s.ctx); err != nil {
			if accessDenied(err) {
				s.mu.Lock()
				previous = s.restrictAccess(previous)
				s.mu.Unlock()
			}
			previous.Status.Phase = "partial"
			previous.Status.Errors = append(previous.Status.Errors, "Repository access: "+err.Error())
			previous.Status.Message = "Could not verify repository access. Showing previously authorized cached history; try refreshing again."
			if accessDenied(err) {
				previous.Status.Message = "GitHub access was denied. Sign in again or manage repository access."
			}
			for i := range previous.Repositories {
				previous.Repositories[i].Stale = true
			}
			s.mu.Lock()
			s.snapshot, s.busy = previous, false
			s.mu.Unlock()
			return previous, nil
		}
		s.mu.Lock()
		previous = s.restrictAccess(previous)
		if len(previous.Settings.Repositories) == 0 {
			previous.Status.Phase = "setup"
			previous.Status.Message = "Your previous repository selection is no longer available. Choose repositories or manage GitHub access."
			s.snapshot, s.busy = previous, false
			s.mu.Unlock()
			return previous, nil
		}
		s.snapshot = cloneSnapshot(previous)
		s.mu.Unlock()
	}
	result := s.refreshRepositories(previous, false, false)
	s.mu.Lock()
	if result.Mode == "live" && s.ctx.Err() == nil && len(result.Commits) > 0 && slices.ContainsFunc(result.Repositories, func(repo Repository) bool { return repo.Fetched }) {
		if s.eligible == nil {
			s.eligible = make(map[string]time.Time)
		}
		s.eligible[s.bookmarkKey()] = s.now()
	}
	s.decorateLocked(&result)
	s.snapshot = result
	s.busy = false
	result = s.restrictAccess(result)
	s.mu.Unlock()
	return result, nil
}

func (s *Service) retainSelected(previous Snapshot) Snapshot {
	keep := map[string]bool{}
	repositories := []Repository{}
	for _, repo := range previous.Repositories {
		owner, _, _ := strings.Cut(repo.ID, "/")
		settings := previous.Settings
		if (settings.PersonalAccount != "" || len(settings.Organizations) > 0) && owner != settings.PersonalAccount && !slices.Contains(settings.Organizations, owner) {
			continue
		}
		if s.included(settings, repo.ID) {
			keep[repo.ID] = true
			repositories = append(repositories, repo)
		}
	}
	previous.Repositories = repositories
	previous.Commits = slices.DeleteFunc(previous.Commits, func(commit Commit) bool { return !keep[commit.Repo] })
	previous.PullRequests = slices.DeleteFunc(previous.PullRequests, func(pr PR) bool { return !keep[pr.Repo] })
	return previous
}

func (s *Service) discover(ctx context.Context, settings Settings) ([]git.Repository, error) {
	if s.accountID != "" {
		return s.discoverAuthorized(ctx, settings)
	}
	var repositories []git.Repository
	var inventoryErr error
	if settings.Mode == "cache" {
		repositories, inventoryErr = s.cache.CachedRepositories()
	} else {
		var discovered []gh.DiscoveredRepository
		if len(settings.Organizations) > 0 {
			rows, err := s.provider.DiscoverRepositories(ctx, settings.Organizations)
			if err != nil {
				return nil, err
			}
			discovered = append(discovered, rows...)
		}
		if settings.PersonalAccount != "" {
			rows, err := s.provider.DiscoverPersonalRepositories(ctx, settings.PersonalAccount)
			if err != nil {
				return nil, err
			}
			discovered = append(discovered, rows...)
		}
		for _, row := range discovered {
			repo, err := s.cache.Repository(row.Name, row.DefaultBranch)
			if err != nil {
				return nil, err
			}
			repo.PushedAt = row.PushedAt
			repositories = append(repositories, repo)
		}
	}
	result := []git.Repository{}
	seen := map[string]bool{}
	for _, repo := range repositories {
		owner, _, _ := strings.Cut(repo.ID, "/")
		if (settings.PersonalAccount != "" || len(settings.Organizations) > 0) && owner != settings.PersonalAccount && !slices.Contains(settings.Organizations, owner) {
			continue
		}
		if !seen[repo.ID] {
			result = append(result, repo)
			seen[repo.ID] = true
		}
	}
	slices.SortFunc(result, func(a, b git.Repository) int { return strings.Compare(a.ID, b.ID) })
	return result, inventoryErr
}

func (s *Service) included(settings Settings, id string) bool {
	if s.group != "" && !slices.Contains(s.analysis.Groups[s.group], id) {
		return false
	}
	if excluded, ok := s.sessionExcluded[id]; ok {
		return !excluded
	}
	return s.configuredIncluded(settings, id)
}

func (s *Service) configuredIncluded(settings Settings, id string) bool {
	if slices.Contains(settings.ExcludedRepositories, id) {
		return false
	}
	if slices.Contains(settings.Repositories, id) {
		return true
	}
	for _, pattern := range s.analysis.Exclude {
		for _, value := range []string{id, filepath.Base(id)} {
			if match, _ := filepath.Match(pattern, value); match || value == pattern {
				return false
			}
		}
	}
	return true
}

func retainedCommits(snapshot Snapshot, id string) []Commit {
	result := []Commit{}
	for _, commit := range snapshot.Commits {
		if commit.Repo == id {
			result = append(result, commit)
		}
	}
	return result
}

func (s *Service) commit(record git.CommitRecord) Commit {
	files := []File{}
	excluded := map[string]int{}
	for _, change := range record.Changes {
		if change.Generated && !s.analysis.AllFiles {
			excluded[change.Exclusion] += change.Added + change.Removed
			continue
		}
		files = append(files, File{Path: change.Path, Added: change.Added, Removed: change.Removed})
	}
	return Commit{ExcludedPathLines: excluded[git.ExclusionPath], ExcludedAttributeLines: excluded[git.ExclusionAttribute], ExcludedBulkLines: excluded[git.ExclusionBulk], ID: record.CommitID, Repo: record.RepoID, Author: record.Author, Email: record.Email, Date: record.Date.Format(time.RFC3339), Subject: record.Subject, Added: record.Added, Removed: record.Removed, AI: record.AIAssisted, Bot: stats.IsBotIdentity(record.Author, record.Email, s.analysis.BotIdentities), LinesUnknown: record.LinesUnknown, PathsUnknown: record.PathsUnknown, Files: files}
}
