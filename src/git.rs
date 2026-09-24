use crate::model::{CommitRecord, Identity, Repository, ScanData};
use crate::progress::{Reporter, ScanStage};
use anyhow::{Context, Result, anyhow, bail};
use chrono::DateTime;
use std::collections::{HashMap, HashSet};
use std::io::{BufRead, BufReader, Read};
use std::path::{Component, Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, Instant};

const GIT_TIMEOUT: Duration = Duration::from_secs(120);
const MAX_LOG_LINE: usize = 4 * 1024 * 1024;
const COAUTHOR_SEP: char = '\x1f';
const IGNORED_DIRS: &[&str] = &[
    "vendor",
    "node_modules",
    "dist",
    "build",
    ".next",
    "target",
    ".yarn",
    ".venv",
    "__pycache__",
    "Pods",
    "Carthage",
];
const IGNORED_FILE_GLOBS: &[&str] = &[
    "*.min.js",
    "*.min.css",
    "*.map",
    "*.snap",
    "*.lock",
    "*.pb.go",
    "*_pb2.py",
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "go.sum",
    "Cargo.lock",
    "composer.lock",
    "Gemfile.lock",
    "poetry.lock",
];
const AI_ADDRESSES: &[&str] = &[
    "noreply@anthropic.com",
    "noreply@openai.com",
    "hi@cursor.com",
    "hi@cursor.sh",
    "copilot@github.com",
    "devin@cognition.ai",
    "noreply@aider.chat",
    "bot@codium.ai",
];
const AI_GITHUB_USERS: &[&str] = &[
    "copilot",
    "copilot-swe-agent",
    "claude",
    "devin-ai-integration",
    "google-labs-jules",
    "cursoragent",
];

#[derive(Clone, Debug, Default)]
pub struct CollectOptions {
    pub include_generated: bool,
    pub ai_identities: Vec<String>,
}

struct GitContext<'a> {
    deadline: Instant,
    cancel: &'a AtomicBool,
    report: &'a Reporter<'a>,
}

impl<'a> GitContext<'a> {
    fn new(cancel: &'a AtomicBool) -> Self {
        Self {
            deadline: Instant::now() + GIT_TIMEOUT,
            cancel,
            report: &|_| {},
        }
    }

    fn check(&self) -> Result<()> {
        if self.cancel.load(Ordering::Relaxed) {
            bail!("operation canceled");
        }
        if Instant::now() >= self.deadline {
            bail!("deadline exceeded");
        }
        Ok(())
    }
}

