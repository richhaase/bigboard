package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Every Git invocation uses the requested repository, independent of a hook's
// environment. Diff settings are explicit so user preferences do not alter totals.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	pinned := []string{"-c", "core.quotePath=false", "-c", "log.showRoot=true", "-c", "log.showSignature=false", "-c", "color.ui=false"}
	cmd := exec.CommandContext(ctx, "git", append(pinned, args...)...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_IMPLICIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_GRAFT_FILE", "GIT_SHALLOW_FILE",
			"GIT_REPLACE_REF_BASE", "GIT_NAMESPACE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_EXTERNAL_DIFF", "GIT_DIFF_OPTS", "GIT_NO_REPLACE_OBJECTS", "GIT_NO_LAZY_FETCH", "GIT_TERMINAL_PROMPT":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	return cmd
}

func partialClone(ctx context.Context, dir string) (bool, error) {
	out, err := runGitContext(ctx, dir, "config", "--null", "--list")
	if err != nil {
		return false, err
	}
	settings := make(map[string]string)
	for _, entry := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		settings[strings.ToLower(key)] = strings.ToLower(value)
	}
	for key, value := range settings {
		if key == "extensions.partialclone" || (strings.HasPrefix(key, "remote.") &&
			(strings.HasSuffix(key, ".partialclonefilter") || (strings.HasSuffix(key, ".promisor") && value != "false" && value != "no" && value != "off" && value != "0"))) {
			return true, nil
		}
	}
	return false, nil
}

func shallowCommits(ctx context.Context, dir string) (map[string]bool, error) {
	path, err := runGitContext(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "shallow")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(strings.TrimSpace(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read shallow boundaries: %w", err)
	}
	ids := make(map[string]bool)
	for _, id := range strings.Fields(string(data)) {
		ids[id] = true
	}
	return ids, nil
}

func splitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return 0, nil, nil
}

func parseGitLog(output string, repoName string) ([]CommitRecord, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	scanner.Split(splitNUL)
	return parseLog(scanner, Repository{ID: repoName, Name: repoName}, legacyPathFilter(), newAIMatcher(nil))
}

// Git terminates each metadata field and each numstat path with NUL. Rename
// records have an empty path followed by the literal old and new paths.
func parseLog(scanner *bufio.Scanner, repo Repository, filter pathFilter, ai aiMatcher) ([]CommitRecord, error) {
	read := func() (string, error) {
		if scanner.Scan() {
			return scanner.Text(), nil
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.ErrUnexpectedEOF
	}
	var records []CommitRecord
	for scanner.Scan() {
		token := strings.TrimLeft(scanner.Text(), "\n")
		if token == "" {
			continue
		}
		if !strings.Contains(token, "\t") {
			if !isCommitID(token) {
				return nil, fmt.Errorf("invalid commit ID %q", token)
			}
			var fields [5]string
			for i := range fields {
				value, err := read()
				if err != nil {
					return nil, err
				}
				fields[i] = value
			}
			date, err := time.Parse(time.RFC3339, fields[2])
			if err != nil {
				return nil, fmt.Errorf("invalid author date: %w", err)
			}
			records = append(records, CommitRecord{CommitID: token, Subject: fields[4], Author: strings.TrimSpace(fields[0]), Email: strings.TrimSpace(fields[1]), Date: date,
				RepoID: repo.ID, RepoName: repo.Name, AIAssisted: ai.isAI(fields[1]) || ai.isAICoAuthor(fields[3])})
			continue
		}
		fields := strings.SplitN(token, "\t", 3)
		if len(fields) != 3 || len(records) == 0 {
			return nil, fmt.Errorf("invalid numstat record %q", token)
		}
		path := fields[2]
		if path == "" {
			if _, err := read(); err != nil { // old path
				return nil, err
			}
			var err error
			path, err = read()
			if err != nil {
				return nil, err
			}
		}
		if fields[0] == "-" || fields[1] == "-" {
			continue // Binary files have no line measurement.
		}
		added, errA := strconv.Atoi(fields[0])
		removed, errR := strconv.Atoi(fields[1])
		if errA != nil || errR != nil || added < 0 || removed < 0 {
			return nil, fmt.Errorf("invalid line counts %q", token)
		}
		if filter.shouldCount(path) {
			records[len(records)-1].Added += added
			records[len(records)-1].Removed += removed
		}
	}
	return records, scanner.Err()
}

func isCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
