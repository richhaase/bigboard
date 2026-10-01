//go:build !windows

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
	"github.com/richhaase/bigboard/tui"
)

// Re-enter the test executable to exercise Bubble Tea's real signal handler
// without installing or sending signals in the parent test process.
func TestShutdownHelperProcess(t *testing.T) {
	repo := os.Getenv("BIGBOARD_SHUTDOWN_REPO")
	if repo == "" {
		return
	}
	model := tui.NewModelWithOptions(git.NewRepositories([]string{repo}), stats.SortByCommits, nil, "test", tui.DefaultTimeIndex, tui.Options{})
	if err := runTUI(model, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer()); err != nil {
		t.Fatal(err)
	}
}

func TestSIGTERMReapsGitAndGitHubCommands(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git", "gh"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			repo := filepath.Join(dir, "repo")
			for _, args := range [][]string{{"init", "-b", "main", repo}, {"-C", repo, "config", "remote.origin.url", "https://github.com/org/repo.git"}, {"-C", repo, "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"}} {
				if output, err := exec.Command(realGit, args...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, output)
				}
			}
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0755); err != nil {
				t.Fatal(err)
			}
			pidfile := filepath.Join(dir, "child.pid")
			// exec leaves one process with inherited pipes, like the slow Git/gh
			// operation being modeled; it never contacts GitHub.
			block := "echo $$ > \"$BIGBOARD_CHILD_PID\"\nexec sleep 60\n"
			script := "#!/bin/sh\n" + block
			if command == "git" {
				script = "#!/bin/sh\nfor arg in \"$@\"; do\n if [ \"$arg\" = log ]; then\n" + block + " fi\ndone\nexec \"$BIGBOARD_REAL_GIT\" \"$@\"\n"
			}
			if err := os.WriteFile(filepath.Join(bin, command), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestShutdownHelperProcess$")
			cmd.Env = append(os.Environ(), "BIGBOARD_SHUTDOWN_REPO="+repo, "BIGBOARD_CHILD_PID="+pidfile, "BIGBOARD_REAL_GIT="+realGit, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			var pid int
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				data, err := os.ReadFile(pidfile)
				if err == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					if pid > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("background command did not start")
			}
			defer func() {
				if pid > 0 {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("signal shutdown: %v", err)
			}
			if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
				t.Fatalf("%s subprocess %d was not reaped: %v", command, pid, err)
			}
			pid = 0
		})
	}
}
