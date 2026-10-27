package tui

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/richhaase/bigboard/git"
)

type localSnapshot struct {
	records      []git.CommitRecord
	repositories []git.Repository
	failed       []string
	errors       map[string]string
	scannedAt    map[string]time.Time
	staleRepos   map[string]bool
}

func (s localSnapshot) loadError() error {
	if len(s.repositories) == 0 && len(s.failed) > 0 {
		return fmt.Errorf("all %d repositories failed to scan", len(s.failed))
	}
	return nil
}

func (s *localSnapshot) retainRepository(previous localSnapshot, id string) {
	for _, repo := range previous.repositories {
		if repo.ID != id {
			continue
		}
		s.repositories = append(s.repositories, repo)
		for _, record := range previous.records {
			if record.RepoID == id {
				s.records = append(s.records, record)
			}
		}
		return
	}
}

type scanOutcome struct {
	result      RepoLoadedMsg
	completedAt time.Time
}

type scanBatch struct {
	generation uint64
	running    bool
	initial    bool
	inFlight   map[string]bool
	remaining  int
	active     int
	next       int
	outcomes   []scanOutcome
}

func newScanBatch(repositories []git.Repository, generation uint64, initial bool) scanBatch {
	batch := scanBatch{
		generation: generation,
		running:    true,
		initial:    initial,
		inFlight:   make(map[string]bool),
		remaining:  len(repositories),
	}
	for range min(len(repositories), maxConcurrentRepoScans) {
		batch.scheduleNext(repositories)
	}
	return batch
}

func (b *scanBatch) scheduleNext(repositories []git.Repository) (git.Repository, bool) {
	if !b.running || b.next >= len(repositories) || b.active >= maxConcurrentRepoScans {
		return git.Repository{}, false
	}
	repo := repositories[b.next]
	b.inFlight = maps.Clone(b.inFlight)
	b.inFlight[repo.ID] = true
	b.active++
	b.next++
	return repo, true
}

func (b *scanBatch) admit(result RepoLoadedMsg, completedAt time.Time) bool {
	if !b.running || result.Generation != b.generation || !b.inFlight[result.Repository.ID] {
		return false
	}
	b.inFlight = maps.Clone(b.inFlight)
	delete(b.inFlight, result.Repository.ID)
	result.Records = slices.Clone(result.Records)
	b.outcomes = append(slices.Clip(b.outcomes), scanOutcome{result: result, completedAt: completedAt})
	b.active--
	b.remaining--
	return true
}

func (b *scanBatch) publish(previous localSnapshot) (localSnapshot, bool) {
	if !b.running || b.remaining > 0 {
		return previous, false
	}
	next := localSnapshot{
		errors:     make(map[string]string),
		scannedAt:  make(map[string]time.Time),
		staleRepos: make(map[string]bool),
	}
	maps.Copy(next.scannedAt, previous.scannedAt)
	maps.Copy(next.staleRepos, previous.staleRepos)
	for _, outcome := range b.outcomes {
		result := outcome.result
		id := result.Repository.ID
		if result.Err != nil || result.FetchErr != nil {
			next.failed = append(next.failed, result.Repository.Name)
			next.errors[id] = displayText(errors.Join(result.FetchErr, result.Err).Error())
			next.staleRepos[id] = true
		}
		if result.Err != nil {
			next.retainRepository(previous, id)
			continue
		}
		if result.FetchErr == nil || b.initial {
			next.scannedAt[id] = outcome.completedAt
		}
		if result.FetchErr == nil {
			delete(next.staleRepos, id)
		}
		next.records = append(next.records, result.Records...)
		next.repositories = append(next.repositories, result.Repository)
	}
	sort.Slice(next.repositories, func(i, j int) bool {
		return next.repositories[i].Name < next.repositories[j].Name
	})
	sort.Strings(next.failed)
	b.running = false
	b.outcomes = nil
	return next, true
}

func (b *scanBatch) stop() {
	b.running = false
}
