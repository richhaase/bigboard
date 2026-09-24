//! GitHub discovery through the user's `gh` account and managed bare Git caches.
use crate::{
    config, git,
    model::{Repository, ScanData},
    progress::{Reporter, ScanStage},
};
use anyhow::{Context, Result, bail};
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::{
    collections::HashSet,
    fs::{self, File, OpenOptions},
    io::{Read, Seek, SeekFrom, Write},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc,
    },
    thread,
    time::{Duration, Instant},
};

const API_TIMEOUT: Duration = Duration::from_secs(120);
const FETCH_TIMEOUT: Duration = Duration::from_secs(900);

#[derive(Clone, Debug, Deserialize)]
pub struct RemoteRepository {
    pub id: u64,
    pub full_name: String,
    pub default_branch: Option<String>,
    #[serde(default)]
    pub private: bool,
    #[serde(default)]
    pub archived: bool,
    #[serde(default)]
    pub fork: bool,
    #[serde(default)]
    pub disabled: bool,
}
impl RemoteRepository {
    pub fn owner(&self) -> &str {
        self.full_name.split('/').next().unwrap_or_default()
    }
    fn validate(&self) -> Result<()> {
        let parts: Vec<_> = self.full_name.split('/').collect();
        if self.id == 0 || parts.len() != 2 || !parts.iter().all(|part| safe_component(part)) {
            bail!("GitHub returned an invalid repository identity");
        }
        Ok(())
    }
}

#[derive(Debug)]
pub struct Catalog {
    pub login: String,
    pub repositories: Vec<RemoteRepository>,
}

#[derive(Clone, Debug)]
pub struct Client {
    pub host: String,
    pub cache_root: PathBuf,
    selection_path: PathBuf,
    gh_program: PathBuf,
}

#[derive(Default, Serialize, Deserialize)]
struct Selection {
    host: String,
    login: String,
    repositories: Vec<u64>,
}

#[derive(Serialize, Deserialize)]
struct AnalysisSnapshot {
    fingerprint: String,
    data: ScanData,
}

fn safe_component(value: &str) -> bool {
    !value.is_empty()
        && value != "."
        && value != ".."
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"-_.".contains(&b))
}

