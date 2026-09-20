//! Git collection deliberately preserves the Go revision's analytics policy.
//!
//! In particular, short reference names, generated-path filtering, broad AI
//! identity defaults, and gitfile discovery are compatibility behavior. They
//! are covered by tests so an analytics-policy change can be made separately.

use crate::model::{CommitRecord, Repository};
use anyhow::{Context, Result, anyhow, bail};
use chrono::DateTime;
use std::collections::HashSet;
use std::io::{BufRead, BufReader, Read};
use std::path::{Component, Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, Instant};

const GIT_TIMEOUT: Duration = Duration::from_secs(120);
const MAX_LOG_LINE: usize = 4 * 1024 * 1024;
const FIELD_SEP: char = '\x1e';
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
const AI_DOMAINS: &[&str] = &[
    "@anthropic.com",
    "@openai.com",
    "@cursor.com",
    "@cursor.sh",
    "@codeium.com",
    "@windsurf.com",
];
const AI_ADDRESSES: &[&str] = &[
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
}

impl<'a> GitContext<'a> {
    fn new(cancel: &'a AtomicBool) -> Self {
        Self {
            deadline: Instant::now() + GIT_TIMEOUT,
            cancel,
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
    let child = Command::new("git")
        .args(["-c", "core.quotePath=false"])
        .args(args)
        .current_dir(dir)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .with_context(|| format!("git {} in {}", args[0], dir.display()))?;
    run_child(ctx, child, consume)
        .map_err(|error| anyhow!("git {} in {}: {error:#}", args[0], dir.display()))
}

fn run_child<T, F>(ctx: &GitContext<'_>, mut child: std::process::Child, consume: F) -> Result<T>
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
                Ok(None) => thread::sleep(Duration::from_millis(10)),
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
        if !status.success() {
            bail!("{}: {}", status, String::from_utf8_lossy(&errors).trim());
        }
        Ok(output)
    })
}

fn git_output(ctx: &GitContext<'_>, dir: &Path, args: &[&str]) -> Result<String> {
    run_git(ctx, dir, args, |mut stdout| {
        let mut bytes = Vec::new();
        stdout.read_to_end(&mut bytes)?;
        Ok(String::from_utf8_lossy(&bytes).into_owned())
    })
}

pub fn detect_default_branch(path: &Path) -> String {
    let cancel = AtomicBool::new(false);
    detect_branch(&GitContext::new(&cancel), path)
}

fn detect_branch(ctx: &GitContext<'_>, dir: &Path) -> String {
    if let Ok(output) = git_output(
        ctx,
        dir,
        &["symbolic-ref", "--short", "refs/remotes/origin/HEAD"],
    ) {
        let reference = output.trim();
        if !reference.is_empty() {
            if let Some(local) = reference.strip_prefix("origin/")
                && git_output(
                    ctx,
                    dir,
                    &["rev-parse", "--verify", &format!("refs/heads/{local}")],
                )
                .is_ok()
            {
                return local.to_owned();
            }
            return reference.to_owned();
        }
    }
    for branch in ["main", "master"] {
        if git_output(ctx, dir, &["rev-parse", "--verify", branch]).is_ok() {
            return branch.to_owned();
        }
    }
    "HEAD".to_owned()
}

/// Detect and collect one repository under a single 120-second deadline.
pub fn scan_repository(
    repo: &Repository,
    options: &CollectOptions,
    cancel: &AtomicBool,
) -> Result<Vec<CommitRecord>> {
    let ctx = GitContext::new(cancel);
    let reference = detect_branch(&ctx, &repo.path);
    collect_repository(&ctx, repo, &reference, options)
}

fn collect_repository(
    ctx: &GitContext<'_>,
    repo: &Repository,
    reference: &str,
    options: &CollectOptions,
) -> Result<Vec<CommitRecord>> {
    let args = [
        "log",
        reference,
        "--no-merges",
        "-M",
        "-C",
        "--format=%aN%x1e%aE%x1e%aI%x1e%(trailers:key=Co-authored-by,valueonly,separator=%x1f)",
        "--numstat",
    ];
    let collected = run_git(ctx, &repo.path, &args, |stdout| {
        let mut reader = BufReader::new(stdout);
        let mut line = Vec::new();
        let mut parser = LogParser::new(repo, options);
        while read_log_line(&mut reader, &mut line)? {
            parser.feed(&String::from_utf8_lossy(&line));
        }
        Ok(parser.finish())
    });
    if collected.is_err() && ctx.check().is_ok() && is_empty_repo(ctx, &repo.path) {
        return Ok(Vec::new());
    }
    collected
}

