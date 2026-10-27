package git

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type gitRunner func(context.Context, string, ...string) (string, error)

type repoStamp [sha256.Size]byte

type repoQueries struct {
	stamp   repoStamp
	outputs map[string]string
}

type queryMemo struct {
	mu      sync.Mutex
	outputs map[string]string
}

var processQueries queryMemo

func repositoryStamp(path string) (repoStamp, error) {
	var stamp repoStamp
	h := sha256.New()
	add := func(value string) {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = io.WriteString(h, value)
	}
	objects := filepath.Join(path, "objects") + string(filepath.Separator)
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache contains a symlink: %s", name)
		}
		if filepath.Dir(name) == path && (entry.Name() == scanCacheFile || strings.HasPrefix(entry.Name(), ".bigboard-scan-")) {
			return nil
		}
		add(strings.TrimPrefix(name, path))
		add(entry.Type().String())
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		add(fmt.Sprintf("%d:%d:%d", info.Size(), info.ModTime().UnixNano(), info.Mode()))
		if strings.HasPrefix(name, objects) {
			return nil
		}
		contents, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		add(string(contents))
		return nil
	})
	copy(stamp[:], h.Sum(nil))
	return stamp, err
}

func (c *RepositoryCache) memoized(stamp repoStamp, tag string, run gitRunner) gitRunner {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		key := queryKey(tag, dir, args)
		c.mu.Lock()
		if entry := c.queries[dir]; entry != nil && entry.stamp == stamp {
			if output, ok := entry.outputs[key]; ok {
				c.mu.Unlock()
				return output, nil
			}
		}
		c.mu.Unlock()
		output, err := run(ctx, dir, args...)
		if err != nil {
			return output, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.queries == nil {
			c.queries = make(map[string]*repoQueries)
		}
		entry := c.queries[dir]
		if entry == nil || entry.stamp != stamp {
			entry = &repoQueries{stamp: stamp, outputs: make(map[string]string)}
			c.queries[dir] = entry
		}
		entry.outputs[key] = output
		return output, nil
	}
}

func (c *RepositoryCache) cacheQuery(stamp repoStamp) gitRunner {
	return c.memoized(stamp, "cache", c.git)
}

func (c *RepositoryCache) scanQuery(stamp repoStamp) gitRunner {
	return c.memoized(stamp, "scan", runGitContext)
}

func (c *RepositoryCache) forget(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.queries, path)
}

func (m *queryMemo) run(ctx context.Context, dir, scope string, args ...string) (string, error) {
	key := queryKey("process", scope, args)
	m.mu.Lock()
	if output, ok := m.outputs[key]; ok {
		m.mu.Unlock()
		return output, nil
	}
	m.mu.Unlock()
	output, err := runGitContext(ctx, dir, args...)
	if err != nil {
		return output, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outputs == nil {
		m.outputs = make(map[string]string)
	}
	m.outputs[key] = output
	return output, nil
}

func queryKey(tag, dir string, args []string) string {
	parts := append([]string{tag, dir}, os.Environ()...)
	parts = append(parts, "")
	parts = append(parts, args...)
	encoded := make([]string, len(parts))
	for i, part := range parts {
		encoded[i] = fmt.Sprintf("%d:%s", len(part), part)
	}
	return strings.Join(encoded, "")
}