impl Client {
    pub fn from_env() -> Result<Self> {
        let host = std::env::var("GH_HOST")
            .ok()
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| "github.com".into())
            .to_lowercase();
        if !safe_component(&host) || host.starts_with('-') {
            bail!("GH_HOST must be a GitHub hostname");
        }
        let base = std::env::var_os("XDG_CACHE_HOME")
            .filter(|s| !s.is_empty())
            .map(PathBuf::from)
            .or_else(|| {
                std::env::var_os("HOME")
                    .filter(|s| !s.is_empty())
                    .map(|home| {
                        PathBuf::from(home).join(if cfg!(target_os = "macos") {
                            "Library/Caches"
                        } else {
                            ".cache"
                        })
                    })
            })
            .context("Set HOME or XDG_CACHE_HOME to store GitHub history")?;
        let selection_path = config::default_config_path().with_file_name("github.json");
        if !base.is_absolute() || !selection_path.is_absolute() {
            bail!("GitHub cache and configuration paths must be absolute");
        }
        Ok(Self {
            host,
            cache_root: base.join("bigboard/github"),
            selection_path,
            gh_program: "gh".into(),
        })
    }

    fn api(&self, endpoint: &str, paginate: bool, cancel: &AtomicBool) -> Result<Vec<u8>> {
        let mut command = Command::new(&self.gh_program);
        command.args(["api", "--hostname", &self.host, "--method", "GET", endpoint]);
        if paginate {
            command.args(["--paginate", "--slurp"]);
        }
        command.env("GH_PROMPT_DISABLED", "1");
        run_command(command, cancel, API_TIMEOUT)
            .context("GitHub request failed; check your GitHub CLI login and repository access")
    }

    pub fn discover(&self, cancel: &AtomicBool) -> Result<Catalog> {
        #[derive(Deserialize)]
        struct User {
            login: String,
        }
        let user: User = serde_json::from_slice(&self.api("user", false, cancel)?)?;
        let pages: Vec<Vec<RemoteRepository>> = serde_json::from_slice(&self.api(
            "user/repos?per_page=100&sort=full_name&affiliation=owner,collaborator,organization_member",
            true, cancel,
        )?).context("Reading GitHub repository pages")?;
        let mut seen = HashSet::new();
        let mut repositories = Vec::new();
        for repo in pages.into_iter().flatten() {
            repo.validate()?;
            if !repo.disabled && seen.insert(repo.id) {
                repositories.push(repo);
            }
        }
        repositories.sort_by_key(|r| r.full_name.to_lowercase());
        Ok(Catalog {
            login: user.login,
            repositories,
        })
    }

    pub fn saved_selection(&self, login: &str) -> Result<HashSet<u64>> {
        let bytes = match fs::read(&self.selection_path) {
            Ok(bytes) => bytes,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(HashSet::new()),
            Err(error) => return Err(error).context("Reading saved GitHub selection"),
        };
        let saved: Selection =
            serde_json::from_slice(&bytes).context("Reading saved GitHub selection")?;
        Ok(if saved.host == self.host && saved.login == login {
            saved.repositories.into_iter().collect()
        } else {
            HashSet::new()
        })
    }

    pub fn save_selection(&self, login: &str, repositories: &[RemoteRepository]) -> Result<()> {
        let parent = self
            .selection_path
            .parent()
            .context("Missing configuration directory")?;
        private_directory(parent)?;
        let mut file = tempfile::NamedTempFile::new_in(parent)?;
        serde_json::to_writer_pretty(
            &mut file,
            &Selection {
                host: self.host.clone(),
                login: login.into(),
                repositories: repositories.iter().map(|r| r.id).collect(),
            },
        )?;
        file.write_all(b"\n")?;
        file.as_file().sync_all()?;
        file.persist(&self.selection_path)
            .context("Saving GitHub selection")?;
        Ok(())
    }

    pub fn repository(&self, remote: &RemoteRepository) -> Repository {
        Repository {
            id: format!("github:{}:{}", self.host, remote.id),
            path: self
                .cache_root
                .join(&self.host)
                .join(format!("{}.git", remote.id)),
            name: remote.full_name.clone(),
        }
    }

    /// Hold the per-repository cache lock through collection, so another Big
    /// Board process cannot replace refs while the analytics traverse them.
    pub fn scan(
        &self,
        remote: &RemoteRepository,
        options: &git::CollectOptions,
        cancel: &AtomicBool,
    ) -> Result<ScanData> {
        self.scan_with_progress(remote, options, cancel, &|_| {})
    }

    pub(crate) fn scan_with_progress(
        &self,
        remote: &RemoteRepository,
        options: &git::CollectOptions,
        cancel: &AtomicBool,
        report: &Reporter<'_>,
    ) -> Result<ScanData> {
        report(ScanStage::Metadata);
        remote.validate()?;
        let current: RemoteRepository = serde_json::from_slice(&self.api(
            &format!("repos/{}", remote.full_name),
            false,
            cancel,
        )?)?;
        current.validate()?;
        if current.id != remote.id || current.disabled {
            bail!("Repository identity or access changed; reopen the GitHub picker");
        }
        let parent = self.cache_root.join(&self.host);
        private_directory(&parent)?;
        report(ScanStage::WaitingForCache);
        let _lock = CacheLock::acquire(&parent.join(format!("{}.lock", remote.id)), cancel)?;
        let url = format!("https://{}/{}.git", self.host, current.full_name);
        report(ScanStage::Downloading);
        self.sync(&current, &url, cancel)
            .context("GitHub history refresh failed; cached totals were not used")?;
        self.analyze(&self.repository(&current), options, cancel, report)
    }

    /// Called only after a successful fetch, while holding the history lock.
    fn analyze(
        &self,
        repo: &Repository,
        options: &git::CollectOptions,
        cancel: &AtomicBool,
        report: &Reporter<'_>,
    ) -> Result<ScanData> {
        report(ScanStage::CheckingAnalysis);
        // A missing/unsupported fingerprint input disables reuse rather than
        // making assumptions about external Git configuration.
        let fingerprint = analysis_fingerprint(repo, options, cancel).ok();
        let path = repo.path.join("bigboard-analysis.json");
        if let Some(expected) = &fingerprint
            && let Ok(file) = File::open(&path)
            && let Ok(snapshot) = serde_json::from_reader::<_, AnalysisSnapshot>(file)
            && snapshot.fingerprint == *expected
        {
            check(cancel, Instant::now() + FETCH_TIMEOUT)?;
            report(ScanStage::ReusingAnalysis);
            return Ok(snapshot.data);
        }
        let data = git::scan_repository_with_progress(repo, options, cancel, report)?;
        // Unsupported/conflicting merges are deterministic, but a transient
        // failure reconstructing a merge must be retried on the next scan.
        if let Some(fingerprint) = fingerprint
            && !data
                .warnings
                .iter()
                .any(|warning| warning.contains("resolution line counts are unknown:"))
        {
            report(ScanStage::SavingAnalysis);
            if analysis_fingerprint(repo, options, cancel).ok().as_ref() != Some(&fingerprint) {
                return Ok(data);
            }
            // Failure to save an optimization never hides successfully read data.
            let _ = (|| -> Result<()> {
                let mut file = tempfile::NamedTempFile::new_in(&repo.path)?;
                serde_json::to_writer(
                    &mut file,
                    &AnalysisSnapshot {
                        fingerprint,
                        data: data.clone(),
                    },
                )?;
                file.as_file().sync_all()?;
                file.persist(path)?;
                Ok(())
            })();
        }
        Ok(data)
    }

    fn sync(&self, remote: &RemoteRepository, url: &str, cancel: &AtomicBool) -> Result<()> {
        let repo = self.repository(remote);
        let parent = repo.path.parent().context("Missing cache directory")?;
        private_directory(parent)?;
        let pending;
        let path = if repo.path.exists() {
            if fs::symlink_metadata(&repo.path)?.file_type().is_symlink() {
                bail!("Refusing a symlink in the managed GitHub cache");
            }
            let bare = run_git(&repo.path, &["rev-parse", "--is-bare-repository"], cancel)?;
            let marker = run_git(
                &repo.path,
                &["config", "--get", "bigboard.repository"],
                cancel,
            )?;
            if String::from_utf8_lossy(&bare).trim() != "true"
                || String::from_utf8_lossy(&marker).trim() != repo.id
            {
                bail!("Directory is not a Big Board cache for this repository");
            }
            &repo.path
        } else {
            pending = tempfile::Builder::new()
                .prefix(".fetch-")
                .tempdir_in(parent)?;
            run_git(pending.path(), &["init", "--bare", "--template="], cancel)?;
            run_git(
                pending.path(),
                &["config", "bigboard.repository", &repo.id],
                cancel,
            )?;
            pending.path()
        };
        run_git(path, &["config", "remote.origin.url", url], cancel)?;
        run_git(
            path,
            &[
                "config",
                "remote.origin.fetch",
                "+refs/heads/*:refs/remotes/origin/*",
            ],
            cancel,
        )?;
        run_git(
            path,
            &[
                "fetch",
                "--atomic",
                "--prune",
                "--no-tags",
                "--no-write-fetch-head",
                "origin",
            ],
            cancel,
        )?;
        if let Some(branch) = remote.default_branch.as_deref().filter(|s| !s.is_empty()) {
            let target = format!("refs/remotes/origin/{branch}");
            run_git(path, &["check-ref-format", &target], cancel)?;
            run_git(
                path,
                &["symbolic-ref", "refs/remotes/origin/HEAD", &target],
                cancel,
            )?;
            run_git(path, &["symbolic-ref", "HEAD", &target], cancel)?;
        } else {
            // Empty repositories have no branch. Remove any previous default
            // pointer rather than presenting a formerly landed branch as current.
            run_git(
                path,
                &["update-ref", "--no-deref", "-d", "refs/remotes/origin/HEAD"],
                cancel,
            )?;
        }
        run_git(path, &["config", "mailmap.blob", "HEAD:.mailmap"], cancel)?;
        if path != repo.path {
            fs::rename(path, &repo.path).context("Publishing downloaded history")?;
        }
        Ok(())
    }
}

