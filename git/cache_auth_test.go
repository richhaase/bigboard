package git

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/github"
)

func cacheAuthGit(t *testing.T, program string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCacheFetchPassesTokenOnlyThroughScopedEnvironment(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	authPath := filepath.Join(dir, "authorization")
	t.Setenv("BIGBOARD_TEST_ARGS", argsPath)
	t.Setenv("BIGBOARD_TEST_AUTH", authPath)
	cacheAuthGit(t, "printf '%s\\n' \"$@\" > \"$BIGBOARD_TEST_ARGS\"\nprintf '%s' \"$BIGBOARD_GIT_AUTHORIZATION\" > \"$BIGBOARD_TEST_AUTH\"\nprintf 'untrusted remote output'\n")
	const token = "fixture-access-token"
	calls := 0
	cache := &RepositoryCache{TokenSource: func(ctx context.Context) (string, error) {
		calls++
		return token, nil
	}}
	out, err := cache.git(context.Background(), dir, "fetch", "https://github.com/example/project.git")
	if err != nil || out != "" || calls != 1 {
		t.Fatalf("authenticated fetch: output=%q, calls=%d, error=%v", out, calls, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	if strings.Contains(string(args), token) || strings.Contains(string(args), header) {
		t.Fatal("credentials appeared in Git arguments")
	}
	for _, want := range []string{"--config-env=http.https://github.com/.extraHeader=BIGBOARD_GIT_AUTHORIZATION", "http.followRedirects=false", "credential.helper=", "https://github.com/example/project.git"} {
		if !strings.Contains(string(args), want+"\n") {
			t.Errorf("missing argument %q", want)
		}
	}
	auth, err := os.ReadFile(authPath)
	if err != nil || string(auth) != header {
		t.Fatal("Git did not receive the expected authorization header in its process environment")
	}
}

func TestCacheFetchRequiresAuthenticationWithoutAffectingLocalReads(t *testing.T) {
	cacheAuthGit(t, "printf 'local fixture'\n")
	cache := &RepositoryCache{}
	if _, err := cache.git(context.Background(), "", "fetch", "https://github.com/example/project.git"); !errors.Is(err, github.ErrAuthentication) {
		t.Fatalf("missing token source: %v", err)
	}
	if out, err := cache.git(context.Background(), "", "--version"); err != nil || out != "local fixture" {
		t.Fatalf("local read required authentication: %q, %v", out, err)
	}
	for _, tc := range []struct {
		name  string
		token string
		err   error
		want  error
	}{
		{name: "missing", want: github.ErrAuthentication},
		{name: "provider failure", err: errors.New("fixture-secret-provider-error"), want: github.ErrRequest},
		{name: "invalid token", token: "fixture\ncredential", want: github.ErrAuthentication},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cache.TokenSource = func(context.Context) (string, error) {
				calls++
				return tc.token, tc.err
			}
			if _, err := cache.git(context.Background(), "", "fetch", "https://github.com/example/project.git"); err != tc.want {
				t.Fatalf("authentication failure was not opaque: %v", err)
			}
			if _, err := cache.git(context.Background(), "", "--version"); err != nil || calls != 1 {
				t.Fatalf("local read queried token source: calls=%d, error=%v", calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache.TokenSource = func(context.Context) (string, error) {
		t.Fatal("canceled fetch queried token source")
		return "", nil
	}
	if _, err := cache.git(ctx, "", "fetch"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fetch: %v", err)
	}
}

func TestCacheFetchPreservesSafeTokenFailuresWithoutDiagnostics(t *testing.T) {
	cacheAuthGit(t, "exit 87\n")
	for _, want := range []error{context.Canceled, context.DeadlineExceeded, github.ErrUnavailable, github.ErrRateLimited, github.ErrRequest, github.ErrResponse, github.ErrAuthentication, github.ErrPermission} {
		t.Run(want.Error(), func(t *testing.T) {
			cache := &RepositoryCache{TokenSource: func(context.Context) (string, error) {
				return "", errors.Join(want, errors.New("fixture-secret-provider-error"))
			}}
			if _, err := cache.git(t.Context(), "", "fetch", "https://github.com/example/project.git"); err != want {
				t.Fatalf("error = %v; want safe typed failure %v", err, want)
			}
		})
	}
}

func TestCacheFetchDoesNotExposeRemoteDiagnostics(t *testing.T) {
	const token = "fixture-access-token"
	for _, tc := range []struct {
		name    string
		message string
		auth    bool
	}{
		{name: "network", message: "connection failed"},
		{name: "authentication", message: "Authentication failed", auth: true},
		{name: "forbidden", message: "The requested URL returned error: 403", auth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BIGBOARD_TEST_FAILURE", tc.message)
			cacheAuthGit(t, "printf '%s %s fixture-access-token' \"$BIGBOARD_TEST_FAILURE\" \"$BIGBOARD_GIT_AUTHORIZATION\" >&2\nexit 128\n")
			cache := &RepositoryCache{TokenSource: func(context.Context) (string, error) { return token, nil }}
			out, err := cache.git(context.Background(), "", "fetch", "https://github.com/example/project.git")
			if err == nil || out != "" || errors.Is(err, github.ErrAuthentication) != tc.auth {
				t.Fatalf("fetch failure: output=%q, error=%v", out, err)
			}
			header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), header) || strings.Contains(err.Error(), tc.message) {
				t.Fatal("fetch failure exposed remote diagnostics")
			}
		})
	}
}
