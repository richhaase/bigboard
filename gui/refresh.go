package gui

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

func (s *Service) refreshRepositories(previous Snapshot, offline, trackedOnly bool) (result Snapshot) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	settings := previous.Settings
	result = emptySnapshot(settings, s.cachePath)
	if offline {
		settings.Mode = "cache"
	}
	if s.accountID != "" && settings.Mode == "live" {
		if err := s.invalidateAccess(); err != nil {
			result = cloneSnapshot(previous)
			result.Status.Phase = "partial"
			result.Status.Errors = append(result.Status.Errors, "Preparing repository access: "+err.Error())
			return result
		}
	}
	denied := map[string]bool{}
	denyAll := false
	defer func() {
		if s.accountID == "" || settings.Mode != "live" {
			return
		}
		s.mu.Lock()
		allowed := make(map[string]git.Repository, len(s.authorized))
		for id, repo := range s.authorized {
			if !denyAll && !denied[id] {
				allowed[id] = repo
			}
		}
		s.mu.Unlock()
		if err := s.replaceAccess(context.WithoutCancel(ctx), allowed); err != nil {
			result.Status.Errors = append(result.Status.Errors, "Saving repository access: "+err.Error())
		}
		if !denyAll && len(denied) == 0 {
			return
		}
		s.mu.Lock()
		result = s.restrictAccess(result)
		s.mu.Unlock()
		result.Status.Phase = "partial"
		result.Status.Message = "GitHub access was denied. Sign in again or manage repository access."
	}()
	result.GeneratedAt = s.now().Format(time.RFC3339)
	repositories, discoveryErr := s.discover(ctx, settings)
	if discoveryErr != nil {
		result.Status.Errors = append(result.Status.Errors, "Repository discovery: "+discoveryErr.Error())
		if len(repositories) == 0 {
			for _, old := range previous.Repositories {
				repo, err := s.cache.Repository(old.ID, old.Branch)
				if err == nil {
					repositories = append(repositories, repo)
				}
			}
		}
	} else if settings.Mode == "live" {
		s.mu.Lock()
		s.listed = slices.Clone(repositories)
		s.inventoryReady = true
		s.mu.Unlock()
	}
	selected := []git.Repository{}
	for _, repo := range repositories {
		if (!trackedOnly || slices.ContainsFunc(previous.Repositories, func(old Repository) bool { return old.ID == repo.ID })) && s.included(settings, repo.ID) {
			selected = append(selected, repo)
		}
	}
	prCtx := gh.WithRequestBudget(ctx, s.analysis.PRRequestBudget)
	probes := map[string]gh.RepositoryProbe{}
	if settings.Mode == "live" {
		if provider, ok := s.provider.(interface {
			ProbeAll(context.Context, []string) []gh.RepositoryProbe
		}); ok {
			ids := make([]string, len(selected))
			for i, repo := range selected {
				ids[i] = repo.ID
			}
			probeCtx, probeCancel := context.WithTimeout(prCtx, 2*time.Minute)
			rows := provider.ProbeAll(probeCtx, ids)
			probeCancel()
			for i, probe := range rows {
				if i < len(ids) {
					probes[ids[i]] = probe
				}
				result.Status.ProbeRequests += probe.Requests
			}
		}
	}
	if s.accountID != "" {
		for id, probe := range probes {
			if accessDenied(probe.Err) {
				denied[id] = true
			}
		}
		selected = slices.DeleteFunc(selected, func(repo git.Repository) bool { return denied[repo.ID] })
		if accessDenied(discoveryErr) {
			denyAll = true
			selected = nil
		}
	}
	type scanned struct {
		repo    Repository
		commits []Commit
		denied  bool
	}
	outcomes := make([]scanned, len(selected))
	semaphore := make(chan struct{}, 8)
	var workers sync.WaitGroup
	for i, repo := range selected {
		workers.Add(1)
		go func() {
			defer workers.Done()
			view := Repository{ID: repo.ID, Name: repo.Name, Branch: repo.DefaultBranch, Included: true}
			if !repo.PushedAt.IsZero() {
				view.PushedAt = repo.PushedAt.Format(time.RFC3339)
			}
			old := Repository{}
			for _, r := range previous.Repositories {
				if r.ID == repo.ID {
					old = r
					break
				}
			}
			view.Revision, view.ScannedAt = old.Revision, old.ScannedAt
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				view.Stale, view.Error = true, ctx.Err().Error()
				outcomes[i] = scanned{repo: view, commits: retainedCommits(previous, repo.ID)}
				return
			}
			fetched := false
			if settings.Mode == "live" {
				var err error
				probe := probes[repo.ID]
				if cache, ok := s.cache.(interface {
					RefreshIfChanged(context.Context, git.Repository, string, string) error
				}); ok && probe.Err == nil && probe.HeadOID != "" && probe.DefaultBranch != "" {
					repo.DefaultBranch, view.Branch = probe.DefaultBranch, probe.DefaultBranch
					err = cache.RefreshIfChanged(ctx, repo, probe.DefaultBranch, probe.HeadOID)
				} else {
					err = s.cache.Refresh(ctx, repo)
				}
				if err != nil {
					if s.accountID != "" && accessDenied(err) {
						outcomes[i] = scanned{repo: view, denied: true}
						return
					}
					view.Stale, view.Error = true, err.Error()
				} else {
					fetched = true
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
				view.Revision = ""
				if revisionErr == nil && err == nil && before == after {
					view.Revision = before
				}
				view.ScannedAt = s.now().Format(time.RFC3339)
				if old.Revision != "" && old.Revision == view.Revision && old.ScannedAt != "" {
					view.ScannedAt = old.ScannedAt
				}
				view.Fetched = fetched
			}
			outcomes[i] = scanned{repo: view, commits: commits}
		}()
	}
	workers.Wait()
	requests := []gh.RepositoryRequest{}
	inventories := map[string]gh.Result{}
	if s.prResults == nil {
		s.prResults = map[string]gh.Result{}
	}
	for _, outcome := range outcomes {
		if outcome.denied {
			denied[outcome.repo.ID] = true
			continue
		}
		result.Repositories = append(result.Repositories, outcome.repo)
		result.Commits = append(result.Commits, outcome.commits...)
		if outcome.repo.Error != "" {
			result.Status.Errors = append(result.Status.Errors, outcome.repo.ID+": "+outcome.repo.Error)
		}
		if settings.Mode != "live" {
			continue
		}
		id := outcome.repo.ID
		prior := s.prResults[id]
		probe := probes[id]
		if !prior.CheckedAt.IsZero() && s.now().Before(prior.CheckedAt.Add(time.Minute)) {
			reused := prior
			reused.InventoryRequests, reused.FileRequests = 0, 0
			inventories[id] = reused
		} else if probe.Err == nil && probe.OpenPRCount != nil && *probe.OpenPRCount == 0 {
			inventory := gh.Result{Repo: id, Complete: true, CheckedAt: s.now()}
			inventories[id], s.prResults[id] = inventory, inventory
		} else {
			requests = append(requests, gh.RepositoryRequest{Repo: id, Previous: prior.PRs})
		}
	}
	if settings.Mode == "live" && len(selected) > 0 {
		fetchCtx, fetchCancel := context.WithTimeout(prCtx, 2*time.Minute)
		beforeAccount := gh.RequestCount(fetchCtx)
		viewer, err := s.provider.CurrentLogin(fetchCtx)
		result.Status.AccountRequests = gh.RequestCount(fetchCtx) - beforeAccount
		if err != nil {
			result.Status.Errors = append(result.Status.Errors, "GitHub account: "+err.Error())
			if s.accountID != "" && accessDenied(err) {
				denyAll = true
			}
		} else {
			result.Viewer = viewer
		}
		if len(requests) > 0 && !denyAll {
			rows := s.provider.FetchAll(fetchCtx, requests)
			for i, request := range requests {
				inventory := gh.Result{Repo: request.Repo, Err: gh.ErrIncomplete}
				if i < len(rows) {
					inventory = rows[i]
				}
				if s.accountID != "" && accessDenied(inventory.Err) {
					denied[request.Repo] = true
					continue
				}
				retained := inventory
				if !retained.Complete {
					seen := map[int]bool{}
					for _, pr := range retained.PRs {
						seen[pr.Number] = true
					}
					retained.PRs = slices.Clone(retained.PRs)
					for _, pr := range s.prResults[request.Repo].PRs {
						if !seen[pr.Number] {
							retained.PRs = append(retained.PRs, pr)
						}
					}
				}
				inventories[request.Repo] = retained
				s.prResults[request.Repo] = retained
			}
		}
		fetchCancel()
		for i := range result.Repositories {
			repo := &result.Repositories[i]
			inventory := inventories[repo.ID]
			if inventory.Err != nil || !inventory.Complete {
				message := "open PR inventory is incomplete"
				if inventory.Err != nil {
					message = inventory.Err.Error()
				}
				result.Status.Errors = append(result.Status.Errors, repo.ID+": "+message)
			}
			result.Status.InventoryRequests += inventory.InventoryRequests
			result.Status.FileRequests += inventory.FileRequests
			repo.PRStale = !inventory.Complete && inventory.Err != nil
			repo.PRInventoryComplete = inventory.Complete
			repo.PRFilesComplete, repo.PRReviewersComplete = inventory.Complete, inventory.Complete
			if !inventory.CheckedAt.IsZero() {
				repo.PRCheckedAt = inventory.CheckedAt.Format(time.RFC3339)
			}
			seen := map[int]bool{}
			for _, pr := range inventory.PRs {
				view := convertPR(pr, inventory.Complete)
				view.Repo, view.CheckedAt = repo.ID, repo.PRCheckedAt
				result.PullRequests = append(result.PullRequests, view)
				repo.PRFilesComplete = repo.PRFilesComplete && pr.FilesComplete
				repo.PRReviewersComplete = repo.PRReviewersComplete && pr.RequestedReviewersComplete && pr.ReviewsComplete
				seen[pr.Number] = true
			}
			if !inventory.Complete {
				for _, pr := range previous.PullRequests {
					if pr.Repo == repo.ID && !seen[pr.Number] {
						pr.Complete, pr.InventoryComplete = false, false
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

func convertPR(pr gh.PullRequest, complete bool) PR {
	files := make([]string, 0, len(pr.Files))
	for _, file := range pr.Files {
		files = append(files, file.Path)
	}
	reviews := make([]Review, 0, len(pr.Reviews))
	for _, review := range pr.Reviews {
		reviews = append(reviews, Review{Author: review.Author, State: review.State, SubmittedAt: review.SubmittedAt.Format(time.RFC3339)})
	}
	return PR{Repo: pr.Repo, URL: pr.URL, Title: pr.Title, Author: pr.Author, Review: pr.ReviewDecision, Checks: pr.CheckState, UpdatedAt: pr.UpdatedAt.Format(time.RFC3339), Number: pr.Number, Draft: pr.Draft, Complete: complete && pr.ContextComplete, Files: files, Reviewers: append([]string{}, pr.RequestedReviewers...), Reviews: reviews, Mergeable: pr.Mergeable, FileCount: pr.FileCount, InventoryComplete: complete, FilesComplete: pr.FilesComplete, ReviewersComplete: pr.RequestedReviewersComplete, ReviewsComplete: pr.ReviewsComplete}
}