fn analysis_fingerprint(
    repo: &Repository,
    options: &git::CollectOptions,
    cancel: &AtomicBool,
) -> Result<String> {
    let mut inputs = vec![
        // The package version invalidates saved analysis when engine behavior changes.
        env!("CARGO_PKG_VERSION").as_bytes().to_vec(),
        serde_json::to_vec(&(
            &repo.id,
            &repo.name,
            options.include_generated,
            &options.ai_identities,
        ))?,
        run_git(&repo.path, &["--version"], cancel)?,
        run_git(
            &repo.path,
            &[
                "for-each-ref",
                "--format=%(refname)%00%(objectname)%00%(symref)",
            ],
            cancel,
        )?,
        run_git(&repo.path, &["symbolic-ref", "HEAD"], cancel)?,
    ];
    let config = run_git(&repo.path, &["config", "--null", "--list"], cancel)?;
    let has_mailmap_file = config
        .split(|byte| *byte == 0)
        .any(|entry| entry.starts_with(b"mailmap.file\n"));
    inputs.push(config);
    let mut files = vec![
        repo.path.join("info/attributes"),
        repo.path.join("info/grafts"),
        repo.path.join("shallow"),
    ];
    // Git resolves platform-specific defaults, user overrides, and ~ expansion.
    // Older Git versions without these variables simply rescan normally.
    for variable in ["GIT_ATTR_SYSTEM", "GIT_ATTR_GLOBAL"] {
        let path = run_git(&repo.path, &["var", variable], cancel)?;
        let path = std::str::from_utf8(&path)?.trim();
        if !path.is_empty() {
            files.push(PathBuf::from(path));
        }
    }
    if has_mailmap_file {
        let path = run_git(
            &repo.path,
            &["config", "--path", "--get", "mailmap.file"],
            cancel,
        )?;
        files.push(PathBuf::from(std::str::from_utf8(&path)?.trim()));
    }
    for variable in ["GIT_ATTR_NOSYSTEM", "GIT_ATTR_SOURCE"] {
        inputs.push(
            std::env::var_os(variable)
                .unwrap_or_default()
                .as_encoded_bytes()
                .to_vec(),
        );
    }
    for path in files {
        let path = if path.is_absolute() {
            path
        } else {
            repo.path.join(path)
        };
        inputs.push(path.as_os_str().as_encoded_bytes().to_vec());
        match fs::read(path) {
            Ok(content) => {
                inputs.push(vec![1]);
                inputs.push(content);
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => inputs.push(vec![0]),
            Err(error) => return Err(error.into()),
        }
    }
    // Hash privately: effective Git configuration may contain credentials.
    // Only the digest goes into the persistent analysis snapshot.
    let mut file = tempfile::NamedTempFile::new()?;
    serde_json::to_writer(&mut file, &inputs)?;
    file.flush()?;
    let hash = run_git(
        &repo.path,
        &[
            "hash-object",
            "--no-filters",
            file.path().to_str().context("Invalid temporary path")?,
        ],
        cancel,
    )?;
    Ok(std::str::from_utf8(&hash)?.trim().into())
}

pub struct Discovery {
    pub receiver: mpsc::Receiver<std::result::Result<Catalog, String>>,
    cancel: Arc<AtomicBool>,
    worker: Option<thread::JoinHandle<()>>,
}
impl Discovery {
    pub fn start(client: Client) -> Self {
        let (sender, receiver) = mpsc::channel();
        let cancel = Arc::new(AtomicBool::new(false));
        let worker_cancel = Arc::clone(&cancel);
        let worker = thread::spawn(move || {
            let _ = sender.send(
                client
                    .discover(&worker_cancel)
                    .map_err(|e| format!("{e:#}")),
            );
        });
        Self {
            receiver,
            cancel,
            worker: Some(worker),
        }
    }
}
impl Drop for Discovery {
    fn drop(&mut self) {
        self.cancel.store(true, Ordering::Relaxed);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

fn private_directory(path: &Path) -> Result<()> {
    if path.exists() && fs::symlink_metadata(path)?.file_type().is_symlink() {
        bail!("Refusing a symlink for Big Board storage");
    }
    fs::create_dir_all(path)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, fs::Permissions::from_mode(0o700))?;
    }
    Ok(())
}

struct CacheLock(File);
impl CacheLock {
    fn acquire(path: &Path, cancel: &AtomicBool) -> Result<Self> {
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(path)?;
        let deadline = Instant::now() + FETCH_TIMEOUT;
        loop {
            check(cancel, deadline)?;
            match file.try_lock_exclusive() {
                Ok(()) => return Ok(Self(file)),
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    thread::sleep(Duration::from_millis(40))
                }
                Err(e) => return Err(e).context("Locking GitHub history cache"),
            }
        }
    }
}
impl Drop for CacheLock {
    fn drop(&mut self) {
        let _ = FileExt::unlock(&self.0);
    }
}

