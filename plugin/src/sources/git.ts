import { execFile } from "node:child_process";
import { readFile, readdir, realpath, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { isAbsolute, join, resolve } from "node:path";
import type { ActivityRecord, ConfiguredRepository, FileChange, GitCollectionResult } from "../shared";

const MAX_COMMITS = 20_000;
const MAX_LINE_COMMITS = 5_000;
const MAX_OUTPUT = 16 * 1024 * 1024;
const OID = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const SKIPPED = new Set(["node_modules", ".build", "build", "dist", "target", "vendor", "Pods", "DerivedData", "Library"]);

function expandHome(path: string): string {
  return path === "~" ? homedir() : path.startsWith("~/") ? join(homedir(), path.slice(2)) : path;
}

function runGit(args: string[], signal?: AbortSignal, input?: string, timeout = 15_000): Promise<string> {
  return new Promise((resolveOutput, reject) => {
    const child = execFile("git", ["-c", "core.pager=cat", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", ...args], {
      encoding: "utf8",
      maxBuffer: MAX_OUTPUT,
      timeout,
      signal,
      env: { ...process.env, GIT_OPTIONAL_LOCKS: "0", GIT_NO_LAZY_FETCH: "1", GIT_TERMINAL_PROMPT: "0", GIT_PAGER: "cat" },
    }, (error, stdout) => error ? reject(new Error(signal?.aborted ? "Git collection was cancelled." : "The local Git command could not complete.")) : resolveOutput(stdout));
    child.stdin?.on("error", () => {});
    child.stdin?.end(input);
  });
}

export function githubRepository(remote: string): string | null {
  const match = remote.trim().match(/^(?:https?:\/\/github\.com\/|ssh:\/\/(?:git@)?github\.com\/|git@github\.com:|git:\/\/github\.com\/)([^/?#\s]+)\/([^/?#\s]+?)\/?$/i);
  if (!match) return null;
  const owner = match[1];
  const name = match[2].replace(/\.git$/i, "");
  if (!/^[a-z0-9](?:[a-z0-9-]{0,38})$/i.test(owner) || !/^[a-z0-9_.-]+$/i.test(name) || name === "." || name === "..") return null;
  return `${owner}/${name}`.toLowerCase();
}

async function gitDirectory(path: string, signal?: AbortSignal): Promise<string> {
  return (await runGit(["-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir"], signal)).trim();
}

export async function describeRepository(path: string, signal?: AbortSignal): Promise<ConfiguredRepository> {
  if (typeof path !== "string" || !path || !isAbsolute(expandHome(path))) throw new Error("Choose an absolute path to an existing local Git clone.");
  const canonical = await realpath(expandHome(path)).catch(() => { throw new Error("The selected clone directory could not be found."); });
  if (!(await stat(canonical)).isDirectory()) throw new Error("Choose a local Git clone directory.");
  const common = await gitDirectory(canonical, signal);
  const listing = await runGit([`--git-dir=${common}`, "config", "--local", "--get-regexp", "^remote\\..*\\.url$"], signal)
    .catch(() => { throw new Error("This clone has no readable GitHub remote."); });
  const remotes = listing.split("\n").flatMap((line) => {
    const match = line.match(/^remote\.(.+)\.url\s+(.+)$/);
    const id = match ? githubRepository(match[2]) : null;
    return match && id ? [{ remote: match[1], id }] : [];
  }).sort((a, b) => a.remote.localeCompare(b.remote, "en"));
  const selected = remotes.find((item) => item.remote === "upstream") ?? remotes.find((item) => item.remote === "origin") ?? remotes[0];
  if (!selected || !/^[a-zA-Z0-9_][a-zA-Z0-9_./-]{0,99}$/.test(selected.remote) || selected.remote.includes("..")) {
    throw new Error("This clone has no supported github.com remote.");
  }
  const prefix = `refs/remotes/${selected.remote}/`;
  const head = await runGit([`--git-dir=${common}`, "symbolic-ref", "--quiet", `${prefix}HEAD`], signal).catch(() => "");
  let defaultBranch = head.trim().startsWith(prefix) ? head.trim().slice(prefix.length) : "";
  if (defaultBranch) {
    const valid = await runGit(["check-ref-format", `refs/heads/${defaultBranch}`], signal).then(() => true, () => false);
    if (!valid) defaultBranch = "";
  }
  return { id: selected.id, path: canonical, remote: selected.remote, defaultBranch };
}

function excluded(id: string, patterns: string[]): boolean {
  return patterns.some((pattern) => {
    const expression = new RegExp("^" + pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replaceAll("*", ".*").replaceAll("?", ".") + "$", "i");
    return expression.test(id) || expression.test(id.split("/")[1]);
  });
}

export async function importNativeSources(signal?: AbortSignal): Promise<ConfiguredRepository[]> {
  const base = process.env.XDG_CONFIG_HOME && isAbsolute(process.env.XDG_CONFIG_HOME) ? process.env.XDG_CONFIG_HOME : join(homedir(), ".config");
  const configRoot = process.env.BIGBOARD_CONFIG_ROOT || join(base, "bigboard");
  const content = await readFile(join(configRoot, "config.json"), "utf8").catch(() => { throw new Error("Native BigBoard settings were not found. Configure explicit local clone paths instead."); });
  if (content.length > 1024 * 1024) throw new Error("Native BigBoard settings are too large to import.");
  let value: unknown;
  try { value = JSON.parse(content); } catch { throw new Error("Native BigBoard settings could not be read."); }
  const config = value as { sourceRoots?: unknown; repositories?: unknown; exclude?: unknown } | null;
  if (!config || !Array.isArray(config.sourceRoots) || !Array.isArray(config.repositories) ||
      !config.sourceRoots.every((item) => typeof item === "string") || !config.repositories.every((item) => typeof item === "string") ||
      (config.exclude !== undefined && (!Array.isArray(config.exclude) || !config.exclude.every((item) => typeof item === "string")))) {
    throw new Error("Native BigBoard settings do not contain a valid repository selection.");
  }
  const patterns = (config.exclude ?? []) as string[];
  const selected = new Set((config.repositories as string[]).map((id) => id.toLowerCase()).filter((id) => !excluded(id, patterns)));
  if (selected.size > 2000 || config.sourceRoots.length > 100) throw new Error("Native BigBoard selection exceeds the import limit.");
  if (selected.size === 0) return [];
  const queue: Array<{ path: string; depth: number }> = [];
  for (const root of config.sourceRoots as string[]) {
    const path = await realpath(expandHome(root)).catch(() => null);
    if (path) queue.push({ path, depth: 0 });
  }
  const visited = new Set<string>();
  const candidates: string[] = [];
  while (queue.length > 0) {
    signal?.throwIfAborted();
    if (visited.size >= 20_000 || candidates.length >= 2000) throw new Error("Repository discovery reached its limit. Narrow the native source folders or configure explicit clone paths.");
    const next = queue.shift()!;
    if (visited.has(next.path)) continue;
    visited.add(next.path);
    const entries = await readdir(next.path, { withFileTypes: true }).catch(() => []);
    if (entries.some((entry) => entry.name === ".git" && (entry.isDirectory() || entry.isFile()))) {
      candidates.push(next.path);
      continue;
    }
    if (next.depth >= 4) continue;
    for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name, "en"))) {
      if (entry.isDirectory() && !entry.isSymbolicLink() && !entry.name.startsWith(".") && !SKIPPED.has(entry.name)) {
        queue.push({ path: join(next.path, entry.name), depth: next.depth + 1 });
      }
    }
  }
  const found = new Map<string, ConfiguredRepository>();
  for (let start = 0; start < candidates.length; start += 4) {
    signal?.throwIfAborted();
    const batch = await Promise.all(candidates.slice(start, start + 4).sort().map((path) => describeRepository(path, signal).catch(() => null)));
    for (const repo of batch) if (repo && selected.has(repo.id) && !found.has(repo.id)) found.set(repo.id, repo);
  }
  const missing = [...selected].filter((id) => !found.has(id));
  if (missing.length) throw new Error(`Selected native repositories have no discoverable local clone: ${missing.join(", ")}. Configure their clone paths or adjust the native selection.`);
  return [...found.values()].sort((a, b) => a.id.localeCompare(b.id, "en"));
}

function parseHeaders(output: string, repository: ConfiguredRepository): { records: ActivityRecord[]; count: number; invalid: boolean } {
  const tokens = output.split("\0");
  if (tokens.at(-1) === "") tokens.pop();
  const records: ActivityRecord[] = [];
  let invalid = tokens.length % 5 !== 0;
  for (let index = 0; index + 4 < tokens.length; index += 5) {
    const [sha, seconds, contributor, email, subject] = tokens.slice(index, index + 5);
    const milliseconds = Number(seconds) * 1000;
    if (!OID.test(sha) || !/^\d+$/.test(seconds) || !Number.isFinite(milliseconds) || Math.abs(milliseconds) > 8.64e15) {
      invalid = true;
      continue;
    }
    records.push({
      id: `${repository.id}@${sha}`, repository: repository.id, sha,
      timestamp: new Date(milliseconds).toISOString(), subject, contributor, email: email.trim().toLowerCase(),
      additions: null, deletions: null, files: [], url: `https://github.com/${repository.id}/commit/${sha}`,
    });
  }
  return { records, count: Math.floor(tokens.length / 5), invalid };
}

function parseNumstat(output: string): Map<string, FileChange[]> {
  const tokens = output.split("\0");
  const result = new Map<string, FileChange[]>();
  let current: FileChange[] | undefined;
  for (let index = 0; index < tokens.length;) {
    if (tokens[index] === "" && OID.test(tokens[index + 1] ?? "") && tokens[index + 2] === "") {
      current = [];
      result.set(tokens[index + 1], current);
      index += 3;
      continue;
    }
    let token = tokens[index++];
    if (!token) continue;
    if (token.startsWith("\n")) token = token.slice(1);
    if (!token) continue;
    const match = token.match(/^(-|\d+)\t(-|\d+)\t([\s\S]*)$/);
    if (!match || !current) throw new Error("Git returned unreadable line evidence.");
    let path = match[3];
    if (path === "") {
      if (!tokens[index] || !tokens[index + 1]) throw new Error("Git returned an incomplete rename record.");
      index += 1;
      path = tokens[index++];
    }
    const additions = match[1] === "-" ? null : Number(match[1]);
    const deletions = match[2] === "-" ? null : Number(match[2]);
    if ((additions !== null && !Number.isSafeInteger(additions)) || (deletions !== null && !Number.isSafeInteger(deletions))) throw new Error("Git line counts exceeded the supported range.");
    current.push({ path, additions, deletions });
  }
  return result;
}

export async function collectGitHistory(repository: ConfiguredRepository, options: { since: string; until: string; signal?: AbortSignal }): Promise<GitCollectionResult> {
  const result: GitCollectionResult = { repository, revision: null, ref: `refs/remotes/${repository.remote}/${repository.defaultBranch}`, shallow: false, partial: false, complete: false, linesComplete: false, records: [], error: null };
  const notes: string[] = [];
  const deadline = Date.now() + 30_000;
  try {
    const since = Date.parse(options.since), until = Date.parse(options.until);
    if (!Number.isFinite(since) || !Number.isFinite(until) || since >= until) throw new Error("Choose a valid Git collection interval.");
    const inspected = await describeRepository(repository.path, options.signal);
    if (inspected.id !== repository.id || inspected.remote !== repository.remote) throw new Error("The clone's GitHub identity changed. Reconfigure the source before collecting it.");
    if (!repository.defaultBranch || inspected.defaultBranch !== repository.defaultBranch) throw new Error("The clone's remote default branch is unavailable or changed. Update the clone's remote HEAD and reconfigure the source.");
    const common = await gitDirectory(repository.path, options.signal);
    const git = [`--git-dir=${common}`, "-c", "core.bare=true", "-c", "mailmap.file=", "-c", "core.attributesFile=/dev/null"];
    result.revision = (await runGit([...git, "rev-parse", "--verify", "--quiet", "--end-of-options", `${result.ref}^{commit}`], options.signal)
      .catch(() => { throw new Error("The clone's remote-tracking default branch has not been fetched. Update the clone outside BigBoard, then refresh."); })).trim();
    if (!OID.test(result.revision)) throw new Error("The clone's remote-tracking default branch has not been fetched. Update the clone outside BigBoard, then refresh.");
    result.shallow = (await runGit([...git, "rev-parse", "--is-shallow-repository"], options.signal)).trim() === "true";
    const config = await runGit([...git, "config", "--local", "--list"], options.signal);
    result.partial = config.split("\n").some((line) => /^extensions\.partialclone=/i.test(line) || /^remote\..*\.promisor=true$/i.test(line));
    const mailmap = result.partial ? [] : ["-c", `mailmap.blob=${result.revision}:.mailmap`];
    const header = await runGit([...git, ...mailmap, "log", "--no-merges", "--no-show-signature", `--max-count=${MAX_COMMITS + 1}`, "--format=%H%x00%at%x00%aN%x00%aE%x00%s", "-z", "--end-of-options", result.revision, "--"], options.signal);
    const parsed = parseHeaders(header, repository);
    result.complete = !result.shallow && !result.partial && parsed.count <= MAX_COMMITS && !parsed.invalid;
    if (parsed.count > MAX_COMMITS) notes.push("History traversal reached its commit limit; counts may be incomplete.");
    if (parsed.invalid) notes.push("Unreadable author dates or commit records were skipped.");
    if (result.shallow) notes.push("Shallow clone: earlier history is unavailable locally.");
    if (result.partial) notes.push("Partial clone: line counts and paths are unavailable locally.");
    result.records = parsed.records.slice(0, MAX_COMMITS).filter((record) => Date.parse(record.timestamp) >= since && Date.parse(record.timestamp) < until);
    let linesFailed = false;
    if (!result.partial) {
      for (let start = 0; start < Math.min(result.records.length, MAX_LINE_COMMITS); start += 100) {
        options.signal?.throwIfAborted();
        const batch = result.records.slice(start, Math.min(start + 100, MAX_LINE_COMMITS));
        const remaining = deadline - Date.now();
        if (remaining <= 0) { linesFailed = true; break; }
        try {
          const output = await runGit([...git, "log", "--no-walk=unsorted", "--stdin", "--root", "-M50%", "-l1000", "--no-ext-diff", "--no-textconv", "--no-color", "--no-relative", "--no-show-signature", "--ignore-submodules=none", "--format=%x00%H%x00", "--numstat", "-z", "--"], options.signal, batch.map((record) => record.sha).join("\n") + "\n", Math.min(15_000, remaining));
          const changes = parseNumstat(output);
          for (const record of batch) {
            const files = changes.get(record.sha);
            if (!files) { linesFailed = true; continue; }
            record.files = files;
            record.additions = files.every((file) => file.additions !== null) ? files.reduce((sum, file) => sum + (file.additions ?? 0), 0) : null;
            record.deletions = files.every((file) => file.deletions !== null) ? files.reduce((sum, file) => sum + (file.deletions ?? 0), 0) : null;
          }
        } catch {
          if (options.signal?.aborted) throw new Error("Git collection was cancelled.");
          linesFailed = true;
        }
      }
    }
    result.linesComplete = result.records.every((record) => record.additions !== null && record.deletions !== null);
    if (result.records.length > MAX_LINE_COMMITS || linesFailed) notes.push("Some commit line evidence could not be collected within the scan limits.");
    if (result.records.some((record) => record.files.some((file) => file.additions === null || file.deletions === null))) notes.push("Binary files have unknown line counts.");
    result.error = notes.length ? notes.join(" ") : null;
    return result;
  } catch (error) {
    result.records = [];
    result.complete = false;
    result.linesComplete = false;
    result.error = options.signal?.aborted ? "Git collection was cancelled." : error instanceof Error ? error.message : "Local Git history is unavailable. Check the configured clone and refresh.";
    return result;
  }
}