/// Run Git while draining both pipes and polling the shared cancellation and
/// deadline. The stdout consumer can parse incrementally instead of retaining
/// an entire repository's log text.
fn run_git<T, F>(ctx: &GitContext<'_>, dir: &Path, args: &[&str], consume: F) -> Result<T>
where
    T: Send,
    F: FnOnce(std::process::ChildStdout) -> Result<T> + Send,
{
    ctx.check()?;
    let child = git_command(dir, args)
        .spawn()
        .with_context(|| format!("git {} in {}", args[0], dir.display()))?;
    run_child(ctx, child, consume)
        .map_err(|error| anyhow!("git {} in {}: {error:#}", args[0], dir.display()))
}

fn git_command(dir: &Path, args: &[&str]) -> Command {
    let mut command = Command::new("git");
    command
        .args([
            "-c",
            "core.quotePath=false",
            "-c",
            "log.showRoot=true",
            "-c",
            "log.showSignature=false",
            "-c",
            "diff.algorithm=myers",
            "-c",
            "diff.indentHeuristic=false",
            "-c",
            "diff.renameLimit=0",
            "-c",
            "color.ui=false",
            "-c",
            "merge.renames=true",
            "-c",
            "merge.conflictStyle=merge",
            "-c",
            "merge.renormalize=false",
        ])
        .args(args)
        .current_dir(dir)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .env("GIT_NO_REPLACE_OBJECTS", "1")
        .env("GIT_NO_LAZY_FETCH", "1")
        .env_remove("GIT_EXTERNAL_DIFF")
        .env_remove("GIT_DIFF_OPTS");
    // A scanner can be launched from a Git hook. Repository-local environment
    // variables then refer to the hook's repository, overriding current_dir.
    // Clear the local variables listed by `git rev-parse --local-env-vars`,
    // plus namespace routing; retain normal global/system configuration.
    for key in [
        "GIT_ALTERNATE_OBJECT_DIRECTORIES",
        "GIT_CONFIG",
        "GIT_CONFIG_PARAMETERS",
        "GIT_CONFIG_COUNT",
        "GIT_OBJECT_DIRECTORY",
        "GIT_DIR",
        "GIT_WORK_TREE",
        "GIT_IMPLICIT_WORK_TREE",
        "GIT_GRAFT_FILE",
        "GIT_INDEX_FILE",
        "GIT_REPLACE_REF_BASE",
        "GIT_PREFIX",
        "GIT_SHALLOW_FILE",
        "GIT_COMMON_DIR",
        "GIT_NAMESPACE",
    ] {
        command.env_remove(key);
    }
    command
}

struct ProcessOutput<T> {
    value: T,
    status: std::process::ExitStatus,
    stderr: Vec<u8>,
}
fn run_child<T, F>(ctx: &GitContext<'_>, child: std::process::Child, consume: F) -> Result<T>
where
    T: Send,
    F: FnOnce(std::process::ChildStdout) -> Result<T> + Send,
{
    let output = run_child_output(ctx, child, consume)?;
    if !output.status.success() {
        bail!(
            "{}: {}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        );
    }
    Ok(output.value)
}

fn run_child_output<T, F>(
    ctx: &GitContext<'_>,
    mut child: std::process::Child,
    consume: F,
) -> Result<ProcessOutput<T>>
where
    T: Send,
    F: FnOnce(std::process::ChildStdout) -> Result<T> + Send,
{
    let stdout = child.stdout.take().expect("piped stdout");
    let mut stderr = child.stderr.take().expect("piped stderr");
    thread::scope(|scope| {
        let (output_sender, output_receiver) = std::sync::mpsc::sync_channel(1);
        let output_thread = scope.spawn(move || {
            let _ = output_sender.send(consume(stdout));
        });
        let error_thread = scope.spawn(move || {
            let mut bytes = Vec::new();
            stderr.read_to_end(&mut bytes).map(|_| bytes)
        });
        let mut output = None;
        let status = loop {
            if let Err(error) = ctx.check() {
                let _ = child.kill();
                let _ = child.wait();
                break Err(error);
            }
            if output.is_none() {
                match output_receiver.try_recv() {
                    Ok(result) => output = Some(result),
                    Err(std::sync::mpsc::TryRecvError::Empty) => {}
                    Err(std::sync::mpsc::TryRecvError::Disconnected) => {
                        let _ = child.kill();
                        let _ = child.wait();
                        break Err(anyhow!("Git output reader stopped unexpectedly"));
                    }
                }
            }
            if output.as_ref().is_some_and(Result::is_err) {
                let _ = child.kill();
                break child.wait().map_err(Into::into);
            }
            match child.try_wait() {
                Ok(Some(status)) => break Ok(status),
                Ok(None) => {
                    // Wake as soon as parsing completes instead of imposing a
                    // 10ms floor on every short Git process (especially merges).
                    if output.is_none() {
                        match output_receiver.recv_timeout(Duration::from_millis(10)) {
                            Ok(result) => output = Some(result),
                            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {}
                            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                                let _ = child.kill();
                                let _ = child.wait();
                                break Err(anyhow!("Git output reader stopped unexpectedly"));
                            }
                        }
                    } else {
                        thread::sleep(Duration::from_millis(1));
                    }
                }
                Err(error) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    break Err(error.into());
                }
            }
        };
        output_thread
            .join()
            .map_err(|_| anyhow!("Git output reader panicked"))?;
        let errors = error_thread
            .join()
            .map_err(|_| anyhow!("Git stderr reader panicked"))??;
        let status = status?;
        let output = match output {
            Some(output) => output,
            None => output_receiver
                .recv()
                .map_err(|_| anyhow!("Git output reader stopped unexpectedly"))?,
        }?;
        Ok(ProcessOutput {
            value: output,
            status,
            stderr: errors,
        })
    })
}

fn git_output(ctx: &GitContext<'_>, dir: &Path, args: &[&str]) -> Result<String> {
    run_git(ctx, dir, args, |mut stdout| {
        let mut bytes = Vec::new();
        stdout.read_to_end(&mut bytes)?;
        Ok(String::from_utf8_lossy(&bytes).into_owned())
    })
}

#[derive(Clone, Debug)]
struct BranchRef {
    oid: String,
    name: String,
    target: String,
}

fn branches(ctx: &GitContext<'_>, path: &Path) -> Result<Vec<BranchRef>> {
    let output = git_output(
        ctx,
        path,
        &[
            "for-each-ref",
            "--format=%(objectname)%00%(refname)%00%(symref)",
            "refs/heads/",
            "refs/remotes/",
        ],
    )?;
    output
        .lines()
        .map(|line| {
            let fields: Vec<_> = line.split('\0').collect();
            if fields.len() != 3 || !is_oid(fields[0]) {
                bail!("invalid branch-ref metadata");
            }
            Ok(BranchRef {
                oid: fields[0].to_owned(),
                name: fields[1].to_owned(),
                target: fields[2].to_owned(),
            })
        })
        .collect()
}

fn default_branch(refs: &[BranchRef]) -> Option<&BranchRef> {
    let find = |name: &str| refs.iter().find(|reference| reference.name == name);
    if let Some(reference) = find("refs/remotes/origin/HEAD") {
        return Some(reference);
    }
    if let Some(reference) = refs.iter().find(|r| {
        r.name.starts_with("refs/remotes/") && r.name.ends_with("/HEAD") && !r.target.is_empty()
    }) {
        return Some(reference);
    }
    for name in ["refs/remotes/origin/main", "refs/remotes/origin/master"] {
        if let Some(reference) = find(name) {
            return Some(reference);
        }
    }
    for branch in ["main", "master"] {
        if let Some(reference) = refs.iter().find(|r| {
            r.name.starts_with("refs/remotes/") && r.name.ends_with(&format!("/{branch}"))
        }) {
            return Some(reference);
        }
    }
    for name in ["refs/heads/main", "refs/heads/master"] {
        if let Some(reference) = find(name) {
            return Some(reference);
        }
    }
    None
}

/// Return a fully resolved commit OID, never an ambiguous short ref name.
pub fn detect_default_branch(path: &Path) -> String {
    let cancel = AtomicBool::new(false);
    let ctx = GitContext::new(&cancel);
    let Ok(refs) = branches(&ctx, path) else {
        return String::new();
    };
    default_branch(&refs)
        .map(|r| r.oid.clone())
        .unwrap_or_default()
}

#[derive(Clone)]
struct Metadata {
    record: CommitRecord,
    tree: String,
    parents: Vec<String>,
    trailers: Vec<String>,
}

/// Collect locally available branch history under one cancelable deadline.
/// The caller selects landed-only or all-branches from the returned records.
pub fn scan_repository(
    repo: &Repository,
    options: &CollectOptions,
    cancel: &AtomicBool,
) -> Result<ScanData> {
    scan_repository_with_progress(repo, options, cancel, &|_| {})
}

pub(crate) fn scan_repository_with_progress(
    repo: &Repository,
    options: &CollectOptions,
    cancel: &AtomicBool,
    report: &Reporter<'_>,
) -> Result<ScanData> {
    let mut ctx = GitContext::new(cancel);
    ctx.report = report;
    report(ScanStage::ReadingHistory);
    let version_text = git_output(&ctx, &repo.path, &["--version"])?;
    let version = parse_git_version(&version_text)
        .ok_or_else(|| anyhow!("unrecognized Git version: {}", version_text.trim()))?;
    scan_repository_with_version(repo, options, &ctx, version)
}

fn parse_git_version(text: &str) -> Option<(u32, u32, u32)> {
    let version = text
        .trim()
        .strip_prefix("git version ")?
        .split_whitespace()
        .next()?;
    let mut parts = version.split('.');
    let major = parts.next()?.parse().ok()?;
    let minor = parts.next()?.parse().ok()?;
    let patch = parts
        .next()
        .unwrap_or("0")
        .chars()
        .take_while(char::is_ascii_digit)
        .collect::<String>()
        .parse()
        .ok()?;
    Some((major, minor, patch))
}

fn is_partial_clone(ctx: &GitContext<'_>, path: &Path) -> Result<bool> {
    // Configuration inspection does not traverse objects or trigger fetching.
    // Honor the last value of each key, as Git does for these settings.
    let output = git_output(ctx, path, &["config", "--null", "--list"])?;
    let settings: HashMap<_, _> = output
        .split('\0')
        .filter(|entry| !entry.is_empty())
        .map(|entry| entry.split_once('\n').unwrap_or((entry, "")))
        .collect();
    Ok(settings.into_iter().any(|(key, value)| {
        let key = key.to_ascii_lowercase();
        key == "extensions.partialclone"
            || (key.starts_with("remote.")
                && (key.ends_with(".partialclonefilter")
                    || (key.ends_with(".promisor")
                        && !matches!(
                            value.to_ascii_lowercase().as_str(),
                            "false" | "no" | "off" | "0"
                        ))))
    }))
}

fn scan_repository_with_version(
    repo: &Repository,
    options: &CollectOptions,
    ctx: &GitContext<'_>,
    version: (u32, u32, u32),
) -> Result<ScanData> {
    if version < (2, 31, 0) {
        bail!("Big Board requires Git 2.31 or newer for reliable history collection");
    }
    let partial = is_partial_clone(ctx, &repo.path)?;
    // Git 2.45.1 introduced GIT_NO_LAZY_FETCH (also backported to some
    // maintenance releases). Conservatively skip older partial clones instead
    // of assuming an arbitrary vendor build honors the variable.
    if partial && version < (2, 45, 1) {
        ctx.check()?;
        bail!(
            "Partial clone skipped: Git 2.45.1 or newer is required to disable automatic object fetching reliably. No remote history was fetched."
        );
    }
    match collect_available_history(repo, options, ctx) {
        Err(error) if partial => {
            ctx.check()?;
            Err(error.context(
                "Partial clone scan failed with automatic object fetching disabled; required history may be unavailable locally",
            ))
        }
        result => result,
    }
}

fn collect_available_history(
    repo: &Repository,
    options: &CollectOptions,
    ctx: &GitContext<'_>,
) -> Result<ScanData> {
    let refs = branches(ctx, &repo.path)?;
    if refs.is_empty() {
        return Ok(ScanData::default());
    }
    let mut tips: Vec<_> = refs.iter().map(|r| r.oid.clone()).collect();
    tips.sort();
    tips.dedup();
    let default = default_branch(&refs);
    let mut warnings = Vec::new();
    let landed: HashSet<String> = if let Some(reference) = default {
        git_output(ctx, &repo.path, &["rev-list", &reference.oid, "--"])?
            .lines()
            .map(str::to_owned)
            .collect()
    } else {
        warnings.push("No available default branch could be identified; landed history is unknown. All locally available branches remain available in All branches.".into());
        HashSet::new()
    };
    let shallow_path = git_output(
        ctx,
        &repo.path,
        &[
            "rev-parse",
            "--path-format=absolute",
            "--git-path",
            "shallow",
        ],
    )?;
    let shallow: HashSet<_> = match std::fs::read_to_string(shallow_path.trim()) {
        Ok(text) => text.lines().map(str::to_owned).collect(),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => HashSet::new(),
        Err(error) => return Err(error).context("reading shallow history boundaries"),
    };
    if !shallow.is_empty() {
        warnings.push("Shallow repository: earlier commits are unavailable and boundary-commit line counts are unknown. Totals cover available history only.".into());
    }
    let mut args = vec![
        "log",
        "--no-patch",
        "-z",
        "--no-show-signature",
        "--format=%H%x00%P%x00%aN%x00%aE%x00%ae%x00%aI%x00%(trailers:key=Co-authored-by,valueonly,unfold=true,separator=%x1f)%x00%T",
    ];
    args.extend(tips.iter().map(String::as_str));
    args.push("--");
    let mut metadata = run_git(ctx, &repo.path, &args, |stdout| {
        let mut reader = BufReader::new(stdout);
        let mut result = Vec::new();
        while let Some(oid) = read_nul(&mut reader)? {
            if oid.is_empty() {
                continue;
            }
            let mut fields = vec![oid];
            for _ in 0..7 {
                fields.push(
                    read_nul(&mut reader)?
                        .ok_or_else(|| anyhow!("truncated Git commit metadata"))?,
                );
            }
            let fields: Vec<_> = fields
                .iter()
                .map(|field| {
                    std::str::from_utf8(field).context("invalid UTF-8 in Git author metadata")
                })
                .collect::<Result<_>>()?;
            if !is_oid(fields[0]) || !is_oid(fields[7]) {
                bail!("invalid Git commit OID");
            }
            let parents: Vec<_> = fields[1].split_whitespace().map(str::to_owned).collect();
            result.push(Metadata {
                tree: fields[7].into(),
                record: CommitRecord {
                    commit_id: fields[0].into(),
                    author: fields[2].trim().into(),
                    email: fields[3].trim().into(),
                    date: DateTime::parse_from_rfc3339(fields[5])
                        .context("invalid Git author date")?,
                    added: 0,
                    removed: 0,
                    repo_id: repo.id.clone(),
                    repo_name: repo.name.clone(),
                    ai_assisted: is_ai(fields[3], &options.ai_identities)
                        || is_ai(fields[4], &options.ai_identities),
                    coauthors: Vec::new(),
                    lines_known: !shallow.contains(fields[0]),
                    landed: landed.contains(fields[0]),
                    is_merge: parents.len() > 1,
                },
                parents,
                trailers: fields[6]
                    .split(COAUTHOR_SEP)
                    .map(str::trim)
                    .filter(|s| !s.is_empty())
                    .map(str::to_owned)
                    .collect(),
            });
        }
        Ok(result)
    })?;
    apply_coauthors(ctx, repo, options, &mut metadata, &mut warnings)?;
    let counts = count_commit_changes(ctx, repo, options, &metadata)?;
    let mut result = Vec::with_capacity(metadata.len());
    let mut scratch = None;
    let total_merges = metadata
        .iter()
        .filter(|entry| entry.parents.len() >= 2 && entry.record.lines_known)
        .count();
    let mut checked_merges = 0;
    (ctx.report)(ScanStage::CheckingMerges {
        done: 0,
        total: total_merges,
    });
    for mut entry in metadata {
        ctx.check()?;
        if !entry.record.lines_known {
            result.push(entry.record);
        } else if entry.parents.len() < 2 {
            if let Some(count) = counts.get(&entry.record.commit_id) {
                entry.record.added = count.added;
                entry.record.removed = count.removed;
            }
            result.push(entry.record);
        } else {
            if scratch.is_none() {
                scratch = Some(ObjectScratch::new(ctx, &repo.path)?);
            }
            match merge_counts(ctx, repo, options, &entry, scratch.as_ref().unwrap()) {
                Ok(Some(count)) => {
                    entry.record.added = count.added;
                    entry.record.removed = count.removed;
                    if count.changed {
                        result.push(entry.record);
                    }
                }
                Ok(None) => {
                    entry.record.lines_known = false;
                    warnings.push(format!("Merge {} has conflict-resolution or unsupported merge work: authored participation is counted, but resolution line counts cannot be allocated reliably and are unknown.", &entry.record.commit_id[..12]));
                    result.push(entry.record);
                }
                Err(error) => {
                    ctx.check()?;
                    entry.record.lines_known = false;
                    warnings.push(format!(
                        "Merge {} resolution line counts are unknown: {error:#}",
                        &entry.record.commit_id[..12]
                    ));
                    result.push(entry.record);
                }
            }
            checked_merges += 1;
            if checked_merges % 25 == 0 || checked_merges == total_merges {
                (ctx.report)(ScanStage::CheckingMerges {
                    done: checked_merges,
                    total: total_merges,
                });
            }
        }
    }
    Ok(ScanData {
        records: result,
        warnings,
    })
}

const DIFF_OPTIONS: &[&str] = &[
    "--no-ext-diff",
    "--no-textconv",
    "--no-color",
    "--no-relative",
    "--diff-algorithm=myers",
    "--no-indent-heuristic",
    "--ignore-submodules=none",
    "-M50%",
    "-C50%",
    "--find-copies-harder",
    "-l0",
];

// A repository with many files makes exhaustive copy detection expensive.
// Split independent commits into bounded batches, retaining every diff option.
// The global permit also bounds CPU use when several repositories scan at once.
static ACTIVE_DIFFS: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);
struct DiffPermit;
impl DiffPermit {
    fn acquire(ctx: &GitContext<'_>) -> Result<Self> {
        let limit = thread::available_parallelism()
            .map_or(1, usize::from)
            .min(4);
        loop {
            ctx.check()?;
            if ACTIVE_DIFFS
                .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
                    (active < limit).then_some(active + 1)
                })
                .is_ok()
            {
                return Ok(Self);
            }
            thread::sleep(Duration::from_millis(5));
        }
    }
}
impl Drop for DiffPermit {
    fn drop(&mut self) {
        ACTIVE_DIFFS.fetch_sub(1, Ordering::Release);
    }
}

