package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func watchScanLogs(t *testing.T) func() int {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	program := fmt.Sprintf("#!/bin/sh\nfor arg do\nif [ \"$arg\" = log ]; then printf 'log\\n' >> %s; fi\ndone\nexec %s \"$@\"\n", quote(logPath), quote(gitPath))
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		contents, err := os.ReadFile(logPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return bytes.Count(contents, []byte("log\n"))
	}
}

func checkCachedScan(t *testing.T, cache *RepositoryCache, repo Repository, options CollectOptions, logs func() int, wantLogs int) []CommitRecord {
	t.Helper()
	before := logs()
	got, err := cache.ScanRepository(t.Context(), repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if count := logs() - before; count != wantLogs {
		t.Fatalf("ran git log %d times, want %d", count, wantLogs)
	}
	want, err := ScanRepository(t.Context(), repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cached/full scan mismatch:\ngot: %#v\nwant: %#v", got, want)
	}
	return got
}

func TestScanCacheRecordParityAcrossLaunches(t *testing.T) {
	cache, repo, source := cacheFixture(t)
	for path, contents := range map[string]string{
		"old\tname.go":        "one\ntwo\nthree\n",
		"binary.bin":          "\x00\x01\x02",
		"odd-name.go":         "literal bytes\n",
		"vendor/generated.go": "generated\n",
		".mailmap":            "Mapped Contributor <mapped@example.invalid> <example@example.invalid>\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, path), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cacheFixtureGit(t, source, "add", ".")
	cacheFixtureGit(t, source, "commit", "--quiet", "-m", "Paths and authors\n\nCo-authored-by: Codex <noreply@openai.com>")
	cacheFixtureGit(t, source, "mv", "old\tname.go", "new\nname.go")
	cacheFixtureGit(t, source, "commit", "--quiet", "-m", "Rename path")
	cacheFixtureGit(t, source, "commit", "--quiet", "--allow-empty", "-m", "Empty commit")
	if err := cache.Refresh(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	logs := watchScanLogs(t)
	options := CollectOptions{Identities: map[string]Identity{"canonical@example.invalid": {Aliases: []string{"mapped@example.invalid"}, Name: "Canonical Contributor"}}}
	first := checkCachedScan(t, cache, repo, options, logs, 1)
	if len(first) != 5 || first[2].Author != "Canonical Contributor" || !first[2].AIAssisted || len(first[0].Changes) != 0 || first[1].Changes[0].PreviousPath != "old\tname.go" {
		t.Fatalf("fixture lost identity, AI, rename, or empty evidence: %+v", first)
	}
	checkCachedScan(t, &RepositoryCache{Root: cache.Root}, repo, options, logs, 0)
	info, err := os.Stat(filepath.Join(repo.Path, scanCacheFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private cache permissions: %v, %v", info, err)
	}
	if err := cache.Remove(repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, scanCacheFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository removal retained parsed scan cache: %v", err)
	}
}

func TestScanCacheInvalidatesInputs(t *testing.T) {
	for _, kind := range []string{"revision", "generated", "ai", "identity", "name", "config", "global-config", "mailmap-file", "mailmap-blob", "head-mailmap", "attributes", "global-attributes"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, source := cacheFixture(t)
			if err := cache.Refresh(t.Context(), repo); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "input")
			if err := os.WriteFile(external, nil, 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "global-config":
				t.Setenv("GIT_CONFIG_GLOBAL", external)
			case "mailmap-file":
				cacheFixtureGit(t, repo.Path, "config", "mailmap.file", external)
			case "mailmap-blob":
				blob := cacheFixtureGit(t, repo.Path, "hash-object", "-w", external)
				cacheFixtureGit(t, repo.Path, "update-ref", "refs/mailmap", blob)
				cacheFixtureGit(t, repo.Path, "config", "mailmap.blob", "refs/mailmap")
			case "global-attributes":
				cacheFixtureGit(t, repo.Path, "config", "core.attributesFile", external)
			}
			options := CollectOptions{}
			logs := watchScanLogs(t)
			checkCachedScan(t, cache, repo, options, logs, 1)
			checkCachedScan(t, cache, repo, options, logs, 0)
			switch kind {
			case "revision":
				cacheFixtureCommit(t, source, "third\n")
				if err := cache.Refresh(t.Context(), repo); err != nil {
					t.Fatal(err)
				}
			case "generated":
				options.IncludeGenerated = true
			case "ai":
				options.AIIdentities = []string{"example@example.invalid"}
			case "identity":
				options.Identities = map[string]Identity{"canonical@example.invalid": {Aliases: []string{"example@example.invalid"}, Name: "New Name"}}
			case "name":
				repo.Name = "Renamed display"
			case "config":
				cacheFixtureGit(t, repo.Path, "config", "i18n.logOutputEncoding", "UTF-8")
			case "global-config":
				if err := os.WriteFile(external, []byte("[i18n]\nlogOutputEncoding = UTF-8\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "mailmap-file", "mailmap-blob":
				if err := os.WriteFile(external, []byte("Mapped Contributor <mapped@example.invalid> <example@example.invalid>\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "mailmap-blob" {
					blob := cacheFixtureGit(t, repo.Path, "hash-object", "-w", external)
					cacheFixtureGit(t, repo.Path, "update-ref", "refs/mailmap", blob)
				}
			case "head-mailmap":
				if err := os.WriteFile(filepath.Join(source, ".mailmap"), []byte("HEAD Contributor <head@example.invalid> <example@example.invalid>\n"), 0600); err != nil {
					t.Fatal(err)
				}
				cacheFixtureGit(t, source, "add", ".mailmap")
				cacheFixtureGit(t, source, "commit", "--quiet", "-m", "Changed HEAD mailmap")
				cacheFixtureGit(t, repo.Path, "fetch", "--quiet", "--no-tags", source, "HEAD:refs/heads/mailmap")
				cacheFixtureGit(t, repo.Path, "symbolic-ref", "HEAD", "refs/heads/mailmap")
			case "attributes":
				if err := os.MkdirAll(filepath.Join(repo.Path, "info"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo.Path, "info", "attributes"), []byte("*.txt binary\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "global-attributes":
				if err := os.WriteFile(external, []byte("*.txt binary\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := checkCachedScan(t, cache, repo, options, logs, 1)
			switch kind {
			case "mailmap-file", "mailmap-blob":
				if got[0].Email != "mapped@example.invalid" {
					t.Fatalf("changed mailmap not applied: %+v", got[0])
				}
			case "head-mailmap":
				if got[0].Email != "head@example.invalid" || len(got) != 2 {
					t.Fatalf("HEAD mailmap and scanned branch confused: %+v", got)
				}
			case "attributes", "global-attributes":
				if got[0].Added != 0 || !got[0].Changes[0].Binary {
					t.Fatalf("changed attributes not applied: %+v", got[0])
				}
			}
			checkCachedScan(t, cache, repo, options, logs, 0)
		})
	}
}

func TestScanCacheFailuresFallBack(t *testing.T) {
	for _, kind := range []string{"truncated", "checksum", "schema", "key", "malformed", "write-failure"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, _ := cacheFixture(t)
			if err := cache.Refresh(t.Context(), repo); err != nil {
				t.Fatal(err)
			}
			logs := watchScanLogs(t)
			checkCachedScan(t, cache, repo, CollectOptions{}, logs, 1)
			path := filepath.Join(repo.Path, scanCacheFile)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "truncated":
				contents = contents[:len(contents)/2]
			case "checksum":
				contents[len(contents)/2] ^= 0xff
			case "schema", "key":
				var header scanCacheHeader
				if err := gob.NewDecoder(bytes.NewReader(contents)).Decode(&header); err != nil {
					t.Fatal(err)
				}
				if kind == "schema" {
					header.Schema++
				} else {
					header.Key[0] ^= 0xff
				}
				var data bytes.Buffer
				encoder := gob.NewEncoder(&data)
				if err := encoder.Encode(header); err != nil {
					t.Fatal(err)
				}
				if err := encoder.Encode([]CommitRecord{{Subject: "Invalid cached record"}}); err != nil {
					t.Fatal(err)
				}
				checksum := sha256.Sum256(data.Bytes())
				contents = append(data.Bytes(), checksum[:]...)
			case "malformed":
				contents = []byte("invalid encoding")
				checksum := sha256.Sum256(contents)
				contents = append(contents, checksum[:]...)
			case "write-failure":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "write-failure" {
				if err := os.WriteFile(path, contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			checkCachedScan(t, cache, repo, CollectOptions{}, logs, 1)
			wantLogs := 0
			if kind == "write-failure" {
				wantLogs = 1
			}
			checkCachedScan(t, cache, repo, CollectOptions{}, logs, wantLogs)
			leftovers, err := filepath.Glob(filepath.Join(repo.Path, ".bigboard-scan-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary cache files left behind: %v, %v", leftovers, err)
			}
		})
	}
}

func TestScanCacheDoesNotHideMissingHistoryOrBypassOwnership(t *testing.T) {
	for _, kind := range []string{"objects", "owner", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			cache, repo, _ := cacheFixture(t)
			if err := cache.Refresh(t.Context(), repo); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.ScanRepository(t.Context(), repo, CollectOptions{}); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch kind {
			case "objects":
				if err := os.RemoveAll(filepath.Join(repo.Path, "objects")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(repo.Path, "objects"), 0700); err != nil {
					t.Fatal(err)
				}
			case "owner":
				if err := os.Remove(filepath.Join(repo.Path, "bigboard-cache")); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := cache.ScanRepository(ctx, repo, CollectOptions{}); err == nil {
				t.Fatalf("cached records bypassed %s failure", kind)
			}
		})
	}
}

func TestScanCachePreservesLiteralBytesAndUnknownEvidence(t *testing.T) {
	for _, records := range [][]CommitRecord{nil, {{
		CommitID: strings.Repeat("1", 40), Subject: "subject-\xff", Author: "author-\xff", Email: "author@example.invalid",
		Date: time.Unix(123456789, 0).UTC(), RepoID: "example/project", RepoName: "example/project",
		AIAssisted: true, Added: 7, Removed: 3, LinesUnknown: true, PathsUnknown: true,
		Changes: []PathChange{{Path: "new\n\xff", PreviousPath: "old\t\xfe", Added: 7, Removed: 3, LinesKnown: true, Binary: true, Generated: true}},
	}}} {
		dir := t.TempDir()
		key := sha256.Sum256([]byte("inputs"))
		if err := writeScanCache(dir, key, records); err != nil {
			t.Fatal(err)
		}
		got, err := readScanCache(dir, key)
		if err != nil || !reflect.DeepEqual(got, records) {
			t.Fatalf("round trip lost records: %+v, %v", got, err)
		}
	}
}

func TestScanCacheOptionalMaintenanceCancellationKeepsScan(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	program := fmt.Sprintf("#!/bin/sh\nfor arg do\nif [ \"$arg\" = --version ]; then\nif [ -f %s ]; then : > %s; exec sleep 30; fi\n: > %s\nfi\ndone\nexec %s \"$@\"\n", quote(first), quote(second), quote(first), quote(gitPath))
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(second); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	records, err := cache.ScanRepository(ctx, repo, CollectOptions{})
	if err != nil || len(records) != 2 || ctx.Err() == nil {
		t.Fatalf("cache maintenance discarded successful scan: %d records, %v, context %v", len(records), err, ctx.Err())
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("cancellation missed optional maintenance: %v", err)
	}
}

func TestScanCacheNonregularInputSkipsFingerprint(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mailmap")
	if err := exec.Command("mkfifo", path).Run(); err != nil {
		t.Skipf("named pipe fixture unavailable: %v", err)
	}
	cacheFixtureGit(t, repo.Path, "config", "mailmap.file", path)
	ref := detectDefaultBranch(t.Context(), repo.Path)
	if _, err := scanFingerprint(t.Context(), repo, ref, CollectOptions{}); err == nil {
		t.Fatal("nonregular identity input accepted for cache fingerprinting")
	}
}

func TestScanCacheAttributeSourceOverrideUsesFullScan(t *testing.T) {
	cache, repo, _ := cacheFixture(t)
	if err := cache.Refresh(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	logs := watchScanLogs(t)
	checkCachedScan(t, cache, repo, CollectOptions{}, logs, 1)
	before, err := os.Stat(filepath.Join(repo.Path, scanCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ATTR_SOURCE", "HEAD")
	checkCachedScan(t, cache, repo, CollectOptions{}, logs, 1)
	checkCachedScan(t, cache, repo, CollectOptions{}, logs, 1)
	after, err := os.Stat(filepath.Join(repo.Path, scanCacheFile))
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("attribute override published a parsed cache: %v, %v", after, err)
	}
}
