package gui

import (
	"context"
	"errors"
	"fmt"
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
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	cache     repositoryCache
	provider  provider
	cachePath string
	path      string
	source    []byte
	analysis  analysisConfig
	snapshot  Snapshot
	busy      bool
	now       func() time.Time
	workers   sync.WaitGroup
}

func NewService() (*Service, error) {
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
	path := filepath.Join(root, "bigboard", "desktop.json")
	settings, analysis, source, err := loadSettings(path)
	if err != nil {
		return nil, err
	}
	cache, err := git.NewRepositoryCache()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{ctx: ctx, cancel: cancel, cache: cache, provider: gh.NewProvider(nil), cachePath: cache.Root, path: path, source: source, analysis: analysis, now: time.Now}
	s.snapshot = emptySnapshot(settings, cache.Root)
	s.snapshot.Status.Phase = "initial"
	s.snapshot.Status.Message = "Loading your repository history…"
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
	return cloneSnapshot(s.snapshot)
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
	}
	result.Settings.Organizations = slices.Clone(snapshot.Settings.Organizations)
	result.Settings.Repositories = slices.Clone(snapshot.Settings.Repositories)
	result.Status.Errors = slices.Clone(snapshot.Status.Errors)
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
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("wait for the current refresh before saving settings")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	source, err := saveSettings(s.path, settings, s.source)
	if err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	s.source = source
	previous := cloneSnapshot(s.snapshot)
	if previous.Mode != settings.Mode {
		previous = emptySnapshot(settings, s.cachePath)
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
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	settings, err := normalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	repositories, err := s.discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	result := []Repository{}
	for _, repo := range repositories {
		result = append(result, Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch})
	}
	return result, nil
}

