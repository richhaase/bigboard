package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type fixtureClient struct{ runner Runner }

func newTestProvider(runner Runner) *Provider {
	p := NewProvider(fixtureClient{runner})
	if runner != nil {
		p.runner = runner
	}
	return p
}

func (f fixtureClient) Do(ctx context.Context, request Request) (Response, error) {
	if f.runner == nil {
		return Response{}, ErrAuthentication
	}
	args := []string{"api", "--hostname", "github.com", "--method", request.Method, "--include"}
	if request.Endpoint == "graphql" {
		args = []string{"api", "graphql", "--hostname", "github.com", "--method", "POST", "--include"}
		var payload struct {
			Query     string
			Variables map[string]any
		}
		if err := json.Unmarshal(request.Body, &payload); err != nil {
			panic(err)
		}
		args = append(args, "-f", "query="+payload.Query)
		for _, key := range slices.Sorted(maps.Keys(payload.Variables)) {
			if value, ok := payload.Variables[key]; ok {
				flag := "-f"
				if key == "number" {
					flag = "-F"
				}
				args = append(args, flag, key+"="+fmt.Sprint(value))
			}
		}
	} else {
		if etag := request.Header.Get("If-None-Match"); etag != "" {
			args = append(args, "-H", "If-None-Match: "+etag)
		}
		args = append(args, request.Endpoint)
	}
	data, err := f.runner.Run(ctx, "gh", args...)
	status, header, body, ok := parseIncludedResponse(data)
	if !ok {
		status, body = 200, data
	}
	return Response{StatusCode: status, Header: http.Header(header), Body: body}, err
}

func parseIncludedResponse(data []byte) (int, textproto.MIMEHeader, []byte, bool) {
	head, body := data, []byte(nil)
	end, size := bytes.Index(data, []byte("\r\n\r\n")), 4
	if bare := bytes.Index(data, []byte("\n\n")); bare >= 0 && (end < 0 || bare < end) {
		end, size = bare, 2
	}
	if end >= 0 {
		head, body = data[:end], data[end+size:]
	}
	lines := strings.Split(strings.ReplaceAll(string(head), "\r\n", "\n"), "\n")
	fields := strings.Fields(lines[0])
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, nil, nil, false
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || status < 100 || status > 599 {
		return 0, nil, nil, false
	}
	header := make(textproto.MIMEHeader)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != key || key == "" {
			return 0, nil, nil, false
		}
		header.Add(key, strings.TrimSpace(value))
	}
	return status, header, body, true
}

type responseClient struct {
	t     *testing.T
	steps []fakeStep
	calls []Request
}

func (c *responseClient) Do(ctx context.Context, request Request) (Response, error) {
	c.t.Helper()
	assertDeadline(c.t, ctx)
	c.calls = append(c.calls, request)
	if len(c.steps) == 0 {
		c.t.Fatalf("unexpected request %s", request.Endpoint)
	}
	step := c.steps[0]
	c.steps = c.steps[1:]
	return Response{StatusCode: 200, Body: step.data}, step.err
}

func TestLegacyFixtureAdapterPreservesAdditionalVariables(t *testing.T) {
	called := false
	client := fixtureClient{runner: batchRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		called = true
		if value, ok := argValue(args, "futureVariable="); !ok || value != "kept" {
			t.Fatal("adapter dropped an unfamiliar variable")
		}
		return []byte(`{}`), nil
	})}
	_, err := client.Do(t.Context(), Request{Method: "POST", Endpoint: "graphql", Body: []byte(`{"query":"query Fixture { viewer { login } }","variables":{"futureVariable":"kept"}}`)})
	if err != nil || !called {
		t.Fatal("fixture request failed")
	}
}
