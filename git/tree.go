package git

import (
	"context"
	"errors"
	"strings"
)

func PathObjectType(ctx context.Context, repo Repository, revision, path string) (string, error) {
	if !isCommitID(revision) {
		return "", errors.New("scanned revision unavailable")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, '\x00') {
			return "", errors.New("literal repository path unavailable")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	partial, err := partialClone(ctx, repo.Path)
	if err != nil || partial {
		return "", errors.New("complete local repository unavailable")
	}
	kind, err := runGitContext(ctx, repo.Path, "cat-file", "-t", revision+"^{commit}:"+path)
	if err != nil {
		return "", errors.New("path is absent or unavailable at the scanned revision")
	}
	return strings.TrimSpace(kind), nil
}
