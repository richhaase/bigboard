package github

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://github.com/Acme/Big.Board", "https://GitHub.com/Acme/Big.Board.git/",
		"ssh://git@github.com/Acme/Big.Board.git", "git@github.com:Acme/Big.Board.git",
		"git@GITHUB.COM:Acme/Big.Board/",
	} {
		t.Run(origin, func(t *testing.T) {
			got, err := ParseOrigin(origin)
			if err != nil || got != "acme/big.board" {
				t.Fatalf("ParseOrigin(%q) = %q, %v", origin, got, err)
			}
		})
	}
	for _, origin := range []string{
		"", "acme/repo", "/local/repo", "file:///local/repo", "git://github.com/a/b", "http://github.com/a/b",
		"https://github.com.evil.test/a/b", "https://evil.test/github.com/a/b", "git@evil.test:a/b",
		"https://user@github.com/a/b", "https://user:secret@github.com/a/b", "https://github.com@evil.test/a/b",
		"https://github.com:443/a/b", "ssh://git@github.com:22/a/b", "ssh://bob@github.com/a/b",
		"ssh://git:secret@github.com/a/b", "ssh://github.com/a/b", "git@github.com:/a/b", "git@github.com:22/a/b",
		"https://github.com/a/b?x=1", "https://github.com/a/b?", "https://github.com/a/b#x", "https://github.com/a/b#",
		"https://github.com/a/%62", "https://github.com/a/b%2fc", "https://github.com/a/b/extra", "https://github.com//a/b",
		"https://github.com/a/..", "https://github.com/a/.", "https://github.com/../repo", "https://github.com/a/../b",
		"https://github.com/a/b//", "https://github.com/a\\b", "https://github.com./a/b", "https://ɡithub.com/a/b",
		" https://github.com/a/b", "https://github.com/a/b\n", "https://github.com/a/b\r", "https://github.com/a/b\x00",
		"https://github.com/a/b\t", "https://github.com/a/b b", "https://github.com/a/b\x1b[31m", "https://github.com/a/💻",
		"https://github.com/-owner/repo", "https://github.com/owner-/repo", "https://github.com/a_b/repo",
	} {
		t.Run(origin, func(t *testing.T) {
			if got, err := ParseOrigin(origin); !errors.Is(err, ErrUnsupportedOrigin) || got != "" {
				t.Fatalf("unsafe origin accepted: %q => %q, %v", origin, got, err)
			}
		})
	}
}

func TestCanonicalRepoAndKey(t *testing.T) {
	got, err := CanonicalRepo("Owner/Repo-_.git")
	if err != nil || got != "owner/repo-_.git" {
		t.Fatalf("canonical = %q, %v", got, err)
	}
	for _, value := range []string{"owner", "owner/repo/extra", "owner/repo/", "owner/repo?x", "--arg/repo", "a/..", "a/.", "a/", "/r", strings.Repeat("a", 40) + "/r", "a/" + strings.Repeat("r", 101), "a/r\n"} {
		if _, err := CanonicalRepo(value); !errors.Is(err, ErrInvalidRepository) {
			t.Errorf("accepted invalid identifier %q", value)
		}
	}
	if got := (PullRequest{Repo: "ACME/Repo", Number: 42}).Key(); got != "acme/repo#42" {
		t.Fatalf("key = %q", got)
	}
	for _, pr := range []PullRequest{{}, {Repo: "a/b", Number: -1}, {Repo: "a/b/evil", Number: 1}} {
		if pr.Key() != "" {
			t.Errorf("invalid PR key = %q", pr.Key())
		}
	}
}

func TestResolveReadsOnlyLocalOrigin(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{{data: []byte("git@github.com:ACME/Repo.git\n")}}}
	p := NewProvider(runner)
	got, err := p.Resolve(context.Background(), "some local path;not a shell")
	if err != nil || got != "acme/repo" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
	absolute, _ := filepath.Abs("some local path;not a shell")
	want := []string{"--no-optional-locks", "-C", absolute, "-c", "core.fsmonitor=false", "config", "--local", "--no-includes", "--get-all", "remote.origin.url"}
	if runner.calls[0].command != "git" || !reflect.DeepEqual(runner.calls[0].args, want) {
		t.Fatalf("command = %#v", runner.calls[0])
	}
}

