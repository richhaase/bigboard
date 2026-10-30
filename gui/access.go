package gui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
	"github.com/richhaase/bigboard/stats"
)

func NewServiceForAccount(ctx context.Context, accountID string, token func(context.Context) (string, error)) (*Service, error) {
	return newAccountService(ctx, accountID, token, true)
}

func NewServiceForStoredAccount(ctx context.Context, accountID string, token func(context.Context) (string, error)) (*Service, error) {
	return newAccountService(ctx, accountID, token, false)
}

func newAccountService(ctx context.Context, accountID string, token func(context.Context) (string, error), verify bool) (*Service, error) {
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != accountID || token == nil {
		return nil, errors.New("a signed-in GitHub account is required")
	}
	s, err := newService(accountID)
	if err != nil {
		return nil, err
	}
	cache := s.cache.(*git.RepositoryCache)
	cache.TokenSource = token
	s.cachePath = cache.Root
	s.provider = gh.NewProvider(gh.NewHTTPClient(token))
	if len(s.analysis.source) == 0 {
		s.snapshot.Settings.Mode = "live"
		s.snapshot.Mode = "live"
	}
	if verify {
		err = s.checkAccess(ctx)
	} else {
		err = s.restoreAccess()
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	s.mu.Lock()
	s.snapshot = s.restrictAccess(s.snapshot)
	s.snapshot.Status.CachePath = cache.Root
	if len(s.snapshot.Settings.Repositories) == 0 {
		s.snapshot.Status.Phase = "setup"
		s.snapshot.Status.Message = "Choose repositories to begin."
	}
	s.mu.Unlock()
	return s, nil
}

func (s *Service) checkAccess(ctx context.Context) error {
	reader, ok := s.provider.(interface {
		DiscoverAccessibleRepositories(context.Context) ([]gh.DiscoveredRepository, error)
	})
	if !ok {
		return errors.New("repository access discovery is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	rows, err := reader.DiscoverAccessibleRepositories(ctx)
	if err != nil {
		if accessDenied(err) {
			err = errors.Join(err, s.replaceAccess(context.WithoutCancel(ctx), map[string]git.Repository{}))
		}
		return err
	}
	allowed := make(map[string]git.Repository, len(rows))
	for _, row := range rows {
		repo, err := s.cache.Repository(row.Name, row.DefaultBranch)
		if err != nil {
			return err
		}
		repo.PushedAt = row.PushedAt
		allowed[repo.ID] = repo
	}
	return s.replaceAccess(ctx, allowed)
}

func accessDenied(err error) bool {
	return errors.Is(err, gh.ErrAuthentication) || errors.Is(err, gh.ErrPermission)
}

func (s *Service) replaceAccess(ctx context.Context, allowed map[string]git.Repository) error {
	s.mu.Lock()
	s.authorized = allowed
	s.snapshot = s.restrictAccess(s.snapshot)
	s.listed = slices.DeleteFunc(s.listed, func(repo git.Repository) bool { _, ok := allowed[repo.ID]; return !ok })
	for id := range s.prResults {
		if _, ok := allowed[id]; !ok {
			delete(s.prResults, id)
		}
	}
	s.mu.Unlock()
	return s.saveAccess(ctx, allowed)
}

func (s *Service) discoverAuthorized(ctx context.Context, settings Settings) ([]git.Repository, error) {
	if settings.Mode == "live" {
		if len(settings.Organizations) > 0 {
			if _, err := s.provider.DiscoverRepositories(ctx, settings.Organizations); err != nil {
				return nil, err
			}
		}
		if settings.PersonalAccount != "" {
			if _, err := s.provider.DiscoverPersonalRepositories(ctx, settings.PersonalAccount); err != nil {
				return nil, err
			}
		}
	}
	s.mu.Lock()
	allowed := make(map[string]git.Repository, len(s.authorized))
	for id, repo := range s.authorized {
		allowed[id] = repo
	}
	s.mu.Unlock()
	var candidates []git.Repository
	var err error
	if settings.Mode == "cache" {
		candidates, err = s.cache.CachedRepositories()
	} else {
		for _, repo := range allowed {
			candidates = append(candidates, repo)
		}
	}
	result := []git.Repository{}
	for _, repo := range candidates {
		if _, ok := allowed[repo.ID]; ok && sourceContains(settings, repo.ID) {
			result = append(result, repo)
		}
	}
	slices.SortFunc(result, func(a, b git.Repository) int { return strings.Compare(a.ID, b.ID) })
	return result, err
}

func (s *Service) accessibleSettings(settings Settings) Settings {
	owners := map[string]bool{}
	for id := range s.authorized {
		owner, _, _ := strings.Cut(id, "/")
		owners[owner] = true
	}
	if !owners[settings.PersonalAccount] {
		settings.PersonalAccount = ""
	}
	settings.Organizations = slices.DeleteFunc(slices.Clone(settings.Organizations), func(owner string) bool { return !owners[owner] })
	settings.Repositories = slices.DeleteFunc(slices.Clone(settings.Repositories), func(id string) bool { _, ok := s.authorized[id]; return !ok })
	settings.ExcludedRepositories = slices.DeleteFunc(slices.Clone(settings.ExcludedRepositories), func(id string) bool { _, ok := s.authorized[id]; return !ok })
	return settings
}

func (s *Service) restrictAccess(snapshot Snapshot) Snapshot {
	if s.accountID == "" {
		return snapshot
	}
	before := len(snapshot.Repositories)
	snapshot = cloneSnapshot(snapshot)
	snapshot.Settings = s.accessibleSettings(snapshot.Settings)
	snapshot.Repositories = slices.DeleteFunc(snapshot.Repositories, func(repo Repository) bool { _, ok := s.authorized[repo.ID]; return !ok })
	snapshot.Commits = slices.DeleteFunc(snapshot.Commits, func(commit Commit) bool { _, ok := s.authorized[commit.Repo]; return !ok })
	snapshot.PullRequests = slices.DeleteFunc(snapshot.PullRequests, func(pr PR) bool { _, ok := s.authorized[pr.Repo]; return !ok })
	snapshot.Groups = map[string][]string{}
	for group, repos := range s.analysis.Groups {
		for _, id := range repos {
			if _, ok := s.authorized[id]; ok {
				snapshot.Groups[group] = append(snapshot.Groups[group], id)
			}
		}
	}
	if before != len(snapshot.Repositories) {
		snapshot.Status.Errors = []string{}
	}
	snapshot.WorkAreas = map[string][]stats.WorkAreaRule{}
	for id, rules := range s.analysis.WorkAreas {
		if _, ok := s.authorized[id]; ok {
			snapshot.WorkAreas[id] = rules
		}
	}
	return snapshot
}

func (s *Service) accountAllows(id string) bool {
	if s.accountID == "" {
		return true
	}
	_, ok := s.authorized[id]
	return ok
}

func (s *Service) ResetSelection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.accessibleSettings(s.snapshot.Settings)
	settings.Repositories = []string{}
	s.snapshot = emptySnapshot(settings, s.cachePath)
	s.snapshot.Status.Phase = "setup"
	s.snapshot.Status.Message = "Choose repositories for this GitHub account."
	s.group, s.sessionExcluded, s.prResults = "", nil, nil
	s.decorateLocked(&s.snapshot)
}
