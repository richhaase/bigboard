package git_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestCollectRetainedChanges(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "old\tname.go", strings.Repeat("source\n", 10), "source")
	gitOutput(t, dir, "mv", "old\tname.go", "new\nname.go")
	gitOutput(t, dir, "commit", "-m", "rename")
	if got := scanFixture(t, dir)[0].Changes; !reflect.DeepEqual(got, []git.PathChange{{Path: "new\nname.go", PreviousPath: "old\tname.go"}}) {
		t.Fatalf("rename paths: %+v", got)
	}
	// Copy detection uses an independently modified source; retaining the old
	// path must not manufacture a second source change for the copied file.
	if err := os.WriteFile(filepath.Join(dir, "copy\tname.go"), []byte(strings.Repeat("source\n", 10)), 0644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "add", "copy\tname.go")
	writeAndCommit(t, dir, "new\nname.go", strings.Repeat("source\n", 10)+"modified\n", "copy")
	copied := scanFixture(t, dir)[0]
	want := []git.PathChange{{Path: "copy\tname.go", PreviousPath: "new\nname.go"}, {Path: "new\nname.go"}}
	if !reflect.DeepEqual(copied.Changes, want) || copied.Added != 1 {
		t.Fatalf("copy paths/counts: %+v", copied)
	}
	if err := os.Mkdir(filepath.Join(dir, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, dir, "vendor/binary\nfile.bin", "\x00\x01\x02", "binary generated")
	binary := scanFixture(t, dir)[0]
	if !reflect.DeepEqual(binary.Changes, []git.PathChange{{Path: "vendor/binary\nfile.bin", Generated: true}}) || binary.Added != 0 || binary.Removed != 0 {
		t.Fatalf("binary/generated paths: %+v", binary)
	}
	gitOutput(t, dir, "commit", "--allow-empty", "-m", "empty")
	empty := scanFixture(t, dir)[0]
	if len(empty.Changes) != 0 || empty.PathsUnknown {
		t.Fatalf("empty commit: %+v", empty)
	}
}

func TestShallowRetainedPathsUnknown(t *testing.T) {
	root := t.TempDir()
	source, clone := filepath.Join(root, "source"), filepath.Join(root, "shallow")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	makeTestRepo(t, source)
	writeAndCommit(t, source, "original.go", "one\n", "root")
	writeAndCommit(t, source, "added.go", "two\n", "boundary")
	gitOutput(t, source, "commit", "--allow-empty", "-m", "known empty")
	gitOutput(t, root, "clone", "--depth=2", "file://"+source, clone)
	records := scanFixture(t, clone)
	if len(records) != 2 {
		t.Fatalf("records: %+v", records)
	}
	if records[0].PathsUnknown || len(records[0].Changes) != 0 {
		t.Fatalf("empty diff must stay known: %+v", records[0])
	}
	if !records[1].PathsUnknown || !records[1].LinesUnknown || len(records[1].Changes) != 0 {
		t.Fatalf("synthetic root paths were retained: %+v", records[1])
	}
	full := scanFixture(t, source)
	if full[1].PathsUnknown || !reflect.DeepEqual(full[1].Changes, []git.PathChange{{Path: "added.go"}}) {
		t.Fatalf("full boundary diff: %+v", full[1])
	}
}
