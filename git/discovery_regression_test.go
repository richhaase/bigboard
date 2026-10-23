package git_test

import (
	"fmt"
	"github.com/richhaase/bigboard/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectRepositoryPreservesCommitsWithUnusualTimezone(t *testing.T) {
	for _, offset := range []string{"+2500", "-2500"} {
		t.Run(offset, func(t *testing.T) {
			dir := t.TempDir()
			makeTestRepo(t, dir)
			run := func(input string, args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				cmd.Stdin = strings.NewReader(input)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			tree := run("", "mktree")
			raw := fmt.Sprintf("tree %s\nauthor Import <import@example.com> 1700000000 %s\ncommitter Import <import@example.com> 1700000000 +0000\n\nImported history\n", tree, offset)
			parent := run(raw, "hash-object", "-t", "commit", "-w", "--stdin")
			raw = fmt.Sprintf("tree %s\nparent %s\nauthor Human <human@example.com> 1700000100 +0000\ncommitter Human <human@example.com> 1700000100 +0000\n\nNormal child\n", tree, parent)
			head := run(raw, "hash-object", "-t", "commit", "-w", "--stdin")
			run("", "update-ref", "refs/heads/main", head)
			records, err := git.CollectRepository(git.NewRepositories([]string{dir})[0], "HEAD", git.CollectOptions{})
			if err != nil || len(records) != 2 {
				t.Fatalf("records=%+v err=%v", records, err)
			}
			if records[0].Date.Unix() != 1700000100 || records[1].Date.Unix() != 1700000000 {
				t.Fatalf("dates changed: %+v", records)
			}
		})
	}
}

func TestDiscoverReposPreservesSymlinkNameAndIdentity(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	parent := filepath.Join(root, "workspace")
	for _, dir := range []string{target, parent} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	makeTestRepo(t, target)
	link := filepath.Join(parent, "alias")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	direct := git.NewRepositories(git.DiscoverReposDepth([]string{link}, 1))
	nested := git.NewRepositories(git.DiscoverReposDepth([]string{parent}, 1))
	if len(direct) != 1 || len(nested) != 1 || direct[0] != nested[0] || nested[0].Name != "alias" || nested[0].ID != link {
		t.Fatalf("direct=%+v parent=%+v", direct, nested)
	}
	dedup := git.DiscoverReposDepth([]string{parent, target, link}, 1)
	if len(dedup) != 1 || dedup[0] != link {
		t.Fatalf("dedup=%v", dedup)
	}
}
