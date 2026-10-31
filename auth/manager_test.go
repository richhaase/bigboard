package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/bigboard/github"
)

type memoryStore struct {
	data                        []byte
	loadErr, saveErr, deleteErr error
	saves                       int
}

func (s *memoryStore) Load() ([]byte, error) { return append([]byte(nil), s.data...), s.loadErr }
func (s *memoryStore) Save(data []byte) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.data = append([]byte(nil), data...)
	s.saves++
	return nil
}
func (s *memoryStore) Delete() error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.data = nil
	return nil
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testManager(t *testing.T, store *memoryStore) *Manager {
	t.Helper()
	m := NewManager(Config{ClientID: "fixture-client", ClientSecret: "fixture-public-client-parameter", RedirectURI: CallbackURI, AppSlug: "fixture-app"}, store)
	m.now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network request")
		return nil, errors.New("unexpected")
	})
	return m
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func tokenReply() string {
	return `{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15897600}`
}
func begin(t *testing.T, m *Manager) *url.URL {
	t.Helper()
	raw, err := m.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func callback(state string) string {
	return CallbackURI + "?" + url.Values{"state": {state}, "code": {"fixture-code"}}.Encode()
}
func savedCredentials(m *Manager) credentials {
	return credentials{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: m.now().Add(time.Hour), RefreshExpiresAt: m.now().Add(30 * 24 * time.Hour), Login: "example", AccountID: "123", ClientID: m.config.ClientID}
}
func seed(t *testing.T, m *Manager, value credentials) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	m.store.(*memoryStore).data = data
}

func TestPKCEStateAndExactCallback(t *testing.T) {
	store := &memoryStore{}
	m := testManager(t, store)
	u := begin(t, m)
	values := u.Query()
	if u.Scheme != "https" || u.Host != "github.com" || u.Path != "/login/oauth/authorize" || values.Get("redirect_uri") != CallbackURI || values.Get("code_challenge_method") != "S256" {
		t.Fatal("incorrect authorization destination or PKCE parameters")
	}
	if values.Get("client_secret") != "" || values.Get("code_verifier") != "" || values.Get("scope") != "" {
		t.Fatal("authorization URL contains unnecessary parameters")
	}
	challenge := sha256.Sum256([]byte(m.pending.verifier))
	if values.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || len(values.Get("state")) < 43 {
		t.Fatal("PKCE or state is not unpredictable")
	}
	var calls int
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("auth request lacks deadline")
		}
		if calls == 1 {
			if r.URL.String() != "https://github.com/login/oauth/access_token" || r.Method != "POST" || r.Header.Get("Authorization") != "" {
				t.Error("unsafe token destination")
			}
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			actual := sha256.Sum256([]byte(form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(actual[:]) != values.Get("code_challenge") || form.Get("client_id") != m.config.ClientID || form.Get("client_secret") != m.config.ClientSecret || form.Get("redirect_uri") != CallbackURI {
				t.Error("token request does not bind the original authorization")
			}
			return reply(200, tokenReply()), nil
		}
		if r.URL.String() != "https://api.github.com/user" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-access" {
			t.Error("identity request not authenticated correctly")
		}
		return reply(200, `{"login":"example","id":123}`), nil
	})
	if err := m.Complete(t.Context(), callback(values.Get("state"))); err != nil {
		t.Fatal(err)
	}
	status := m.Status(t.Context())
	if !status.SignedIn || status.Login != "example" || status.AccountID != "123" || status.Pending || store.saves != 1 || calls != 2 {
		t.Fatalf("sign-in did not complete: %+v", status)
	}
	data, _ := json.Marshal(status)
	if strings.Contains(string(data), "fixture-access") || strings.Contains(string(data), "fixture-refresh") || strings.Contains(string(data), m.config.ClientSecret) {
		t.Fatal("credentials leaked in status")
	}
	if err := m.Complete(t.Context(), callback(values.Get("state"))); !errors.Is(err, ErrCallback) || calls != 2 {
		t.Fatal("authorization callback was replayable")
	}
	if value, err := m.Token(t.Context()); err != nil || value != "fixture-access" || calls != 2 {
		t.Fatal("valid token was not reused")
	}
}

