import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, realpath, rm, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { test, type TestContext } from "node:test";
import { collectGitHistory, describeRepository, githubRepository, importNativeSources } from "../src/sources/git";

const exec = promisify(execFile);
async function fixture(t: TestContext, root?: string) {
  const directory = root ?? await mkdtemp(join(tmpdir(), "bigboard-git-"));
  if (root) await mkdir(directory, { recursive: true });
  else t.after(() => rm(directory, { recursive: true, force: true }));
  const git = async (args: string[], env: Record<string, string> = {}) => (await exec("git", ["-C", directory, ...args], { env: { ...process.env, ...env }, encoding: "utf8" })).stdout.trim();
  await git(["init", "--initial-branch=main", "--quiet"]);
  await git(["config", "user.name", "Contributor A"]);
  await git(["config", "user.email", "alias@example.invalid"]);
  await git(["remote", "add", "origin", "git@github.com:sample/source.git"]);
  const commit = async (subject: string, author = "2026-11-12T12:00:00Z", committer = author) => {
    await git(["add", "--all"]);
    await git(["commit", "--quiet", "-m", subject], { GIT_AUTHOR_DATE: author, GIT_COMMITTER_DATE: committer });
    return await git(["rev-parse", "HEAD"]);
  };
  const publish = async () => {
    const revision = await git(["rev-parse", "HEAD"]);
    await git(["update-ref", "refs/remotes/origin/main", revision]);
    await git(["symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"]);
    return revision;
  };
  return { directory, git, commit, publish };
}
const range = { since: "2026-11-09T00:00:00Z", until: "2026-11-17T00:00:00Z" };

test("GitHub remote parsing rejects credentials and non-GitHub hosts", () => {
  for (const value of ["git@github.com:Sample/Source.git", "https://github.com/Sample/Source.git", "ssh://git@github.com/Sample/Source/", "git://github.com/Sample/Source"]) assert.equal(githubRepository(value), "sample/source");
  for (const value of ["https://github.com.evil.invalid/sample/source", "https://secret@github.com/sample/source", "https://github.com/sample/source?token=value", "https://github.com/sample/../source"]) assert.equal(githubRepository(value), null);
});

test("history uses the remote default revision and author dates without changing the clone", async (t) => {
  const repo = await fixture(t);
  await writeFile(join(repo.directory, "content.txt"), "initial\n");
  await repo.commit("Before interval", "2026-11-01T12:00:00Z");
  await writeFile(join(repo.directory, "content.txt"), "initial\ninside\n");
  const included = await repo.commit("Author date inside despite old committer date", "2026-11-12T12:00:00Z", "2026-10-01T12:00:00Z");
  await writeFile(join(repo.directory, "later.txt"), "future\n");
  await repo.commit("Author date after interval", "2026-11-20T12:00:00Z");
  const revision = await repo.publish();
  await repo.git(["switch", "-c", "local-feature", "--quiet"]);
  await writeFile(join(repo.directory, "feature.txt"), "feature\n");
  const feature = await repo.commit("Local feature should not count");
  await writeFile(join(repo.directory, "dirty.txt"), "uncommitted\n");
  const status = await repo.git(["status", "--porcelain=v1"]);
  const config = await readFile(join(repo.directory, ".git", "config"), "utf8");
  const result = await collectGitHistory(await describeRepository(repo.directory), range);
  assert.equal(result.revision, revision);
  assert.equal(result.ref, "refs/remotes/origin/main");
  assert.equal(result.complete, true);
  assert.equal(result.linesComplete, true);
  assert.deepEqual(result.records.map((record) => record.sha), [included]);
  assert.equal(result.records[0].additions, 1);
  assert.equal(result.records[0].deletions, 0);
  assert.ok(result.records.every((record) => record.sha !== feature));
  assert.equal(await repo.git(["status", "--porcelain=v1"]), status);
  assert.equal(await repo.git(["branch", "--show-current"]), "local-feature");
  assert.equal(await repo.git(["rev-parse", "refs/remotes/origin/main"]), revision);
  assert.equal(await readFile(join(repo.directory, ".git", "config"), "utf8"), config);
});

test("literal filenames and rename records survive numstat while binary lines remain unknown", async (t) => {
  const repo = await fixture(t);
  const original = "name\twith\nline.txt", renamed = "renamed\nname.txt";
  await writeFile(join(repo.directory, original), "one\ntwo\n");
  const initial = await repo.commit("Add literal filename");
  await repo.git(["mv", original, renamed]);
  const rename = await repo.commit("Rename literal filename", "2026-11-13T12:00:00Z");
  await writeFile(join(repo.directory, "binary.dat"), Buffer.from([0, 1, 2, 0, 255]));
  const binary = await repo.commit("Binary file", "2026-11-14T12:00:00Z");
  await repo.publish();
  const result = await collectGitHistory(await describeRepository(repo.directory), range);
  assert.equal(result.complete, true);
  assert.equal(result.records.find((record) => record.sha === initial)?.files[0].path, original);
  assert.equal(result.records.find((record) => record.sha === rename)?.files[0].path, renamed);
  assert.equal(result.records.find((record) => record.sha === rename)?.additions, 0);
  assert.equal(result.records.find((record) => record.sha === binary)?.additions, null);
  assert.equal(result.records.find((record) => record.sha === binary)?.files[0].path, "binary.dat");
  assert.equal(result.linesComplete, false);
  assert.match(result.error ?? "", /Binary/);
});

