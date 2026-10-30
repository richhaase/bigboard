package git

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const scanCacheFile = "bigboard-scan"
const scanCacheSchema = 1
const scanCountingVersion = 2

type scanCacheHeader struct {
	Schema  int
	Key     [sha256.Size]byte
	Records int
}

func (c *RepositoryCache) scanCached(ctx context.Context, repo Repository, options CollectOptions, stamp repoStamp) ([]CommitRecord, error) {
	identities, err := newIdentityResolver(options.Identities)
	if err != nil {
		return nil, err
	}
	query := c.scanQuery(stamp)
	ref := detectDefaultBranchWith(ctx, repo.Path, query)
	key, keyErr := scanFingerprint(ctx, repo, ref, options, query)
	if keyErr == nil {
		if records, err := readScanCache(repo.Path, key); err == nil {
			return records, ctx.Err()
		}
	}
	records, err := collectRepository(ctx, repo, ref, defaultPathFilter(options), newAIMatcher(options.AIIdentities), identities)
	if err != nil {
		return nil, err
	}
	if keyErr == nil && ctx.Err() == nil {
		if stamp, err := repositoryStamp(repo.Path); err == nil {
			query = c.scanQuery(stamp)
			current, err := scanFingerprint(ctx, repo, detectDefaultBranchWith(ctx, repo.Path, query), options, query)
			if err == nil && current == key {
				_ = writeScanCache(repo.Path, key, records)
			}
		}
	}
	return records, nil
}

func scanFingerprint(ctx context.Context, repo Repository, ref string, options CollectOptions, query gitRunner) ([sha256.Size]byte, error) {
	var key [sha256.Size]byte
	if !isCommitID(ref) {
		return key, fmt.Errorf("scan revision is unresolved")
	}
	if os.Getenv("GIT_ATTR_SOURCE") != "" {
		return key, fmt.Errorf("attribute source override requires a full scan")
	}
	h := sha256.New()
	add := func(value string) {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = io.WriteString(h, value)
	}
	encoded, err := json.Marshal(struct {
		Schema   int
		Counting int
		RepoID   string
		RepoName string
		Options  CollectOptions
	}{scanCacheSchema, scanCountingVersion, repo.ID, repo.Name, options})
	if err != nil {
		return key, err
	}
	add(string(encoded))
	add(ref)
	version, err := processQueries.run(ctx, repo.Path, "", "--version")
	if err != nil {
		return key, err
	}
	add(version)
	config, err := runGitContext(ctx, repo.Path, "config", "--null", "--list")
	if err != nil {
		return key, err
	}
	add(config)
	settings := make(map[string]string)
	for _, entry := range strings.Split(config, "\x00") {
		name, value, _ := strings.Cut(entry, "\n")
		settings[strings.ToLower(name)] = value
	}
	refs := []string{"HEAD"}
	if blob, ok := settings["mailmap.blob"]; ok && blob != "" {
		refs = append(refs, blob)
	}
	if tree, ok := settings["attr.tree"]; ok && tree != "" {
		refs = append(refs, tree)
	}
	for _, input := range refs {
		oid, err := query(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", input)
		if err != nil {
			return key, err
		}
		add(input)
		add(oid)
	}
	files := []string{filepath.Join(repo.Path, ".mailmap"), filepath.Join(repo.Path, "info", "attributes"), filepath.Join(repo.Path, "info", "grafts")}
	for _, name := range []string{"mailmap.file", "core.attributesfile", "diff.orderfile"} {
		if _, ok := settings[name]; !ok {
			continue
		}
		path, err := runGitContext(ctx, repo.Path, "config", "--null", "--path", "--get", name)
		if err != nil {
			return key, err
		}
		path = strings.TrimSuffix(path, "\x00")
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo.Path, path)
		}
		files = append(files, path)
	}
	for _, name := range []string{"GIT_ATTR_SYSTEM", "GIT_ATTR_GLOBAL"} {
		scope := ""
		if attributes, ok := settings["core.attributesfile"]; ok {
			scope = repo.Path + "\x00" + attributes
		}
		path, err := processQueries.run(ctx, repo.Path, scope, "var", name)
		if err != nil {
			return key, err
		}
		files = append(files, strings.TrimSuffix(path, "\n"))
	}
	add(os.Getenv("GIT_ATTR_NOSYSTEM"))
	for _, path := range files {
		add(path)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			add("missing")
			continue
		}
		if err != nil {
			return key, err
		}
		if !info.Mode().IsRegular() {
			return key, fmt.Errorf("scan input is not a regular file")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return key, err
		}
		add("present")
		add(string(contents))
	}
	objects := filepath.Join(repo.Path, "objects")
	err = filepath.WalkDir(objects, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		add(strings.TrimPrefix(path, objects))
		add(fmt.Sprintf("%d:%d:%d", info.Size(), info.ModTime().UnixNano(), info.Mode()))
		return nil
	})
	copy(key[:], h.Sum(nil))
	return key, err
}

func readScanCache(dir string, key [sha256.Size]byte) ([]CommitRecord, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(scanCacheFile)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("scan cache is unavailable")
	}
	file, err := root.Open(scanCacheFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < sha256.Size {
		return nil, fmt.Errorf("scan cache is invalid")
	}
	size := info.Size() - sha256.Size
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(file, size)); err != nil {
		return nil, err
	}
	var checksum [sha256.Size]byte
	if _, err := io.ReadFull(file, checksum[:]); err != nil {
		return nil, err
	}
	if string(h.Sum(nil)) != string(checksum[:]) {
		return nil, fmt.Errorf("scan cache checksum mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	decoder := gob.NewDecoder(io.LimitReader(file, size))
	var header scanCacheHeader
	if err := decoder.Decode(&header); err != nil {
		return nil, err
	}
	if header.Schema != scanCacheSchema || header.Key != key || header.Records < 0 || int64(header.Records) > size {
		return nil, fmt.Errorf("scan cache inputs changed")
	}
	var records []CommitRecord
	for range header.Records {
		var record CommitRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("scan cache contains extra data")
	}
	return records, nil
}

func writeScanCache(dir string, key [sha256.Size]byte, records []CommitRecord) error {
	file, err := os.CreateTemp(dir, ".bigboard-scan-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	defer func() { _ = file.Close() }()
	h := sha256.New()
	writer := bufio.NewWriter(file)
	encoder := gob.NewEncoder(io.MultiWriter(writer, h))
	if err := encoder.Encode(scanCacheHeader{Schema: scanCacheSchema, Key: key, Records: len(records)}); err != nil {
		return err
	}
	for i := range records {
		if err := encoder.Encode(records[i]); err != nil {
			return err
		}
	}
	if _, err := writer.Write(h.Sum(nil)); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, scanCacheFile))
}