func TestCallbackRejectsUnsolicitedMalformedAndExpiredResponses(t *testing.T) {
	for _, value := range []string{"https://github.com/oauth/github", "com.richhaase.bigboard://oauth/other", "com.richhaase.bigboard://other/github", "com.richhaase.bigboard://name@oauth/github", "com.richhaase.bigboard://oauth:443/github", "com.richhaase.bigboard://oauth/%67ithub", "com.richhaase.bigboard://oauth/github#fragment"} {
		t.Run(value, func(t *testing.T) {
			m := testManager(t, &memoryStore{})
			begin(t, m)
			if err := m.Complete(t.Context(), value); !errors.Is(err, ErrCallback) {
				t.Fatal("untrusted callback accepted")
			}
		})
	}
	m := testManager(t, &memoryStore{})
	u := begin(t, m)
	state := u.Query().Get("state")
	for _, raw := range []string{callback("wrong"), callback(state) + "&state=" + state, callback(state) + "&code=second", CallbackURI + "?state=" + state + "&code=%00"} {
		if err := m.Complete(t.Context(), raw); !errors.Is(err, ErrCallback) {
			t.Fatal("malformed callback accepted")
		}
	}
	u = begin(t, m)
	m.now = func() time.Time { return time.Date(2026, 10, 29, 13, 0, 0, 0, time.UTC) }
	if err := m.Complete(t.Context(), callback(u.Query().Get("state"))); !errors.Is(err, ErrCallback) {
		t.Fatal("expired authorization accepted")
	}
	u = begin(t, m)
	m.Cancel()
	if err := m.Complete(t.Context(), callback(u.Query().Get("state"))); !errors.Is(err, ErrCallback) {
		t.Fatal("canceled authorization accepted")
	}
}

func TestRefreshRotatesOnceForConcurrentRequests(t *testing.T) {
	m := testManager(t, &memoryStore{})
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	var calls atomic.Int32
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" {
			t.Error("incorrect refresh request")
		}
		return reply(200, tokenReply()), nil
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			value, err := m.Token(t.Context())
			if err != nil || value != "fixture-access" {
				t.Error("refresh did not yield a token")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 || m.store.(*memoryStore).saves != 1 || m.Status(t.Context()).AccountID != "123" {
		t.Fatal("refresh rotation was duplicated or lost identity")
	}
}

func TestRefreshPersistenceFailureDoesNotReuseSpentRefreshToken(t *testing.T) {
	store := &memoryStore{saveErr: errors.New("private diagnostic")}
	m := testManager(t, store)
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	calls := 0
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, tokenReply()), nil })
	if token, err := m.Token(t.Context()); token != "" || err != github.ErrRequest {
		t.Fatal("unpersisted token was exposed")
	}
	if token, err := m.Token(t.Context()); token != "" || err != github.ErrRequest || calls != 1 {
		t.Fatal("pending storage failure was treated as revoked access or repeated rotation")
	}
	store.saveErr = nil
	if token, err := m.Token(t.Context()); token != "fixture-access" || err != nil || calls != 1 || store.saves != 1 {
		t.Fatal("rotated refresh credentials were lost after Keychain error")
	}
}

func TestTransientExpiredTokenRefreshPreservesGrantAndErrorType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport transportFunc
		want      error
	}{
		{name: "offline", transport: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("private diagnostic")
		}, want: github.ErrUnavailable},
		{name: "rate limited", transport: func(*http.Request) (*http.Response, error) {
			response := reply(http.StatusTooManyRequests, "private diagnostic")
			response.Header.Set("Retry-After", "120")
			return response, nil
		}, want: github.ErrRateLimited},
		{name: "provider unavailable", transport: func(*http.Request) (*http.Response, error) {
			return reply(http.StatusServiceUnavailable, "private diagnostic"), nil
		}, want: github.ErrRequest},
		{name: "malformed response", transport: func(*http.Request) (*http.Response, error) {
			return reply(http.StatusOK, `{"access_token":"private diagnostic"}`), nil
		}, want: github.ErrRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memoryStore{}
			m := testManager(t, store)
			old := savedCredentials(m)
			old.ExpiresAt = m.now().Add(-time.Minute)
			seed(t, m, old)
			m.client.Transport = tc.transport
			if token, err := m.Token(t.Context()); token != "" || err != tc.want {
				t.Fatalf("error = %v; want safe transient failure %v", err, tc.want)
			}
			if status := m.Status(t.Context()); !status.SignedIn || status.AccountID != old.AccountID || strings.Contains(status.Error, "private diagnostic") {
				t.Fatal("transient refresh lost the account or exposed provider diagnostics")
			}
			if store.saves != 0 || m.credentials != old || m.unsaved != nil {
				t.Fatal("transient refresh changed the stored grant")
			}
			if tc.want == github.ErrRateLimited {
				clock := m.retryAt.Add(time.Second)
				m.now = func() time.Time { return clock }
			}
			m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				return reply(http.StatusOK, tokenReply()), nil
			})
			if token, err := m.Token(t.Context()); token != "fixture-access" || err != nil || store.saves != 1 {
				t.Fatal("transient failure prevented retrying the retained grant")
			}
		})
	}
}

