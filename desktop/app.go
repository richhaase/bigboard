package main

import (
	"context"
	"errors"
	"net/url"
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