fn count_commit_changes(
    ctx: &GitContext<'_>,
    repo: &Repository,
    options: &CollectOptions,
    metadata: &[Metadata],
) -> Result<HashMap<String, LineCounts>> {
    let commits: Vec<_> = metadata
        .iter()
        .filter(|entry| entry.parents.len() < 2)
        .map(|entry| entry.record.commit_id.as_str())
        .collect();
    let total = commits.len();
    (ctx.report)(ScanStage::CountingChanges { done: 0, total });
    let batches: Vec<_> = commits.chunks(64).collect();
    let next = std::sync::atomic::AtomicUsize::new(0);
    let completed = std::sync::Mutex::new(0);
    let failed = AtomicBool::new(false);
    thread::scope(|scope| {
        let mut workers = Vec::new();
        for _ in 0..batches.len().min(4) {
            workers.push(scope.spawn(|| -> Result<HashMap<String, LineCounts>> {
                let mut result = HashMap::new();
                while !failed.load(Ordering::Acquire) {
                    let Some(batch) = batches.get(next.fetch_add(1, Ordering::Relaxed)) else {
                        break;
                    };
                    let _permit = DiffPermit::acquire(ctx)?;
                    let mut args = vec![
                        "log",
                        "--no-walk=unsorted",
                        "--no-merges",
                        "--root",
                        "--format=%H",
                        "--numstat",
                        "-z",
                    ];
                    args.extend(DIFF_OPTIONS);
                    args.extend_from_slice(batch);
                    args.push("--");
                    match run_git(ctx, &repo.path, &args, |stdout| {
                        parse_numstat(BufReader::new(stdout), options, true)
                    }) {
                        Ok(counts) => result.extend(counts),
                        Err(error) => {
                            failed.store(true, Ordering::Release);
                            return Err(error);
                        }
                    }
                    let mut done = completed.lock().unwrap();
                    *done += batch.len();
                    (ctx.report)(ScanStage::CountingChanges { done: *done, total });
                }
                Ok(result)
            }));
        }
        let mut result = HashMap::new();
        for worker in workers {
            result.extend(
                worker
                    .join()
                    .map_err(|_| anyhow!("Git diff worker panicked"))??,
            );
        }
        Ok(result)
    })
}

fn is_oid(value: &str) -> bool {
    matches!(value.len(), 40 | 64) && value.bytes().all(|byte| byte.is_ascii_hexdigit())
}

