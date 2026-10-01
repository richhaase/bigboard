package github

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxRefreshRequests bounds all unique repositories in one dashboard refresh.
const MaxRefreshRequests = 128

type budgetKey struct{}
type requestBudget struct {
	mu        sync.Mutex
	remaining int
}

// WithRequestBudget shares a request allowance across repository fetches. Local
// origin lookups do not consume it. Cancellation never refunds a started call.
func WithRequestBudget(ctx context.Context, requests int) context.Context {
	return context.WithValue(ctx, budgetKey{}, &requestBudget{remaining: max(0, requests)})
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

// gh --include prefixes the JSON body with HTTP headers. Retain only the safe
// quota fields, never diagnostic text or authentication headers. Plain JSON is
// also accepted for injected runners.
func responseBody(data []byte, now time.Time) ([]byte, time.Time, bool, bool) {
	var retryAt time.Time
	limited, throttled := false, false
	for bytes.HasPrefix(data, []byte("HTTP/")) {
		end, sep := bytes.Index(data, []byte("\r\n\r\n")), 4
		if end < 0 {
			end, sep = bytes.Index(data, []byte("\n\n")), 2
		}
		if end < 0 {
			break
		}
		lines := strings.Split(strings.ReplaceAll(string(data[:end]), "\r\n", "\n"), "\n")
		status := strings.Fields(lines[0])
		if len(status) > 1 && status[1] == "429" {
			limited, throttled = true, true
		}
		var reset time.Time
		remainingZero := false
		for _, line := range lines[1:] {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			switch strings.ToLower(key) {
			case "retry-after":
				var until time.Time
				if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
					until = now.Add(time.Duration(seconds) * time.Second)
				} else {
					for _, layout := range []string{time.RFC1123, time.RFC850, time.ANSIC} {
						if date, err := time.Parse(layout, value); err == nil {
							until = date
							break
						}
					}
				}
				if until.After(retryAt) {
					retryAt = until
				}
			case "x-ratelimit-remaining":
				remainingZero = value == "0"
			case "x-ratelimit-reset":
				if epoch, err := strconv.ParseInt(value, 10, 64); err == nil {
					reset = time.Unix(epoch, 0)
				}
			}
		}
		if len(status) > 1 && status[1] == "403" && retryAt.After(now) {
			limited, throttled = true, true
		}
		if remainingZero {
			limited = true
			if reset.After(retryAt) {
				retryAt = reset
			}
		}
		data = data[end+sep:]
	}
	return data, retryAt, limited, throttled
}
