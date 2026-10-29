package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func timestamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format(time.RFC3339Nano)
}

func (s *Service) bookmarkKey() string {
	if s.group != "" {
		return "group:" + s.group
	}
	return "all"
}

func (s *Service) loadBookmarks() {
	s.eligible = make(map[string]time.Time)
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			s.bookmarkError = err.Error()
			return
		}
		root = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(root) {
		s.bookmarkError = "XDG_STATE_HOME must be absolute; bookmarks unavailable"
		return
	}
	s.bookmarkPath = filepath.Join(root, "bigboard", "last-seen.json")
	bookmarks, err := readBookmarks(s.bookmarkPath)
	if os.IsNotExist(err) {
		s.bookmarkError = "No saved bookmark yet"
	} else if err != nil {
		s.bookmarkError = "Cannot read bookmarks: " + err.Error()
	}
	s.bookmarks = bookmarks
}

func (s *Service) SaveBookmark() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bookmarkPath == "" {
		return nil
	}
	for key, at := range s.eligible {
		if err := saveBookmark(s.bookmarkPath, key, at); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) decorateLocked(snapshot *Snapshot) {
	snapshot.Preferences = Preferences{Since: s.analysis.Since, Sort: s.analysis.Sort}
	if snapshot.Preferences.Since == "" {
		snapshot.Preferences.Since = "14d"
	}
	if snapshot.Preferences.Sort == "" {
		snapshot.Preferences.Sort = "total"
	}
	snapshot.Groups = s.analysis.Groups
	snapshot.WorkAreas = s.analysis.WorkAreas
	snapshot.ActiveGroup = s.group
	seen := s.bookmarks[s.bookmarkKey()]
	snapshot.Bookmark = Bookmark{SeenAt: timestamp(seen), EligibleAt: timestamp(s.eligible[s.bookmarkKey()]), Message: s.bookmarkError, Path: s.bookmarkPath}
	if seen.IsZero() && snapshot.Bookmark.Message == "" {
		snapshot.Bookmark.Message = "No saved bookmark for this selection yet"
	}
}

func readBookmarks(path string) (map[string]time.Time, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bookmark state is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bookmarks map[string]time.Time
	if err := json.Unmarshal(data, &bookmarks); err != nil || bookmarks == nil {
		return nil, fmt.Errorf("bookmark state must be a JSON object of timestamps")
	}
	for key, at := range bookmarks {
		if key == "" || at.IsZero() {
			return nil, fmt.Errorf("bookmark state contains an invalid selection or timestamp")
		}
	}
	return bookmarks, nil
}

func saveBookmark(path, key string, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another session is saving bookmarks: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	bookmarks, err := readBookmarks(path)
	if errors.Is(err, os.ErrNotExist) {
		bookmarks = make(map[string]time.Time)
	} else if err != nil {
		return fmt.Errorf("existing bookmark state was left unchanged: %w", err)
	}
	if !at.After(bookmarks[key]) {
		return nil
	}
	bookmarks[key] = at.UTC()
	data, err := json.MarshalIndent(bookmarks, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".last-seen-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(append(data, '\n')); err != nil {
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
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return lock.Sync()
}