fn read_log_line(reader: &mut impl BufRead, line: &mut Vec<u8>) -> Result<bool> {
    line.clear();
    loop {
        let available = reader.fill_buf()?;
        if available.is_empty() {
            return Ok(!line.is_empty());
        }
        let newline = available.iter().position(|byte| *byte == b'\n');
        let consumed = newline.map_or(available.len(), |index| index + 1);
        if line.len() + consumed > MAX_LOG_LINE {
            bail!("Git log line exceeds the 4 MiB scanner limit");
        }
        line.extend_from_slice(&available[..consumed]);
        reader.consume(consumed);
        if newline.is_some() {
            line.pop();
            if line.last() == Some(&b'\r') {
                line.pop();
            }
            return Ok(true);
        }
    }
}

fn is_empty_repo(ctx: &GitContext<'_>, dir: &Path) -> bool {
    git_output(ctx, dir, &["rev-parse", "--git-dir"]).is_ok()
        && git_output(ctx, dir, &["rev-parse", "--verify", "--quiet", "HEAD"]).is_err()
}

struct LogParser<'a> {
    repo: &'a Repository,
    options: &'a CollectOptions,
    records: Vec<CommitRecord>,
    current: Option<CommitRecord>,
}

impl<'a> LogParser<'a> {
    fn new(repo: &'a Repository, options: &'a CollectOptions) -> Self {
        Self {
            repo,
            options,
            records: Vec::new(),
            current: None,
        }
    }

    fn flush(&mut self) {
        if let Some(record) = self.current.take() {
            self.records.push(record);
        }
    }

    fn feed(&mut self, line: &str) {
        if line.is_empty() {
            return;
        }
        if line.contains(FIELD_SEP) {
            let parts: Vec<_> = line.splitn(4, FIELD_SEP).collect();
            if parts.len() >= 3 {
                self.flush();
                let Ok(date) = DateTime::parse_from_rfc3339(parts[2].trim()) else {
                    return;
                };
                let email = parts[1].trim();
                let ai_assisted = is_ai(email, &self.options.ai_identities)
                    || parts.get(3).is_some_and(|trailers| {
                        trailers
                            .split(COAUTHOR_SEP)
                            .any(|value| is_ai(value, &self.options.ai_identities))
                    });
                self.current = Some(CommitRecord {
                    author: parts[0].trim().to_owned(),
                    email: email.to_owned(),
                    date,
                    added: 0,
                    removed: 0,
                    repo_id: self.repo.id.clone(),
                    repo_name: self.repo.name.clone(),
                    ai_assisted,
                });
                return;
            }
        }
        let Some(current) = &mut self.current else {
            return;
        };
        let fields: Vec<_> = line.splitn(3, '\t').collect();
        if fields.len() != 3
            || fields[0] == "-"
            || fields[1] == "-"
            || !should_count_path(fields[2], self.options.include_generated)
        {
            return;
        }
        if let (Ok(added), Ok(removed)) = (fields[0].parse::<i64>(), fields[1].parse::<i64>()) {
            current.added = current.added.wrapping_add(added);
            current.removed = current.removed.wrapping_add(removed);
        }
    }

    fn finish(mut self) -> Vec<CommitRecord> {
        self.flush();
        self.records
    }
}

fn effective_path(path: &str) -> String {
    let path = path.trim();
    if !path.contains("=>") {
        return path.to_owned();
    }
    if let Some(open) = path.find('{')
        && let Some(close) = path.find('}').filter(|close| *close > open)
    {
        let inner = &path[open + 1..close];
        let destination = inner.split_once("=>").map_or(inner, |(_, value)| value);
        return format!(
            "{}{}{}",
            &path[..open],
            destination.trim(),
            &path[close + 1..]
        )
        .trim()
        .to_owned();
    }
    path.split_once("=>")
        .map_or(path, |(_, value)| value)
        .trim()
        .to_owned()
}

