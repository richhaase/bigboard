package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRepositoryScanCache(b *testing.B) {
	cache := &RepositoryCache{Root: b.TempDir()}
	repo, err := cache.Repository("example/benchmark", "main")
	if err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(repo.Path, 0700); err != nil {
		b.Fatal(err)
	}
	run := func(input []byte, args ...string) {
		b.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + os.DevNull}, args...)...)
		cmd.Dir = repo.Path
		cmd.Stdin = bytes.NewReader(input)
		if output, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("fixture git %v: %v: %s", args, err, output)
		}
	}
	run(nil, "init", "--quiet", "--bare", "--template=", "--initial-branch=main")
	run(nil, "config", "remote.origin.url", "https://github.com/example/benchmark.git")
	if err := os.WriteFile(filepath.Join(repo.Path, "bigboard-cache"), []byte(repo.ID+"\n"), 0600); err != nil {
		b.Fatal(err)
	}
	var input strings.Builder
	for commit := range 2000 {
		fmt.Fprintf(&input, "commit refs/heads/main\ncommitter Example Contributor <contributor@example.invalid> %d +0000\ndata 9\nSynthetic\n", 1700000000+commit)
		for file := range 12 {
			data := fmt.Sprintf("revision %d\n", commit) + strings.Repeat("retained line\n", 20)
			fmt.Fprintf(&input, "M 100644 inline dir%d/file.txt\ndata %d\n%s", file, len(data), data)
		}
		input.WriteByte('\n')
	}
	input.WriteString("done\n")
	run([]byte(input.String()), "fast-import", "--quiet")
	run(nil, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/heads/main")
	if records, err := cache.ScanRepository(b.Context(), repo, CollectOptions{}); err != nil || len(records) != 2000 {
		b.Fatalf("fixture scan: %d records, %v", len(records), err)
	}
	for _, mode := range []string{"full", "cold", "cached"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var records []CommitRecord
				var err error
				if mode == "full" {
					if _, err = cache.verify(b.Context(), repo); err == nil {
						records, err = ScanRepository(b.Context(), repo, CollectOptions{})
					}
				} else {
					if mode == "cold" {
						if err := os.Remove(filepath.Join(repo.Path, scanCacheFile)); err != nil {
							b.Fatal(err)
						}
					}
					records, err = cache.ScanRepository(b.Context(), repo, CollectOptions{})
				}
				if err != nil || len(records) != 2000 {
					b.Fatalf("scan: %d records, %v", len(records), err)
				}
			}
		})
	}
}
