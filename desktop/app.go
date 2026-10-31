package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/richhaase/bigboard/gui"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	auth        authentication
	session     sessionState
	connect     func(context.Context, string, func(context.Context) (string, error)) (*gui.Service, error)
	openBrowser func(string)
	ctx         context.Context
	service     *gui.Service
}

func (a *App) Refresh() (DesktopSnapshot, error) {
	return a.withSnapshot(func(service *gui.Service) (gui.Snapshot, error) { return service.Refresh() })
}
func (a *App) SaveSettings(settings gui.Settings) (DesktopSnapshot, error) {
	return a.withSnapshot(func(service *gui.Service) (gui.Snapshot, error) { return service.SaveSettings(settings) })
}
func (a *App) Discover(settings gui.Settings) ([]gui.Repository, error) {
	service, err := a.currentService()
	if err != nil {
		return nil, err
	}
	result, err := service.Discover(settings)
	if !a.sessionCurrent(service) {
		return nil, errors.New("GitHub account changed")
	}
	return result, err
}
func (a *App) IdentityOptions() gui.IdentityOptions {
	service, err := a.currentService()
	if err != nil {
		return gui.IdentityOptions{Identities: []gui.IdentityOption{}}
	}
	result := service.IdentityOptions()
	if !a.sessionCurrent(service) {
		return gui.IdentityOptions{Identities: []gui.IdentityOption{}}
	}
	return result
}
func (a *App) SaveIdentity(request gui.IdentityRequest) (DesktopSnapshot, error) {
	return a.withSnapshot(func(service *gui.Service) (gui.Snapshot, error) { return service.SaveIdentity(request) })
}
func (a *App) RepositoryControls() (gui.RepositoryControls, error) {
	service, err := a.currentService()
	if err != nil {
		return gui.RepositoryControls{}, err
	}
	result, err := service.RepositoryControls()
	if !a.sessionCurrent(service) {
		return gui.RepositoryControls{}, errors.New("GitHub account changed")
	}
	return result, err
}
func (a *App) ApplyRepositoryControls(request gui.RepositorySelection) (DesktopSnapshot, error) {
	return a.withSnapshot(func(service *gui.Service) (gui.Snapshot, error) { return service.ApplyRepositoryControls(request) })
}
func (a *App) RemoveCachedRepository(id string) (gui.RepositoryControls, error) {
	service, err := a.currentService()
	if err != nil {
		return gui.RepositoryControls{}, err
	}
	result, err := service.RemoveCachedRepository(id)
	if !a.sessionCurrent(service) {
		return gui.RepositoryControls{}, errors.New("GitHub account changed")
	}
	return result, err
}
func (a *App) SelectGroup(name string) (DesktopSnapshot, error) {
	return a.withSnapshot(func(service *gui.Service) (gui.Snapshot, error) { return service.SelectGroup(name) })
}
func (a *App) OpenPath(request gui.PathRequest) (string, error) {
	service, err := a.currentService()
	if err != nil {
		return "", err
	}
	result, err := service.OpenPath(request)
	if !a.sessionCurrent(service) {
		return "", errors.New("GitHub account changed")
	}
	return result, err
}
func (a *App) ExportJSON() (string, error) {
	service, err := a.currentService()
	if err != nil {
		return "", err
	}
	result, err := service.ExportJSON()
	if !a.sessionCurrent(service) {
		return "", errors.New("GitHub account changed")
	}
	return result, err
}
func (a *App) SaveExportJSON() (string, error) {
	if a.ctx == nil {
		return "", errors.New("use the preview download action to export")
	}
	data, err := a.ExportJSON()
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export contributor statistics", DefaultFilename: "bigboard-contributors.json", Filters: []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}}})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		return "", err
	}
	return path, nil
}
func (a *App) OpenURL(raw string) error {
	if !validExternalURL(raw) {
		return errors.New("only GitHub repository links can be opened")
	}
	if a.ctx == nil {
		return errors.New("open links directly in the preview")
	}
	runtime.BrowserOpenURL(a.ctx, raw)
	return nil
}
func (a *App) CopyText(text string) error {
	if a.ctx == nil {
		return errors.New("use your browser's clipboard in the preview")
	}
	return runtime.ClipboardSetText(a.ctx, text)
}
func validExternalURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil && strings.HasPrefix(parsed.Path, "/") && !strings.ContainsAny(raw, "\r\n")
}