fn read_nul(reader: &mut impl BufRead) -> Result<Option<Vec<u8>>> {
    let mut bytes = Vec::new();
    loop {
        let available = reader.fill_buf()?;
        if available.is_empty() {
            if bytes.is_empty() {
                return Ok(None);
            }
            bail!("truncated NUL-delimited Git output");
        }
        let end = available.iter().position(|byte| *byte == 0);
        let consumed = end.map_or(available.len(), |index| index + 1);
        if bytes.len() + consumed > MAX_LOG_LINE {
            bail!("Git field exceeds the 4 MiB scanner limit");
        }
        bytes.extend_from_slice(&available[..consumed]);
        reader.consume(consumed);
        if end.is_some() {
            bytes.pop();
            return Ok(Some(bytes));
        }
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct LineCounts {
    added: i64,
    removed: i64,
    changed: bool,
}

fn parse_numstat(
    mut reader: impl BufRead,
    options: &CollectOptions,
    commit_headers: bool,
) -> Result<HashMap<String, LineCounts>> {
    let mut counts = HashMap::new();
    let mut current = String::new();
    while let Some(token) = read_nul(&mut reader)? {
        let token = token.strip_prefix(b"\n").unwrap_or(&token);
        if token.is_empty() {
            continue;
        }
        if commit_headers && std::str::from_utf8(token).is_ok_and(is_oid) {
            current = String::from_utf8(token.to_vec())?;
            counts
                .entry(current.clone())
                .or_insert(LineCounts::default());
            continue;
        }
        let fields: Vec<_> = token.splitn(3, |byte| *byte == b'\t').collect();
        if fields.len() != 3 {
            bail!("invalid Git numstat record");
        }
        let path = if fields[2].is_empty() {
            let _old = read_nul(&mut reader)?.ok_or_else(|| anyhow!("missing rename source"))?;
            read_nul(&mut reader)?.ok_or_else(|| anyhow!("missing rename destination"))?
        } else {
            fields[2].to_vec()
        };
        if !should_count_path(&String::from_utf8_lossy(&path), options.include_generated) {
            continue;
        }
        let total = counts
            .entry(current.clone())
            .or_insert(LineCounts::default());
        total.changed = true;
        if fields[0] == b"-" || fields[1] == b"-" {
            continue;
        }
        let added = std::str::from_utf8(fields[0])?.parse::<i64>()?;
        let removed = std::str::from_utf8(fields[1])?.parse::<i64>()?;
        if added < 0 || removed < 0 {
            bail!("negative Git numstat line count");
        }
        total.added = total
            .added
            .checked_add(added)
            .ok_or_else(|| anyhow!("line count overflow"))?;
        total.removed = total
            .removed
            .checked_add(removed)
            .ok_or_else(|| anyhow!("line count overflow"))?;
    }
    Ok(counts)
}

fn parsed_identity(value: &str) -> Option<Identity> {
    let mut parser = MailboxParser { rest: value.trim() };
    let email = parser.mailbox(true)?;
    parser.skip_comments()?;
    if !parser.rest.is_empty() {
        return None;
    }
    let mut names = MailboxParser { rest: value.trim() };
    let name = if names.address_spec().is_some() {
        names.space();
        names
            .rest
            .strip_prefix('(')
            .and_then(|s| s.strip_suffix(')'))
            .unwrap_or(&email)
            .to_owned()
    } else if names.rest.starts_with('<') {
        email.clone()
    } else {
        let name = names.phrase()?;
        names.space();
        if names.consume(':') {
            return parsed_identity(names.rest.trim().strip_suffix(';')?);
        }
        name
    };
    Some(Identity {
        name: if name.is_empty() { email.clone() } else { name },
        email,
    })
}

fn apply_coauthors(
    ctx: &GitContext<'_>,
    repo: &Repository,
    options: &CollectOptions,
    metadata: &mut [Metadata],
    warnings: &mut Vec<String>,
) -> Result<()> {
    let mut cache = HashMap::new();
    let mut contacts = Vec::new();
    for entry in metadata.iter() {
        for trailer in &entry.trailers {
            if cache.contains_key(trailer) {
                continue;
            }
            let identity = parsed_identity(trailer);
            if let Some(identity) = &identity {
                contacts.push((
                    trailer.clone(),
                    format!(
                        "{} <{}>",
                        identity.name.replace(['\n', '\r'], " "),
                        identity.email
                    ),
                ));
            } else {
                warnings.push(format!(
                    "Commit {} contains an invalid coauthor trailer; it was not attributed.",
                    &entry.record.commit_id[..12]
                ));
            }
            cache.insert(trailer.clone(), identity);
        }
    }
    for batch in contacts.chunks(128) {
        let mut args = vec!["check-mailmap", "--"];
        args.extend(batch.iter().map(|(_, contact)| contact.as_str()));
        let output = git_output(ctx, &repo.path, &args)?;
        let mapped: Vec<_> = output.lines().collect();
        if mapped.len() != batch.len() {
            bail!("unexpected check-mailmap output");
        }
        for ((trailer, _), mapped) in batch.iter().zip(mapped) {
            let identity = mapped.rsplit_once('<').and_then(|(name, email)| {
                Some(Identity {
                    name: name.trim().to_owned(),
                    email: email.strip_suffix('>')?.to_owned(),
                })
            });
            cache.insert(trailer.clone(), identity);
        }
    }
    for entry in metadata {
        let mut seen = HashSet::new();
        for trailer in &entry.trailers {
            let Some(Some(identity)) = cache.get(trailer) else {
                continue;
            };
            if is_ai(trailer, &options.ai_identities)
                || is_ai(&identity.email, &options.ai_identities)
            {
                entry.record.ai_assisted = true;
            } else if seen.insert(identity.email.to_lowercase()) {
                entry.record.coauthors.push(identity.clone());
            }
        }
    }
    Ok(())
}

struct ObjectScratch {
    path: PathBuf,
    alternate: String,
    disabled_drivers: Vec<String>,
}
impl ObjectScratch {
    fn new(ctx: &GitContext<'_>, repo: &Path) -> Result<Self> {
        static NEXT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
        let alternate = git_output(
            ctx,
            repo,
            &[
                "rev-parse",
                "--path-format=absolute",
                "--git-path",
                "objects",
            ],
        )?;
        let alternate = serde_json::to_string(alternate.trim())?;
        let disabled_drivers = git_output(ctx, repo, &["config", "--list", "--name-only"])?
            .lines()
            .filter(|key| key.starts_with("merge.") && key.ends_with(".driver"))
            .map(|key| format!("{key}=false"))
            .collect::<Vec<_>>();
        for _ in 0..100 {
            let path = std::env::temp_dir().join(format!(
                "bigboard-merge-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            ));
            match std::fs::create_dir(&path) {
                Ok(()) => {
                    return Ok(Self {
                        path,
                        alternate,
                        disabled_drivers,
                    });
                }
                Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(error) => return Err(error).context("creating temporary merge object storage"),
            }
        }
        bail!("unable to allocate temporary merge object storage")
    }
    fn command(&self, repo: &Path, args: &[&str]) -> Command {
        let mut command = git_command(repo, &[]);
        // Never execute repository-configured external merge drivers while
        // reconstructing history; affected resolutions become unknown instead.
        for driver in &self.disabled_drivers {
            command.args(["-c", driver]);
        }
        command
            .args(args)
            .env("GIT_OBJECT_DIRECTORY", &self.path)
            .env("GIT_ALTERNATE_OBJECT_DIRECTORIES", &self.alternate);
        command
    }
}
impl Drop for ObjectScratch {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.path);
    }
}

fn merge_counts(
    ctx: &GitContext<'_>,
    repo: &Repository,
    options: &CollectOptions,
    entry: &Metadata,
    scratch: &ObjectScratch,
) -> Result<Option<LineCounts>> {
    if entry.parents.len() != 2 {
        return Ok(None);
    }
    let child = scratch
        .command(
            &repo.path,
            &[
                "merge-tree",
                "--write-tree",
                "--no-messages",
                "-z",
                &entry.parents[0],
                &entry.parents[1],
            ],
        )
        .spawn()?;
    let output = run_child_output(ctx, child, |mut stdout| {
        let mut bytes = Vec::new();
        stdout.read_to_end(&mut bytes)?;
        Ok(bytes)
    })?;
    if output.status.code() == Some(1) {
        return Ok(None);
    }
    if !output.status.success() {
        bail!(
            "merge baseline unavailable: {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    let tree = std::str::from_utf8(
        output
            .value
            .split(|byte| *byte == 0 || *byte == b'\n')
            .next()
            .unwrap_or_default(),
    )?;
    if !is_oid(tree) {
        bail!("invalid automatic merge-tree object");
    }
    // Identical trees prove that the merge introduced no resolution changes.
    // Avoid launching a separate diff process for the common clean-merge case.
    if tree == entry.tree {
        return Ok(Some(LineCounts::default()));
    }
    let mut args = vec!["diff", "--numstat", "-z"];
    args.extend(DIFF_OPTIONS);
    args.extend([tree, &entry.record.commit_id, "--"]);
    let child = scratch.command(&repo.path, &args).spawn()?;
    let mut counts = run_child(ctx, child, |stdout| {
        parse_numstat(BufReader::new(stdout), options, false)
    })?;
    Ok(Some(counts.remove("").unwrap_or_default()))
}

fn should_count_path(path: &str, include_generated: bool) -> bool {
    if include_generated {
        return true;
    }
    if path
        .split('/')
        .any(|segment| IGNORED_DIRS.contains(&segment))
    {
        return false;
    }
    let basename = path.rsplit('/').next().unwrap_or(path);
    static PATTERNS: std::sync::LazyLock<Vec<glob::Pattern>> = std::sync::LazyLock::new(|| {
        IGNORED_FILE_GLOBS
            .iter()
            .map(|pattern| glob::Pattern::new(pattern).expect("valid built-in glob"))
            .collect()
    });
    !PATTERNS.iter().any(|pattern| pattern.matches(basename))
}

// Extract a single mailbox using the same address grammar as Go's net/mail:
// comments follow a mailbox or a display-name word, quoted local parts are
// unescaped, and a list (or a multi-member group) is rejected. Keep the original
// fallback text when parsing fails; even that fallback affects AI matching.
fn normalize_address(value: &str) -> String {
    let value = value.trim().to_lowercase();
    let mut parser = MailboxParser { rest: &value };
    if let Some(address) = parser.mailbox(true)
        && parser.skip_comments().is_some()
        && parser.rest.is_empty()
    {
        return address;
    }
    value.trim_matches(['<', '>', ' ']).to_owned()
}

#[derive(Clone, Copy)]
struct MailboxParser<'a> {
    rest: &'a str,
}

impl MailboxParser<'_> {
    fn space(&mut self) {
        self.rest = self.rest.trim_start_matches([' ', '\t']);
    }

    fn consume(&mut self, token: char) -> bool {
        if let Some(rest) = self.rest.strip_prefix(token) {
            self.rest = rest;
            true
        } else {
            false
        }
    }

    fn skip_comments(&mut self) -> Option<()> {
        self.space();
        while self.consume('(') {
            let mut depth = 1usize;
            let mut escaped = false;
            let mut consumed = 0;
            for (index, ch) in self.rest.char_indices() {
                consumed = index + ch.len_utf8();
                if escaped {
                    escaped = false;
                } else {
                    match ch {
                        '\\' => escaped = true,
                        '(' => depth += 1,
                        ')' => depth -= 1,
                        _ => {}
                    }
                }
                if depth == 0 {
                    break;
                }
            }
            if depth != 0 {
                return None;
            }
            self.rest = &self.rest[consumed..];
            self.space();
        }
        Some(())
    }

    fn quoted(&mut self) -> Option<String> {
        let remaining = self.rest.strip_prefix('"')?;
        let mut result = String::new();
        let mut escaped = false;
        for (index, ch) in remaining.char_indices() {
            if !escaped && ch == '"' {
                self.rest = &remaining[index + 1..];
                return Some(result);
            }
            if !escaped && ch == '\\' {
                escaped = true;
                continue;
            }
            if !(matches!(ch, ' '..='~' | '\t') || !ch.is_ascii()) {
                return None;
            }
            result.push(ch);
            escaped = false;
        }
        None
    }

    fn atom(&mut self, permissive_dots: bool) -> Option<String> {
        let end = self
            .rest
            .char_indices()
            .find_map(|(index, ch)| {
                let special = "()<>[]:;@\\,\"".contains(ch);
                let visible = matches!(ch, '!'..='~') || !ch.is_ascii();
                (!visible || special).then_some(index)
            })
            .unwrap_or(self.rest.len());
        let atom = &self.rest[..end];
        if atom.is_empty()
            || (!permissive_dots
                && (atom.starts_with('.') || atom.ends_with('.') || atom.contains("..")))
        {
            return None;
        }
        self.rest = &self.rest[end..];
        Some(atom.to_owned())
    }

    fn address_spec(&mut self) -> Option<String> {
        // Work on a copy so a failed addr-spec can be retried as a name-addr.
        let mut parser = *self;
        parser.space();
        let local = if parser.rest.starts_with('"') {
            parser.quoted()?
        } else {
            parser.atom(false)?
        };
        if local.is_empty() || !parser.consume('@') {
            return None;
        }
        parser.space();
        let domain = if parser.consume('[') {
            let end = parser.rest.find(']')?;
            let address = &parser.rest[..end];
            // The Go source accepts IPv4 literals and an uppercase IPv6 prefix.
            // Input has already been lowercased by normalize_address, as in Go.
            if let Some(ipv6) = address.strip_prefix("IPv6:") {
                ipv6.parse::<std::net::Ipv6Addr>().ok()?;
            } else {
                address.parse::<std::net::Ipv4Addr>().ok()?;
            }
            parser.rest = &parser.rest[end + 1..];
            format!("[{address}]")
        } else {
            parser.atom(false)?
        };
        *self = parser;
        Some(format!("{local}@{domain}"))
    }

    fn phrase(&mut self) -> Option<String> {
        let mut words = Vec::new();
        loop {
            if !words.is_empty() {
                self.skip_comments()?;
            }
            self.space();
            let word = if self.rest.starts_with('"') {
                self.quoted()
            } else {
                self.atom(true)
            };
            let Some(word) = word else { break };
            words.push(word);
        }
        (!words.is_empty()).then(|| words.join(" "))
    }

    fn mailbox(&mut self, allow_group: bool) -> Option<String> {
        self.space();
        if let Some(address) = self.address_spec() {
            return Some(address);
        }
        if !self.rest.starts_with('<') {
            self.phrase()?;
        }
        self.space();
        if allow_group && self.consume(':') {
            let address = self.mailbox(false)?;
            self.skip_comments()?;
            return self.consume(';').then_some(address);
        }
        if !self.consume('<') {
            return None;
        }
        let address = self.address_spec()?;
        self.consume('>').then_some(address)
    }
}

fn is_ai(value: &str, identities: &[String]) -> bool {
    let address = normalize_address(value);
    if address.is_empty() {
        return false;
    }
    for entry in identities {
        let entry = entry.trim().to_lowercase();
        if (!entry.is_empty() && entry == address)
            || (entry.starts_with('@') && address.ends_with(&entry))
        {
            return true;
        }
    }
    if AI_ADDRESSES.contains(&address.as_str()) {
        return true;
    }
    if let Some(local) = address.strip_suffix("@users.noreply.github.com") {
        let local = local.split_once('+').map_or(local, |(_, value)| value);
        let local = local.strip_suffix("[bot]").unwrap_or(local);
        return AI_GITHUB_USERS.contains(&local);
    }
    false
}

pub fn new_repositories(paths: &[PathBuf]) -> Vec<Repository> {
    let mut seen = HashSet::new();
    let mut repos = Vec::new();
    for path in paths {
        let path = absolute_path(path);
        if seen.insert(path.clone()) {
            repos.push(Repository {
                id: path.to_string_lossy().into_owned(),
                path,
                name: String::new(),
            });
        }
    }
    for i in 0..repos.len() {
        repos[i].name = shortest_unique_name(&repos[i].path, &repos);
    }
    repos
}

fn absolute_path(path: &Path) -> PathBuf {
    let path = if path.is_absolute() {
        path.to_owned()
    } else {
        let Ok(current) = std::env::current_dir() else {
            return path.to_owned();
        };
        current.join(path)
    };
    let mut result = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                result.pop();
            }
            _ => result.push(component.as_os_str()),
        }
    }
    result
}

