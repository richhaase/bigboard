import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { ConfiguredRepository, GitHubCollectionResult, PullRequestRecord } from "../shared";

const execFileAsync = promisify(execFile);
const REPOSITORY = /^[a-z0-9](?:[a-z0-9-]{0,38})\/[a-z0-9_.-]{1,100}$/;
const LOGIN = /^[a-z0-9](?:[a-z0-9-]{0,100})(?:\[bot\])?$/i;
const MAXIMUM_BYTES = 2 * 1024 * 1024;
const PAGE_SIZE = 50;

export interface GitHubCollectionOptions {
  since: string;
  until: string;
  signal?: AbortSignal;
  onProgress?: (progress: GitHubCollectionProgress) => void;
}

export interface GitHubCollectionProgress {
  completedRepositories: number;
  totalRepositories: number;
  requests: number;
}

export interface GitHubDependencies {
  token?: (signal: AbortSignal) => Promise<string>;
  request?: (url: URL, init: RequestInit) => Promise<Response>;
  maximumRequests?: number;
  maximumPages?: number;
  requestTimeoutMs?: number;
  timeoutMs?: number;
  repositoryConcurrency?: number;
}

type FailureKind = "auth" | "denied" | "rate" | "budget" | "timeout" | "cancelled" | "unavailable" | "response";

class GitHubFailure extends Error {
  constructor(readonly kind: FailureKind, message: string) {
    super(message);
  }
}

function failure(kind: FailureKind): GitHubFailure {
  const messages: Record<FailureKind, string> = {
    auth: "GitHub authentication is unavailable. Check the active GitHub CLI account with gh auth status.",
    denied: "GitHub denied access to this repository. Check repository and organization access for the active account.",
    rate: "GitHub rate-limited this collection. Retry after the account's rate limit resets.",
    budget: "The GitHub request budget was reached; PR coverage is incomplete.",
    timeout: "GitHub collection timed out; PR coverage is incomplete.",
    cancelled: "GitHub collection was cancelled; PR coverage is incomplete.",
    unavailable: "GitHub could not be reached; PR coverage is incomplete.",
    response: "GitHub returned unreadable PR evidence; coverage is incomplete.",
  };
  return new GitHubFailure(kind, messages[kind]);
}

async function tokenFromCLI(signal: AbortSignal): Promise<string> {
  try {
    const result = await execFileAsync("gh", ["auth", "token", "--hostname", "github.com"], {
      encoding: "utf8", timeout: 10_000, maxBuffer: 16_384, windowsHide: true, signal,
    });
    return result.stdout.trim();
  } catch {
    if (signal.aborted) throw failure("cancelled");
    throw failure("auth");
  }
}

function date(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : null;
}

function object(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function record(value: unknown, repository: ConfiguredRepository, state: "open" | "merged"): PullRequestRecord | null {
  const row = object(value);
  const user = object(row?.user);
  const base = object(row?.base);
  const head = object(row?.head);
  const baseRepository = object(base?.repo);
  const createdAt = date(row?.created_at);
  const updatedAt = date(row?.updated_at);
  const mergedAt = date(row?.merged_at);
  if (!row || !user || !base || !head || !Number.isSafeInteger(row.number) || (row.number as number) <= 0 ||
      typeof row.title !== "string" || row.title.length > 10_000 || typeof user.login !== "string" || !LOGIN.test(user.login) ||
      typeof base.ref !== "string" || typeof head.sha !== "string" || !/^[a-f0-9]{40,64}$/i.test(head.sha) ||
      !createdAt || !updatedAt || updatedAt < createdAt ||
      (baseRepository?.full_name !== undefined && (typeof baseRepository.full_name !== "string" || baseRepository.full_name.toLowerCase() !== repository.id)) ||
      (state === "open" ? row.state !== "open" || row.merged_at !== null : !mergedAt || mergedAt < createdAt || mergedAt > updatedAt)) {
    return null;
  }
  return {
    id: `${repository.id}#${row.number}`,
    repository: repository.id,
    number: row.number as number,
    title: row.title,
    url: `https://github.com/${repository.id}/pull/${row.number}`,
    author: user.login,
    state,
    draft: row.draft === true,
    createdAt,
    updatedAt,
    mergedAt: state === "merged" ? mergedAt : null,
    baseBranch: base.ref,
    headSha: head.sha,
  };
}

async function abortable<T>(operation: Promise<T>, signal: AbortSignal): Promise<T> {
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason);
    signal.addEventListener("abort", abort, { once: true });
    operation.then(resolve, reject).finally(() => signal.removeEventListener("abort", abort));
  });
}

