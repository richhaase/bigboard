package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/richhaase/bigboard/auth"
	"github.com/richhaase/bigboard/gui"
)

type fakeAuthentication struct {
	mu          sync.Mutex
	status      auth.Status
	signOutErr  error
	completeErr error
	completed   string
}

func (a *fakeAuthentication) Token(context.Context) (string, error) { return "test-token", nil }
func (a *fakeAuthentication) Status(context.Context) auth.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}
func (a *fakeAuthentication) Start(context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.Pending = true
	return "https://github.com/login/oauth/authorize?client_id=test", nil
}
func (a *fakeAuthentication) Complete(_ context.Context, callback string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.completed = callback
	a.status.Pending = false
	if a.completeErr != nil {
		a.status.Error = a.completeErr.Error()
		return a.completeErr
	}
	a.status = auth.Status{Configured: true, SignedIn: true, Login: "second", AccountID: "42"}
	return nil
}
func (a *fakeAuthentication) Cancel() { a.mu.Lock(); defer a.mu.Unlock(); a.status.Pending = false }
func (a *fakeAuthentication) SignOut(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.signOutErr != nil {
		a.status.Error = a.signOutErr.Error()
		return a.signOutErr
	}
	a.status = auth.Status{Configured: true}
	return nil
}
func (a *fakeAuthentication) InstallationURL() string {
	return "https://github.com/apps/bigboard-test/installations/new"
}

func authenticatedTestApp(t *testing.T) (*App, *fakeAuthentication) {
	t.Helper()
	a := testApp(t)
	manager := &fakeAuthentication{status: auth.Status{Configured: true, SignedIn: true, Login: "first", AccountID: "41"}}
	a.auth, a.ctx, a.session.accountID = manager, context.Background(), "41"
	return a, manager
}

func TestSignOutGatesEveryWorkspaceSurfaceAndLateSnapshot(t *testing.T) {
	a, _ := authenticatedTestApp(t)
	old := a.service
	result, err := a.SignOut()
	if err != nil || result.Auth.SignedIn || result.Status.Phase != "signin" {
		t.Fatalf("sign out failed: %+v %v", result.Auth, err)
	}
	late := a.wrapSnapshot(old, gui.Snapshot{Repositories: []gui.Repository{{ID: "private/repo"}}, Commits: []gui.Commit{{Subject: "Private history"}}})
	if len(late.Repositories) != 0 || len(late.Commits) != 0 || late.Auth.SignedIn {
		t.Fatal("late result exposed signed-out data")
	}
	if _, err := a.ExportJSON(); err == nil {
		t.Fatal("signed-out export was allowed")
	}
	if _, err := a.RepositoryControls(); err == nil {
		t.Fatal("signed-out controls were allowed")
	}
	if len(a.IdentityOptions().Identities) != 0 {
		t.Fatal("signed-out identities were exposed")
	}
	if _, err := old.Refresh(); err == nil {
		t.Fatal("old service was not canceled")
	}
}

func TestFailedKeychainSignOutStillHidesWorkspace(t *testing.T) {
	a, manager := authenticatedTestApp(t)
	manager.signOutErr = errors.New("Keychain unavailable")
	if _, err := a.SignOut(); err == nil {
		t.Fatal("failed credential deletion was hidden")
	}
	if a.AuthStatus().SignedIn || len(a.Snapshot().Repositories) != 0 {
		t.Fatal("failed sign out reopened old credentials")
	}
	if _, err := a.currentService(); err == nil {
		t.Fatal("failed sign out allowed automatic reconnection")
	}
}