fn path_parts(path: &Path) -> Vec<String> {
    let parts: Vec<_> = path
        .components()
        .filter_map(|component| match component {
            Component::Normal(part) => Some(part.to_string_lossy().into_owned()),
            _ => None,
        })
        .collect();
    if parts.is_empty() {
        vec![path.to_string_lossy().replace('\\', "/")]
    } else {
        parts
    }
}

fn path_suffix(parts: &[String], depth: usize) -> String {
    parts[parts.len().saturating_sub(depth)..].join("/")
}

fn shortest_unique_name(path: &Path, repos: &[Repository]) -> String {
    let parts = path_parts(path);
    for depth in 1..=parts.len() {
        let candidate = path_suffix(&parts, depth);
        if repos
            .iter()
            .filter(|repo| repo.path != path)
            .all(|repo| path_suffix(&path_parts(&repo.path), depth) != candidate)
        {
            return candidate;
        }
    }
    path.to_string_lossy().replace('\\', "/")
}

fn is_git_repo(path: &Path) -> bool {
    std::fs::metadata(path.join(".git")).is_ok()
}

fn is_worktree(path: &Path) -> bool {
    let git_path = path.join(".git");
    if !std::fs::symlink_metadata(&git_path).is_ok_and(|m| !m.is_dir()) {
        return false;
    }
    let Ok(contents) = std::fs::read_to_string(git_path) else {
        return false;
    };
    let Some(git_dir) = contents.strip_prefix("gitdir:").map(str::trim) else {
        return false;
    };
    // A linked worktree's private Git directory has a commondir backlink.
    // Separate-git-dir repositories and submodules use gitfiles without it.
    path.join(git_dir).join("commondir").is_file()
}

