package github

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

const maxETagLength = 256

type conditionalEntry struct {
	etag string
	body []byte
}

type conditionalCache struct {
	mu      sync.Mutex
	entries map[string]conditionalEntry
}

func (c *conditionalCache) get(endpoint string) (conditionalEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[endpoint]
	return entry, ok
}

func (c *conditionalCache) put(endpoint string, entry conditionalEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]conditionalEntry)
	}
	c.entries[endpoint] = entry
}

func (c *conditionalCache) forget(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, endpoint)
}

func (p *Provider) conditionalRequest(ctx context.Context, endpoint string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous, cached := p.conditional.get(endpoint)
	header := make(http.Header)
	if cached {
		header.Set("If-None-Match", previous.etag)
	}
	response, err := p.apiRequest(ctx, Request{Method: "GET", Endpoint: endpoint, Header: header})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if response.StatusCode == http.StatusNotModified && cached && err == nil {
		return slices.Clone(previous.body), nil
	}
	if err != nil {
		p.conditional.forget(endpoint)
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		p.conditional.forget(endpoint)
		return nil, ErrResponse
	}
	header, body := response.Header, response.Body
	if etag := header.Get("Etag"); validETag(etag) {
		p.conditional.put(endpoint, conditionalEntry{etag: etag, body: slices.Clone(body)})
	} else {
		p.conditional.forget(endpoint)
	}
	return body, nil
}

func validETag(etag string) bool {
	if etag == "" || len(etag) > maxETagLength {
		return false
	}
	for _, r := range etag {
		if r < '!' || r > '~' {
			return false
		}
	}
	return true
}
