package main

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartupFailureIsVisibleAndEscaped(t *testing.T) {
	options := startupErrorOptions(errors.New(`invalid configuration at /tmp/<script>config</script>.json`))
	if options.Title != "Bigboard could not start" || options.StartHidden || options.AssetServer == nil || len(options.Bind) != 0 {
		t.Fatal("startup failure must show an independent error window")
	}
	response := httptest.NewRecorder()
	options.AssetServer.Handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "invalid configuration at /tmp/&lt;script&gt;config&lt;/script&gt;.json") || strings.Contains(body, "<script>config</script>") {
		t.Fatalf("unexpected error page: %s", body)
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("startup error is not served as HTML")
	}
}