/// Discover in deterministic directory order, following and deduplicating
/// symlinked repositories. Descent stops at repositories and skips dot folders.
pub fn discover_repos_depth(paths: &[PathBuf], max_depth: usize) -> Vec<PathBuf> {
    fn walk(
        path: PathBuf,
        depth: usize,
        max_depth: usize,
        seen: &mut HashSet<PathBuf>,
        result: &mut Vec<PathBuf>,
    ) {
        if is_git_repo(&path) {
            if !is_worktree(&path) {
                let key = std::fs::canonicalize(&path).unwrap_or_else(|_| path.clone());
                if seen.insert(key) {
                    result.push(path);
                }
            }
            return;
        }
        if depth >= max_depth {
            return;
        }
        let Ok(entries) = std::fs::read_dir(&path) else {
            return;
        };
        let mut entries: Vec<_> = entries.filter_map(Result::ok).collect();
        entries.sort_by_key(|entry| entry.file_name());
        for entry in entries {
            if entry.file_name().to_string_lossy().starts_with('.') {
                continue;
            }
            let Ok(kind) = entry.file_type() else {
                continue;
            };
            let mut child = entry.path();
            if !kind.is_dir() {
                if !kind.is_symlink() {
                    continue;
                }
                let Ok(resolved) = std::fs::canonicalize(&child) else {
                    continue;
                };
                if !resolved.is_dir() {
                    continue;
                }
                child = resolved;
            }
            walk(child, depth + 1, max_depth, seen, result);
        }
    }
    let mut seen = HashSet::new();
    let mut result = Vec::new();
    for path in paths {
        walk(absolute_path(path), 0, max_depth, &mut seen, &mut result);
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    fn git(dir: &Path, args: &[&str]) -> String {
        let output = Command::new("git")
            .args(args)
            .current_dir(dir)
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "git {args:?}: {}",
            String::from_utf8_lossy(&output.stderr)
        );
        String::from_utf8_lossy(&output.stdout).into_owned()
    }

    fn init(dir: &Path) {
        std::fs::create_dir_all(dir).unwrap();
        git(dir, &["init", "-b", "main"]);
        git(dir, &["config", "user.name", "Test User"]);
        git(dir, &["config", "user.email", "test@example.com"]);
        git(dir, &["config", "commit.gpgsign", "false"]);
        git(dir, &["config", "log.showRoot", "true"]);
    }

    fn commit(dir: &Path, filename: &str, content: &str, message: &str) {
        let path = dir.join(filename);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(path, content).unwrap();
        git(dir, &["add", "-A"]);
        git(dir, &["commit", "-m", message]);
    }

    fn collect(dir: &Path, options: &CollectOptions) -> Vec<CommitRecord> {
        scan_repository(
            &new_repositories(&[dir.to_owned()])[0],
            options,
            &AtomicBool::new(false),
        )
        .unwrap()
        .records
    }

    #[test]
    fn collection_counts_lines_filters_generated_and_honors_mailmap() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "one.go", "one\ntwo\nthree\n", "first");
        git(root.path(), &["config", "user.name", "T. User"]);
        git(root.path(), &["config", "user.email", "other@example.com"]);
        commit(
            root.path(),
            "vendor/naïve.go",
            "generated\ngenerated\n",
            "vendored",
        );
        std::fs::write(
            root.path().join(".mailmap"),
            "Test User <test@example.com> <other@example.com>\n",
        )
        .unwrap();
        let records = collect(root.path(), &CollectOptions::default());
        assert_eq!(records.len(), 2);
        assert_eq!(records.iter().map(|record| record.added).sum::<i64>(), 3);
        assert!(
            records
                .iter()
                .all(|record| record.author == "Test User" && record.email == "test@example.com")
        );
        let records = collect(
            root.path(),
            &CollectOptions {
                include_generated: true,
                ..Default::default()
            },
        );
        assert_eq!(records.iter().map(|record| record.added).sum::<i64>(), 5);
    }

    #[test]
    fn collection_detects_ai_authors_and_multiple_coauthors() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "human.go", "human\n", "plain");
        commit(
            root.path(),
            "ai.go",
            "AI\n",
            "AI commit\n\nCo-authored-by: Pat <pat@example.com>\nCo-authored-by: Claude <noreply@anthropic.com>",
        );
        git(
            root.path(),
            &[
                "config",
                "user.email",
                "198982749+Copilot@users.noreply.github.com",
            ],
        );
        commit(root.path(), "agent.go", "agent\n", "agent-authored");
        let records = collect(root.path(), &CollectOptions::default());
        assert_eq!(records.len(), 3);
        assert_eq!(
            records.iter().filter(|record| record.ai_assisted).count(),
            2
        );
    }

    #[test]
    fn default_branch_remote_only_and_empty_repository() {
        let root = TempDir::new().unwrap();
        init(root.path());
        assert!(collect(root.path(), &CollectOptions::default()).is_empty());
        commit(root.path(), "a.go", "a\n", "first");
        git(
            root.path(),
            &["update-ref", "refs/remotes/origin/main", "HEAD"],
        );
        git(
            root.path(),
            &[
                "symbolic-ref",
                "refs/remotes/origin/HEAD",
                "refs/remotes/origin/main",
            ],
        );
        git(root.path(), &["checkout", "--detach", "HEAD"]);
        git(root.path(), &["branch", "-D", "main"]);
        assert_eq!(
            detect_default_branch(root.path()),
            git(root.path(), &["rev-parse", "HEAD"]).trim()
        );
        assert_eq!(collect(root.path(), &CollectOptions::default()).len(), 1);
    }

    #[test]
    fn cancellation_and_deadline_are_observed() {
        let root = TempDir::new().unwrap();
        init(root.path());
        let repo = new_repositories(&[root.path().to_owned()]).remove(0);
        let error =
            scan_repository(&repo, &CollectOptions::default(), &AtomicBool::new(true)).unwrap_err();
        assert!(error.to_string().contains("canceled"));
        let cancel = AtomicBool::new(false);
        let ctx = GitContext {
            deadline: Instant::now() - Duration::from_secs(1),
            cancel: &cancel,
            report: &|_| {},
        };
        assert!(
            git_output(&ctx, root.path(), &["status"])
                .unwrap_err()
                .to_string()
                .contains("deadline")
        );
    }

    #[test]
    fn invalid_repository_reports_git_stderr() {
        let root = TempDir::new().unwrap();
        let repo = new_repositories(&[root.path().to_owned()]).remove(0);
        let error = scan_repository(&repo, &CollectOptions::default(), &AtomicBool::new(false))
            .unwrap_err();
        assert!(error.to_string().contains("not a git repository"));
    }

    #[test]
    fn repository_names_are_unique_and_paths_are_deduplicated() {
        let repos = new_repositories(&[
            PathBuf::from("/workspace/org-a/api"),
            PathBuf::from("/workspace/org-b/api"),
            PathBuf::from("/workspace/web"),
            PathBuf::from("/workspace/org-a/api"),
        ]);
        assert_eq!(repos.len(), 3);
        assert_eq!(
            repos
                .iter()
                .map(|repo| repo.name.as_str())
                .collect::<Vec<_>>(),
            ["org-a/api", "org-b/api", "web"]
        );
        assert!(
            repos
                .iter()
                .all(|repo| repo.path.is_absolute() && repo.id == repo.path.to_string_lossy())
        );
    }

    #[test]
    fn discovery_depth_and_worktree_behavior() {
        let root = TempDir::new().unwrap();
        let repo = root.path().join("project");
        init(&repo);
        commit(&repo, "a.go", "a\n", "first");
        let nested = root.path().join("group").join("nested");
        init(&nested);
        let hidden = root.path().join(".hidden");
        init(&hidden);
        let worktree = root.path().join("project-wt");
        git(
            &repo,
            &[
                "worktree",
                "add",
                "-b",
                "feature",
                worktree.to_str().unwrap(),
                "HEAD",
            ],
        );
        assert_eq!(
            discover_repos_depth(&[root.path().to_owned()], 1),
            vec![repo.clone()]
        );
        assert_eq!(
            discover_repos_depth(&[root.path().to_owned()], 2),
            vec![nested, repo.clone()]
        );
        assert!(discover_repos_depth(&[worktree], 1).is_empty());
        assert_eq!(
            discover_repos_depth(&[repo.clone(), repo.clone()], 0),
            vec![repo]
        );
    }

    #[cfg(unix)]
    #[test]
    fn discovery_follows_symlinks_and_deduplicates_targets() {
        let root = TempDir::new().unwrap();
        let repo = root.path().join("repo");
        init(&repo);
        let alias = root.path().join("alias");
        std::os::unix::fs::symlink(&repo, &alias).unwrap();
        assert_eq!(discover_repos_depth(&[alias.clone(), repo], 1), vec![alias]);
        assert_eq!(discover_repos_depth(&[root.path().to_owned()], 1).len(), 1);
    }

    #[test]
    fn default_branch_is_not_confused_by_a_same_named_tag() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "a.go", "a\n", "first");
        git(root.path(), &["tag", "main"]);
        commit(root.path(), "b.go", "b\n", "second");
        assert_eq!(
            detect_default_branch(root.path()),
            git(root.path(), &["rev-parse", "refs/heads/main"]).trim()
        );
        assert_eq!(collect(root.path(), &CollectOptions::default()).len(), 2);
    }

    #[test]
    fn remote_default_precedes_stale_local_branch() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "a.go", "a\n", "first");
        git(root.path(), &["checkout", "-b", "remote-tip"]);
        commit(root.path(), "b.go", "b\n", "second");
        git(
            root.path(),
            &["update-ref", "refs/remotes/origin/main", "HEAD"],
        );
        git(
            root.path(),
            &[
                "symbolic-ref",
                "refs/remotes/origin/HEAD",
                "refs/remotes/origin/main",
            ],
        );
        git(root.path(), &["checkout", "main"]);
        let records = collect(root.path(), &CollectOptions::default());
        assert_eq!(records.len(), 2);
        assert!(records.iter().all(|record| record.landed));
    }

    #[cfg(unix)]
    #[test]
    fn nul_delimited_vendor_paths_are_filtered() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "normal.go", "one\n", "normal");
        commit(
            root.path(),
            "vendor/bad\tfile.go",
            "one\ntwo\nthree\n",
            "quoted",
        );
        assert_eq!(
            collect(root.path(), &CollectOptions::default())
                .iter()
                .map(|record| record.added)
                .sum::<i64>(),
            1
        );
    }

    #[test]
    fn separate_git_directory_is_discovered() {
        let root = TempDir::new().unwrap();
        let repo = root.path().join("project");
        let metadata = root.path().join("metadata");
        std::fs::create_dir(&repo).unwrap();
        git(
            &repo,
            &[
                "init",
                "-b",
                "main",
                "--separate-git-dir",
                metadata.to_str().unwrap(),
            ],
        );
        assert_eq!(
            discover_repos_depth(std::slice::from_ref(&repo), 1),
            vec![repo]
        );
    }

    #[test]
    fn vendor_employee_email_is_not_ai() {
        assert!(!is_ai("Human Dev <human.employee@openai.com>", &[]));
        assert!(!is_ai("Eve <person@openai.com.evil.example>", &[]));
        assert!(is_ai(
            "Worker <agent@custom.example>",
            &["@custom.example".to_owned()]
        ));
        assert!(!is_ai("agent@custom.example", &[]));
        assert!(is_ai(
            "Claude Opus 4.6 (1M context) <noreply@anthropic.com>",
            &[]
        ));
        assert!(!is_ai(
            "49699333+dependabot[bot]@users.noreply.github.com",
            &[]
        ));
    }

    #[test]
    fn nul_parser_handles_renames_literal_arrows_and_binary_files() {
        let bytes = b"3\t1\t\0old\tname\0src/new\nname.go\0-\t-\timage.png\0";
        let counts = parse_numstat(
            BufReader::new(bytes.as_slice()),
            &CollectOptions::default(),
            false,
        )
        .unwrap();
        assert_eq!((counts[""].added, counts[""].removed), (3, 1));
        assert!(!should_count_path("vendor/x => src/x.go", false));
        assert!(should_count_path("src/{a => b}.go", false));
        assert!(!should_count_path("src/build/x.go", false));
    }

    #[test]
    fn scanner_rejects_oversized_fields() {
        let bytes = vec![b'a'; MAX_LOG_LINE + 1];
        assert!(read_nul(&mut BufReader::new(bytes.as_slice())).is_err());
    }

    #[cfg(unix)]
    fn sleeping_child() -> std::process::Child {
        Command::new("sleep")
            .arg("30")
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap()
    }

    #[cfg(unix)]
    fn assert_process_reaped(pid: u32) {
        let running = Command::new("kill")
            .args(["-0", &pid.to_string()])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .unwrap()
            .success();
        assert!(!running, "child {pid} was not killed and reaped");
    }

    #[cfg(unix)]
    fn read_child_output(mut stdout: std::process::ChildStdout) -> Result<Vec<u8>> {
        let mut bytes = Vec::new();
        stdout.read_to_end(&mut bytes)?;
        Ok(bytes)
    }

    #[cfg(unix)]
    #[test]
    fn cancellation_kills_and_reaps_a_running_process() {
        let cancel = AtomicBool::new(false);
        let child = sleeping_child();
        let pid = child.id();
        let started = Instant::now();
        let error = thread::scope(|scope| {
            scope.spawn(|| {
                thread::sleep(Duration::from_millis(30));
                cancel.store(true, Ordering::Release);
            });
            run_child(&GitContext::new(&cancel), child, read_child_output).unwrap_err()
        });
        assert!(error.to_string().contains("canceled"));
        assert!(started.elapsed() < Duration::from_secs(2));
        assert_process_reaped(pid);
    }

    #[cfg(unix)]
    #[test]
    fn timeout_kills_and_reaps_a_running_process() {
        let cancel = AtomicBool::new(false);
        let child = sleeping_child();
        let pid = child.id();
        let started = Instant::now();
        let ctx = GitContext {
            deadline: started + Duration::from_millis(30),
            cancel: &cancel,
            report: &|_| {},
        };
        let error = run_child(&ctx, child, read_child_output).unwrap_err();
        assert!(error.to_string().contains("deadline exceeded"));
        assert!(started.elapsed() < Duration::from_secs(2));
        assert_process_reaped(pid);
    }

    #[cfg(unix)]
    #[test]
    fn parser_failure_kills_and_reaps_a_running_process() {
        let cancel = AtomicBool::new(false);
        let child = sleeping_child();
        let pid = child.id();
        let started = Instant::now();
        let error = run_child::<Vec<u8>, _>(&GitContext::new(&cancel), child, |_| {
            anyhow::bail!("deliberate output parser failure")
        })
        .unwrap_err();
        assert!(
            error
                .to_string()
                .contains("deliberate output parser failure")
        );
        assert!(started.elapsed() < Duration::from_secs(2));
        assert_process_reaped(pid);
    }

    #[cfg(unix)]
    #[test]
    fn stderr_is_drained_while_stdout_is_read() {
        let cancel = AtomicBool::new(false);
        let child = Command::new("sh")
            .args(["-c", "head -c 1048576 /dev/zero >&2; printf done"])
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        assert_eq!(
            run_child(&GitContext::new(&cancel), child, read_child_output).unwrap(),
            b"done"
        );
    }
    #[test]
    fn mailbox_normalization_matches_go_comments_quotes_and_groups() {
        // Expected strings were checked against Go net/mail.ParseAddress, then
        // the original normalizeAddress fallback, rather than inferred from RFCs.
        let cases = [
            ("noreply@anthropic.com (Claude)", "noreply@anthropic.com"),
            (
                "Claude <noreply@anthropic.com> (Claude)",
                "noreply@anthropic.com",
            ),
            (
                "\"Claude (Assistant)\" <noreply@anthropic.com>",
                "noreply@anthropic.com",
            ),
            ("\"noreply\"@anthropic.com", "noreply@anthropic.com"),
            ("\"copilot\"@github.com", "copilot@github.com"),
            ("\"co pilot\"@github.com", "co pilot@github.com"),
            (
                "\"Claude, Assistant\" <noreply@anthropic.com>",
                "noreply@anthropic.com",
            ),
            (
                "Claude <noreply@anthropic.com>, Other <human@example.com>",
                "claude <noreply@anthropic.com>, other <human@example.com",
            ),
            (
                "Claude <noreply@anthropic.com> invalid",
                "claude <noreply@anthropic.com> invalid",
            ),
            (
                "Claude <noreply@anthropic.com> (nested (comment))",
                "noreply@anthropic.com",
            ),
            (
                "Claude <noreply@anthropic.com> (unterminated",
                "claude <noreply@anthropic.com> (unterminated",
            ),
            (
                "noreply(comment)@anthropic.com",
                "noreply(comment)@anthropic.com",
            ),
            (
                "(comment)noreply@anthropic.com",
                "(comment)noreply@anthropic.com",
            ),
            ("group: noreply@anthropic.com;", "noreply@anthropic.com"),
            (
                "group: noreply@anthropic.com, human@example.com;",
                "group: noreply@anthropic.com, human@example.com;",
            ),
            ("<noreply@anthropic.com>", "noreply@anthropic.com"),
            (
                "Claude (some) <noreply@anthropic.com>",
                "noreply@anthropic.com",
            ),
            (
                "Claude Opus 4.6 (1M context) <noreply@anthropic.com>",
                "noreply@anthropic.com",
            ),
        ];
        for (input, expected) in cases {
            assert_eq!(normalize_address(input), expected, "{input}");
        }
        assert!(is_ai("noreply@anthropic.com (Claude)", &[]));
        assert!(is_ai("\"copilot\"@github.com", &[]));
        assert!(!is_ai(
            "Claude <noreply@anthropic.com>, Other <human@example.com>",
            &[]
        ));
        assert!(!is_ai("Claude <noreply@anthropic.com> invalid", &[]));
    }
    fn scan(dir: &Path) -> ScanData {
        scan_repository(
            &new_repositories(&[dir.to_owned()])[0],
            &CollectOptions::default(),
            &AtomicBool::new(false),
        )
        .unwrap()
    }

    #[test]
    fn all_branch_history_excludes_tag_only_commits_and_marks_landed() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "base.go", "base\n", "base");
        git(root.path(), &["checkout", "-b", "feature"]);
        commit(root.path(), "feature.go", "feature\n", "feature");
        let feature = git(root.path(), &["rev-parse", "HEAD"]).trim().to_owned();
        git(root.path(), &["checkout", "main"]);
        commit(root.path(), "main.go", "main\n", "main");
        git(root.path(), &["checkout", "--orphan", "tag-only"]);
        git(root.path(), &["rm", "-rf", "."]);
        commit(root.path(), "tag.go", "tag only\n", "tag only");
        let tag = git(root.path(), &["rev-parse", "HEAD"]).trim().to_owned();
        git(root.path(), &["tag", "archive"]);
        git(root.path(), &["checkout", "main"]);
        git(root.path(), &["branch", "-D", "tag-only"]);
        let data = scan(root.path());
        assert_eq!(data.records.len(), 3);
        assert_eq!(data.records.iter().filter(|r| r.landed).count(), 2);
        assert!(
            !data
                .records
                .iter()
                .find(|r| r.commit_id == feature)
                .unwrap()
                .landed
        );
        assert!(!data.records.iter().any(|r| r.commit_id == tag));
    }

    #[test]
    fn arbitrary_feature_head_is_not_assumed_to_be_default() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "base.go", "base\n", "base");
        git(root.path(), &["branch", "-m", "feature"]);
        let data = scan(root.path());
        assert_eq!(data.records.len(), 1);
        assert!(!data.records[0].landed);
        assert!(
            data.warnings
                .iter()
                .any(|w| w.contains("No available default branch"))
        );
    }

    #[test]
    fn coauthors_are_parsed_mailmapped_deduplicated_and_ai_is_separate() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(
            root.path(),
            "a.go",
            "a\n",
            "pairing\n\nCo-authored-by: Pat <old@example.com>\nCo-authored-by: Pat Canonical <pat@example.com>\nCo-authored-by: noreply@anthropic.com (Claude)\nCo-authored-by: Vendor Employee <human@openai.com>",
        );
        std::fs::write(
            root.path().join(".mailmap"),
            "Pat Canonical <pat@example.com> <old@example.com>\n",
        )
        .unwrap();
        let records = scan(root.path()).records;
        assert!(records[0].ai_assisted);
        assert_eq!(records[0].coauthors.len(), 2);
        assert!(records[0].coauthors.contains(&Identity {
            name: "Pat Canonical".into(),
            email: "pat@example.com".into()
        }));
        assert!(
            records[0]
                .coauthors
                .iter()
                .any(|i| i.email == "human@openai.com")
        );
    }

    #[test]
    fn copy_and_unusual_rename_paths_count_only_actual_line_edits() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "old\tname.go", "one\ntwo\nthree\n", "base");
        std::fs::copy(
            root.path().join("old\tname.go"),
            root.path().join("copy.go"),
        )
        .unwrap();
        git(root.path(), &["add", "-A"]);
        git(root.path(), &["commit", "-m", "copy unchanged file"]);
        git(root.path(), &["mv", "old\tname.go", "new\nname.go"]);
        commit(
            root.path(),
            "new\nname.go",
            "one\ntwo\nthree\nfour\n",
            "rename and edit",
        );
        let records = scan(root.path()).records;
        assert_eq!(records.iter().map(|r| r.added).sum::<i64>(), 4);
        assert_eq!(records.iter().map(|r| r.removed).sum::<i64>(), 0);
    }

    #[test]
    fn repository_environment_does_not_redirect_scan() {
        const CHILD_DIR: &str = "BIGBOARD_ROUTING_TEST_DIR";
        if let Some(path) = std::env::var_os(CHILD_DIR) {
            let records = collect(Path::new(&path), &CollectOptions::default());
            assert_eq!(records.len(), 1);
            assert_eq!(records[0].author, "Requested Repository");
            assert_eq!(records[0].added, 2);
            assert!(records[0].landed);
            return;
        }
        let root = TempDir::new().unwrap();
        let requested = root.path().join("requested");
        let foreign = root.path().join("foreign");
        init(&requested);
        git(&requested, &["config", "user.name", "Requested Repository"]);
        commit(&requested, "requested.rs", "one\ntwo\n", "requested");
        init(&foreign);
        commit(&foreign, "foreign.rs", "foreign\n", "foreign");
        // Use a separate test process: changing the current process environment
        // while other Git tests run would introduce an unrelated race.
        let output = Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "git::tests::repository_environment_does_not_redirect_scan",
                "--nocapture",
            ])
            .env(CHILD_DIR, &requested)
            .env("GIT_DIR", foreign.join(".git"))
            .env("GIT_COMMON_DIR", foreign.join(".git"))
            .env("GIT_WORK_TREE", &foreign)
            .env("GIT_OBJECT_DIRECTORY", foreign.join(".git/objects"))
            .env("GIT_ALTERNATE_OBJECT_DIRECTORIES", foreign.join("missing"))
            .env("GIT_SHALLOW_FILE", foreign.join("missing-shallow"))
            .env("GIT_GRAFT_FILE", foreign.join("missing-grafts"))
            .env("GIT_INDEX_FILE", foreign.join(".git/index"))
            .env("GIT_NAMESPACE", "foreign")
            .env("GIT_CONFIG_COUNT", "1")
            .env("GIT_CONFIG_KEY_0", "core.bare")
            .env("GIT_CONFIG_VALUE_0", "true")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "isolated environment test failed: {} {}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
    }

    #[test]
    fn batched_counts_match_full_walk_including_copies_and_renames() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "source.rs", "one\ntwo\nthree\nfour\n", "root");
        for index in 0..66 {
            commit(root.path(), "counter.rs", &format!("{index}\n"), "change");
        }
        commit(
            root.path(),
            "copy.rs",
            "one\ntwo\nthree\nfour\nfive\n",
            "copy and edit",
        );
        git(root.path(), &["mv", "copy.rs", "renamed.rs"]);
        commit(
            root.path(),
            "renamed.rs",
            "one\ntwo\nthree\nfour\nsix\n",
            "rename and edit",
        );
        commit(root.path(), "generated.lock", "ignored\n", "generated file");
        let repo = new_repositories(&[root.path().to_owned()]).remove(0);
        let cancel = AtomicBool::new(false);
        let ctx = GitContext::new(&cancel);
        for include_generated in [false, true] {
            let options = CollectOptions {
                include_generated,
                ..Default::default()
            };
            let events = std::sync::Mutex::new(Vec::new());
            let data = scan_repository_with_progress(&repo, &options, &cancel, &|stage| {
                events.lock().unwrap().push(stage)
            })
            .unwrap();
            let mut args = vec![
                "log",
                "--no-merges",
                "--root",
                "--format=%H",
                "--numstat",
                "-z",
            ];
            args.extend(DIFF_OPTIONS);
            args.extend(["--all", "--"]);
            let expected = run_git(&ctx, root.path(), &args, |stdout| {
                parse_numstat(BufReader::new(stdout), &options, true)
            })
            .unwrap();
            assert_eq!(data.records.len(), expected.len());
            for record in data.records {
                let count = expected.get(&record.commit_id).unwrap();
                assert_eq!((record.added, record.removed), (count.added, count.removed));
            }
            let progress: Vec<_> = events
                .into_inner()
                .unwrap()
                .into_iter()
                .filter_map(|event| match event {
                    ScanStage::CountingChanges { done, total } => Some((done, total)),
                    _ => None,
                })
                .collect();
            assert_eq!(progress.first(), Some(&(0, 70)));
            assert_eq!(progress.last(), Some(&(70, 70)));
            assert!(progress.windows(2).all(|pair| pair[0].0 < pair[1].0));
        }
    }

    #[test]
    fn ambient_diff_settings_do_not_change_counts() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "old.go", "one\ntwo\nthree\n", "base");
        git(root.path(), &["mv", "old.go", "new.go"]);
        commit(root.path(), "new.go", "one\ntwo\nthree\nfour\n", "rename");
        let before = scan(root.path()).records;
        for (key, value) in [
            ("log.showRoot", "false"),
            ("diff.renames", "false"),
            ("diff.algorithm", "histogram"),
            ("diff.indentHeuristic", "true"),
            ("diff.external", "false"),
            ("log.showSignature", "true"),
        ] {
            git(root.path(), &["config", key, value]);
        }
        let after = scan(root.path()).records;
        assert_eq!(
            before
                .iter()
                .map(|r| (r.added, r.removed))
                .collect::<Vec<_>>(),
            after
                .iter()
                .map(|r| (r.added, r.removed))
                .collect::<Vec<_>>()
        );
        assert_eq!(after.iter().map(|r| r.added).sum::<i64>(), 4);
    }

    fn prepare_partial_clone(root: &Path) -> PathBuf {
        let source = root.join("source");
        let partial = root.join("partial");
        init(&source);
        git(&source, &["config", "uploadpack.allowFilter", "true"]);
        commit(&source, "a.go", "one\ntwo\n", "first");
        commit(&source, "a.go", "one\ntwo\nthree\n", "second");
        git(
            root,
            &[
                "clone",
                "--filter=blob:none",
                "--no-checkout",
                &format!("file://{}", source.display()),
                partial.to_str().unwrap(),
            ],
        );
        partial
    }

    #[test]
    fn partial_clone_missing_objects_are_not_fetched_and_scan_fails() {
        let root = TempDir::new().unwrap();
        let partial = prepare_partial_clone(root.path());
        let before = git(
            &partial,
            &["rev-list", "--objects", "--all", "--missing=print"],
        );
        assert_eq!(
            before.lines().filter(|line| line.starts_with('?')).count(),
            2
        );
        let error = scan_repository(
            &new_repositories(std::slice::from_ref(&partial))[0],
            &CollectOptions::default(),
            &AtomicBool::new(false),
        )
        .unwrap_err();
        let message = format!("{error:#}");
        let version = parse_git_version(&git(&partial, &["--version"])).unwrap();
        if version >= (2, 45, 1) {
            assert!(message.contains("scan failed with automatic object fetching disabled"));
            assert!(
                error.chain().count() > 1,
                "original Git error must be retained: {message}"
            );
        } else {
            assert!(message.contains("skipped"));
            assert!(message.contains("No remote history was fetched"));
        }
        assert!(message.contains("Partial clone"));
        assert_eq!(
            git(
                &partial,
                &["rev-list", "--objects", "--all", "--missing=print"]
            ),
            before
        );
    }

    #[test]
    fn older_git_skips_partial_clones_but_retains_complete_repositories() {
        let root = TempDir::new().unwrap();
        let partial = prepare_partial_clone(root.path());
        let before = git(
            &partial,
            &["rev-list", "--objects", "--all", "--missing=print"],
        );
        let repo = new_repositories(std::slice::from_ref(&partial)).remove(0);
        let cancel = AtomicBool::new(false);
        let ctx = GitContext::new(&cancel);
        let error =
            scan_repository_with_version(&repo, &CollectOptions::default(), &ctx, (2, 44, 0))
                .unwrap_err();
        assert!(error.to_string().contains("Git 2.45.1 or newer"));
        assert!(error.to_string().contains("No remote history was fetched"));
        assert_eq!(
            git(
                &partial,
                &["rev-list", "--objects", "--all", "--missing=print"]
            ),
            before
        );
        let full = new_repositories(&[root.path().join("source")]).remove(0);
        let data =
            scan_repository_with_version(&full, &CollectOptions::default(), &ctx, (2, 31, 0))
                .unwrap();
        assert_eq!(data.records.len(), 2);
        assert!(data.warnings.is_empty());
        // Partial-clone configuration is not itself a failure when every
        // required object is available and Git supports the no-fetch guard.
        git(&full.path, &["config", "remote.origin.promisor", "true"]);
        let available =
            scan_repository_with_version(&full, &CollectOptions::default(), &ctx, (2, 45, 1))
                .unwrap();
        assert_eq!(available.records.len(), data.records.len());
        assert_eq!(
            available
                .records
                .iter()
                .map(|record| (record.added, record.removed))
                .collect::<Vec<_>>(),
            data.records
                .iter()
                .map(|record| (record.added, record.removed))
                .collect::<Vec<_>>(),
        );
        assert!(available.warnings.is_empty());
        assert!(
            scan_repository_with_version(&full, &CollectOptions::default(), &ctx, (2, 30, 0))
                .unwrap_err()
                .to_string()
                .contains("Git 2.31")
        );
        assert_eq!(
            parse_git_version("git version 2.50.1 (Apple Git-155)\n"),
            Some((2, 50, 1))
        );
        assert_eq!(
            parse_git_version("git version 2.45.1.windows.1"),
            Some((2, 45, 1))
        );
        assert_eq!(parse_git_version("git version 2.31"), Some((2, 31, 0)));
        assert_eq!(parse_git_version("invalid"), None);
    }

    #[test]
    fn shallow_boundary_counts_are_unknown_instead_of_a_fake_root_diff() {
        let root = TempDir::new().unwrap();
        let source = root.path().join("source");
        init(&source);
        commit(&source, "a.go", "one\ntwo\nthree\n", "base");
        commit(&source, "a.go", "one\ntwo\nthree\nfour\n", "edit");
        let clone = root.path().join("clone");
        git(
            root.path(),
            &[
                "clone",
                "--depth=1",
                &format!("file://{}", source.display()),
                clone.to_str().unwrap(),
            ],
        );
        let data = scan(&clone);
        assert_eq!(data.records.len(), 1);
        assert!(!data.records[0].lines_known);
        assert_eq!((data.records[0].added, data.records[0].removed), (0, 0));
        assert!(data.warnings.iter().any(|w| w.contains("Shallow")));
    }

    fn prepare_clean_merge(dir: &Path) {
        init(dir);
        commit(dir, "base.go", "base\n", "base");
        git(dir, &["checkout", "-b", "feature"]);
        commit(dir, "feature.go", "feature\n", "feature");
        git(dir, &["checkout", "main"]);
        commit(dir, "main.go", "main\n", "main");
    }

    #[test]
    fn clean_integration_merges_are_omitted_without_recounting_branches() {
        let root = TempDir::new().unwrap();
        prepare_clean_merge(root.path());
        git(
            root.path(),
            &["merge", "--no-ff", "feature", "-m", "integrate"],
        );
        let before = git(root.path(), &["count-objects", "-v"]);
        let index = std::fs::read(root.path().join(".git/index")).unwrap();
        let data = scan(root.path());
        assert_eq!(data.records.len(), 3);
        assert!(data.records.iter().all(|r| !r.is_merge && r.landed));
        assert_eq!(data.records.iter().map(|r| r.added).sum::<i64>(), 3);
        assert!(data.warnings.is_empty(), "{:?}", data.warnings);
        assert_eq!(git(root.path(), &["count-objects", "-v"]), before);
        assert_eq!(
            std::fs::read(root.path().join(".git/index")).unwrap(),
            index
        );
        assert!(git(root.path(), &["status", "--porcelain"]).is_empty());
    }

    #[test]
    fn extra_edits_in_clean_merges_are_credited_without_branch_changes() {
        let root = TempDir::new().unwrap();
        prepare_clean_merge(root.path());
        git(root.path(), &["merge", "--no-ff", "--no-commit", "feature"]);
        commit(
            root.path(),
            "resolution.go",
            "additional\nwork\n",
            "extra merge work",
        );
        let data = scan(root.path());
        let merge = data.records.iter().find(|r| r.is_merge).unwrap();
        assert!(merge.lines_known);
        assert_eq!((merge.added, merge.removed), (2, 0));
        assert_eq!(data.records.len(), 4);
        assert_eq!(data.records.iter().map(|r| r.added).sum::<i64>(), 5);
        assert!(data.warnings.is_empty(), "{:?}", data.warnings);
    }

    #[test]
    fn conflicted_merge_resolution_is_credited_with_unknown_lines() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "a.go", "base\n", "base");
        git(root.path(), &["checkout", "-b", "feature"]);
        commit(root.path(), "a.go", "theirs\n", "feature");
        git(root.path(), &["checkout", "main"]);
        commit(root.path(), "a.go", "ours\n", "main");
        let output = Command::new("git")
            .args(["merge", "--no-ff", "feature", "-m", "conflict"])
            .current_dir(root.path())
            .output()
            .unwrap();
        assert_eq!(output.status.code(), Some(1));
        commit(
            root.path(),
            "a.go",
            "resolved\nadditional\n",
            "resolve conflict",
        );
        let data = scan(root.path());
        let merge = data.records.iter().find(|r| r.is_merge).unwrap();
        assert!(!merge.lines_known);
        assert_eq!((merge.added, merge.removed), (0, 0));
        assert_eq!(data.records.len(), 4);
        assert!(
            data.warnings
                .iter()
                .any(|w| w.contains("conflict-resolution"))
        );
    }
    #[test]
    fn external_merge_drivers_are_not_executed_by_scans() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(
            root.path(),
            ".gitattributes",
            "a.go merge=custom\n",
            "attributes",
        );
        commit(root.path(), "a.go", "base\n", "base");
        git(root.path(), &["checkout", "-b", "feature"]);
        commit(root.path(), "a.go", "theirs\n", "feature");
        git(root.path(), &["checkout", "main"]);
        commit(root.path(), "a.go", "ours\n", "main");
        let output = Command::new("git")
            .args(["merge", "--no-ff", "feature", "-m", "conflict"])
            .current_dir(root.path())
            .output()
            .unwrap();
        assert_eq!(output.status.code(), Some(1));
        commit(root.path(), "a.go", "resolved\n", "resolve");
        git(
            root.path(),
            &["config", "merge.custom.driver", "touch driver-ran; true"],
        );
        let data = scan(root.path());
        assert!(!root.path().join("driver-ran").exists());
        assert!(
            !data
                .records
                .iter()
                .find(|r| r.is_merge)
                .unwrap()
                .lines_known
        );
    }

    #[test]
    fn octopus_merges_are_explicit_unknowns() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "base.go", "base\n", "base");
        git(root.path(), &["checkout", "-b", "left"]);
        commit(root.path(), "left.go", "left\n", "left");
        git(root.path(), &["checkout", "main"]);
        git(root.path(), &["checkout", "-b", "right"]);
        commit(root.path(), "right.go", "right\n", "right");
        git(root.path(), &["checkout", "main"]);
        commit(root.path(), "main.go", "main\n", "main");
        git(
            root.path(),
            &["merge", "--no-ff", "left", "right", "-m", "octopus"],
        );
        let data = scan(root.path());
        assert!(
            !data
                .records
                .iter()
                .find(|r| r.is_merge)
                .unwrap()
                .lines_known
        );
        assert!(
            data.warnings
                .iter()
                .any(|w| w.contains("unsupported merge"))
        );
    }

    #[test]
    fn parsed_coauthor_names_preserve_quotes_and_mailmap_name_matching() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(
            root.path(),
            "a.go",
            "a\n",
            "pairing\n\nCo-authored-by: \"Pat, Developer\" (human) <old@example.com>",
        );
        std::fs::write(
            root.path().join(".mailmap"),
            "Pat Canonical <pat@example.com> Pat, Developer <old@example.com>\n",
        )
        .unwrap();
        let data = scan(root.path());
        assert_eq!(
            data.records[0].coauthors,
            vec![Identity {
                name: "Pat Canonical".into(),
                email: "pat@example.com".into()
            }]
        );
    }
}
