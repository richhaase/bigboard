package github

import (
	"bytes"
	"context"
	"errors"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
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
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	previous, cached := p.conditional.get(endpoint)
	args := []string{"api", "--hostname", "github.com", "--method", "GET", "--include"}
	if cached {
		args = append(args, "-H", "If-None-Match: "+previous.etag)
	}
	data, err := p.runner.Run(ctx, "gh", append(args, endpoint)...)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(data) > MaxOutputBytes {
		return nil, ErrOutputLimit
	}
	status, header, body, parsed := parseIncludedResponse(data)
	if parsed && status == 304 && cached && (err == nil || errors.Is(err, ErrRequest)) {
		return slices.Clone(previous.body), nil
	}
	if err != nil {
		p.conditional.forget(endpoint)
		return nil, safeRequestError(err)
	}
	if !parsed || status != 200 {
		p.conditional.forget(endpoint)
		return nil, ErrResponse
	}
	if etag := header.Get("Etag"); validETag(etag) {
		p.conditional.put(endpoint, conditionalEntry{etag: etag, body: slices.Clone(body)})
	} else {
		p.conditional.forget(endpoint)
	}
	return body, nil
}

func parseIncludedResponse(data []byte) (int, textproto.MIMEHeader, []byte, bool) {
	head, body := data, []byte(nil)
	end, size := bytes.Index(data, []byte("\r\n\r\n")), 4
	if bare := bytes.Index(data, []byte("\n\n")); bare >= 0 && (end < 0 || bare < end) {
		end, size = bare, 2
	}
	if end >= 0 {
		head, body = data[:end], data[end+size:]
	}
	lines := strings.Split(strings.ReplaceAll(string(head), "\r\n", "\n"), "\n")
	fields := strings.Fields(lines[0])
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, nil, nil, false
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || status < 100 || status > 599 {
		return 0, nil, nil, false
	}
	header := make(textproto.MIMEHeader)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != key || key == "" {
			return 0, nil, nil, false
		}
		header.Add(key, strings.TrimSpace(value))
	}
	return status, header, body, true
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
