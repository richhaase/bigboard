package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func scanFixture(t *testing.T, dir string) []git.CommitRecord {
	t.Helper()
	records, err := git.ScanRepository(context.Background(), git.NewRepositories([]string{dir})[0], git.CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func TestBranchTagCollisionAndRemotePreference(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "a.go", "one\n", "first")
	gitOutput(t, dir, "tag", "main")
	writeAndCommit(t, dir, "a.go", "one\ntwo\n", "second")
	if got := scanFixture(t, dir); len(got) != 2 {
		t.Fatalf("tag hid branch history: %+v", got)
	}
	// Even callers explicitly supplying a short branch name get the branch.
	if got, err := git.CollectCommits(dir, "main"); err != nil || len(got) != 2 {
		t.Fatalf("short branch: %v %+v", err, got)
	}
	gitOutput(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitOutput(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	gitOutput(t, dir, "reset", "--hard", "HEAD~1")
	if got := scanFixture(t, dir); len(got) != 2 {
		t.Fatalf("stale local branch hid cached remote history: %+v", got)
	}
}

func TestUnusualPathsAndAmbientDiffSettings(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	writeAndCommit(t, dir, "old\tname.go", "one\ntwo\nthree\n", "source")
	if err := os.Mkdir(filepath.Join(dir, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, dir, "vendor/bad\tfile.go", "ignored\nignored\nignored\n", "vendor")
	writeAndCommit(t, dir, "vendor/literal => source.go", "ignored\n", "literal arrow")
	gitOutput(t, dir, "mv", "old\tname.go", "new\nname.go")
	writeAndCommit(t, dir, "new\nname.go", "one\ntwo\nthree\nfour\n", "rename")
	before := scanFixture(t, dir)
	total := 0
	for _, r := range before {
		total += r.Added
		if r.CommitID == "" {
			t.Fatal("missing commit ID")
		}
	}
	if total != 4 {
		t.Fatalf("got %d added lines, want 4", total)
	}
	for key, value := range map[string]string{"log.showRoot": "false", "log.showSignature": "true", "diff.renames": "false", "diff.algorithm": "histogram", "diff.indentHeuristic": "true", "diff.external": "false", "color.ui": "always"} {
		gitConfig(t, dir, key, value)
	}
	if after := scanFixture(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatalf("settings changed counts:\nbefore=%+v\nafter=%+v", before, after)
	}
	all, err := git.CollectRepository(git.NewRepositories([]string{dir})[0], git.DetectDefaultBranch(dir), git.CollectOptions{IncludeGenerated: true})
	if err != nil {
		t.Fatal(err)
	}
	total = 0
	for _, r := range all {
		total += r.Added
	}
	if total != 8 {
		t.Fatalf("all_files added=%d, want 8", total)
	}
}

func TestInheritedEnvironmentCannotRedirectCollection(t *testing.T) {
	requested, foreign := t.TempDir(), t.TempDir()
	makeTestRepo(t, requested)
	makeTestRepo(t, foreign)
	writeAndCommit(t, requested, "a.go", "one\n", "requested")
	writeAndCommit(t, foreign, "b.go", "one\ntwo\nthree\n", "foreign")
	want := scanFixture(t, requested)
	t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_WORK_TREE", foreign)
	t.Setenv("GIT_COMMON_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_SHALLOW_FILE", filepath.Join(foreign, "missing"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "log.showRoot")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	if got := scanFixture(t, requested); !reflect.DeepEqual(got, want) {
		t.Fatalf("environment redirected collection: %+v", got)
	}
}

func TestShallowCountsAndSeparateGitDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	clone := filepath.Join(root, "shallow")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	makeTestRepo(t, source)
	writeAndCommit(t, source, "a.go", "one\ntwo\nthree\n", "root")
	writeAndCommit(t, source, "a.go", "one\ntwo\nthree\nfour\n", "latest")
	gitOutput(t, root, "clone", "--depth=1", "file://"+source, clone)
	got := scanFixture(t, clone)
	if len(got) != 1 || !got[0].LinesUnknown || got[0].Added != 0 || got[0].Removed != 0 {
		t.Fatalf("invented shallow diff: %+v", got)
	}
	full := scanFixture(t, source)
	if full[0].CommitID != got[0].CommitID || full[0].LinesUnknown || full[0].Added != 1 {
		t.Fatalf("full history: %+v", full)
	}
	separate := filepath.Join(root, "separate")
	gitOutput(t, root, "init", "-b", "main", "--separate-git-dir="+filepath.Join(root, "metadata"), separate)
	if got := git.DiscoverReposDepth([]string{separate}, 1); len(got) != 1 {
		t.Fatalf("separate Git directory skipped: %v", got)
	}
}

func TestPartialCloneDoesNotFetchMissingObjects(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	clone := filepath.Join(root, "partial")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	makeTestRepo(t, source)
	gitConfig(t, source, "uploadpack.allowFilter", "true")
	writeAndCommit(t, source, "a.go", "one\n", "root")
	writeAndCommit(t, source, "a.go", "one\ntwo\n", "second")
	gitOutput(t, root, "clone", "--filter=blob:none", "--no-checkout", "file://"+source, clone)
	before := gitOutput(t, clone, "rev-list", "--objects", "--all", "--missing=print")
	if strings.Count(before, "?") != 2 {
		t.Fatalf("fixture needs two missing blobs: %s", before)
	}
	_, err := git.ScanRepository(context.Background(), git.NewRepositories([]string{clone})[0], git.CollectOptions{})
	if err == nil || !strings.Contains(err.Error(), "partial clone") {
		t.Fatalf("missing-data error: %v", err)
	}
	after := gitOutput(t, clone, "rev-list", "--objects", "--all", "--missing=print")
	if before != after {
		t.Fatal("scan fetched missing objects")
	}
}

func TestAICompanyEmployeesAreHumanUnlessConfigured(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	gitConfig(t, dir, "user.email", "employee@openai.com")
	writeAndCommit(t, dir, "a.go", "human\n", "human commit")
	if scanFixture(t, dir)[0].AIAssisted {
		t.Fatal("employee classified as AI")
	}
	got, err := git.CollectRepository(git.NewRepositories([]string{dir})[0], git.DetectDefaultBranch(dir), git.CollectOptions{AIIdentities: []string{"@openai.com"}})
	if err != nil || !got[0].AIAssisted {
		t.Fatalf("explicit override ignored: %v %+v", err, got)
	}
}

func TestCollectCommitSubjects(t *testing.T) {
	dir := t.TempDir()
	makeTestRepo(t, dir)
	subject := "Fix parsing | preserve\tcontrol\x1fbytes\x1b[31m"
	writeAndCommit(t, dir, "a.go", "one\n", subject+"\n\nDetails do not belong in the subject.\n\nCo-authored-by: Claude <noreply@anthropic.com>")
	gitOutput(t, dir, "commit", "--allow-empty", "--allow-empty-message", "-m", "")
	records := scanFixture(t, dir)
	if len(records) != 2 {
		t.Fatalf("expected two commits: %+v", records)
	}
	if records[0].Subject != "" || records[1].Subject != subject {
		t.Fatalf("subjects not preserved: empty=%q, actual=%q", records[0].Subject, records[1].Subject)
	}
	if !records[1].AIAssisted || records[1].Added != 1 || records[0].Added != 0 {
		t.Fatalf("subject changed metadata or line counts: %+v", records)
	}
}
