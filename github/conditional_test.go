package github

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func taggedStep(etag, body string) fakeStep {
	return fakeStep{data: []byte("HTTP/2.0 200 OK\nContent-Type: application/json\r\nEtag: " + etag + "\r\nX-Ratelimit-Used: 61\r\n\r\n" + body)}
}

func notModifiedStep(etag string) fakeStep {
	return fakeStep{data: []byte("HTTP/2.0 304 Not Modified\nEtag: " + etag + "\r\nX-Ratelimit-Used: 61\r\n\r\n"), err: ErrRequest}
}

func conditionalHeader(args []string) string {
	if i := slices.Index(args, "-H"); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func TestDiscoverRepositoriesReusesPagesOnNotModified(t *testing.T) {
	body := `[{"full_name":"example/api","default_branch":"main","archived":false},{"full_name":"example/old","default_branch":"main","archived":true}]`
	runner := &discoveryRunner{t: t, steps: []fakeStep{
		taggedStep(`W/"page-1"`, body),
		notModifiedStep(`"page-1"`),
		taggedStep(`W/"page-1b"`, `[{"full_name":"example/api","default_branch":"main"},{"full_name":"example/new","default_branch":"trunk"}]`),
	}}
	provider := NewProvider(runner)
	want := []DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}}
	for i := range 2 {
		got, err := provider.DiscoverRepositories(t.Context(), []string{"example"})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("discovery %d = %+v, %v; want %+v", i, got, err, want)
		}
	}
	got, err := provider.DiscoverRepositories(t.Context(), []string{"example"})
	want = []DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}, {Name: "example/new", DefaultBranch: "trunk"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("changed discovery = %+v, %v; want %+v", got, err, want)
	}
	headers := []string{conditionalHeader(runner.calls[0].args), conditionalHeader(runner.calls[1].args), conditionalHeader(runner.calls[2].args)}
	if !reflect.DeepEqual(headers, []string{"", `If-None-Match: W/"page-1"`, `If-None-Match: W/"page-1"`}) {
		t.Fatalf("conditional headers = %q", headers)
	}
	for _, call := range runner.calls {
		if !slices.Contains(call.args, "--include") || call.args[len(call.args)-1] != "orgs/example/repos?type=all&per_page=100&page=1" {
			t.Fatalf("request = %+v", call)
		}
	}
}

func TestDiscoverPersonalRepositoriesSendsStoredETags(t *testing.T) {
	runner := &fakeRunner{t: t, steps: []fakeStep{
		taggedStep(`"user"`, `{"login":"example"}`),
		taggedStep(`"repos"`, `[{"full_name":"example/api","default_branch":"main"}]`),
		notModifiedStep(`"user"`),
		notModifiedStep(`"repos"`),
	}}
	provider := NewProvider(runner)
	for i := range 2 {
		got, err := provider.DiscoverPersonalRepositories(t.Context(), "example")
		if err != nil || !reflect.DeepEqual(got, []DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}}) {
			t.Fatalf("discovery %d = %+v, %v", i, got, err)
		}
	}
	if conditionalHeader(runner.calls[2].args) != `If-None-Match: "user"` || conditionalHeader(runner.calls[3].args) != `If-None-Match: "repos"` {
		t.Fatalf("calls = %+v", runner.calls)
	}
}

func TestConditionalRequestRejectsUnusableResponses(t *testing.T) {
	cases := []struct {
		name  string
		first fakeStep
		step  fakeStep
		want  error
	}{
		{"304-without-cache", fakeStep{}, notModifiedStep(`"x"`), ErrRequest},
		{"missing-status", fakeStep{}, fakeStep{data: []byte(`[]`)}, ErrResponse},
		{"not-found", fakeStep{}, fakeStep{data: []byte("HTTP/2.0 404 Not Found\r\n\r\n{}"), err: ErrRepository}, ErrRepository},
		{"redirect-status", fakeStep{}, fakeStep{data: []byte("HTTP/2.0 302 Found\r\n\r\n")}, ErrResponse},
		{"304-after-auth-failure", taggedStep(`"x"`, `[]`), fakeStep{data: []byte("HTTP/2.0 304 Not Modified\r\n\r\n"), err: ErrAuthentication}, ErrAuthentication},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := []fakeStep{tc.step}
			if tc.first.data != nil {
				steps = append([]fakeStep{tc.first}, steps...)
			}
			runner := &discoveryRunner{t: t, steps: steps}
			provider := NewProvider(runner)
			if tc.first.data != nil {
				if _, err := provider.DiscoverRepositories(t.Context(), []string{"example"}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := provider.DiscoverRepositories(t.Context(), []string{"example"})
			if got != nil || !errors.Is(err, tc.want) {
				t.Fatalf("repositories = %+v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestConditionalRequestForgetsETagAfterFailure(t *testing.T) {
	runner := &discoveryRunner{t: t, steps: []fakeStep{
		taggedStep(`"x"`, `[]`),
		{err: ErrRateLimited},
		includedStep(`[]`),
	}}
	provider := NewProvider(runner)
	_, _ = provider.DiscoverRepositories(t.Context(), []string{"example"})
	if _, err := provider.DiscoverRepositories(t.Context(), []string{"example"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if _, err := provider.DiscoverRepositories(t.Context(), []string{"example"}); err != nil || conditionalHeader(runner.calls[2].args) != "" {
		t.Fatalf("err = %v, calls = %+v", err, runner.calls)
	}
}

func TestParseIncludedResponseAcceptsGHHeaderFormats(t *testing.T) {
	for _, data := range []string{
		"HTTP/2.0 200 OK\nEtag: \"a\"\r\nX-Other: b\r\n\r\n[1]",
		"HTTP/1.1 200 OK\r\nETag: \"a\"\r\n\r\n[1]",
		"HTTP/2.0 200 OK\nEtag: \"a\"\n\n[1]",
	} {
		status, header, body, ok := parseIncludedResponse([]byte(data))
		if !ok || status != 200 || header.Get("Etag") != `"a"` || string(body) != "[1]" {
			t.Fatalf("parse(%q) = %d %v %q %v", data, status, header, body, ok)
		}
	}
	for _, data := range []string{"", "[1]", "HTTP/2.0 OK\r\n\r\n", "HTTP/2.0 999 Odd\r\n\r\n", "HTTP/2.0 200 OK\n bad\r\n\r\n"} {
		if _, _, _, ok := parseIncludedResponse([]byte(data)); ok {
			t.Fatalf("parse(%q) accepted", data)
		}
	}
	for _, etag := range []string{"", "a b", "\"a\"\n", string(make([]byte, maxETagLength+1))} {
		if validETag(etag) {
			t.Fatalf("validETag(%q) accepted", etag)
		}
	}
}
