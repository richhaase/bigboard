package github

import (
	"context"
	"sync"
	"time"
)

type repositoryAllowance struct {
	mu       sync.Mutex
	limit    int
	requests int
	elapsed  time.Duration
}

func (a *repositoryAllowance) take(ctx context.Context) (time.Duration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	remaining := FetchTimeout - a.elapsed
	if remaining <= 0 {
		return 0, context.DeadlineExceeded
	}
	if a.requests >= a.limit || !takeRequest(ctx) {
		return 0, ErrBudgetExceeded
	}
	a.requests++
	return min(RequestTimeout, remaining), nil
}

func (a *repositoryAllowance) charge(elapsed time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.elapsed += elapsed
}

func (a *repositoryAllowance) include(other *repositoryAllowance) {
	other.mu.Lock()
	requests, elapsed := other.requests, other.elapsed
	other.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests += requests
	a.elapsed += elapsed
}

type inventoryRegistry struct {
	mu      sync.Mutex
	known   map[string]*repositoryFetch
	blocked error
}

func (r *inventoryRegistry) lookup(repo string) *repositoryFetch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.known[repo]
}

func (r *inventoryRegistry) claim(repo string, batch *repositoryFetch) *repositoryFetch {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.known[repo]; existing != nil {
		existing.session.allowance.include(batch.session.allowance)
		batch.session.allowance = existing.session.allowance
		return existing
	}
	r.known[repo] = batch
	return batch
}

func (r *inventoryRegistry) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.blocked
}

func (r *inventoryRegistry) fail(err error) {
	if !sharedFailure(err) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blocked == nil {
		r.blocked = err
	}
}

func reserveShared(ctx context.Context, allowances []*repositoryAllowance) (time.Duration, error) {
	timeout := RequestTimeout
	for _, allowance := range allowances {
		allowance.mu.Lock()
		remaining, full := FetchTimeout-allowance.elapsed, allowance.requests >= allowance.limit
		allowance.mu.Unlock()
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}
		if full {
			return 0, ErrBudgetExceeded
		}
		timeout = min(timeout, remaining)
	}
	if !takeRequest(ctx) {
		return 0, ErrBudgetExceeded
	}
	for _, allowance := range allowances {
		allowance.mu.Lock()
		allowance.requests++
		allowance.mu.Unlock()
	}
	return timeout, nil
}
