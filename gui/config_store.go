package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/richhaase/bigboard/git"
)

func identityConfigTarget(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil || path == "" {
		return "", fmt.Errorf("configuration path is unavailable")
	}
	target, err := filepath.EvalSymlinks(abs)
	if os.IsNotExist(err) {
		parent, err := identityConfigTarget(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	return target, err
}

type configStore struct {
	mu        sync.Mutex
	path      string
	target    string
	targetErr error
	expected  []byte
}

func newConfigStore(path string, source []byte) *configStore {
	target, err := identityConfigTarget(path)
	return &configStore{path: path, target: target, targetErr: err, expected: slices.Clone(source)}
}

func (s *configStore) saveIdentities(identities map[string]git.Identity) error {
	normalized, err := git.NormalizeIdentities(identities)
	if err != nil {
		return err
	}
	return s.update(context.Background(), map[string]any{"identities": normalized})
}

func (s *configStore) update(ctx context.Context, values map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.targetErr != nil {
		return fmt.Errorf("cannot save configuration: %w", s.targetErr)
	}
	checkCurrent := func() (os.FileMode, error) {
		currentTarget, err := identityConfigTarget(s.path)
		if err != nil || currentTarget != s.target {
			return 0, fmt.Errorf("configuration path changed; restart BigBoard before saving")
		}
		data, err := os.ReadFile(s.target)
		if os.IsNotExist(err) && s.expected == nil {
			return 0o600, nil
		}
		if err != nil {
			return 0, fmt.Errorf("cannot read configuration: %w", err)
		}
		if s.expected == nil || !bytes.Equal(data, s.expected) {
			return 0, fmt.Errorf("configuration changed outside BigBoard; restart before saving")
		}
		info, err := os.Stat(s.target)
		if err != nil || !info.Mode().IsRegular() {
			return 0, fmt.Errorf("configuration is not a regular file")
		}
		if info.Mode().Perm()&0o222 == 0 {
			return 0, fmt.Errorf("configuration is read-only")
		}
		file, err := os.OpenFile(s.target, os.O_WRONLY, 0)
		if err != nil {
			return 0, fmt.Errorf("configuration is not writable: %w", err)
		}
		if err := file.Close(); err != nil {
			return 0, err
		}
		return info.Mode().Perm(), nil
	}
	mode, err := checkCurrent()
	if err != nil {
		return err
	}
	fields := make(map[string]json.RawMessage)
	if s.expected != nil {
		if err := json.Unmarshal(s.expected, &fields); err != nil || fields == nil {
			return fmt.Errorf("configuration must be a JSON object")
		}
	}
	for key, value := range values {
		fields[key], err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.target), 0o700); err != nil {
		return fmt.Errorf("cannot create configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.target), ".bigboard-config-*")
	if err != nil {
		return fmt.Errorf("cannot prepare configuration update: %w", err)
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if _, err := checkCurrent(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.expected == nil {
		err = os.Link(temp.Name(), s.target)
	} else {
		err = os.Rename(temp.Name(), s.target)
	}
	if err != nil {
		return fmt.Errorf("cannot save configuration: %w", err)
	}
	s.expected = slices.Clone(data)
	return nil
}
