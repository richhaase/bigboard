package github

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// MaxRefreshRequests bounds all unique repositories in one dashboard refresh.
const MaxRefreshRequests = 128

type budgetKey struct{}
type requestBudget struct {
	mu        sync.Mutex
	remaining int
	limit     int
}

// WithRequestBudget shares a request allowance across repository fetches. Local
// origin lookups do not consume it. Cancellation never refunds a started call.
func WithRequestBudget(ctx context.Context, requests int) context.Context {
	return context.WithValue(ctx, budgetKey{}, &requestBudget{remaining: max(0, requests), limit: max(0, requests)})
}

func RequestCount(ctx context.Context) int {
	budget, ok := ctx.Value(budgetKey{}).(*requestBudget)
	if !ok {
		return 0
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.limit - budget.remaining
}

func repositoryRequestLimit(ctx context.Context, fallback int) int {
	if budget, ok := ctx.Value(budgetKey{}).(*requestBudget); ok && fallback == MaxRequests {
		return max(1, budget.limit/2)
	}
	return fallback
}
func takeRequest(ctx context.Context) bool {
	budget, ok := ctx.Value(budgetKey{}).(*requestBudget)
	if !ok {
		return true
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.remaining == 0 {
		return false
	}
	budget.remaining--
	return true
}

type quotaState struct {
	mu       sync.Mutex
	retryAt  time.Time
	failures int
}

func (p *Provider) cooldown() time.Time {
	p.quota.mu.Lock()
	defer p.quota.mu.Unlock()
	return p.quota.retryAt
}
func (p *Provider) limitUntil(until time.Time) {
	p.quota.mu.Lock()
	defer p.quota.mu.Unlock()
	now := p.now()
	if !until.After(now) {
		until = now.Add(time.Minute * time.Duration(1<<min(p.quota.failures, 4)))
	}
	p.quota.failures++
	if until.After(p.quota.retryAt) {
		p.quota.retryAt = until
	}
}
func (p *Provider) quotaSuccess() {
	p.quota.mu.Lock()
	defer p.quota.mu.Unlock()
	p.quota.failures = 0
}

func responseQuota(response Response, now time.Time) (time.Time, bool, bool) {
	var retryAt time.Time
	if value := response.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
			retryAt = now.Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(value); err == nil {
			retryAt = date
		}
	}
	limited := response.StatusCode == 429 || (response.StatusCode == 403 && retryAt.After(now))
	throttled := limited
	if response.Header.Get("X-RateLimit-Remaining") == "0" {
		limited = true
		if epoch, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if reset := time.Unix(epoch, 0); reset.After(retryAt) {
				retryAt = reset
			}
		}
		throttled = throttled || response.StatusCode == 403
	}
	return retryAt, limited, throttled
}