fn check(cancel: &AtomicBool, deadline: Instant) -> Result<()> {
    if cancel.load(Ordering::Relaxed) {
        bail!("Operation canceled");
    }
    if Instant::now() >= deadline {
        bail!("GitHub operation timed out; retry with R");
    }
    Ok(())
}

fn run_git(path: &Path, args: &[&str], cancel: &AtomicBool) -> Result<Vec<u8>> {
    let mut command = Command::new("git");
    // Credentials stay with gh. No token is read, written into a remote URL,
    // or persisted in our cache. Hooks and interactive credential prompts are off.
    command
        .args([
            "-c",
            "credential.helper=",
            "-c",
            "credential.helper=!gh auth git-credential",
            "-c",
            "core.hooksPath=/dev/null",
            "-c",
            "gc.auto=0",
        ])
        .args(args)
        .current_dir(path)
        .env("GIT_TERMINAL_PROMPT", "0")
        .env("GCM_INTERACTIVE", "never")
        .env("GH_PROMPT_DISABLED", "1");
    for key in [
        "GIT_DIR",
        "GIT_WORK_TREE",
        "GIT_COMMON_DIR",
        "GIT_OBJECT_DIRECTORY",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES",
        "GIT_INDEX_FILE",
        "GIT_NAMESPACE",
        "GIT_CONFIG",
        "GIT_CONFIG_COUNT",
        "GIT_CONFIG_PARAMETERS",
        "GIT_SHALLOW_FILE",
        "GIT_GRAFT_FILE",
        "GIT_TEMPLATE_DIR",
    ] {
        command.env_remove(key);
    }
    run_command(command, cancel, FETCH_TIMEOUT).with_context(|| format!("Git {} failed", args[0]))
}