func TestExpiredTokenRefreshCancellationPreservesGrant(t *testing.T) {
	m := testManager(t, &memoryStore{})
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})
	if token, err := m.Token(ctx); token != "" || err != context.Canceled || m.credentials != old {
		t.Fatal("canceled refresh invalidated the grant")
	}
	if !m.Status(t.Context()).SignedIn {
		t.Fatal("canceled refresh signed out the account")
	}
}

func TestStorageAndAuthFailuresAreOpaque(t *testing.T) {
	m := testManager(t, &memoryStore{loadErr: errors.New("private diagnostic")})
	if status := m.Status(t.Context()); status.Error != ErrStorage.Error() || status.SignedIn {
		t.Fatal("storage diagnostics escaped")
	}
	if token, err := m.Token(t.Context()); token != "" || err != github.ErrRequest {
		t.Fatal("storage failure was treated as revoked access")
	}
	m = testManager(t, &memoryStore{})
	u := begin(t, m)
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return reply(200, `{"error":"bad_verification_code","error_description":"private diagnostic"}`), nil
	})
	if err := m.Complete(t.Context(), callback(u.Query().Get("state"))); !errors.Is(err, ErrSignIn) || m.Status(t.Context()).SignedIn {
		t.Fatal("provider diagnostics escaped")
	}
	m = testManager(t, &memoryStore{})
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return reply(400, `{"error":"bad_refresh_token","error_description":"private diagnostic"}`), nil
	})
	if _, err := m.Token(t.Context()); !errors.Is(err, github.ErrAuthentication) {
		t.Fatal("invalid refresh did not require sign-in")
	}
	if status := m.Status(t.Context()); status.SignedIn || status.Error != ErrReauthorize.Error() {
		t.Fatal("expired authorization remained usable")
	}
}

func TestSignOutClearsOnlyOwnedStoreAndPendingState(t *testing.T) {
	store := &memoryStore{}
	m := testManager(t, store)
	seed(t, m, savedCredentials(m))
	begin(t, m)
	if err := m.SignOut(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.Status(t.Context()).SignedIn || m.Status(t.Context()).Pending || len(store.data) != 0 {
		t.Fatal("sign-out retained credentials")
	}
	if _, err := m.Token(t.Context()); !errors.Is(err, github.ErrAuthentication) {
		t.Fatal("signed-out credentials usable")
	}
}

func TestAuthNeverFollowsRedirectsAndBoundsResponses(t *testing.T) {
	for _, kind := range []string{"redirect", "oversized", "transport"} {
		t.Run(kind, func(t *testing.T) {
			m := testManager(t, &memoryStore{})
			u := begin(t, m)
			calls := 0
			m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if kind == "transport" {
					return nil, errors.New("private diagnostic")
				}
				if kind == "oversized" {
					return reply(200, strings.Repeat("a", 32*1024+1)), nil
				}
				r := reply(302, "")
				r.Header.Set("Location", "https://other.example/token")
				return r, nil
			})
			err := m.Complete(t.Context(), callback(u.Query().Get("state")))
			if err == nil || calls != 1 || strings.Contains(err.Error(), "private diagnostic") {
				t.Fatal("unsafe auth response accepted")
			}
		})
	}
}

func TestAuthCancellationAndPublisherConfiguration(t *testing.T) {
	m := testManager(t, &memoryStore{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled sign-in started")
	}
	m.config.ClientSecret = ""
	if _, err := m.Start(t.Context()); !errors.Is(err, ErrConfiguration) || m.Status(t.Context()).Configured {
		t.Fatal("unconfigured build allowed sign-in")
	}
	m = testManager(t, &memoryStore{})
	value := savedCredentials(m)
	value.ClientID = "another-publisher-client"
	seed(t, m, value)
	if m.Status(t.Context()).SignedIn {
		t.Fatal("credentials crossed publisher client boundary")
	}
	if m.InstallationURL() != "https://github.com/apps/fixture-app/installations/new" {
		t.Fatal("unsafe installation link")
	}
}

func TestAuthRefreshRespectsRetryAfterWithoutRepeatedRequests(t *testing.T) {
	m := testManager(t, &memoryStore{})
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	calls := 0
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := reply(429, "untrusted body")
		r.Header.Set("Retry-After", "120")
		return r, nil
	})
	for range 2 {
		if _, err := m.Token(t.Context()); err != github.ErrRateLimited {
			t.Fatal("limited refresh produced a token")
		}
	}
	if calls != 1 || !m.retryAt.Equal(m.now().Add(2*time.Minute)) {
		t.Fatal("authorization rate limit was ignored")
	}
}

