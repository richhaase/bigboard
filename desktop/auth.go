package main

import (
	"context"
	"errors"
	"sync"

	"github.com/richhaase/bigboard/auth"
	"github.com/richhaase/bigboard/gui"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type authentication interface {
	Token(context.Context) (string, error)
	Status(context.Context) auth.Status
	Start(context.Context) (string, error)
	Complete(context.Context, string) error
	Cancel()
	SignOut(context.Context) error
	InstallationURL() string
}

type DesktopSnapshot struct {
	gui.Snapshot
	Auth *auth.Status `json:"auth,omitempty"`
}

type DesktopSnapshotStatus struct {
	gui.SnapshotStatus
	Auth *auth.Status `json:"auth,omitempty"`
}

type sessionState struct {
	mu            sync.Mutex
	connecting    sync.Mutex
	generation    uint64
	accountID     string
	blocked       bool
	fresh         bool
	connectCancel context.CancelFunc
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) AuthStatus() auth.Status {
	if a.auth == nil {
		return auth.Status{}
	}
	status := a.auth.Status(a.context())
	a.session.mu.Lock()
	blocked := a.session.blocked
	a.session.mu.Unlock()
	if blocked {
		status.SignedIn, status.Login, status.AccountID = false, "", ""
	}
	return status
}

func (a *App) currentService() (*gui.Service, error) {
	if a.auth == nil {
		if a.service == nil {
			return nil, errors.New("sign in with GitHub to open your workspace")
		}
		return a.service, nil
	}
	status := a.AuthStatus()
	if !status.SignedIn {
		return nil, errors.New("sign in with GitHub to open your workspace")
	}
	a.session.connecting.Lock()
	defer a.session.connecting.Unlock()
	status = a.AuthStatus()
	if !status.SignedIn {
		return nil, errors.New("sign in with GitHub to open your workspace")
	}
	a.session.mu.Lock()
	if a.service != nil && a.session.accountID == status.AccountID {
		service := a.service
		a.session.mu.Unlock()
		return service, nil
	}
	previous := a.service
	changed := a.session.accountID != "" && a.session.accountID != status.AccountID
	fresh := a.session.fresh || changed
	a.service = nil
	generation := a.session.generation
	ctx, cancel := context.WithCancel(a.context())
	a.session.connectCancel = cancel
	a.session.mu.Unlock()
	defer cancel()
	if previous != nil {
		previous.Close()
	}
	connect := a.connect
	if connect == nil {
		connect = gui.NewServiceForStoredAccount
		if fresh {
			connect = gui.NewServiceForAccount
		}
	}
	service, err := connect(ctx, status.AccountID, a.auth.Token)
	if err != nil {
		return nil, err
	}
	if changed {
		service.ResetSelection()
	}
	current := a.AuthStatus()
	a.session.mu.Lock()
	if generation != a.session.generation || !current.SignedIn || current.AccountID != status.AccountID {
		a.session.mu.Unlock()
		service.Close()
		return nil, errors.New("GitHub account changed; reopen the workspace")
	}
	a.service, a.session.accountID, a.session.connectCancel = service, status.AccountID, nil
	a.session.fresh = false
	a.session.mu.Unlock()
	return service, nil
}

func (a *App) wrapSnapshot(service *gui.Service, snapshot gui.Snapshot) DesktopSnapshot {
	if a.auth == nil {
		return DesktopSnapshot{Snapshot: snapshot}
	}
	status := a.AuthStatus()
	valid := a.sessionCurrent(service)
	if !valid {
		return signedOutSnapshot(status)
	}
	return DesktopSnapshot{Snapshot: snapshot, Auth: &status}
}

func (a *App) sessionCurrent(service *gui.Service) bool {
	if a.auth == nil {
		return true
	}
	status := a.AuthStatus()
	a.session.mu.Lock()
	defer a.session.mu.Unlock()
	return !a.session.blocked && service != nil && service == a.service && a.session.accountID == status.AccountID && status.SignedIn
}

func signedOutSnapshot(status auth.Status) DesktopSnapshot {
	phase, message := "signin", "Sign in with GitHub to choose repositories."
	if status.SignedIn {
		phase, message = "access", "Verify repository access to open your workspace."
	}
	return DesktopSnapshot{Snapshot: gui.Snapshot{
		Mode: "live", Repositories: []gui.Repository{}, Commits: []gui.Commit{}, PullRequests: []gui.PR{},
		Settings: gui.Settings{Mode: "live", Organizations: []string{}, Repositories: []string{}, ExcludedRepositories: []string{}},
		Status:   gui.Status{Phase: phase, Message: message, Errors: []string{}}, Preferences: gui.Preferences{Since: "14d", Sort: "total"},
	}, Auth: &status}
}

func (a *App) Snapshot() DesktopSnapshot {
	service, err := a.currentService()
	if err != nil {
		status := a.AuthStatus()
		if status.SignedIn {
			status.Error = err.Error()
		}
		return signedOutSnapshot(status)
	}
	return a.wrapSnapshot(service, service.Snapshot())
}

func (a *App) SnapshotStatus() DesktopSnapshotStatus {
	service, err := a.currentService()
	if err != nil {
		status := a.AuthStatus()
		if status.SignedIn {
			status.Error = err.Error()
		}
		snapshot := signedOutSnapshot(status)
		return DesktopSnapshotStatus{SnapshotStatus: gui.SnapshotStatus{Phase: snapshot.Status.Phase, Message: snapshot.Status.Message}, Auth: &status}
	}
	result := service.SnapshotStatus()
	if a.auth == nil {
		return DesktopSnapshotStatus{SnapshotStatus: result}
	}
	status := a.AuthStatus()
	if !a.sessionCurrent(service) {
		snapshot := signedOutSnapshot(status)
		result = gui.SnapshotStatus{Phase: snapshot.Status.Phase, Message: snapshot.Status.Message}
	}
	return DesktopSnapshotStatus{SnapshotStatus: result, Auth: &status}
}

func (a *App) withSnapshot(operation func(*gui.Service) (gui.Snapshot, error)) (DesktopSnapshot, error) {
	service, err := a.currentService()
	if err != nil {
		return DesktopSnapshot{}, err
	}
	snapshot, err := operation(service)
	if err != nil {
		return DesktopSnapshot{}, err
	}
	return a.wrapSnapshot(service, snapshot), nil
}

func (a *App) dropService() {
	a.session.mu.Lock()
	service, cancel := a.service, a.session.connectCancel
	a.service, a.session.connectCancel = nil, nil
	a.session.generation++
	a.session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if service != nil {
		service.Close()
	}
}

func (a *App) StartSignIn() (auth.Status, error) {
	if a.auth == nil || a.ctx == nil {
		return auth.Status{}, errors.New("sign in from the Bigboard desktop app")
	}
	address, err := a.auth.Start(a.context())
	if err != nil {
		return a.AuthStatus(), err
	}
	a.openAuthURL(address)
	return a.AuthStatus(), nil
}

func (a *App) CancelSignIn() auth.Status {
	if a.auth != nil {
		a.auth.Cancel()
	}
	return a.AuthStatus()
}

func (a *App) SignOut() (DesktopSnapshot, error) {
	if a.auth == nil {
		return DesktopSnapshot{}, errors.New("GitHub sign-in is unavailable")
	}
	a.session.mu.Lock()
	a.session.blocked = true
	a.session.mu.Unlock()
	a.dropService()
	err := a.auth.SignOut(a.context())
	return signedOutSnapshot(a.AuthStatus()), err
}

func (a *App) ManageGitHubAccess() error {
	if a.auth == nil || a.ctx == nil {
		return errors.New("manage GitHub access from the Bigboard desktop app")
	}
	address := a.auth.InstallationURL()
	if address == "" || !validExternalURL(address) {
		return errors.New("GitHub access management is unavailable in this build")
	}
	a.openAuthURL(address)
	return nil
}

func (a *App) openAuthURL(address string) {
	if a.openBrowser != nil {
		a.openBrowser(address)
		return
	}
	runtime.BrowserOpenURL(a.ctx, address)
}

func (a *App) handleAuthURL(address string) {
	if a.auth == nil || !a.auth.Status(a.context()).Pending {
		return
	}
	a.dropService()
	if err := a.auth.Complete(a.context(), address); err != nil {
		return
	}
	a.session.mu.Lock()
	a.session.blocked = false
	a.session.fresh = true
	a.session.mu.Unlock()
}

func (a *App) shutdown() {
	a.session.mu.Lock()
	service, cancel := a.service, a.session.connectCancel
	a.service, a.session.connectCancel = nil, nil
	a.session.generation++
	a.session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if service != nil {
		service.Close()
		_ = service.SaveBookmark()
	}
}