fn should_count_path(path: &str, include_generated: bool) -> bool {
    if include_generated {
        return true;
    }
    let path = effective_path(path);
    if path
        .split('/')
        .any(|segment| IGNORED_DIRS.contains(&segment))
    {
        return false;
    }
    let basename = path.rsplit('/').next().unwrap_or(&path);
    !IGNORED_FILE_GLOBS
        .iter()
        .any(|pattern| glob::Pattern::new(pattern).is_ok_and(|pattern| pattern.matches(basename)))
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

    fn phrase(&mut self) -> Option<()> {
        let mut words = 0;
        loop {
            if words > 0 {
                self.skip_comments()?;
            }
            self.space();
            let word = if self.rest.starts_with('"') {
                self.quoted()
            } else {
                self.atom(true)
            };
            if word.is_none() {
                break;
            }
            words += 1;
        }
        (words > 0).then_some(())
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
    if AI_DOMAINS.iter().any(|domain| address.ends_with(domain))
        || AI_ADDRESSES.contains(&address.as_str())
    {
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
    match std::fs::symlink_metadata(&git_path) {
        Ok(metadata) if !metadata.is_dir() => {
            std::fs::read(&git_path).is_ok_and(|bytes| bytes.starts_with(b"gitdir:"))
        }
        _ => false,
    }
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
        assert_eq!(detect_default_branch(root.path()), "origin/main");
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
    fn compatibility_short_branch_reference_can_resolve_a_tag() {
        let root = TempDir::new().unwrap();
        init(root.path());
        commit(root.path(), "a.go", "a\n", "first");
        git(root.path(), &["tag", "main"]);
        commit(root.path(), "b.go", "b\n", "second");
        assert_eq!(detect_default_branch(root.path()), "main");
        assert_eq!(collect(root.path(), &CollectOptions::default()).len(), 1);
    }

    #[test]
    fn compatibility_local_branch_precedes_newer_remote_tracking_branch() {
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
        assert_eq!(collect(root.path(), &CollectOptions::default()).len(), 1);
    }

    #[cfg(unix)]
    #[test]
    fn compatibility_git_quoted_vendor_paths_are_counted() {
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
            4
        );
    }

    #[test]
    fn compatibility_separate_git_directory_is_skipped() {
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
        assert!(discover_repos_depth(&[repo], 1).is_empty());
    }

    #[test]
    fn compatibility_vendor_employee_email_is_ai() {
        assert!(is_ai("Human Dev <human.employee@openai.com>", &[]));
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
    fn parser_preserves_pipe_names_binary_skip_and_malformed_date_behavior() {
        let repo = new_repositories(&[PathBuf::from("/repo")]).remove(0);
        let options = CollectOptions::default();
        let mut parser = LogParser::new(&repo, &options);
        for line in [
            "Bad|Name\x1ebad@example.com\x1e2026-01-01T00:00:00Z\x1e",
            "7\t2\tcode.go",
            "-\t-\timage.png",
            "Ghost\x1eghost@example.com\x1enot-a-date\x1e",
            "99\t99\tx.go",
        ] {
            parser.feed(line);
        }
        let records = parser.finish();
        assert_eq!(records.len(), 1);
        assert_eq!(records[0].author, "Bad|Name");
        assert_eq!((records[0].added, records[0].removed), (7, 2));
    }

    #[test]
    fn path_filter_and_rename_paths_match_go_policy() {
        assert_eq!(
            effective_path("src/{old => new}/file.go"),
            "src/new/file.go"
        );
        assert_eq!(effective_path("old.go => new.go"), "new.go");
        assert!(should_count_path("src/{old.go => new.go}", false));
        assert!(!should_count_path("vendor/naïve.go", false));
        assert!(!should_count_path("src/build/x.go", false));
        assert!(!should_count_path("src/foo.min.js", false));
        assert!(should_count_path("src/build/x.go", true));
    }

    #[test]
    fn scanner_rejects_oversized_lines() {
        let bytes = vec![b'a'; MAX_LOG_LINE + 1];
        let mut reader = BufReader::new(bytes.as_slice());
        assert!(read_log_line(&mut reader, &mut Vec::new()).is_err());
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
}
