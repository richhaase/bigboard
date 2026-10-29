package main

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/richhaase/bigboard/gui"
)

func testApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	service, err := gui.NewService()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return &App{service: service}
}

func TestPreviewOriginAndRoutes(t *testing.T) {
	var files fs.FS = fstest.MapFS{"index.html": {Data: []byte("Bigboard")}}
	handler := previewHandler(files, testApp(t), "127.0.0.1:34115")
	cases := []struct {
		method, path, host, origin, content, body string
		want                                      int
	}{
		{"GET", "/api/snapshot", "127.0.0.1:34115", "", "", "", 200},
		{"GET", "/api/snapshot", "attacker.example", "", "", "", 403},
		{"GET", "/api/snapshot", "127.0.0.1:34115", "https://attacker.example", "", "", 403},
		{"POST", "/api/refresh", "127.0.0.1:34115", "", "text/plain", "", 415},
		{"POST", "/api/refresh", "127.0.0.1:34115", "http://127.0.0.1:34115", "application/json", "{}", 200},
		{"GET", "/api/refresh", "127.0.0.1:34115", "", "", "", 405},
		{"POST", "/api/settings", "127.0.0.1:34115", "", "application/json", "{} {}", 400},
		{"POST", "/api/settings", "127.0.0.1:34115", "", "application/json", "{\"mode\":\"invalid\"}", 400},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.host+tc.origin+tc.body, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestExternalLinks(t *testing.T) {
	for _, raw := range []string{"https://github.com/acme/board", "https://github.com/acme/board/pull/12"} {
		if !validExternalURL(raw) {
			t.Errorf("rejected %q", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://github.com.evil.example/a/b", "https://user@github.com/a/b", "https://github.com:444/a/b", "https://github.com/a\nb"} {
		if validExternalURL(raw) {
			t.Errorf("accepted unsafe URL")
		}
	}
}

func TestDesktopToolPathPreservesUserOrderAndSupportsFinder(t *testing.T) {
	initial := "/usr/bin:/bin:/usr/sbin:/sbin"
	got := desktopToolPath(initial)
	if got != initial+":/opt/homebrew/bin:/usr/local/bin" {
		t.Fatalf("unexpected desktop search path: %s", got)
	}
	custom := "/custom/bin:/opt/homebrew/bin:/usr/bin:/usr/local/bin"
	if desktopToolPath(custom) != custom {
		t.Fatal("changed the user's command precedence")
	}
	if desktopToolPath("") != got {
		t.Fatal("empty PATH should include standard command directories")
	}
}
