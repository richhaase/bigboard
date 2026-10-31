package gui

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/richhaase/bigboard/git"
	gh "github.com/richhaase/bigboard/github"
)

type cachedAccess struct {
	AccountID    string   `json:"accountId"`
	Repositories []string `json:"repositories"`
}

func (s *Service) openAccessRoot(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(s.cachePath, 0700); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(s.cachePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("account cache must be an owned directory")
	}
	root, err := os.OpenRoot(s.cachePath)
	if err != nil {
		return nil, err
	}
	info, err = root.Lstat("access.json")
	if err != nil && !os.IsNotExist(err) {
		_ = root.Close()
		return nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		_ = root.Close()
		return nil, errors.New("cached repository access must be a regular file")
	}
	return root, nil
}

func (s *Service) restoreAccess() error {
	s.authorized = map[string]git.Repository{}
	root, err := s.openAccessRoot(false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open("access.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return errors.New("cached repository access could not be read")
	}
	if len(data) == 0 {
		return nil
	}
	var saved cachedAccess
	if json.Unmarshal(data, &saved) != nil || saved.AccountID != s.accountID || saved.Repositories == nil {
		return errors.New("cached repository access does not match this GitHub account")
	}
	for _, id := range saved.Repositories {
		canonical, err := gh.CanonicalRepo(id)
		if err != nil || canonical != id {
			return errors.New("cached repository access contains an invalid repository")
		}
		repo, err := s.cache.Repository(id, "")
		if err != nil {
			return err
		}
		s.authorized[id] = repo
	}
	return nil
}

func (s *Service) saveAccess(ctx context.Context, allowed map[string]git.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	data, err := json.Marshal(cachedAccess{AccountID: s.accountID, Repositories: ids})
	if err != nil {
		return err
	}
	root, err := s.openAccessRoot(true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := ".access-" + rand.Text() + ".tmp"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return root.Rename(name, "access.json")
}

func (s *Service) invalidateAccess() error {
	root, err := s.openAccessRoot(false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.OpenFile("access.json", os.O_WRONLY|os.O_TRUNC, 0600)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	err = file.Sync()
	return errors.Join(err, file.Close())
}
