package gui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/richhaase/bigboard/git"
)

func (s *Service) IdentityOptions() IdentityOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	byEmail := map[string]IdentityOption{}
	for _, commit := range s.snapshot.Commits {
		if commit.Email == "" {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(commit.Email))
		if _, ok := byEmail[email]; !ok {
			byEmail[email] = IdentityOption{Email: email, Name: commit.Author, Aliases: []string{}}
		}
	}
	for email, identity := range s.analysis.Identities {
		option := byEmail[email]
		option.Email = email
		if identity.Name != "" {
			option.Name = identity.Name
		}
		option.Aliases = append([]string{}, identity.Aliases...)
		byEmail[email] = option
		for _, alias := range identity.Aliases {
			delete(byEmail, alias)
		}
	}
	result := IdentityOptions{Identities: []IdentityOption{}}
	for _, option := range byEmail {
		result.Identities = append(result.Identities, option)
	}
	slices.SortFunc(result.Identities, func(a, b IdentityOption) int {
		if order := strings.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return strings.Compare(a.Email, b.Email)
	})
	return result
}

func (s *Service) SaveIdentity(request IdentityRequest) (Snapshot, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Snapshot{}, errors.New("wait for refresh before editing contributor identities")
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	var identities map[string]git.Identity
	var err error
	if len(request.Emails) <= 1 {
		identities, err = git.SetIdentityDisplayName(s.analysis.Identities, request.PrimaryEmail, request.Name)
	} else {
		identities, err = git.MergeIdentities(s.analysis.Identities, request.PrimaryEmail, request.Emails, request.Name)
	}
	if err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	if s.configStore == nil {
		s.configStore = newConfigStore(filepath.Join(filepath.Dir(s.path), "config.json"), s.analysis.source)
	}
	if err := s.configStore.saveIdentities(identities); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	s.analysis.Identities = identities
	previous := cloneSnapshot(s.snapshot)
	s.busy = true
	s.workers.Add(1)
	s.snapshot.Status.Phase = "refreshing"
	s.snapshot.Status.Message = "Rescanning cached history with saved contributor identities…"
	s.mu.Unlock()
	defer s.workers.Done()
	local := cloneSnapshot(previous)
	local.Settings.Repositories = []string{}
	local.Settings.ExcludedRepositories = nil
	for _, repo := range previous.Repositories {
		local.Settings.Repositories = append(local.Settings.Repositories, repo.ID)
	}
	result := previous
	if len(local.Settings.Repositories) > 0 {
		result = s.refreshRepositories(local, true)
	}
	result.Settings = previous.Settings
	for _, old := range previous.Repositories {
		if !slices.ContainsFunc(result.Repositories, func(repo Repository) bool { return repo.ID == old.ID }) {
			old.Stale = true
			old.Error = "Cached history unavailable during identity rescan"
			result.Repositories = append(result.Repositories, old)
			result.Commits = append(result.Commits, retainedCommits(previous, old.ID)...)
			result.Status.Errors = append(result.Status.Errors, old.ID+": "+old.Error)
		}
	}
	result.PullRequests = previous.PullRequests
	result.Viewer = previous.Viewer
	result.Status.ProbeRequests = previous.Status.ProbeRequests
	result.Status.InventoryRequests = previous.Status.InventoryRequests
	result.Status.FileRequests = previous.Status.FileRequests
	result.Status.AccountRequests = previous.Status.AccountRequests
	for i := range result.Repositories {
		for _, old := range previous.Repositories {
			if old.ID == result.Repositories[i].ID {
				result.Repositories[i].PRCheckedAt = old.PRCheckedAt
				result.Repositories[i].PRInventoryComplete = old.PRInventoryComplete
				result.Repositories[i].PRFilesComplete = old.PRFilesComplete
				result.Repositories[i].PRReviewersComplete = old.PRReviewersComplete
				result.Repositories[i].PRStale = old.PRStale
				if old.Stale {
					result.Repositories[i].Stale = true
					result.Repositories[i].Error = old.Error
				}
			}
		}
	}
	result.Status.Errors = append(result.Status.Errors, previous.Status.Errors...)
	result.Status.Message = "Contributor identities saved; cached history rescanned without network requests."
	result.Status.Phase = "ready"
	if len(result.Status.Errors) > 0 {
		result.Status.Phase = "partial"
	}
	s.mu.Lock()
	s.decorateLocked(&result)
	s.snapshot = result
	s.busy = false
	result = cloneSnapshot(result)
	s.mu.Unlock()
	return result, nil
}
