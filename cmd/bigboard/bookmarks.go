package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/richhaase/bigboard/tui"
)

func newBookmarkOptions(group string) *tui.BookmarkOptions {
	key, selection := "all", "All repositories"
	if group != "" {
		key, selection = "group:"+group, "Group: "+group
	}
	options := &tui.BookmarkOptions{Selection: selection}
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			options.Error = "Bookmark location unavailable: " + err.Error()
			return options
		}
		root = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(root) {
		options.Error = "XDG_STATE_HOME must be absolute; bookmarks are unavailable."
		return options
	}
	options.Path = filepath.Join(root, "bigboard", "last-seen.json")
	options.Save = func(at time.Time) error {
		return saveBookmark(options.Path, key, at)
	}
	bookmarks, err := readBookmarks(options.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		options.Error = "No bookmark file yet; no last-look markers this session."
	case err != nil:
		options.Error = "Cannot read bookmarks; no last-look markers this session: " + err.Error()
	default:
		options.Time = bookmarks[key]
		if options.Time.IsZero() {
			options.Error = "No bookmark for this selection yet; no last-look markers this session."
		}
	}
	return options
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