func (s *Service) refresh(previous Snapshot) (Snapshot, error) {
	defer s.workers.Done()
	if previous.Settings.Mode == "live" && previous.GeneratedAt == "" {
		cached := s.refreshRepositories(previous, true)
		cached.Status.Phase = "refreshing"
		cached.Status.Message = "Loading GitHub repositories…"
		if len(cached.Commits) > 0 {
			cached.Status.Message = "Cached history loaded; refreshing GitHub…"
		}
		s.mu.Lock()
		s.snapshot = cloneSnapshot(cached)
		s.mu.Unlock()
		previous = cached
	}
	result := s.refreshRepositories(previous, false)
	s.mu.Lock()
	s.snapshot = result
	s.busy = false
	result = cloneSnapshot(result)
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
	if len(settings.Repositories) > 0 {
		return slices.Contains(settings.Repositories, id)
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

func (s *Service) refreshRepositories(previous Snapshot, offline bool) Snapshot {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	settings := previous.Settings
	result := emptySnapshot(settings, s.cachePath)
	if offline {
		settings.Mode = "cache"
	}
	result.GeneratedAt = s.now().Format(time.RFC3339)
	repositories, err := s.discover(ctx, settings)
	if err != nil && len(repositories) == 0 {
		result = previous
		result.Status.Phase = "partial"
		result.Status.Message = "Repository discovery failed; retained evidence is stale. Check Settings and try Refresh."
		if len(result.Repositories) == 0 {
			result.Status.Message = "Could not load repositories. Check Settings and GitHub access, then refresh."
		}
		result.Status.Errors = []string{err.Error()}
		for i := range result.Repositories {
			result.Repositories[i].Stale = true
		}
		for i := range result.PullRequests {
			result.PullRequests[i].Complete = false
		}
		return result
	}
	if err != nil {
		result.Status.Errors = append(result.Status.Errors, "Repository cache inventory: "+err.Error())
	}
	selected := []git.Repository{}
	for _, repo := range repositories {
		if s.included(settings, repo.ID) {
			selected = append(selected, repo)
		}
	}
	for _, id := range settings.Repositories {
		if !slices.ContainsFunc(selected, func(repo git.Repository) bool { return repo.ID == id }) {
			result.Status.Errors = append(result.Status.Errors, id+": selected repository is unavailable in this data source")
		}
	}
	type scanned struct {
		repo    Repository
		commits []Commit
	}
	outcomes := make([]scanned, len(selected))
	semaphore := make(chan struct{}, 4)
	var workers sync.WaitGroup
	for i, repo := range selected {
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				outcomes[i] = scanned{Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch, Stale: true, Error: ctx.Err().Error()}, retainedCommits(previous, repo.ID)}
				return
			}
			view := Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch}
			if settings.Mode == "live" {
				if err := s.cache.Refresh(ctx, repo); err != nil {
					view.Stale, view.Error = true, err.Error()
				}
			}
			before, revisionErr := s.cache.Revision(ctx, repo)
			records, scanErr := s.cache.ScanRepository(ctx, repo, git.CollectOptions{IncludeGenerated: s.analysis.AllFiles, AIIdentities: s.analysis.AIIdentities, Identities: s.analysis.Identities})
			commits := []Commit{}
			if scanErr != nil {
				view.Stale = true
				if view.Error != "" {
					view.Error += "; "
				}
				view.Error += scanErr.Error()
				commits = retainedCommits(previous, repo.ID)
			} else {
				for _, record := range records {
					commits = append(commits, s.commit(record))
				}
				after, err := s.cache.Revision(ctx, repo)
				if revisionErr == nil && err == nil && before == after {
					view.Revision = before
				}
			}
			outcomes[i] = scanned{view, commits}
		}()
	}
	workers.Wait()
	requests := []gh.RepositoryRequest{}
	for _, outcome := range outcomes {
		result.Repositories = append(result.Repositories, outcome.repo)
		result.Commits = append(result.Commits, outcome.commits...)
		if outcome.repo.Error != "" {
			result.Status.Errors = append(result.Status.Errors, outcome.repo.ID+": "+outcome.repo.Error)
		}
		requests = append(requests, gh.RepositoryRequest{Repo: outcome.repo.ID})
	}
	if settings.Mode == "live" && len(requests) > 0 {
		prCtx := gh.WithRequestBudget(ctx, s.analysis.PRRequestBudget)
		viewer, err := s.provider.CurrentLogin(prCtx)
		if err != nil {
			result.Status.Errors = append(result.Status.Errors, "GitHub account: "+err.Error())
		} else {
			result.Viewer = viewer
		}
		for _, inventory := range s.provider.FetchAll(prCtx, requests) {
			if inventory.Err != nil || !inventory.Complete {
				message := "open PR inventory is incomplete"
				if inventory.Err != nil {
					message = inventory.Err.Error()
				}
				result.Status.Errors = append(result.Status.Errors, inventory.Repo+": "+message)
			}
			seen := map[int]bool{}
			for _, pr := range inventory.PRs {
				result.PullRequests = append(result.PullRequests, convertPR(pr, inventory.Complete))
				seen[pr.Number] = true
			}
			if !inventory.Complete {
				for _, pr := range previous.PullRequests {
					if pr.Repo == inventory.Repo && !seen[pr.Number] {
						pr.Complete = false
						result.PullRequests = append(result.PullRequests, pr)
					}
				}
			}
		}
	}
	result.Status.Message = fmt.Sprintf("Loaded %d repositories. Refresh is manual.", len(result.Repositories))
	if settings.Mode == "cache" {
		result.Status.Message = "Offline cached history. GitHub PRs are unavailable; switch to live data to load them."
	}
	if len(result.Status.Errors) > 0 {
		result.Status.Phase = "partial"
		result.Status.Message = "Some evidence is unavailable or stale. Open Data status for details."
	}
	if len(result.Repositories) == 0 && len(result.Status.Errors) == 0 {
		result.Status.Message = "No repositories in this selection. Open Settings to choose a source or change repository selection."
		if settings.Mode == "cache" && settings.PersonalAccount == "" && len(settings.Organizations) == 0 {
			result.Status.Phase = "setup"
			result.Status.Message = "No cached repositories found. Open Settings to connect your GitHub account or organization."
		}
	}
	return result
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
	for _, change := range record.Changes {
		if change.Generated && !s.analysis.AllFiles {
			continue
		}
		files = append(files, File{Path: change.Path, Added: change.Added, Removed: change.Removed})
	}
	return Commit{ID: record.CommitID, Repo: record.RepoID, Author: record.Author, Email: record.Email, Date: record.Date.Format(time.RFC3339), Subject: record.Subject, Added: record.Added, Removed: record.Removed, AI: record.AIAssisted, Bot: stats.IsBotIdentity(record.Author, record.Email, s.analysis.BotIdentities), LinesUnknown: record.LinesUnknown, Files: files}
}

func convertPR(pr gh.PullRequest, complete bool) PR {
	files := []string{}
	for _, file := range pr.Files {
		files = append(files, file.Path)
	}
	return PR{Repo: pr.Repo, URL: pr.URL, Title: pr.Title, Author: pr.Author, Review: pr.ReviewDecision, Checks: pr.CheckState, UpdatedAt: pr.UpdatedAt.Format(time.RFC3339), Number: pr.Number, Draft: pr.Draft, Complete: complete && pr.ContextComplete, Files: files, Reviewers: append([]string{}, pr.RequestedReviewers...)}
}
