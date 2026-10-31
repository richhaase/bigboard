package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/richhaase/bigboard/github"
)

const CallbackURI = "com.richhaase.bigboard://oauth/github"

var (
	ErrConfiguration = errors.New("GitHub sign-in is unavailable in this build")
	ErrStorage       = errors.New("unable to access Bigboard sign-in credentials in Keychain")
	ErrSignIn        = errors.New("GitHub sign-in could not be completed; please try again")
	ErrCallback      = errors.New("this GitHub sign-in response is invalid or expired; please sign in again")
	ErrDenied        = errors.New("GitHub sign-in was canceled or declined")
	ErrConnection    = errors.New("could not connect to GitHub; check your connection and try again")
)

var ErrReauthorize = errors.New("your GitHub sign-in has expired. Sign in again in Settings")

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AppSlug      string
}

type Store interface {
	Load() ([]byte, error)
	Save([]byte) error
	Delete() error
}

type Status struct {
	Configured bool   `json:"configured"`
	SignedIn   bool   `json:"signedIn"`
	Login      string `json:"login"`
	AccountID  string `json:"accountId"`
	Pending    bool   `json:"pending"`
	Error      string `json:"error"`
}

type credentials struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	Login            string    `json:"login"`
	AccountID        string    `json:"account_id"`
	ClientID         string    `json:"client_id"`
}

type pendingSignIn struct {
	generation uint64
	state      string
	verifier   string
	expiresAt  time.Time
}

type Manager struct {
	attemptMu         sync.Mutex
	attemptGeneration uint64
	attemptCancel     context.CancelFunc
	gateOnce          sync.Once
	gate              chan struct{}
	config            Config
	store             Store
	client            *http.Client
	now               func() time.Time
	loaded            bool
	credentials       credentials
	pending           *pendingSignIn
	unsaved           *credentials
	message           string
	retryAt           time.Time
	limitFailures     int
}

func NewManager(config Config, store Store) *Manager {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = 16 * 1024
	return &Manager{config: config, store: store, now: time.Now, client: &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (m *Manager) configured() bool {
	return validSecret(m.config.ClientID) && validSecret(m.config.ClientSecret) && m.config.RedirectURI == CallbackURI && validSlug(m.config.AppSlug) && m.store != nil
}

func (m *Manager) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.gateOnce.Do(func() { m.gate = make(chan struct{}, 1) })
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.release()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) release() {
	<-m.gate
}

func (m *Manager) load() error {
	if m.loaded {
		return nil
	}
	data, err := m.store.Load()
	if err != nil {
		return ErrStorage
	}
	var saved credentials
	if len(data) != 0 {
		if len(data) > 32*1024 || json.Unmarshal(data, &saved) != nil || !validCredentials(saved) {
			return ErrStorage
		}
	}
	if saved.ClientID != "" && saved.ClientID != m.config.ClientID {
		saved = credentials{}
	}
	m.credentials, m.loaded = saved, true
	return nil
}

func (m *Manager) Status(ctx context.Context) Status {
	if err := m.acquire(ctx); err != nil {
		return Status{Configured: m.configured(), Error: err.Error()}
	}
	defer m.release()
	status := Status{Configured: m.configured(), Error: m.message}
	if !status.Configured {
		return status
	}
	if ctx.Err() != nil {
		return status
	}
	if err := m.load(); err != nil {
		status.Error = err.Error()
		return status
	}
	if m.pending != nil && !m.pending.expiresAt.After(m.now()) {
		m.pending = nil
		m.message = ErrCallback.Error()
	}
	status.Pending = m.pending != nil
	status.SignedIn = validCredentials(m.credentials) && m.credentials.RefreshExpiresAt.After(m.now())
	if status.SignedIn {
		status.Login = m.credentials.Login
		status.AccountID = m.credentials.AccountID
	}
	status.Error = m.message
	return status
}

func (m *Manager) Token(ctx context.Context) (string, error) {
	if err := m.acquire(ctx); err != nil {
		return "", err
	}
	defer m.release()
	if !m.configured() {
		return "", github.ErrRequest
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := m.load(); err != nil {
		m.message = err.Error()
		return "", github.ErrRequest
	}
	if m.unsaved != nil {
		if err := m.save(*m.unsaved); err != nil {
			m.message = err.Error()
			return "", github.ErrRequest
		}
		m.unsaved = nil
		m.message = ""
	}
	if !validCredentials(m.credentials) || !m.credentials.RefreshExpiresAt.After(m.now()) {
		return "", github.ErrAuthentication
	}
	if m.credentials.ExpiresAt.After(m.now().Add(time.Minute)) {
		return m.credentials.AccessToken, nil
	}
	values := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {m.credentials.RefreshToken}}
	next, err := m.exchange(ctx, values)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		m.message = err.Error()
		if errors.Is(err, ErrReauthorize) {
			m.credentials.RefreshExpiresAt = time.Time{}
			return "", github.ErrAuthentication
		}
		for _, known := range []error{context.Canceled, context.DeadlineExceeded, github.ErrRateLimited} {
			if errors.Is(err, known) {
				return "", known
			}
		}
		if errors.Is(err, ErrConnection) {
			return "", github.ErrUnavailable
		}
		return "", github.ErrRequest
	}
	next.Login, next.AccountID = m.credentials.Login, m.credentials.AccountID
	if err := m.save(next); err != nil {
		m.unsaved = &next
		m.message = err.Error()
		return "", github.ErrRequest
	}
	m.message = ""
	return next.AccessToken, nil
}

