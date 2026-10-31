package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type clientRoundTripFunc func(*http.Request) (*http.Response, error)

func (f clientRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type clientReadCloser struct {
	read   func([]byte) (int, error)
	closed bool
}

func (b *clientReadCloser) Read(data []byte) (int, error) { return b.read(data) }
func (b *clientReadCloser) Close() error {
	b.closed = true
	return nil
}

func clientFixture(transport clientRoundTripFunc) *HTTPClient {
	client := NewHTTPClient(func(context.Context) (string, error) { return "fixture-token", nil })
	client.client.Transport = transport
	return client
}

func clientResponse(status int, header http.Header, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func TestHTTPClientPinsDestinationAndRequestHeaders(t *testing.T) {
	for _, tc := range []struct {
		method, endpoint, body string
	}{
		{http.MethodGet, "user/repos?per_page=100&page=2", ""},
		{http.MethodPost, "graphql", `{"query":"query { viewer { login } }"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			calls := 0
			client := clientFixture(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.String() != "https://api.github.com/"+tc.endpoint || request.Method != tc.method || request.Host != "api.github.com" {
					t.Fatal("request did not use the pinned GitHub destination and method")
				}
				for key, want := range map[string]string{
					"Authorization":        "Bearer fixture-token",
					"Accept":               "application/vnd.github+json",
					"X-GitHub-Api-Version": "2026-03-10",
					"User-Agent":           "Bigboard",
					"If-None-Match":        `W/"fixture"`,
				} {
					if request.Header.Get(key) != want {
						t.Errorf("unexpected %s header", key)
					}
				}
				if request.Header.Get("X-Caller-Header") != "" || request.Header.Get("Host") != "" {
					t.Error("caller-controlled headers were forwarded")
				}
				if tc.method == http.MethodPost && request.Header.Get("Content-Type") != "application/json" {
					t.Error("GraphQL request did not identify JSON content")
				}
				body, err := io.ReadAll(request.Body)
				if err != nil || string(body) != tc.body {
					t.Errorf("request body = %q, %v", body, err)
				}
				if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > RequestTimeout {
					t.Error("request has no bounded deadline")
				}
				return clientResponse(http.StatusOK, http.Header{"Etag": {`"response"`}}, `{"ok":true}`), nil
			})
			response, err := client.Do(t.Context(), Request{Method: tc.method, Endpoint: tc.endpoint, Body: []byte(tc.body), Header: http.Header{
				"Authorization": {"caller override"}, "Accept": {"text/plain"}, "Host": {"example.invalid"},
				"X-Caller-Header": {"ignored"}, "If-None-Match": {`W/"fixture"`},
			}})
			if err != nil || calls != 1 || response.StatusCode != http.StatusOK || response.Header.Get("Etag") != `"response"` || string(response.Body) != `{"ok":true}` {
				t.Fatalf("response = %+v, calls=%d, error=%v", response, calls, err)
			}
		})
	}
}

func TestHTTPClientRejectsUnsafeRequestsBeforeCredentials(t *testing.T) {
	for _, request := range []Request{
		{Method: http.MethodGet, Endpoint: ""},
		{Method: http.MethodGet, Endpoint: "https://example.invalid/user"},
		{Method: http.MethodGet, Endpoint: "https://api.github.com/user"},
		{Method: http.MethodGet, Endpoint: "//example.invalid/user"},
		{Method: http.MethodGet, Endpoint: "/user"},
		{Method: http.MethodGet, Endpoint: "user#fragment"},
		{Method: http.MethodGet, Endpoint: "repos/example/../other"},
		{Method: http.MethodGet, Endpoint: "%2fuser"},
		{Method: http.MethodGet, Endpoint: "user\\example"},
		{Method: http.MethodGet, Endpoint: "user\r\nX-Test: invalid"},
		{Method: http.MethodGet, Endpoint: "user\tother"},
		{Method: http.MethodGet, Endpoint: "%invalid"},
		{Method: http.MethodPost, Endpoint: "user"},
		{Method: http.MethodPost, Endpoint: "graphql?other=true"},
		{Method: http.MethodPut, Endpoint: "graphql"},
		{Method: "", Endpoint: "user"},
	} {
		t.Run(request.Method+" "+request.Endpoint, func(t *testing.T) {
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				t.Fatal("unsafe request reached the network")
				return nil, nil
			})
			client.token = func(context.Context) (string, error) {
				t.Fatal("unsafe request queried credentials")
				return "", nil
			}
			if _, err := client.Do(t.Context(), request); err != ErrRequest {
				t.Fatalf("error = %v; want request rejection", err)
			}
		})
	}
}

func TestHTTPClientSanitizesCredentialAndTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token TokenSource
		want  error
	}{
		{name: "missing source", want: ErrAuthentication},
		{name: "source error", token: func(context.Context) (string, error) { return "", errors.New("fixture-secret-provider-error") }, want: ErrRequest},
		{name: "empty token", token: func(context.Context) (string, error) { return "", nil }, want: ErrAuthentication},
		{name: "control characters", token: func(context.Context) (string, error) { return "fixture\ncredential", nil }, want: ErrAuthentication},
		{name: "whitespace", token: func(context.Context) (string, error) { return "fixture credential", nil }, want: ErrAuthentication},
		{name: "oversized token", token: func(context.Context) (string, error) { return strings.Repeat("x", 4097), nil }, want: ErrAuthentication},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid credentials reached the network")
				return nil, nil
			})
			client.token = tc.token
			if _, err := client.Do(t.Context(), Request{Method: http.MethodGet, Endpoint: "user"}); err != tc.want {
				t.Fatalf("credential failure was not sanitized: %v", err)
			}
		})
	}
	client := clientFixture(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fixture-secret-transport-error")
	})
	if _, err := client.Do(t.Context(), Request{Method: http.MethodGet, Endpoint: "user"}); err != ErrUnavailable {
		t.Fatalf("transport failure was not sanitized: %v", err)
	}
}

func TestHTTPClientPreservesSafeTokenFailuresWithoutDiagnostics(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded, ErrUnavailable, ErrRateLimited, ErrRequest, ErrResponse, ErrAuthentication, ErrPermission} {
		t.Run(want.Error(), func(t *testing.T) {
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				t.Fatal("failed token source reached the API")
				return nil, nil
			})
			client.token = func(context.Context) (string, error) {
				return "", errors.Join(want, errors.New("fixture-secret-provider-error"))
			}
			if _, err := client.Do(t.Context(), Request{Method: http.MethodGet, Endpoint: "user"}); err != want {
				t.Fatalf("error = %v; want safe typed failure %v", err, want)
			}
		})
	}
}

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	for _, destination := range []string{"https://example.invalid/collect", "https://api.github.com/redirected", "http://api.github.com/user"} {
		t.Run(destination, func(t *testing.T) {
			calls := 0
			client := clientFixture(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 || request.URL.String() != "https://api.github.com/user" {
					t.Fatal("client followed a redirect with credentials")
				}
				return clientResponse(http.StatusFound, http.Header{"Location": {destination}}, "redirect"), nil
			})
			response, err := client.Do(t.Context(), Request{Method: http.MethodGet, Endpoint: "user"})
			if err != nil || response.StatusCode != http.StatusFound || calls != 1 {
				t.Fatalf("redirect response status=%d, calls=%d, error=%v", response.StatusCode, calls, err)
			}
		})
	}
}

func TestHTTPClientBoundsResponsesAndClosesBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		err  error
	}{
		{name: "at limit", size: MaxOutputBytes},
		{name: "over limit", size: MaxOutputBytes + 16, err: ErrOutputLimit},
		{name: "read failure", err: ErrResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(strings.Repeat("x", tc.size))
			read := 0
			body := &clientReadCloser{read: func(data []byte) (int, error) {
				if tc.name == "read failure" {
					return 0, errors.New("fixture-secret-response-error")
				}
				n, err := reader.Read(data)
				read += n
				return n, err
			}}
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Body: body, Header: http.Header{
					"Retry-After": {"120"}, "X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1803816600"},
					"Set-Cookie": {"fixture-secret"}, "Authorization": {"fixture-secret"},
				}}, nil
			})
			response, err := client.Do(t.Context(), Request{Method: http.MethodGet, Endpoint: "user"})
			if err != tc.err || !body.closed || read > MaxOutputBytes+1 {
				t.Fatalf("response bound: read=%d, closed=%v, error=%v", read, body.closed, err)
			}
			if tc.err != nil && len(response.Body) != 0 {
				t.Fatal("failed response exposed partial data")
			}
			if tc.err == nil && len(response.Body) != tc.size {
				t.Fatal("successful response body was truncated")
			}
			if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "120" || response.Header.Get("X-RateLimit-Remaining") != "0" || response.Header.Get("X-RateLimit-Reset") != "1803816600" || len(response.Header) != 3 {
				t.Fatal("response did not preserve only safe rate-limit metadata")
			}
		})
	}
}

func TestHTTPProviderRetainsCooldownAfterInterruptedResponse(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, failure := range []string{"read error", "cancellation", "oversized"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				calls++
				var body io.ReadCloser = &clientReadCloser{read: func([]byte) (int, error) {
					if failure == "cancellation" {
						cancel()
					}
					return 0, errors.New("fixture-secret-response-error")
				}}
				if failure == "oversized" {
					body = io.NopCloser(strings.NewReader(strings.Repeat("x", MaxOutputBytes+1)))
				}
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"120"}}, Body: body}, nil
			})
			provider := NewProvider(client)
			provider.now = func() time.Time { return now }
			want := ErrRateLimited
			if failure == "cancellation" {
				want = context.Canceled
			}
			if _, err := provider.SetupAccount(ctx); !errors.Is(err, want) || !provider.cooldown().Equal(now.Add(2*time.Minute)) {
				t.Fatalf("interrupted response lost cooldown: retry=%s, error=%v", provider.cooldown(), err)
			}
			if _, err := provider.SetupAccount(t.Context()); !errors.Is(err, ErrRateLimited) || calls != 1 {
				t.Fatalf("request started during cooldown: calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestHTTPClientPreservesCancellation(t *testing.T) {
	for _, phase := range []string{"before request", "credentials", "transport", "body"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if phase == "before request" {
				cancel()
			}
			client := clientFixture(func(request *http.Request) (*http.Response, error) {
				if phase == "transport" {
					cancel()
					return nil, request.Context().Err()
				}
				if phase != "body" {
					t.Fatal("canceled request reached the network")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: &clientReadCloser{read: func([]byte) (int, error) {
					cancel()
					return 0, io.EOF
				}}}, nil
			})
			client.token = func(context.Context) (string, error) {
				if phase == "before request" {
					t.Fatal("canceled request queried credentials")
				}
				if phase == "credentials" {
					cancel()
				}
				return "fixture-token", nil
			}
			if _, err := client.Do(ctx, Request{Method: http.MethodGet, Endpoint: "user"}); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
		})
	}
}

func TestHTTPProviderClassifiesStatusWithoutRemoteDiagnostics(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status int
		header http.Header
		body   string
		want   error
	}{
		{name: "authentication", status: 401, want: ErrAuthentication},
		{name: "permission", status: 403, want: ErrPermission},
		{name: "missing repository", status: 404, want: ErrRepository},
		{name: "throttled", status: 429, want: ErrRateLimited},
		{name: "retry after", status: 403, header: http.Header{"Retry-After": {"90"}}, want: ErrRateLimited},
		{name: "quota exhausted", status: 403, header: http.Header{"X-Ratelimit-Remaining": {"0"}}, want: ErrRateLimited},
		{name: "secondary limit", status: 403, body: `{"message":"secondary rate limit: fixture-secret"}`, want: ErrRateLimited},
		{name: "redirect", status: 302, want: ErrRequest},
		{name: "service failure", status: 503, want: ErrRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFixture(func(*http.Request) (*http.Response, error) {
				body := tc.body
				if body == "" {
					body = `{"message":"fixture-secret"}`
				}
				return clientResponse(tc.status, tc.header, body), nil
			})
			provider := NewProvider(client)
			provider.now = func() time.Time { return now }
			if _, err := provider.SetupAccount(t.Context()); err != tc.want {
				t.Fatalf("classified error = %v; want %v", err, tc.want)
			}
		})
	}
}

func TestHTTPProviderHonorsSuccessfulRESTAndGraphQLQuota(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	reset := now.Add(10 * time.Minute)
	for _, graphql := range []bool{false, true} {
		t.Run(strconv.FormatBool(graphql), func(t *testing.T) {
			calls := 0
			client := clientFixture(func(request *http.Request) (*http.Response, error) {
				calls++
				if graphql {
					return clientResponse(200, nil, `{"data":{"viewer":{"login":"example"},"rateLimit":{"remaining":0,"resetAt":"`+reset.Format(time.RFC3339)+`"}}}`), nil
				}
				return clientResponse(200, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)}}, `{"total_count":0,"installations":[]}`), nil
			})
			provider := NewProvider(client)
			provider.now = func() time.Time { return now }
			if graphql {
				if login, err := provider.CurrentLogin(t.Context()); login != "example" || err != nil {
					t.Fatalf("successful final GraphQL response was lost: %q, %v", login, err)
				}
			} else if repos, err := provider.DiscoverAccessibleRepositories(t.Context()); len(repos) != 0 || err != nil {
				t.Fatalf("successful final REST response was lost: %v, %v", repos, err)
			}
			if _, err := provider.SetupAccount(t.Context()); !errors.Is(err, ErrRateLimited) || calls != 1 || !provider.cooldown().Equal(reset) {
				t.Fatalf("shared cooldown: calls=%d, retry=%s, error=%v", calls, provider.cooldown(), err)
			}
		})
	}
}