func TestResolveRejectsMissingMultipleAndUnsafeOrigins(t *testing.T) {
	for _, step := range []fakeStep{
		{err: errors.New("missing")}, {data: []byte("https://github.com/a/b\nhttps://github.com/c/d\n")},
		{data: []byte("https://github.com/a/b\n\n")}, {data: []byte("https://other.test/a/b\n")},
		{data: []byte("https://secret:password@github.com/a/b\n")},
	} {
		p := NewProvider(&fakeRunner{t: t, steps: []fakeStep{step}})
		if got, err := p.Resolve(context.Background(), "."); err == nil || got != "" {
			t.Fatalf("resolve accepted %#v => %q, %v", step, got, err)
		}
	}
	p := NewProvider(&fakeRunner{t: t})
	if _, err := p.Resolve(context.Background(), ""); err == nil {
		t.Fatal("empty path accepted")
	}
}

func TestCommandEnvironmentIsolated(t *testing.T) {
	in := []string{"HOME=/test/home", "PATH=/test/bin", "GH_TOKEN=fake-test-token",
		"GIT_DIR=/other/repo", "GIT_WORK_TREE=/other", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=alias.config",
		"GIT_CONFIG_VALUE_0=!unsafe", "GIT_CONFIG_PARAMETERS=unsafe", "GIT_CONFIG_SYSTEM=/bad", "GIT_CONFIG_GLOBAL=/bad",
		"GIT_ASKPASS=unsafe", "GH_HOST=evil.test", "GH_REPO=evil/repo", "GH_DEBUG=api", "DEBUG=1",
		"GH_PAGER=unsafe", "PAGER=unsafe", "GH_PROMPT_DISABLED=0", "GH_TELEMETRY=log", "GH_FORCE_TTY=1"}
	out := commandEnv(in)
	values := make(map[string]string)
	for _, entry := range out {
		k, v, _ := strings.Cut(entry, "=")
		if _, exists := values[k]; exists {
			t.Fatalf("duplicate environment key %s", k)
		}
		values[k] = v
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL", "GIT_ASKPASS", "GH_REPO", "GH_DEBUG", "DEBUG", "GH_FORCE_TTY"} {
		if _, exists := values[k]; exists {
			t.Errorf("inherited unsafe key %s", k)
		}
	}
	for k, want := range map[string]string{"GH_HOST": "github.com", "GH_PROMPT_DISABLED": "1", "GH_PAGER": "cat", "PAGER": "cat", "GIT_TERMINAL_PROMPT": "0", "GH_TELEMETRY": "0", "GH_TOKEN": "fake-test-token"} {
		if values[k] != want {
			t.Errorf("env %s = %q; want %q", k, values[k], want)
		}
	}
}

func TestLimitedBufferCancelsOnOverflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buffer := &limitedBuffer{limit: 4, cancel: cancel}
	if n, err := buffer.Write([]byte("ab")); n != 2 || err != nil {
		t.Fatalf("first write = %d, %v", n, err)
	}
	if n, err := buffer.Write([]byte("cdefgh")); n != 2 || !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("overflow write = %d, %v", n, err)
	}
	if buffer.String() != "abcd" || !buffer.exceeded || ctx.Err() == nil {
		t.Fatalf("buffer not bounded/canceled: %#v", buffer)
	}
}

func TestClassifyFailureDoesNotExposeDiagnostics(t *testing.T) {
	for diagnostic, want := range map[string]error{
		"API rate limit exceeded secret":    ErrRateLimited,
		"To get started, run gh auth login": ErrAuthentication,
		"HTTP 401 Bad credentials":          ErrAuthentication,
		"HTTP 404 sensitive path":           ErrRepository,
		"HTTP 500 secret raw body":          ErrRequest,
	} {
		if got := classifyFailure(diagnostic); got != want || strings.Contains(got.Error(), "secret") {
			t.Errorf("classify %q = %v, want %v", diagnostic, got, want)
		}
	}
}

func TestCanceledResolveDoesNotPretendSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeRunner{t: t, steps: []fakeStep{{err: context.Canceled}}}
	if _, err := NewProvider(runner).Resolve(ctx, "."); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func assertDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > RequestTimeout || time.Until(deadline) < -time.Second {
		t.Fatalf("runner missing bounded request deadline: %v, %v", deadline, ok)
	}
}