func (m *Manager) Start(ctx context.Context) (string, error) {
	if err := m.acquire(ctx); err != nil {
		return "", err
	}
	defer m.release()
	if !m.configured() {
		return "", ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.cancelAttempt()
	state, verifier := randomValue(), randomValue()
	challenge := sha256.Sum256([]byte(verifier))
	m.attemptMu.Lock()
	generation := m.attemptGeneration
	m.attemptMu.Unlock()
	m.pending = &pendingSignIn{generation: generation, state: state, verifier: verifier, expiresAt: m.now().Add(10 * time.Minute)}
	m.message = ""
	values := url.Values{"client_id": {m.config.ClientID}, "redirect_uri": {CallbackURI}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	return "https://github.com/login/oauth/authorize?" + values.Encode(), nil
}

func (m *Manager) Complete(ctx context.Context, raw string) error {
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	fail := func(err error) error { m.message = err.Error(); return err }
	if !m.configured() {
		return fail(ErrConfiguration)
	}
	callback, err := url.Parse(raw)
	if err != nil || len(raw) > 16*1024 || callback.Scheme != "com.richhaase.bigboard" || callback.Host != "oauth" || callback.Path != "/github" || callback.RawPath != "" || callback.User != nil || callback.Fragment != "" {
		return fail(ErrCallback)
	}
	values, err := url.ParseQuery(callback.RawQuery)
	if err != nil || len(values["state"]) != 1 || m.pending == nil || !m.pending.expiresAt.After(m.now()) || subtle.ConstantTimeCompare([]byte(values.Get("state")), []byte(m.pending.state)) != 1 {
		return fail(ErrCallback)
	}
	pending := m.pending
	m.pending = nil
	if values.Get("error") != "" {
		return fail(ErrDenied)
	}
	if len(values["code"]) != 1 || !validSecret(values.Get("code")) {
		return fail(ErrCallback)
	}
	m.attemptMu.Lock()
	generation := pending.generation
	ctx, cancel := context.WithCancel(ctx)
	if generation != m.attemptGeneration {
		cancel()
	}
	m.attemptCancel = cancel
	m.attemptMu.Unlock()
	defer cancel()
	next, err := m.exchange(ctx, url.Values{"code": {values.Get("code")}, "redirect_uri": {CallbackURI}, "code_verifier": {pending.verifier}})
	if err != nil {
		return fail(err)
	}
	login, accountID, err := m.login(ctx, next.AccessToken)
	if err != nil {
		return fail(err)
	}
	next.Login, next.AccountID = login, accountID
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()
	if generation != m.attemptGeneration || ctx.Err() != nil {
		return fail(context.Canceled)
	}
	m.attemptCancel = nil
	if err := m.save(next); err != nil {
		return fail(err)
	}
	m.message = ""
	return nil
}

func (m *Manager) cancelAttempt() {
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()
	m.attemptGeneration++
	if m.attemptCancel != nil {
		m.attemptCancel()
		m.attemptCancel = nil
	}
}

func (m *Manager) Cancel() {
	m.cancelAttempt()
	if err := m.acquire(context.Background()); err != nil {
		return
	}
	defer m.release()
	m.pending = nil
	m.message = ""
}

func (m *Manager) SignOut(ctx context.Context) error {
	m.cancelAttempt()
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.store == nil || m.store.Delete() != nil {
		m.message = ErrStorage.Error()
		return ErrStorage
	}
	m.credentials, m.pending, m.unsaved, m.loaded, m.message = credentials{}, nil, nil, true, ""
	return nil
}

func validSecret(value string) bool {
	if value == "" || len(value) > 4096 {
		return false
	}
	for _, c := range value {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}

func validCredentials(value credentials) bool {
	return validSecret(value.AccessToken) && validSecret(value.RefreshToken) && !value.ExpiresAt.IsZero() && !value.RefreshExpiresAt.IsZero() && validLogin(value.Login) && validAccountID(value.AccountID) && validSecret(value.ClientID)
}

func validLogin(login string) bool {
	if login == "" || len(login) > 100 {
		return false
	}
	for _, c := range login {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func randomValue() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func (m *Manager) exchange(ctx context.Context, values url.Values) (credentials, error) {
	values.Set("client_id", m.config.ClientID)
	values.Set("client_secret", m.config.ClientSecret)
	data, err := m.request(ctx, "POST", "https://github.com/login/oauth/access_token", values.Encode(), "")
	if err != nil && len(data) == 0 {
		return credentials{}, err
	}
	var result struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
	}
	decodeErr := json.Unmarshal(data, &result)
	if values.Get("grant_type") == "refresh_token" && (result.Error == "bad_refresh_token" || result.Error == "invalid_grant" || result.Error == "expired_token") {
		return credentials{}, ErrReauthorize
	}
	if err != nil {
		return credentials{}, err
	}
	if decodeErr != nil || result.Error != "" || !validSecret(result.AccessToken) || !validSecret(result.RefreshToken) || !strings.EqualFold(result.TokenType, "bearer") || result.ExpiresIn <= 0 || result.ExpiresIn > 24*60*60 || result.RefreshExpiresIn <= 0 || result.RefreshExpiresIn > 366*24*60*60 {
		return credentials{}, ErrSignIn
	}
	return credentials{ClientID: m.config.ClientID, AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: m.now().Add(time.Duration(result.ExpiresIn) * time.Second), RefreshExpiresAt: m.now().Add(time.Duration(result.RefreshExpiresIn) * time.Second)}, nil
}

func (m *Manager) login(ctx context.Context, token string) (string, string, error) {
	data, err := m.request(ctx, "GET", "https://api.github.com/user", "", token)
	if err != nil {
		return "", "", err
	}
	var user struct {
		Login string      `json:"login"`
		ID    json.Number `json:"id"`
	}
	if json.Unmarshal(data, &user) != nil || !validLogin(user.Login) || !validAccountID(string(user.ID)) {
		return "", "", ErrSignIn
	}
	return user.Login, string(user.ID), nil
}

func validAccountID(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}

func validSlug(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func (m *Manager) InstallationURL() string {
	if !validSlug(m.config.AppSlug) {
		return ""
	}
	return "https://github.com/apps/" + m.config.AppSlug + "/installations/new"
}

func (m *Manager) request(ctx context.Context, method, endpoint, body, token string) ([]byte, error) {
	if m.retryAt.After(m.now()) {
		return nil, github.ErrRateLimited
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, ErrSignIn
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Bigboard")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	response, err := m.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrConnection
	}
	defer response.Body.Close()
	limited := response.StatusCode == http.StatusTooManyRequests || (response.StatusCode == http.StatusForbidden && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0"))
	if limited || response.Header.Get("X-RateLimit-Remaining") == "0" {
		var until time.Time
		if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 32); err == nil && seconds >= 0 {
			until = m.now().Add(time.Duration(seconds) * time.Second)
		} else if parsed, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
			until = parsed
		}
		if epoch, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && response.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset := time.Unix(epoch, 0); reset.After(until) {
				until = reset
			}
		}
		if !until.After(m.now()) {
			until = m.now().Add(time.Minute * time.Duration(1<<min(m.limitFailures, 4)))
		}
		m.retryAt = until
		m.limitFailures++
		if limited {
			return nil, github.ErrRateLimited
		}
	} else if response.StatusCode == http.StatusOK {
		m.limitFailures = 0
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32*1024+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || len(data) > 32*1024 {
		return nil, ErrSignIn
	}
	if response.StatusCode == http.StatusForbidden {
		var problem struct{ Message string }
		if json.Unmarshal(data, &problem) == nil {
			message := strings.ToLower(problem.Message)
			if strings.Contains(message, "rate limit") || strings.Contains(message, "rate_limit") {
				m.retryAt = m.now().Add(time.Minute * time.Duration(1<<min(m.limitFailures, 4)))
				m.limitFailures++
				return nil, github.ErrRateLimited
			}
		}
	}
	if response.StatusCode != http.StatusOK {
		return data, ErrSignIn
	}
	return data, nil
}