test("mailmap comes from the scanned revision rather than dirty worktree data", async (t) => {
  const repo = await fixture(t);
  await writeFile(join(repo.directory, "content.txt"), "one\n");
  await repo.commit("Mapped author");
  await writeFile(join(repo.directory, ".mailmap"), "Contributor Canonical <canonical@example.invalid> <alias@example.invalid>\n");
  await repo.commit("Add identity rule", "2026-11-13T12:00:00Z");
  await repo.publish();
  await writeFile(join(repo.directory, ".mailmap"), "Uncommitted Identity <dirty@example.invalid> <alias@example.invalid>\n");
  const result = await collectGitHistory(await describeRepository(repo.directory), range);
  assert.equal(result.complete, true);
  assert.ok(result.records.length > 0);
  assert.ok(result.records.every((record) => record.email === "canonical@example.invalid"));
  assert.ok(result.records.every((record) => record.contributor === "Contributor Canonical"));
});

test("partial clones expose unknown lines and unavailable default refs never use HEAD", async (t) => {
  const repo = await fixture(t);
  await writeFile(join(repo.directory, "content.txt"), "one\n");
  await repo.commit("Available commit");
  await repo.publish();
  await repo.git(["config", "remote.origin.promisor", "true"]);
  const result = await collectGitHistory(await describeRepository(repo.directory), range);
  assert.equal(result.partial, true);
  assert.equal(result.complete, false);
  assert.equal(result.linesComplete, false);
  assert.equal(result.records.length, 1);
  assert.equal(result.records[0].additions, null);
  assert.deepEqual(result.records[0].files, []);
  await repo.git(["symbolic-ref", "--delete", "refs/remotes/origin/HEAD"]);
  const unavailable = await describeRepository(repo.directory);
  assert.equal(unavailable.defaultBranch, "");
  const failed = await collectGitHistory(unavailable, range);
  assert.equal(failed.complete, false);
  assert.deepEqual(failed.records, []);
  assert.match(failed.error ?? "", /default branch/);
});

test("shallow history remains explicitly incomplete", async (t) => {
  const repo = await fixture(t);
  await writeFile(join(repo.directory, "content.txt"), "one\n");
  const initial = await repo.commit("Available shallow root");
  await repo.publish();
  await writeFile(join(repo.directory, ".git", "shallow"), initial + "\n");
  const result = await collectGitHistory(await describeRepository(repo.directory), range);
  assert.equal(result.shallow, true);
  assert.equal(result.complete, false);
  assert.equal(result.records.length, 1);
  assert.match(result.error ?? "", /Shallow/);
});

test("native import honors selection and exclusions and reports missing clones", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "bigboard-native-import-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const repo = await fixture(t, join(root, "sources", "source"));
  await writeFile(join(repo.directory, "content.txt"), "one\n");
  await repo.commit("Importable source");
  await repo.publish();
  const other = await fixture(t, join(root, "sources", "other"));
  await other.git(["remote", "set-url", "origin", "https://github.com/sample/other.git"]);
  await writeFile(join(other.directory, "content.txt"), "other\n");
  await other.commit("Excluded source");
  await other.publish();
  const settings = join(root, "settings");
  await mkdir(settings);
  const previous = process.env.BIGBOARD_CONFIG_ROOT;
  process.env.BIGBOARD_CONFIG_ROOT = settings;
  try {
    await writeFile(join(settings, "config.json"), JSON.stringify({ sourceRoots: [join(root, "sources")], repositories: ["sample/source", "sample/other"], exclude: ["other"] }));
    const imported = await importNativeSources();
    assert.equal(imported.length, 1);
    assert.equal(imported[0].id, "sample/source");
    assert.equal(imported[0].path, await realpath(repo.directory));
    await writeFile(join(settings, "config.json"), JSON.stringify({ sourceRoots: [join(root, "sources")], repositories: ["sample/missing"] }));
    await assert.rejects(importNativeSources(), /no discoverable local clone/);
  } finally {
    if (previous === undefined) delete process.env.BIGBOARD_CONFIG_ROOT;
    else process.env.BIGBOARD_CONFIG_ROOT = previous;
  }
});

test("upstream wins over origin and identity changes require reconfiguration", async (t) => {
  const repo = await fixture(t);
  await writeFile(join(repo.directory, "content.txt"), "one\n");
  await repo.commit("Source commit");
  await repo.publish();
  const original = await describeRepository(repo.directory);
  await repo.git(["remote", "add", "upstream", "git@github.com:sample/upstream.git"]);
  const next = await describeRepository(repo.directory);
  assert.equal(next.id, "sample/upstream");
  assert.equal(next.remote, "upstream");
  const changed = await collectGitHistory(original, range);
  assert.equal(changed.complete, false);
  assert.match(changed.error ?? "", /identity changed/);
});
