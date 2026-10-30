package gui

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/richhaase/bigboard/git"
)

type cacheManager interface {
	Info([]git.Repository) (git.CacheInfo, error)
	Remove(string) error
}

func (s *Service) RepositoryControls() (RepositoryControls, error) {
	s.mu.Lock()
	listed := slices.Clone(s.listed)
	ready := s.inventoryReady
	snapshot := cloneSnapshot(s.snapshot)
	controls := RepositoryControls{Repositories: []ControlledRepository{}, CachePath: s.cachePath, Errors: []string{}}
	known := map[string]ControlledRepository{}
	for _, repo := range listed {
		if !s.accountAllows(repo.ID) {
			continue
		}
		known[repo.ID] = ControlledRepository{Repository: Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch, PushedAt: timestamp(repo.PushedAt)}, Listed: true, Selectable: true, Included: s.included(snapshot.Settings, repo.ID)}
	}
	for _, repo := range snapshot.Repositories {
		if !s.accountAllows(repo.ID) {
			continue
		}
		row := known[repo.ID]
		row.Repository = repo
		row.Selectable = true
		row.Included = s.included(snapshot.Settings, repo.ID)
		known[repo.ID] = row
	}
	s.mu.Unlock()
	cached, err := s.cache.CachedRepositories()
	if err != nil {
		controls.Errors = append(controls.Errors, err.Error())
	}
	for _, repo := range cached {
		s.mu.Lock()
		allowed := s.accountAllows(repo.ID)
		s.mu.Unlock()
		if !allowed {
			continue
		}
		row, exists := known[repo.ID]
		if !exists {
			row.Repository = Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch}
			s.mu.Lock()
			row.Selectable = snapshot.Mode == "cache" && sourceContains(snapshot.Settings, repo.ID)
			row.Included = row.Selectable && s.included(snapshot.Settings, repo.ID)
			s.mu.Unlock()
		}
		row.Cached = true
		row.CanRemove = ready && !row.Listed
		known[repo.ID] = row
	}
	if manager, ok := s.cache.(cacheManager); ok {
		info, err := manager.Info(listed)
		controls.CacheBytes = info.Bytes
		controls.Errors = append(controls.Errors, info.Problems...)
		if err != nil {
			controls.Errors = append(controls.Errors, err.Error())
		}
	}
	for _, row := range known {
		controls.Repositories = append(controls.Repositories, row)
	}
	slices.SortFunc(controls.Repositories, func(a, b ControlledRepository) int { return strings.Compare(a.ID, b.ID) })
	if !ready {
		controls.Errors = append(controls.Errors, "Refresh live GitHub sources before removing unlisted caches.")
	}
	return controls, nil
}

func (s *Service) ApplyRepositoryControls(selection RepositorySelection) (Snapshot, error) {
	cached, _ := s.cache.CachedRepositories()
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("wait for refresh before changing repository controls")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	known := map[string]bool{}
	for _, repo := range s.listed {
		known[repo.ID] = true
	}
	for _, repo := range s.snapshot.Repositories {
		known[repo.ID] = true
	}
	if s.snapshot.Mode == "cache" {
		for _, repo := range cached {
			if s.accountAllows(repo.ID) && sourceContains(s.snapshot.Settings, repo.ID) {
				known[repo.ID] = true
			}
		}
	}
	for _, id := range selection.Included {
		if !known[id] {
			s.mu.Unlock()
			return Snapshot{}, errors.New("repository selection contains an unavailable repository")
		}
	}
	if s.sessionExcluded == nil {
		s.sessionExcluded = make(map[string]bool, len(known))
	}
	for id := range known {
		if s.group != "" && !slices.Contains(s.analysis.Groups[s.group], id) {
			continue
		}
		s.sessionExcluded[id] = !slices.Contains(selection.Included, id)
	}
	previous := s.retainSelected(cloneSnapshot(s.snapshot))
	s.snapshot = cloneSnapshot(previous)
	s.snapshot.Status.Phase = "refreshing"
	s.busy = true
	s.workers.Add(1)
	s.mu.Unlock()
	return s.refresh(previous)
}

func sourceContains(settings Settings, id string) bool {
	owner, _, _ := strings.Cut(id, "/")
	return settings.PersonalAccount == "" && len(settings.Organizations) == 0 || owner == settings.PersonalAccount || slices.Contains(settings.Organizations, owner)
}

func (s *Service) RemoveCachedRepository(id string) (RepositoryControls, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return RepositoryControls{}, errors.New("wait for refresh before removing a cache")
	}
	if !s.inventoryReady {
		s.mu.Unlock()
		return RepositoryControls{}, errors.New("refresh live sources before removing an unlisted cache")
	}
	if slices.ContainsFunc(s.listed, func(repo git.Repository) bool { return repo.ID == id }) {
		s.mu.Unlock()
		return RepositoryControls{}, errors.New("listed repositories cannot be removed; filters and groups do not make a cache unlisted")
	}
	manager, ok := s.cache.(cacheManager)
	if !ok {
		s.mu.Unlock()
		return RepositoryControls{}, errors.New("cache removal is unavailable")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return RepositoryControls{}, err
	}
	s.busy = true
	s.workers.Add(1)
	s.mu.Unlock()
	err := manager.Remove(id)
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	s.workers.Done()
	if err != nil {
		return RepositoryControls{}, err
	}
	return s.RepositoryControls()
}

func (s *Service) SelectGroup(name string) (Snapshot, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("wait for refresh before changing groups")
	}
	if name != "" {
		if _, ok := s.analysis.Groups[name]; !ok {
			s.mu.Unlock()
			return Snapshot{}, errors.New("unknown repository group")
		}
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	s.group = name
	previous := emptySnapshot(s.snapshot.Settings, s.cachePath)
	s.decorateLocked(&previous)
	s.snapshot = cloneSnapshot(previous)
	s.snapshot.Status.Phase = "refreshing"
	s.busy = true
	s.workers.Add(1)
	s.mu.Unlock()
	return s.refresh(previous)
}

func (s *Service) OpenPath(request PathRequest) (string, error) {
	s.mu.Lock()
	var selected *Repository
	for _, repo := range s.snapshot.Repositories {
		if repo.ID == request.Repo {
			copy := repo
			selected = &copy
			break
		}
	}
	if selected == nil {
		s.mu.Unlock()
		return "", errors.New("repository is unavailable in this snapshot")
	}
	if request.Path == "" {
		s.mu.Unlock()
		return "https://github.com/" + selected.ID, nil
	}
	if request.Revision == "" || request.Revision != selected.Revision {
		s.mu.Unlock()
		return "", errors.New("the requested revision is not the retained scan revision")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return "", err
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	repo, err := s.cache.Repository(selected.ID, selected.Branch)
	if err != nil {
		return "", err
	}
	if _, err := s.cache.Revision(ctx, repo); err != nil {
		return "", err
	}
	kind, err := git.PathObjectType(ctx, repo, request.Revision, request.Path)
	if err != nil {
		return "", err
	}
	if kind != "tree" && kind != "blob" {
		return "", errors.New("path is not a file or directory at the retained revision")
	}
	parts := strings.Split(request.Path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "https://github.com/" + selected.ID + "/" + kind + "/" + request.Revision + "/" + strings.Join(parts, "/"), nil
}
