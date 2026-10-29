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
	ctx     context.Context
	service *gui.Service
}

func (a *App) Snapshot() gui.Snapshot         { return a.service.Snapshot() }
func (a *App) Refresh() (gui.Snapshot, error) { return a.service.Refresh() }
func (a *App) SaveSettings(settings gui.Settings) (gui.Snapshot, error) {
	return a.service.SaveSettings(settings)
}
func (a *App) Discover(settings gui.Settings) ([]gui.Repository, error) {
	return a.service.Discover(settings)
}
func (a *App) IdentityOptions() gui.IdentityOptions { return a.service.IdentityOptions() }
func (a *App) SaveIdentity(request gui.IdentityRequest) (gui.Snapshot, error) {
	return a.service.SaveIdentity(request)
}
func (a *App) RepositoryControls() (gui.RepositoryControls, error) {
	return a.service.RepositoryControls()
}
func (a *App) ApplyRepositoryControls(request gui.RepositorySelection) (gui.Snapshot, error) {
	return a.service.ApplyRepositoryControls(request)
}
func (a *App) RemoveCachedRepository(id string) (gui.RepositoryControls, error) {
	return a.service.RemoveCachedRepository(id)
}
func (a *App) SelectGroup(name string) (gui.Snapshot, error)    { return a.service.SelectGroup(name) }
func (a *App) OpenPath(request gui.PathRequest) (string, error) { return a.service.OpenPath(request) }
func (a *App) ExportJSON() (string, error)                      { return a.service.ExportJSON() }
func (a *App) SaveExportJSON() (string, error) {
	if a.ctx == nil {
		return "", errors.New("Use the preview download action to export")
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
		return errors.New("Only GitHub repository links can be opened")
	}
	if a.ctx == nil {
		return errors.New("Open links directly in the preview")
	}
	runtime.BrowserOpenURL(a.ctx, raw)
	return nil
}
func (a *App) CopyText(text string) error {
	if a.ctx == nil {
		return errors.New("Use your browser's clipboard in the preview")
	}
	return runtime.ClipboardSetText(a.ctx, text)
}
func validExternalURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil && strings.HasPrefix(parsed.Path, "/") && !strings.ContainsAny(raw, "\r\n")
}