fn run_command(mut command: Command, cancel: &AtomicBool, timeout: Duration) -> Result<Vec<u8>> {
    let deadline = Instant::now() + timeout;
    check(cancel, deadline)?;
    // File-backed output prevents pipe deadlocks if a credential helper exits
    // late, and keeps large fetch progress off the alternate-screen terminal.
    let mut stdout = tempfile::tempfile()?;
    let mut stderr = tempfile::tempfile()?;
    command
        .stdin(Stdio::null())
        .stdout(stdout.try_clone()?)
        .stderr(stderr.try_clone()?);
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }
    let mut child = command
        .spawn()
        .context("Cannot start GitHub tooling; install Git and the GitHub CLI (gh)")?;
    let status = loop {
        if let Err(error) = check(cancel, deadline) {
            #[cfg(unix)]
            unsafe {
                // This child was placed in its own group immediately above.
                libc::kill(-(child.id() as i32), libc::SIGKILL);
            }
            let _ = child.kill();
            let _ = child.wait();
            return Err(error);
        }
        if let Some(status) = child.try_wait()? {
            break status;
        }
        thread::sleep(Duration::from_millis(30));
    };
    if !status.success() {
        stderr.seek(SeekFrom::Start(0))?;
        let mut error = String::new();
        stderr.take(16 * 1024).read_to_string(&mut error)?;
        bail!("{}", error.trim());
    }
    stdout.seek(SeekFrom::Start(0))?;
    let mut bytes = Vec::new();
    stdout.read_to_end(&mut bytes)?;
    Ok(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn client(root: &Path) -> Client {
        Client {
            host: "github.com".into(),
            cache_root: root.join("cache"),
            selection_path: root.join("config/github.json"),
            gh_program: "gh".into(),
        }
    }
    fn remote(id: u64, name: &str, branch: &str) -> RemoteRepository {
        RemoteRepository {
            id,
            full_name: name.into(),
            default_branch: Some(branch.into()),
            private: false,
            archived: false,
            fork: false,
            disabled: false,
        }
    }
    fn git(dir: &Path, args: &[&str]) -> String {
        let output = Command::new("git")
            .current_dir(dir)
            .args(args)
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_AUTHOR_NAME", "Raw Name")
            .env("GIT_AUTHOR_EMAIL", "raw@example.test")
            .env("GIT_COMMITTER_NAME", "Raw Name")
            .env("GIT_COMMITTER_EMAIL", "raw@example.test")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{args:?}: {}",
            String::from_utf8_lossy(&output.stderr)
        );
        String::from_utf8_lossy(&output.stdout).trim().into()
    }
    fn commit(dir: &Path, file: &str, content: &str, message: &str) {
        fs::write(dir.join(file), content).unwrap();
        git(dir, &["add", "--", file]);
        git(dir, &["commit", "-m", message]);
    }
    #[test]
    fn bare_cache_matches_local_analytics_and_refreshes_branch_history() {
        let root = tempfile::tempdir().unwrap();
        let source = root.path().join("source");
        fs::create_dir(&source).unwrap();
        git(&source, &["init", "-b", "trunk"]);
        commit(
            &source,
            ".mailmap",
            "Canonical <canonical@example.test> Raw Name <raw@example.test>\n",
            "identity",
        );
        commit(
            &source,
            "code.rs",
            "a\nb\n",
            "feature\n\nCo-authored-by: Teammate <team@example.test>",
        );
        commit(&source, "generated.lock", "a\nb\nc\n", "generated");
        git(&source, &["checkout", "-b", "feature"]);
        commit(&source, "merged.rs", "landed\n", "feature branch");
        git(&source, &["checkout", "trunk"]);
        commit(&source, "parallel.rs", "parallel\n", "parallel work");
        git(
            &source,
            &["merge", "--no-ff", "feature", "-m", "merge feature"],
        );
        git(&source, &["checkout", "-b", "unlanded"]);
        commit(&source, "draft.rs", "draft\n", "work in progress");
        git(&source, &["checkout", "trunk"]);
        git(
            &source,
            &[
                "symbolic-ref",
                "refs/remotes/origin/HEAD",
                "refs/heads/trunk",
            ],
        );
        let client = client(root.path());
        let repo = remote(1, "org/project", "trunk");
        let cancel = AtomicBool::new(false);
        client
            .sync(&repo, source.to_str().unwrap(), &cancel)
            .unwrap();
        let options = git::CollectOptions::default();
        let local = git::scan_repository(
            &Repository {
                id: "local".into(),
                path: source.clone(),
                name: "local".into(),
            },
            &options,
            &cancel,
        )
        .unwrap();
        let cached = git::scan_repository(&client.repository(&repo), &options, &cancel).unwrap();
        assert_eq!(local.records.len(), cached.records.len());
        for expected in &local.records {
            let actual = cached
                .records
                .iter()
                .find(|r| r.commit_id == expected.commit_id)
                .unwrap();
            assert_eq!(
                (
                    &actual.author,
                    &actual.email,
                    actual.date,
                    actual.added,
                    actual.removed,
                    actual.landed,
                    actual.lines_known,
                    actual.ai_assisted,
                    &actual.coauthors
                ),
                (
                    &expected.author,
                    &expected.email,
                    expected.date,
                    expected.added,
                    expected.removed,
                    expected.landed,
                    expected.lines_known,
                    expected.ai_assisted,
                    &expected.coauthors
                )
            );
        }
        assert!(cached.records.iter().any(|r| !r.landed));
        assert!(cached.records.iter().all(|r| r.author == "Canonical"));
        git(&source, &["branch", "-D", "unlanded"]);
        git(&source, &["branch", "-m", "release"]);
        commit(&source, "later.rs", "new\n", "later");
        let changed = remote(1, "org/project", "release");
        client
            .sync(&changed, source.to_str().unwrap(), &cancel)
            .unwrap();
        let updated =
            git::scan_repository(&client.repository(&changed), &options, &cancel).unwrap();
        assert!(updated.records.iter().all(|r| r.landed));
        assert!(
            updated
                .records
                .iter()
                .any(|r| r.commit_id == git(&source, &["rev-parse", "HEAD"]))
        );
        let refs = git(
            &client.repository(&changed).path,
            &["for-each-ref", "--format=%(refname)"],
        );
        assert!(!refs.contains("unlanded"));
        assert!(!refs.contains("origin/trunk"));
        assert!(
            client
                .sync(&changed, "/missing/bigboard-test-source", &cancel)
                .is_err()
        );
    }

    #[test]
    fn analysis_reuse_invalidates_history_options_configuration_and_external_files() {
        let root = tempfile::tempdir().unwrap();
        let source = root.path().join("source");
        fs::create_dir(&source).unwrap();
        git(&source, &["init", "-b", "main"]);
        commit(&source, "code.rs", "one\ntwo\n", "initial");
        commit(&source, "generated.lock", "generated\n", "generated");
        let client = client(root.path());
        let remote = remote(1, "org/project", "main");
        let cancel = AtomicBool::new(false);
        client
            .sync(&remote, source.to_str().unwrap(), &cancel)
            .unwrap();
        let repo = client.repository(&remote);
        let run = |options: &git::CollectOptions, reuse: bool| {
            let events = std::sync::Mutex::new(Vec::new());
            let data = client
                .analyze(&repo, options, &cancel, &|stage| {
                    events.lock().unwrap().push(stage)
                })
                .unwrap();
            let events = events.into_inner().unwrap();
            assert_eq!(
                events.contains(&ScanStage::ReusingAnalysis),
                reuse,
                "{events:?}"
            );
            assert_eq!(
                events.contains(&ScanStage::ReadingHistory),
                !reuse,
                "{events:?}"
            );
            data
        };
        let options = git::CollectOptions::default();
        let fresh = run(&options, false);
        // Older Git versions conservatively disable caching if they cannot
        // resolve the external attribute paths. They still return fresh data.
        if analysis_fingerprint(&repo, &options, &cancel).is_err() {
            return;
        }
        let reused = run(&options, true);
        assert_eq!(
            serde_json::to_value(&fresh).unwrap(),
            serde_json::to_value(&reused).unwrap()
        );
        let ai = git::CollectOptions {
            ai_identities: vec!["raw@example.test".into()],
            ..Default::default()
        };
        assert!(
            run(&ai, false)
                .records
                .iter()
                .all(|record| record.ai_assisted)
        );
        run(&ai, true);
        let generated = git::CollectOptions {
            include_generated: true,
            ..Default::default()
        };
        assert_eq!(
            run(&generated, false)
                .records
                .iter()
                .map(|r| r.added)
                .sum::<i64>(),
            3
        );
        run(&generated, true);
        git(&repo.path, &["config", "core.abbrev", "10"]);
        run(&generated, false);
        let mailmap = root.path().join("mailmap");
        fs::write(
            &mailmap,
            "Canonical <canonical@example.test> Raw Name <raw@example.test>\n",
        )
        .unwrap();
        git(
            &repo.path,
            &["config", "mailmap.file", mailmap.to_str().unwrap()],
        );
        assert!(
            run(&options, false)
                .records
                .iter()
                .all(|r| r.author == "Canonical")
        );
        run(&options, true);
        fs::write(
            &mailmap,
            "Renamed <renamed@example.test> Raw Name <raw@example.test>\n",
        )
        .unwrap();
        assert!(
            run(&options, false)
                .records
                .iter()
                .all(|r| r.author == "Renamed")
        );
        fs::create_dir_all(repo.path.join("info")).unwrap();
        fs::write(repo.path.join("info/attributes"), "*.rs binary\n").unwrap();
        assert_eq!(
            run(&options, false)
                .records
                .iter()
                .map(|r| r.added)
                .sum::<i64>(),
            0
        );
        run(&options, true);
        fs::write(repo.path.join("bigboard-analysis.json"), "truncated").unwrap();
        run(&options, false);
        commit(&source, "new.txt", "new\n", "new commit");
        client
            .sync(&remote, source.to_str().unwrap(), &cancel)
            .unwrap();
        assert_eq!(run(&options, false).records.len(), 3);
        run(&options, true);
        assert!(
            client
                .analyze(&repo, &options, &AtomicBool::new(true), &|_| {})
                .is_err()
        );
    }

    #[cfg(unix)]
    #[test]
    fn failed_metadata_request_never_falls_back_to_saved_analysis() {
        use std::os::unix::fs::PermissionsExt;
        let root = tempfile::tempdir().unwrap();
        let mut client = client(root.path());
        let remote = remote(1, "org/project", "main");
        let repo = client.repository(&remote);
        fs::create_dir_all(&repo.path).unwrap();
        fs::write(repo.path.join("bigboard-analysis.json"), "{}").unwrap();
        let script = root.path().join("gh");
        fs::write(
            &script,
            "#!/bin/sh\necho 'repository unavailable' >&2\nexit 1\n",
        )
        .unwrap();
        fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
        client.gh_program = script;
        let events = std::sync::Mutex::new(Vec::new());
        assert!(
            client
                .scan_with_progress(
                    &remote,
                    &git::CollectOptions::default(),
                    &AtomicBool::new(false),
                    &|stage| events.lock().unwrap().push(stage)
                )
                .is_err()
        );
        assert_eq!(events.into_inner().unwrap(), [ScanStage::Metadata]);
    }

    #[test]
    fn empty_remote_has_no_activity_and_can_receive_its_first_commit() {
        let root = tempfile::tempdir().unwrap();
        let source = root.path().join("source");
        fs::create_dir(&source).unwrap();
        git(&source, &["init", "-b", "trunk"]);
        let client = client(root.path());
        let mut repo = remote(1, "org/empty", "trunk");
        repo.default_branch = None;
        let cancel = AtomicBool::new(false);
        client
            .sync(&repo, source.to_str().unwrap(), &cancel)
            .unwrap();
        let options = git::CollectOptions::default();
        assert!(
            git::scan_repository(&client.repository(&repo), &options, &cancel)
                .unwrap()
                .records
                .is_empty()
        );
        commit(&source, "first.rs", "first\n", "initial commit");
        repo.default_branch = Some("trunk".into());
        client
            .sync(&repo, source.to_str().unwrap(), &cancel)
            .unwrap();
        let data = git::scan_repository(&client.repository(&repo), &options, &cancel).unwrap();
        assert_eq!(data.records.len(), 1);
        assert!(data.records[0].landed);
    }

    #[cfg(unix)]
    #[test]
    fn dropping_discovery_waits_for_network_cancellation() {
        use std::os::unix::fs::PermissionsExt;
        let root = tempfile::tempdir().unwrap();
        let script = root.path().join("gh");
        let started = root.path().join("started");
        fs::write(
            &script,
            format!(
                "#!/bin/sh\ntouch '{}'\nsleep 30 & wait\n",
                started.display()
            ),
        )
        .unwrap();
        fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
        let mut client = client(root.path());
        client.gh_program = script;
        let discovery = Discovery::start(client);
        let deadline = Instant::now() + Duration::from_secs(3);
        while !started.exists() && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }
        assert!(started.exists(), "fake network command did not start");
        let start = Instant::now();
        drop(discovery);
        assert!(start.elapsed() < Duration::from_secs(3));
    }

    #[test]
    fn cache_rejects_unmanaged_directories_and_isolates_repository_ids() {
        let root = tempfile::tempdir().unwrap();
        let client = client(root.path());
        let one = remote(1, "one/same", "main");
        let two = remote(2, "two/same", "main");
        assert_ne!(client.repository(&one).path, client.repository(&two).path);
        let path = client.repository(&one).path;
        fs::create_dir_all(&path).unwrap();
        git(&path, &["init", "--bare"]);
        assert!(
            client
                .sync(&one, "/unused", &AtomicBool::new(false))
                .is_err()
        );
        for name in [
            "../project",
            "org/../project",
            "org/repo?token=secret",
            "org/repo\n",
            "org/",
        ] {
            assert!(remote(1, name, "main").validate().is_err());
        }
    }

    #[test]
    fn saved_selection_is_scoped_to_account_and_host() {
        let root = tempfile::tempdir().unwrap();
        let mut client = client(root.path());
        assert!(client.saved_selection("alice").unwrap().is_empty());
        client
            .save_selection("alice", &[remote(1, "org/project", "main")])
            .unwrap();
        assert_eq!(client.saved_selection("alice").unwrap(), HashSet::from([1]));
        assert!(client.saved_selection("bob").unwrap().is_empty());
        client.host = "enterprise.example".into();
        assert!(client.saved_selection("alice").unwrap().is_empty());
        fs::write(&client.selection_path, "invalid").unwrap();
        assert!(client.saved_selection("alice").is_err());
    }

    #[test]
    fn waiting_for_another_cache_scan_can_be_canceled() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("lock");
        let _lock = CacheLock::acquire(&path, &AtomicBool::new(false)).unwrap();
        let cancel = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&cancel);
        let worker = thread::spawn(move || CacheLock::acquire(&path, &flag).is_err());
        thread::sleep(Duration::from_millis(50));
        cancel.store(true, Ordering::Relaxed);
        assert!(worker.join().unwrap());
    }

    #[cfg(unix)]
    #[test]
    fn discovery_reads_every_page_and_uses_the_requested_host() {
        use std::os::unix::fs::PermissionsExt;
        let root = tempfile::tempdir().unwrap();
        let mut client = client(root.path());
        let script = root.path().join("gh");
        fs::write(&script, r##"#!/bin/sh
[ "$1" = api ] && [ "$2" = --hostname ] && [ "$3" = github.com ] && [ "$4" = --method ] && [ "$5" = GET ] || exit 4
case "$6" in
  user) printf '%s' '{"login":"alice"}' ;;
  user/repos*)
    [ "$7" = --paginate ] && [ "$8" = --slurp ] || exit 5
    printf '%s' '[[{"id":1,"full_name":"org/z","default_branch":"main"}],[{"id":2,"full_name":"org/a","default_branch":"main"},{"id":1,"full_name":"org/z","default_branch":"main"}]]' ;;
  *) exit 6 ;;
esac
"##).unwrap();
        fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
        client.gh_program = script;
        let catalog = client.discover(&AtomicBool::new(false)).unwrap();
        assert_eq!(catalog.login, "alice");
        assert_eq!(
            catalog
                .repositories
                .iter()
                .map(|r| r.full_name.as_str())
                .collect::<Vec<_>>(),
            ["org/a", "org/z"]
        );
    }

    #[cfg(unix)]
    #[test]
    fn network_deadline_and_cancellation_stop_the_process_group() {
        let mut command = Command::new("sh");
        command.args(["-c", "sleep 30 & wait"]);
        let start = Instant::now();
        let error =
            run_command(command, &AtomicBool::new(false), Duration::from_millis(80)).unwrap_err();
        assert!(error.to_string().contains("timed out"));
        assert!(start.elapsed() < Duration::from_secs(3));
        assert!(
            run_command(
                Command::new("not-a-command"),
                &AtomicBool::new(true),
                Duration::from_secs(1)
            )
            .unwrap_err()
            .to_string()
            .contains("canceled")
        );
    }
}
