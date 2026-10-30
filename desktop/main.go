package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/richhaase/bigboard/auth"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed frontend/index.html frontend/app.js frontend/model.js frontend/styles.css
var assets embed.FS

var version = "development"
var githubClientID string
var githubClientSecret string
var githubAppSlug string

func main() {
	serve := flag.Bool("serve", false, "Serve a local preview instead of opening the desktop window")
	address := flag.String("addr", "127.0.0.1:34115", "Loopback preview address")
	flag.Parse()
	if err := run(*serve, *address); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(serve bool, address string) error {
	if runtime.GOOS == "darwin" {
		if err := os.Setenv("PATH", desktopToolPath(os.Getenv("PATH"))); err != nil {
			return err
		}
	}
	frontend, err := fs.Sub(assets, "frontend")
	if err != nil {
		return err
	}
	var store auth.Store
	if !serve {
		store = newKeychainStore()
	}
	app := &App{auth: auth.NewManager(auth.Config{ClientID: githubClientID, ClientSecret: githubClientSecret, RedirectURI: auth.CallbackURI, AppSlug: githubAppSlug}, store)}
	defer app.shutdown()
	if serve {
		return servePreview(address, frontend, app)
	}
	appMenu := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
	}
	err = wails.Run(&options.App{
		Menu:  appMenu,
		Title: "Bigboard", Width: 1360, Height: 900, MinWidth: 760, MinHeight: 560,
		BackgroundColour: options.NewRGB(19, 28, 41),
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        func(ctx context.Context) { app.ctx = ctx },
		OnShutdown:       func(context.Context) { app.shutdown() },
		Bind:             []interface{}{app},
		Mac:              &mac.Options{OnUrlOpen: app.handleAuthURL, TitleBar: mac.TitleBarDefault(), Appearance: mac.NSAppearanceNameDarkAqua, About: &mac.AboutInfo{Title: "Bigboard", Message: "Version " + version}},
	})
	if err != nil {
		return err
	}
	return nil
}

func desktopToolPath(current string) string {
	paths := strings.Split(current, string(os.PathListSeparator))
	if current == "" {
		paths = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	}
	for _, directory := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		found := false
		for _, existing := range paths {
			if existing == directory {
				found = true
				break
			}
		}
		if !found {
			paths = append(paths, directory)
		}
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

func startupErrorOptions(err error) *options.App {
	page := `<!doctype html><html><head><meta charset="utf-8"><title>Bigboard could not start</title><style>body{background:#131c29;color:#edf2f7;font:16px -apple-system,BlinkMacSystemFont,sans-serif;margin:36px;line-height:1.5}h1{font-size:24px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:14px ui-monospace,monospace;color:#fecaca}</style></head><body><h1>Bigboard could not start</h1><p>Fix the configuration error below, then open Bigboard again.</p><pre>` + html.EscapeString(err.Error()) + `</pre></body></html>`
	return &options.App{
		Title: "Bigboard could not start", Width: 680, Height: 360, MinWidth: 420, MinHeight: 240,
		BackgroundColour: options.NewRGB(19, 28, 41),
		AssetServer: &assetserver.Options{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" && r.URL.Path != "/index.html" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(page))
		})},
		Mac: &mac.Options{TitleBar: mac.TitleBarDefault(), Appearance: mac.NSAppearanceNameDarkAqua},
	}
}