func TestNewSignInDiscardsUnsavedPreviousAccountRotation(t *testing.T) {
	store := &memoryStore{saveErr: errors.New("storage unavailable")}
	m := testManager(t, store)
	old := savedCredentials(m)
	old.ExpiresAt = m.now()
	seed(t, m, old)
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			return reply(200, `{"login":"another","id":456}`), nil
		}
		return reply(200, tokenReply()), nil
	})
	if _, err := m.Token(t.Context()); err == nil || m.unsaved == nil {
		t.Fatal("expected pending rotated credential storage")
	}
	store.saveErr = nil
	u := begin(t, m)
	if err := m.Complete(t.Context(), callback(u.Query().Get("state"))); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.Status(t.Context()).AccountID != "456" || m.unsaved != nil {
		t.Fatal("old account rotation replaced new sign-in")
	}
}

func TestAuthRefreshBacksOffOnHeaderlessSecondaryLimit(t *testing.T) {
	m := testManager(t, &memoryStore{})
	clock := m.now()
	m.now = func() time.Time { return clock }
	old := savedCredentials(m)
	old.ExpiresAt = clock
	seed(t, m, old)
	calls := 0
	m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return reply(http.StatusForbidden, `{"message":"You have exceeded a secondary rate limit. private diagnostic"}`), nil
	})
	for attempt := range 7 {
		for range 2 {
			if token, err := m.Token(t.Context()); token != "" || err != github.ErrRateLimited {
				t.Fatal("rate-limited refresh produced credentials")
			}
		}
		want := time.Minute * time.Duration(1<<min(attempt, 4))
		if calls != attempt+1 || !m.retryAt.Equal(clock.Add(want)) || m.Status(t.Context()).Error != github.ErrRateLimited.Error() {
			t.Fatal("headerless secondary limit did not retain a sanitized bounded cooldown")
		}
		clock = m.retryAt.Add(time.Second)
	}
}

func TestQueuedAuthOperationsHonorCancellationDuringRefresh(t *testing.T) {
	operations := map[string]func(*Manager, context.Context) error{
		"token":    func(m *Manager, ctx context.Context) error { _, err := m.Token(ctx); return err },
		"start":    func(m *Manager, ctx context.Context) error { _, err := m.Start(ctx); return err },
		"complete": func(m *Manager, ctx context.Context) error { return m.Complete(ctx, CallbackURI) },
		"sign out": func(m *Manager, ctx context.Context) error { return m.SignOut(ctx) },
		"status": func(m *Manager, ctx context.Context) error {
			status := m.Status(ctx)
			if status.Error == context.Canceled.Error() && !status.SignedIn && status.Login == "" && status.AccountID == "" {
				return context.Canceled
			}
			return errors.New("queued status did not report cancellation")
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			store := &memoryStore{}
			m := testManager(t, store)
			old := savedCredentials(m)
			old.ExpiresAt = m.now()
			seed(t, m, old)
			started, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			var calls atomic.Int32
			m.client.Transport = transportFunc(func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				close(started)
				select {
				case <-release:
					return reply(200, tokenReply()), nil
				case <-request.Context().Done():
					return nil, request.Context().Err()
				}
			})
			refreshed := make(chan error, 1)
			go func() {
				_, err := m.Token(t.Context())
				refreshed <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("initial refresh did not start")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			queued, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(queued)
				done <- operation(m, ctx)
			}()
			<-queued
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("queued operation error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled operation waited for the active refresh")
			}
			close(release)
			if err := <-refreshed; err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || store.saves != 1 || m.Status(t.Context()).AccountID != "123" {
				t.Fatal("queued cancellation changed credential rotation or identity")
			}
		})
	}
}

func TestCancelInterruptsInFlightSignInWithoutPersisting(t *testing.T) {
	store := &memoryStore{}
	m := testManager(t, store)
	u := begin(t, m)
	started := make(chan struct{})
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	complete := make(chan error, 1)
	go func() { complete <- m.Complete(t.Context(), callback(u.Query().Get("state"))) }()
	<-started
	canceled := make(chan struct{})
	go func() { m.Cancel(); close(canceled) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("cancel waited for the exchange timeout")
	}
	if err := <-complete; err == nil {
		t.Fatal("canceled exchange succeeded")
	}
	if store.saves != 0 || m.Status(t.Context()).SignedIn || m.Status(t.Context()).Pending {
		t.Fatal("canceled sign-in persisted credentials")
	}
}
