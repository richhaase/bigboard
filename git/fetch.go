package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func FetchRepository(ctx context.Context, repo Repository) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	remotes, err := runGitContext(ctx, repo.Path, "remote")
	if err != nil {
		return err
	}
	if !strings.Contains("\n"+strings.TrimSpace(remotes)+"\n", "\norigin\n") {
		return nil
	}
	sshCommand := os.Getenv("GIT_SSH_COMMAND")
	if sshCommand == "" {
		sshCommand, _ = runGitContext(ctx, repo.Path, "config", "--get", "core.sshCommand")
		sshCommand = strings.TrimSpace(sshCommand)
	}
	if sshCommand == "" {
		if ssh := os.Getenv("GIT_SSH"); ssh != "" {
			sshCommand = "'" + strings.ReplaceAll(ssh, "'", "'\\''") + "'"
		} else {
			sshCommand = "ssh"
		}
	}
	cmd := gitCommand(ctx, repo.Path, "-c", "credential.interactive=false", "-c", "fetch.pruneTags=false", "-c", "remote.origin.pruneTags=false", "fetch", "--quiet", "--no-tags", "--prune", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "origin", "+refs/heads/*:refs/remotes/origin/*")
	cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+sshCommand+" -o BatchMode=yes", "GIT_SSH_VARIANT=ssh", "GIT_ASKPASS=", "SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=never")
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("fetch failed: %w", ctx.Err())
		}
		return fmt.Errorf("fetch failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
