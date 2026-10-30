package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/richhaase/bigboard/gui"
)

func previewHandler(frontend fs.FS, app *App, host string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, err error) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		var result any
		var err error
		decode := func(target any) error {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(target); err != nil {
				return err
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				return errors.New("expected one JSON object")
			}
			return nil
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/api/snapshot" || r.URL.Path == "/api/identities" || r.URL.Path == "/api/repository-controls" || r.URL.Path == "/api/export" || r.URL.Path == "/api/auth") {
			switch r.URL.Path {
			case "/api/auth":
				result = app.AuthStatus()
			case "/api/snapshot":
				result = app.Snapshot()
			case "/api/identities":
				result = app.IdentityOptions()
			case "/api/repository-controls":
				result, err = app.RepositoryControls()
			case "/api/export":
				result, err = app.ExportJSON()
			}
		} else if r.Method == http.MethodPost {
			if r.Header.Get("Content-Type") != "application/json" {
				fail(415, errors.New("JSON content type required"))
				return
			}
			var settings gui.Settings
			if r.URL.Path == "/api/settings" || r.URL.Path == "/api/discover" {
				decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&settings); err != nil {
					fail(400, err)
					return
				}
				if err := decoder.Decode(new(any)); err != io.EOF {
					fail(400, errors.New("expected one JSON object"))
					return
				}
			}
			switch r.URL.Path {
			case "/api/sign-in":
				result, err = app.StartSignIn()
			case "/api/cancel-sign-in":
				result = app.CancelSignIn()
			case "/api/sign-out":
				result, err = app.SignOut()
			case "/api/manage-access":
				err = app.ManageGitHubAccess()
			case "/api/refresh":
				result, err = app.Refresh()
			case "/api/settings":
				result, err = app.SaveSettings(settings)
			case "/api/discover":
				result, err = app.Discover(settings)
			case "/api/identity":
				var request gui.IdentityRequest
				if err = decode(&request); err == nil {
					result, err = app.SaveIdentity(request)
				}
			case "/api/repository-controls":
				var request gui.RepositorySelection
				if err = decode(&request); err == nil {
					result, err = app.ApplyRepositoryControls(request)
				}
			case "/api/remove-cache":
				var request struct {
					ID string `json:"id"`
				}
				if err = decode(&request); err == nil {
					result, err = app.RemoveCachedRepository(request.ID)
				}
			case "/api/group":
				var request struct {
					Name string `json:"name"`
				}
				if err = decode(&request); err == nil {
					result, err = app.SelectGroup(request.Name)
				}
			case "/api/path":
				var request gui.PathRequest
				if err = decode(&request); err == nil {
					result, err = app.OpenPath(request)
				}
			default:
				fail(404, errors.New("unknown operation"))
				return
			}
		} else {
			fail(405, errors.New("method not allowed"))
			return
		}
		if err != nil {
			fail(400, err)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	mux.Handle("/", http.FileServer(http.FS(frontend)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host) {
			http.Error(w, "Local origin required", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}

func servePreview(address string, frontend fs.FS, app *App) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("preview must bind to a loopback IP address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: previewHandler(frontend, app, listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		app.shutdown()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("Bigboard preview: http://%s\n", listener.Addr())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
