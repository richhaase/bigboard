package main

import (
	"bytes"
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

func newIdentitySaver(path string, cfg *Config) func(map[string]git.Identity) error {
	expected := slices.Clone(cfg.source)
	target, targetErr := identityConfigTarget(path)
	var mu sync.Mutex
	return func(identities map[string]git.Identity) error {
		mu.Lock()
		defer mu.Unlock()
		if targetErr != nil {
			return fmt.Errorf("cannot save identities: %w", targetErr)
		}
		normalized, err := git.NormalizeIdentities(identities)
		if err != nil {
			return err
		}
		checkCurrent := func() (os.FileMode, error) {
			currentTarget, err := identityConfigTarget(path)
			if err != nil || currentTarget != target {
				return 0, fmt.Errorf("configuration path changed; restart BigBoard before merging identities")
			}
			data, err := os.ReadFile(target)
			if os.IsNotExist(err) && expected == nil {
				return 0o600, nil
			}
			if err != nil {
				return 0, fmt.Errorf("cannot read configuration: %w", err)
			}
			if expected == nil || !bytes.Equal(data, expected) {
				return 0, fmt.Errorf("configuration changed outside BigBoard; restart before merging identities")
			}
			info, err := os.Stat(target)
			if err != nil || !info.Mode().IsRegular() {
				return 0, fmt.Errorf("configuration is not a regular file")
			}
			if info.Mode().Perm()&0o222 == 0 {
				return 0, fmt.Errorf("configuration is read-only")
			}
			file, err := os.OpenFile(target, os.O_WRONLY, 0)
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
		if expected != nil {
			if err := json.Unmarshal(expected, &fields); err != nil || fields == nil {
				return fmt.Errorf("configuration must be a JSON object")
			}
		}
		fields["identities"], err = json.Marshal(normalized)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("cannot create configuration directory: %w", err)
		}
		temp, err := os.CreateTemp(filepath.Dir(target), ".bigboard-config-*")
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
		if expected == nil {
			err = os.Link(temp.Name(), target)
		} else {
			err = os.Rename(temp.Name(), target)
		}
		if err != nil {
			return fmt.Errorf("cannot save configuration: %w", err)
		}
		expected = slices.Clone(data)
		return nil
	}
}