func TestNativeCallbackAndAccountSwitchRebuildSession(t *testing.T) {
	a, manager := authenticatedTestApp(t)
	old := a.service
	opened := ""
	a.openBrowser = func(address string) { opened = address }
	status, err := a.StartSignIn()
	if err != nil || !status.Pending || !strings.HasPrefix(opened, "https://github.com/login/oauth/authorize") {
		t.Fatal("native sign-in was not started")
	}
	a.connect = func(_ context.Context, id string, token func(context.Context) (string, error)) (*gui.Service, error) {
		if id != "42" || token == nil {
			t.Fatal("new stable account was not used")
		}
		service, err := gui.NewService()
		if err == nil {
			t.Cleanup(service.Close)
		}
		return service, err
	}
	a.handleAuthURL(auth.CallbackURI + "?code=test&state=test")
	if manager.completed == "" || a.service != nil {
		t.Fatal("callback retained the old service")
	}
	if _, err := old.Refresh(); err == nil {
		t.Fatal("old account work was not canceled")
	}
	next := a.Snapshot()
	if next.Auth.AccountID != "42" || next.Status.Phase != "setup" || len(next.Settings.Repositories) != 0 {
		t.Fatal("account switch did not reset repository selection")
	}
	if err := a.ManageGitHubAccess(); err != nil || opened != manager.InstallationURL() {
		t.Fatal("access management did not open publisher installation")
	}
}

func TestSignOutCancelsInFlightAccountConnection(t *testing.T) {
	a, _ := authenticatedTestApp(t)
	a.dropService()
	started := make(chan struct{})
	a.connect = func(ctx context.Context, _ string, _ func(context.Context) (string, error)) (*gui.Service, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan DesktopSnapshot, 1)
	go func() { done <- a.Snapshot() }()
	<-started
	if _, err := a.SignOut(); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.Auth.SignedIn || a.service != nil {
		t.Fatal("canceled connection published workspace data")
	}
}

func TestUnsolicitedNativeCallbackDoesNotResetWorkspace(t *testing.T) {
	a, manager := authenticatedTestApp(t)
	before := a.service
	a.handleAuthURL(auth.CallbackURI + "?code=unexpected&state=unexpected")
	if a.service != before || manager.completed != "" {
		t.Fatal("unsolicited callback changed the session")
	}
}

func TestSnapshotStatusDoesNotExposeSignedOutOrChangedAccountData(t *testing.T) {
	a, manager := authenticatedTestApp(t)
	status := a.SnapshotStatus()
	if status.Auth == nil || status.Auth.AccountID != "41" {
		t.Fatal("status omitted the signed-in account")
	}
	manager.mu.Lock()
	manager.status = auth.Status{Configured: true, SignedIn: true, Login: "second", AccountID: "42"}
	manager.mu.Unlock()
	a.connect = func(context.Context, string, func(context.Context) (string, error)) (*gui.Service, error) {
		return nil, errors.New("access verification failed")
	}
	status = a.SnapshotStatus()
	if status.Auth.AccountID != "42" || status.Phase != "access" || status.Commits != 0 || status.Repositories != 0 || status.PullRequests != 0 {
		t.Fatal("changed account exposed previous status")
	}
	if _, err := a.SignOut(); err != nil {
		t.Fatal(err)
	}
	handler := previewHandler(fstest.MapFS{"index.html": {Data: []byte("Bigboard")}}, a, "127.0.0.1:34115")
	request := httptest.NewRequest("GET", "http://127.0.0.1:34115/api/snapshot-status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &status) != nil {
		t.Fatal("status route failed")
	}
	if status.Auth.SignedIn || status.Phase != "signin" || status.Commits != 0 || status.Repositories != 0 || status.PullRequests != 0 {
		t.Fatal("signed-out status leaked counts")
	}
}

type delayedCallbackAuthentication struct {
	*fakeAuthentication
	ready   chan struct{}
	release chan struct{}
}

func (a *delayedCallbackAuthentication) Complete(ctx context.Context, callback string) error {
	err := a.fakeAuthentication.Complete(ctx, callback)
	close(a.ready)
	<-a.release
	return err
}

func TestLateNativeCallbackCannotUndoFailedSignOut(t *testing.T) {
	a, manager := authenticatedTestApp(t)
	manager.status.Pending = true
	manager.signOutErr = errors.New("Keychain unavailable")
	delayed := &delayedCallbackAuthentication{fakeAuthentication: manager, ready: make(chan struct{}), release: make(chan struct{})}
	a.auth = delayed
	done := make(chan struct{})
	go func() { a.handleAuthURL(auth.CallbackURI + "?code=test&state=test"); close(done) }()
	<-delayed.ready
	if _, err := a.SignOut(); err == nil {
		t.Fatal("expected failed credential deletion")
	}
	close(delayed.release)
	<-done
	if a.AuthStatus().SignedIn {
		t.Fatal("late callback reopened the signed-out session")
	}
}
