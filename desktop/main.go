package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/richhaase/bigboard/gui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"runtime"
)

//go:embed frontend/index.html frontend/app.js frontend/model.js frontend/styles.css
var assets embed.FS

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
	service, err := gui.NewService()
	if err != nil {
		return err
	}
	defer service.Close()
	frontend, err := fs.Sub(assets, "frontend")
	if err != nil {
		return err
	}
	app := &App{service: service}
	if serve {
		return servePreview(address, frontend, app)
	}
	appMenu := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
	}
	return wails.Run(&options.App{
		Menu:  appMenu,
		Title: "Bigboard", Width: 1360, Height: 900, MinWidth: 760, MinHeight: 560,
		BackgroundColour: options.NewRGB(19, 28, 41),
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        func(ctx context.Context) { app.ctx = ctx },
		OnShutdown:       func(context.Context) { service.Close() },
		Bind:             []interface{}{app},
		Mac:              &mac.Options{TitleBar: mac.TitleBarDefault(), Appearance: mac.NSAppearanceNameDarkAqua},
	})
}
