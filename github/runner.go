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

// ExecRunner invokes only git and gh, using existing gh authentication. It never
// reads a token/config file, logs command output, starts a shell, or signs in.
// Output and execution time are bounded even when a command misbehaves.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	if command != "gh" && command != "git" {
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
		var missing *exec.Error
		if errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return out.Bytes(), classifyFailure(diagnostic.String())
	}
	return out.Bytes(), nil
}

// Inherited Git redirections cannot change the repository whose origin is read.
// Preserve gh's normal authentication environment, while pinning the host and
// disabling prompts, debug output, paging, updates, and repository inference.
func commandEnv(inherited []string) []string {
	env := make([]string, 0, len(inherited)+12)
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		switch key {
		case "GH_HOST", "GH_REPO", "GH_PROMPT_DISABLED", "GH_DEBUG", "DEBUG", "GH_PAGER", "PAGER",
			"GH_FORCE_TTY", "GH_COLOR_LABELS", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "GH_NO_UPDATE_NOTIFIER",
			"GH_NO_EXTENSION_UPDATE_NOTIFIER", "GH_SPINNER_DISABLED", "GH_TELEMETRY":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GH_HOST=github.com", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat",
		"GH_NO_UPDATE_NOTIFIER=1", "GH_NO_EXTENSION_UPDATE_NOTIFIER=1", "GH_SPINNER_DISABLED=1",
		"GH_TELEMETRY=0", "NO_COLOR=1", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
}

// Discard command diagnostics after classifying them: raw diagnostics can
// contain private local paths or unintended gh debug/authentication output.
func classifyFailure(diagnostic string) error {
	message := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(message, "rate limit"), strings.Contains(message, "rate_limit"), strings.Contains(message, "http 429"):
		return ErrRateLimited
	case strings.Contains(message, "authentication"), strings.Contains(message, "not logged"),
		strings.Contains(message, "gh auth login"), strings.Contains(message, "bad credentials"),
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

// Mask arbitrary injected-runner errors without losing cancellation or known
// actionable states. A remote or CLI error must never become terminal markup.
func safeRequestError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrUnavailable, ErrAuthentication,
		ErrRateLimited, ErrRepository, ErrOutputLimit, ErrBudgetExceeded, ErrResponse, ErrIncomplete} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrResponse
	}
	return ErrRequest
}
