package github

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	if command != "git" {
		return nil, ErrRequest
	}
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = commandEnv(os.Environ())
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	out := &limitedBuffer{limit: MaxOutputBytes, cancel: cancel}
	diagnostic := &limitedBuffer{limit: 16 * 1024, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	err := cmd.Run()
	if out.exceeded || diagnostic.exceeded {
		return out.Bytes(), ErrOutputLimit
	}
	if ctx.Err() != nil {
		return out.Bytes(), ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if command == "git" && errors.As(err, &exit) && exit.ExitCode() == 1 && out.Len() == 0 && diagnostic.Len() == 0 {
			return nil, ErrUnsupportedOrigin
		}
		var missing *exec.Error
		if errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return out.Bytes(), classifyFailure(diagnostic.String())
	}
	return out.Bytes(), nil
}

func commandEnv(inherited []string) []string {
	env := make([]string, 0, len(inherited)+5)
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "GH_") || key == "GITHUB_TOKEN" || key == "DEBUG" || key == "PAGER" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "PAGER=cat", "NO_COLOR=1", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
}

func classifyFailure(diagnostic string) error {
	message := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(message, "rate limit"), strings.Contains(message, "rate_limit"), strings.Contains(message, "http 429"):
		return ErrRateLimited
	case strings.Contains(message, "authentication"), strings.Contains(message, "not logged"),
		strings.Contains(message, "bad credentials"),
		strings.Contains(message, "http 401"):
		return ErrAuthentication
	case strings.Contains(message, "could not resolve to a repository"), strings.Contains(message, "http 404"):
		return ErrRepository
	default:
		return ErrRequest
	}
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	available := b.limit - b.Len()
	if len(data) <= available {
		return b.Buffer.Write(data)
	}
	if available > 0 {
		_, _ = b.Buffer.Write(data[:available])
	}
	b.exceeded = true
	b.cancel()
	return available, ErrOutputLimit
}

func safeRequestError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrUnavailable, ErrAuthentication,
		ErrRateLimited, ErrPermission, ErrRepository, ErrOutputLimit, ErrBudgetExceeded, ErrResponse, ErrIncomplete} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrResponse
	}
	return ErrRequest
}
