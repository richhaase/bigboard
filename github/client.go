package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenSource func(context.Context) (string, error)

type Request struct {
	Method   string
	Endpoint string
	Header   http.Header
	Body     []byte
	timeout  time.Duration
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	elapsed    time.Duration
}

type Client interface {
	Do(context.Context, Request) (Response, error)
}

type HTTPClient struct {
	token  TokenSource
	client *http.Client
}

func NewHTTPClient(token TokenSource) *HTTPClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = 16 * 1024
	return &HTTPClient{token: token, client: &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *HTTPClient) Do(ctx context.Context, request Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	endpoint, err := url.Parse(request.Endpoint)
	if err != nil || endpoint.IsAbs() || endpoint.Host != "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.Path == "" || strings.HasPrefix(endpoint.Path, "/") || endpoint.RawPath != "" || strings.Contains(endpoint.Path, "..") || strings.ContainsAny(request.Endpoint, "\r\n\\") {
		return Response{}, ErrRequest
	}
	if request.Method != http.MethodGet && (request.Method != http.MethodPost || request.Endpoint != "graphql") {
		return Response{}, ErrRequest
	}
	if c.token == nil {
		return Response{}, ErrAuthentication
	}
	token, err := c.token(ctx)
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	if err != nil {
		return Response{}, safeRequestError(err)
	}
	if !validToken(token) {
		return Response{}, ErrAuthentication
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, "https://api.github.com/"+request.Endpoint, bytes.NewReader(request.Body))
	if err != nil {
		return Response{}, ErrRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "Bigboard")
	if request.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag := request.Header.Get("If-None-Match"); validETag(etag) {
		req.Header.Set("If-None-Match", etag)
	}
	response, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, ErrUnavailable
	}
	defer response.Body.Close()
	result := Response{StatusCode: response.StatusCode, Header: make(http.Header)}
	for _, key := range []string{"Etag", "Retry-After", "X-RateLimit-Remaining", "X-RateLimit-Reset"} {
		if value := response.Header.Get(key); value != "" {
			result.Header.Set(key, value)
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxOutputBytes+1))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if len(body) > MaxOutputBytes {
		return result, ErrOutputLimit
	}
	if err != nil {
		return result, ErrResponse
	}
	result.Body = body
	return result, nil
}

func validToken(token string) bool {
	if token == "" || len(token) > 4096 {
		return false
	}
	for _, c := range token {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}

func (p *Provider) apiRequest(ctx context.Context, request Request) (Response, error) {
	select {
	case p.requests <- struct{}{}:
		defer func() { <-p.requests }()
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
	if p.cooldown().After(p.now()) {
		return Response{}, ErrRateLimited
	}
	timeout := request.timeout
	if timeout <= 0 || timeout > RequestTimeout {
		timeout = RequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	response, err := p.client.Do(ctx, request)
	response.elapsed = time.Since(started)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	err = safeRequestError(err)
	if len(response.Body) > MaxOutputBytes {
		return Response{}, ErrOutputLimit
	}
	retryAt, limited, throttled := responseQuota(response, p.now())
	if response.StatusCode >= 400 && err == nil {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			err = ErrAuthentication
		case http.StatusForbidden:
			err = ErrPermission
		case http.StatusNotFound:
			err = ErrRepository
		default:
			err = ErrRequest
		}
	}
	if response.StatusCode >= 300 && response.StatusCode != http.StatusNotModified && response.StatusCode < 400 && err == nil {
		err = ErrRequest
	}
	var envelope struct {
		Data struct {
			RateLimit *struct {
				Remaining int
				ResetAt   string
			}
		}
		Errors  []apiError
		Message string
	}
	if json.Unmarshal(response.Body, &envelope) == nil {
		if request.Endpoint == "graphql" && envelope.Data.RateLimit != nil && envelope.Data.RateLimit.Remaining <= 0 {
			limited = true
			if reset, parseErr := time.Parse(time.RFC3339, envelope.Data.RateLimit.ResetAt); parseErr == nil && reset.After(retryAt) {
				retryAt = reset
			}
		}
		for _, problem := range envelope.Errors {
			if kind := classifyFailure(problem.Type + " " + problem.Message); kind == ErrRateLimited || kind == ErrAuthentication {
				err = kind
			}
		}
		if response.StatusCode == 403 && classifyFailure(envelope.Message) == ErrRateLimited {
			throttled = true
		}
	}
	if throttled {
		err = ErrRateLimited
	}
	if limited || errors.Is(err, ErrRateLimited) {
		p.limitUntil(retryAt)
	} else if err == nil {
		p.quotaSuccess()
	}
	return response, err
}