async function json(response: Response, signal: AbortSignal): Promise<unknown> {
  const declaredLength = Number(response.headers.get("content-length"));
  if (declaredLength > MAXIMUM_BYTES) {
    await response.body?.cancel();
    throw failure("response");
  }
  if (!response.body) throw failure("response");
  const reader = response.body.getReader();
  let text = "";
  let bytes = 0;
  const decoder = new TextDecoder();
  try {
    while (true) {
      const part = await abortable(reader.read(), signal);
      if (part.done) break;
      bytes += part.value.byteLength;
      if (bytes > MAXIMUM_BYTES) throw failure("response");
      text += decoder.decode(part.value, { stream: true });
    }
    text += decoder.decode();
    return JSON.parse(text);
  } catch (error) {
    void reader.cancel().catch(() => {});
    throw error instanceof SyntaxError ? failure("response") : error;
  } finally {
    reader.releaseLock();
  }
}

export function createGitHubCollector(dependencies: GitHubDependencies = {}) {
  const getToken = dependencies.token ?? tokenFromCLI;
  const transport = dependencies.request ?? ((url: URL, init: RequestInit) => fetch(url, init));
  const maximumRequests = dependencies.maximumRequests ?? 128;
  const maximumPages = dependencies.maximumPages ?? 8;
  const requestTimeoutMs = dependencies.requestTimeoutMs ?? 15_000;
  const timeoutMs = dependencies.timeoutMs ?? 120_000;
  const requestedConcurrency = dependencies.repositoryConcurrency ?? 3;
  const repositoryConcurrency = Number.isFinite(requestedConcurrency) ? Math.max(1, Math.min(3, Math.floor(requestedConcurrency))) : 3;

  return async function collect(repositories: ConfiguredRepository[], options: GitHubCollectionOptions): Promise<GitHubCollectionResult> {
    const result: GitHubCollectionResult = {
      account: null,
      repositories: repositories.map(({ id }) => ({ id, openComplete: false, mergedComplete: false, accessDenied: false, error: null })),
      pullRequests: [],
      error: null,
    };
    let requests = 0;
    let completedRepositories = 0;
    const report = () => {
      try { options.onProgress?.({ completedRepositories, totalRepositories: repositories.length, requests }); }
      catch {}
    };
    report();
    const since = date(options.since);
    const until = date(options.until);
    if (!since || !until || since >= until || repositories.some((repository) => !REPOSITORY.test(repository.id) ||
        [".", ".."].includes(repository.id.split("/")[1]))) {
      const message = "GitHub collection requires canonical repository IDs and a valid date interval.";
      result.error = message;
      result.repositories.forEach((repository) => { repository.error = message; });
      return result;
    }
    if (repositories.length === 0) return result;
    const deadline = AbortSignal.timeout(Math.max(1, timeoutMs));
    const collectionSignal = AbortSignal.any([deadline, ...(options.signal ? [options.signal] : [])]);
    let terminal: GitHubFailure | null = null;
    const authenticationAbort = new AbortController();
    let credential: string;
    const abortFailure = () => failure(options.signal?.aborted ? "cancelled" : "timeout");
    const halt = (issue: GitHubFailure): GitHubFailure => {
      if (!terminal || issue.kind === "auth") terminal = issue;
      if (issue.kind === "auth") authenticationAbort.abort();
      return terminal;
    };
    const request = async (endpoint: string, query?: Record<string, string>): Promise<{ value: unknown; next: boolean }> => {
      if (terminal) throw terminal;
      if (collectionSignal.aborted) throw halt(abortFailure());
      if (requests >= maximumRequests) throw halt(failure("budget"));
      const url = new URL(`https://api.github.com/${endpoint}`);
      for (const [key, value] of Object.entries(query ?? {})) url.searchParams.set(key, value);
      if (url.origin !== "https://api.github.com" || url.username || url.password || url.hash) throw failure("response");
      const signal = AbortSignal.any([collectionSignal, authenticationAbort.signal, AbortSignal.timeout(Math.max(1, requestTimeoutMs))]);
      requests += 1;
      report();
      try {
        signal.throwIfAborted();
        const response = await abortable(transport(url, {
          method: "GET",
          headers: {
            Authorization: `Bearer ${credential}`,
            Accept: "application/vnd.github+json",
            "X-GitHub-Api-Version": "2026-03-10",
            "User-Agent": "BigBoard-MCP",
          },
          redirect: "error",
          signal,
        }), signal);
        if (response.status === 401) throw halt(failure("auth"));
        if (response.status === 429 || (response.status === 403 &&
            (response.headers.get("x-ratelimit-remaining") === "0" || response.headers.has("retry-after")))) {
          throw halt(failure("rate"));
        }
        if (response.status === 403 || response.status === 404) throw failure("denied");
        if (!response.ok) throw failure(response.status >= 500 ? "unavailable" : "response");
        return { value: await json(response, signal), next: /<[^>]+>;\s*rel="next"/.test(response.headers.get("link") ?? "") };
      } catch (error) {
        if (error instanceof GitHubFailure) throw error;
        if ((terminal as GitHubFailure | null)?.kind === "auth") throw terminal;
        if (signal.aborted) throw halt(collectionSignal.aborted ? abortFailure() : failure("timeout"));
        throw failure("unavailable");
      }
    };

    try {
      if (collectionSignal.aborted) throw abortFailure();
      credential = await abortable(getToken(collectionSignal), collectionSignal);
      if (!credential || credential.length > 8192 || !/^[\x21-\x7e]+$/.test(credential)) throw failure("auth");
      const viewer = object((await request("user")).value);
      if (!viewer || !Number.isSafeInteger(viewer.id) || (viewer.id as number) < 1 || typeof viewer.login !== "string" || !LOGIN.test(viewer.login)) {
        throw failure("auth");
      }
      result.account = { id: String(viewer.id), login: viewer.login };
    } catch (error) {
      const issue = error instanceof GitHubFailure ? error : collectionSignal.aborted ? abortFailure() : failure("auth");
      result.error = issue.message;
      result.repositories.forEach((repository) => { repository.error = issue.message; });
      return result;
    }

    const inventory = async (repository: ConfiguredRepository, state: "open" | "merged") => {
      const records: PullRequestRecord[] = [];
      const seen = new Set<string>();
      let malformed = false;
      for (let page = 1; page <= maximumPages; page += 1) {
        try {
          const response = await request(`repos/${repository.id}/pulls`, {
            state: state === "open" ? "open" : "closed", sort: "updated", direction: "desc", per_page: String(PAGE_SIZE), page: String(page),
          });
          if (!Array.isArray(response.value) || response.value.length > PAGE_SIZE) throw failure("response");
          const rows = response.value;
          for (const value of rows) {
            const row = object(value);
            if (state === "merged" && row?.merged_at === null) continue;
            const normalized = record(value, repository, state);
            if (!normalized) {
              malformed = true;
              continue;
            }
            if (seen.has(normalized.id)) {
              malformed = true;
              continue;
            }
            seen.add(normalized.id);
            if (state === "merged" && (normalized.baseBranch !== repository.defaultBranch || normalized.mergedAt! < since || normalized.mergedAt! >= until)) continue;
            records.push(normalized);
          }
          const lastUpdated = rows.length > 0 ? date(object(rows.at(-1))?.updated_at) : null;
          const validUpdatedDates = rows.every((row) => date(object(row)?.updated_at) !== null);
          const reachedBoundary = state === "merged" && validUpdatedDates && lastUpdated !== null && lastUpdated < since;
          if (!response.next || reachedBoundary) {
            return { records, complete: !malformed, error: malformed ? failure("response").message : null };
          }
        } catch (error) {
          const issue = error instanceof GitHubFailure ? error : failure("unavailable");
          if (issue.kind === "auth" || issue.kind === "denied") throw issue;
          return { records, complete: false, error: issue.message };
        }
      }
      return { records, complete: false, error: "The GitHub page limit was reached; PR coverage is incomplete." };
    };

    const recordsByRepository: PullRequestRecord[][] = repositories.map(() => []);
    let nextRepository = 0;
    const worker = async () => {
      while (nextRepository < repositories.length && !terminal && !collectionSignal.aborted) {
        const index = nextRepository++;
        const repository = repositories[index];
        const coverage = result.repositories[index];
        if (typeof repository.defaultBranch !== "string" || !repository.defaultBranch.trim()) {
          coverage.error = "The repository's default branch is unavailable; PR collection was skipped.";
          completedRepositories += 1;
          report();
          continue;
        }
        try {
          const open = await inventory(repository, "open");
          const merged = await inventory(repository, "merged");
          coverage.openComplete = open.complete;
          coverage.mergedComplete = merged.complete;
          coverage.error = [...new Set([open.error, merged.error].filter((value): value is string => value !== null))].join(" ") || null;
          recordsByRepository[index] = [...open.records, ...merged.records];
        } catch (error) {
          const issue = error instanceof GitHubFailure ? error : failure("unavailable");
          if (issue.kind === "auth") halt(issue);
          coverage.accessDenied = issue.kind === "denied";
          coverage.error = issue.message;
        } finally {
          completedRepositories += 1;
          report();
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(repositoryConcurrency, repositories.length) }, worker));
    if (collectionSignal.aborted && !terminal) halt(abortFailure());
    if (terminal) {
      const issue = terminal as GitHubFailure;
      result.error = issue.message;
      for (; nextRepository < repositories.length; nextRepository += 1) {
        result.repositories[nextRepository].error = issue.message;
        completedRepositories += 1;
        report();
      }
      if (issue.kind === "auth") {
        result.account = null;
        result.repositories.forEach((item) => {
          item.openComplete = false;
          item.mergedComplete = false;
          item.error = issue.message;
        });
        return result;
      }
    }
    result.pullRequests = [...new Map(recordsByRepository.flat().map((row) => [row.id, row])).values()];
    return result;
  };
}

export const collectGitHub = createGitHubCollector();
