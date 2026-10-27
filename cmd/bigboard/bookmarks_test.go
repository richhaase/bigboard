package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBookmarkLocationAndFirstRunAreReadOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	options := newBookmarkOptions("")
	if options.Path != filepath.Join(dir, "state", "bigboard", "last-seen.json") || options.Selection != "All repositories" || !options.Time.IsZero() || options.Error == "" || options.Save == nil {
		t.Fatalf("unexpected first-run options: %+v", options)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatalf("loading a bookmark created state: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", dir)
	options = newBookmarkOptions("backend")
	if options.Path != filepath.Join(dir, ".local", "state", "bigboard", "last-seen.json") || options.Selection != "Group: backend" {
		t.Fatalf("unexpected fallback options: %+v", options)
	}
	t.Setenv("XDG_STATE_HOME", "relative/state")
	options = newBookmarkOptions("")
	if options.Save != nil || options.Error == "" || options.Path != "" {
		t.Fatal("relative state path was accepted")
	}
}

func TestBookmarkSavesPreserveSelectionsAndNeverMoveBackward(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	all, group := newBookmarkOptions(""), newBookmarkOptions("all")
	first := time.Date(2026, 10, 27, 10, 0, 0, 0, time.FixedZone("local", -6*3600))
	second := first.Add(time.Minute)
	if err := all.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := group.Save(second); err != nil {
		t.Fatal(err)
	}
	if err := all.Save(first.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := newBookmarkOptions(""); !got.Time.Equal(first) || got.Error != "" {
		t.Fatalf("full-view bookmark changed: %+v", got)
	}
	if got := newBookmarkOptions("all"); !got.Time.Equal(second) || got.Error != "" {
		t.Fatalf("group bookmark collided with full-view bookmark: %+v", got)
	}
	if got := newBookmarkOptions("another"); !got.Time.IsZero() || got.Error == "" {
		t.Fatal("new group inherited another selection's bookmark")
	}
	bookmarks, err := readBookmarks(all.Path)
	if err != nil || len(bookmarks) != 2 || !bookmarks["all"].Equal(first) || !bookmarks["group:all"].Equal(second) {
		t.Fatalf("wrong persisted timestamps: %v, %v", bookmarks, err)
	}
	info, err := os.Stat(all.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected state permissions: %v, %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Dir(all.Path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "last-seen.json" {
		t.Fatalf("extra persistent state files: %v, %v", entries, err)
	}
}

func TestBookmarkCorruptionIsNonfatalAndPreserved(t *testing.T) {
	for _, contents := range []string{"{", "null", `{"all":"not a date"}`, `{"all":null}`, `{"all":3}`} {
		t.Run(contents, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			options := newBookmarkOptions("")
			if err := os.MkdirAll(filepath.Dir(options.Path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(options.Path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			options = newBookmarkOptions("")
			if !options.Time.IsZero() || options.Error == "" {
				t.Fatal("corrupt state became a usable bookmark")
			}
			if err := options.Save(time.Now()); err == nil {
				t.Fatal("corrupt state was overwritten")
			}
			data, err := os.ReadFile(options.Path)
			if err != nil || string(data) != contents {
				t.Fatalf("corrupt state was not retained: %q, %v", data, err)
			}
		})
	}
}

func TestBookmarkConcurrentSaverCannotOverwriteState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	options := newBookmarkOptions("")
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.Open(filepath.Dir(options.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := options.Save(time.Now()); err == nil || !strings.Contains(err.Error(), "another session") {
		t.Fatalf("concurrent save ignored the lock: %v", err)
	}
	if _, err := os.Stat(options.Path); !os.IsNotExist(err) {
		t.Fatalf("contended save created state: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := options.Save(time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestBookmarkSymlinkIsNotReadOrReplaced(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	options := newBookmarkOptions("")
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, options.Path); err != nil {
		t.Fatal(err)
	}
	options = newBookmarkOptions("")
	if options.Error == "" || !options.Time.IsZero() || options.Save(time.Now()) == nil {
		t.Fatal("bookmark symlink was accepted")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "{}" {
		t.Fatal("symlink target changed")
	}
	if _, err := os.Readlink(options.Path); err != nil {
		t.Fatal("bookmark symlink was replaced")
	}
}
