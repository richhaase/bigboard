//! Lightweight GitHub activity: API metadata only, never Git objects or working files.
use crate::{
    config, git,
    model::{CommitRecord, Identity, Repository, ScanData},
    progress::{Reporter, ScanStage},
};
use anyhow::{Context, Result, bail};
use chrono::{DateTime, TimeZone, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::HashSet,
    fs::{self, File},
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
const MAX_PAGES: usize = 500; // Bound new requests per load; never publish partial totals.

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

/// One shared cutoff for server-side collection and dashboard filtering.
#[derive(Clone, Copy, Debug)]
pub struct Window {
    pub since: DateTime<Utc>,
    pub until: DateTime<Utc>,
}
impl Window {
    pub fn new(days: i64, until: DateTime<Utc>) -> Self {
        Self {
            since: if days == 0 {
                Utc.with_ymd_and_hms(1970, 1, 1, 0, 0, 0).unwrap()
            } else {
                until - chrono::Duration::days(days)
            },
            until,
        }
    }
    // Cache whole UTC days so a moving cutoff can reuse an unchanged head.
    // Returned records are still filtered to the exact requested window.
    fn cache_bounds(self) -> Self {
        Self {
            since: self
                .since
                .date_naive()
                .and_hms_opt(0, 0, 0)
                .unwrap()
                .and_utc(),
            until: self
                .until
                .date_naive()
                .succ_opt()
                .unwrap()
                .and_hms_opt(0, 0, 0)
                .unwrap()
                .and_utc(),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Actor {
    name: String,
    email: String,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PageInfo {
    has_next_page: bool,
    end_cursor: Option<String>,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Authors {
    nodes: Vec<Actor>,
    page_info: PageInfo,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Parents {
    total_count: usize,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ApiCommit {
    oid: String,
    committed_date: DateTime<Utc>,
    author: Actor,
    authors: Authors,
    parents: Parents,
    additions: i64,
    deletions: i64,
    changed_files_if_available: Option<usize>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct History {
    nodes: Vec<ApiCommit>,
    page_info: PageInfo,
}
#[derive(Serialize, Deserialize)]
struct Snapshot {
    schema: u32,
    viewer: String,
    repository: u64,
    head: String,
    since: DateTime<Utc>,
    until: DateTime<Utc>,
    commits: Vec<ApiCommit>,
}
const METADATA: &str = r#"query($owner:String!, $name:String!) {
  viewer { id }
  repository(owner:$owner, name:$name) {
    databaseId nameWithOwner isEmpty
    defaultBranchRef { target { oid } }
  }
}"#;
const HISTORY: &str = r#"query($owner:String!, $name:String!, $head:GitObjectID!,
                             $since:GitTimestamp!, $until:GitTimestamp!, $cursor:String) {
  repository(owner:$owner, name:$name) {
    object(oid:$head) { ... on Commit {
      history(first:100, after:$cursor, since:$since, until:$until) {
        pageInfo { hasNextPage endCursor }
        nodes {
          oid committedDate additions deletions changedFilesIfAvailable
          author { name email }
          authors(first:10) { nodes { name email } pageInfo { hasNextPage endCursor } }
          parents { totalCount }
        }
      }
    } }
  }
}"#;
const AUTHORS: &str = r#"query($owner:String!, $name:String!, $head:GitObjectID!, $cursor:String) {
  repository(owner:$owner, name:$name) {
    object(oid:$head) { ... on Commit {
      authors(first:100, after:$cursor) {
        nodes { name email } pageInfo { hasNextPage endCursor }
      }
    } }
  }
}"#;

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
            .context("Set HOME or XDG_CACHE_HOME to store GitHub summaries")?;
        let selection_path = config::default_config_path().with_file_name("github.json");
        if !base.is_absolute() || !selection_path.is_absolute() {
            bail!("GitHub cache and configuration paths must be absolute");
        }
        Ok(Self {
            host,
            cache_root: base.join("bigboard/github-api"),
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
                .join(format!("{}.json", remote.id)),
            name: remote.full_name.clone(),
        }
    }

    fn graphql(&self, query: &str, variables: Value, cancel: &AtomicBool) -> Result<Value> {
        let mut input = tempfile::NamedTempFile::new()?;
        serde_json::to_writer(&mut input, &json!({"query":query,"variables":variables}))?;
        input.flush()?;
        let mut command = Command::new(&self.gh_program);
        command
            .args(["api", "--hostname", &self.host, "graphql", "--input"])
            .arg(input.path())
            .env("GH_PROMPT_DISABLED", "1");
        let bytes = run_command(command, cancel, API_TIMEOUT).context(
            "GitHub request failed; check gh login, repository access, and API rate limits",
        )?;
        let response: Value = serde_json::from_slice(&bytes).context("Reading GitHub response")?;
        if let Some(errors) = response.get("errors").and_then(Value::as_array)
            && !errors.is_empty()
        {
            bail!(
                "GitHub returned incomplete data: {}",
                errors
                    .iter()
                    .filter_map(|e| e["message"].as_str())
                    .collect::<Vec<_>>()
                    .join("; ")
            );
        }
        response
            .get("data")
            .filter(|d| !d.is_null())
            .cloned()
            .context("GitHub returned no data")
    }

    pub(crate) fn scan_with_progress(
        &self,
        remote: &RemoteRepository,
        options: &git::CollectOptions,
        window: Window,
        cancel: &AtomicBool,
        report: &Reporter<'_>,
    ) -> Result<ScanData> {
        report(ScanStage::Metadata);
        remote.validate()?;
        let (owner, name) = remote.full_name.split_once('/').unwrap();
        let vars = json!({"owner":owner,"name":name});
        // Validate access on every load, even when reusing summaries. Pin one
        // head throughout pagination so advancing branches cannot mix snapshots.
        let metadata = self.graphql(METADATA, vars.clone(), cancel)?;
        let viewer = metadata["viewer"]["id"]
            .as_str()
            .context("Missing GitHub account")?;
        let repo = &metadata["repository"];
        if repo["databaseId"].as_u64() != Some(remote.id) {
            bail!("Repository identity or access changed; reopen the GitHub picker");
        }
        if repo["isEmpty"] == true {
            return Ok(ScanData::default());
        }
        let head = repo["defaultBranchRef"]["target"]["oid"]
            .as_str()
            .context("No accessible default branch; use local mode for other branches")?;
        let bounds = window.cache_bounds();
        let parent = self.cache_root.join(&self.host);
        private_directory(&parent)?;
        let path = self.repository(remote).path;
        let cached = File::open(&path)
            .ok()
            .and_then(|file| serde_json::from_reader::<_, Snapshot>(file).ok())
            .filter(|s| {
                s.schema == 1
                    && s.viewer == viewer
                    && s.repository == remote.id
                    && s.head == head
                    && s.since <= s.until
                    && s.commits.iter().all(|c| {
                        c.parents.total_count > 1 || c.changed_files_if_available.is_some()
                    })
            });
        let mut snapshot = cached.unwrap_or_else(|| Snapshot {
            schema: 1,
            viewer: viewer.into(),
            repository: remote.id,
            head: head.into(),
            since: bounds.since,
            until: bounds.since,
            commits: vec![],
        });
        let mut intervals = Vec::new();
        if bounds.since < snapshot.since {
            intervals.push((bounds.since, snapshot.since));
        }
        if bounds.until > snapshot.until {
            intervals.push((snapshot.until, bounds.until));
        }
        // Disjoint ranges should not download the unrequested gap.
        if bounds.until < snapshot.since || bounds.since > snapshot.until {
            snapshot.commits.clear();
            snapshot.since = bounds.since;
            snapshot.until = bounds.since;
            intervals = vec![(bounds.since, bounds.until)];
        }
        if intervals.is_empty() {
            report(ScanStage::ReusingSummaries);
        }
        let mut pages = 0;
        for (since, until) in intervals {
            let mut variables = vars.clone();
            variables["head"] = json!(head);
            variables["since"] = json!(since.to_rfc3339());
            variables["until"] = json!(until.to_rfc3339());
            variables["cursor"] = Value::Null;
            let mut cursors = HashSet::new();
            loop {
                if pages >= MAX_PAGES {
                    bail!(
                        "GitHub load exceeds 500 history pages; choose a shorter range or use local mode. No partial totals were used"
                    );
                }
                report(ScanStage::FetchingSummaries {
                    pages,
                    commits: snapshot.commits.len(),
                });
                let data = self.graphql(HISTORY, variables.clone(), cancel)?;
                let mut history: History =
                    serde_json::from_value(data["repository"]["object"]["history"].clone())
                        .context("GitHub commit history was incomplete; retry or use local mode")?;
                pages += 1;
                for commit in &mut history.nodes {
                    let mut seen = HashSet::new();
                    while commit.authors.page_info.has_next_page {
                        let cursor = next_cursor(&commit.authors.page_info, &mut seen)?;
                        let mut author_vars = vars.clone();
                        author_vars["head"] = json!(commit.oid);
                        author_vars["cursor"] = json!(cursor);
                        let data = self.graphql(AUTHORS, author_vars, cancel)?;
                        let authors: Authors = serde_json::from_value(
                            data["repository"]["object"]["authors"].clone(),
                        )?;
                        commit.authors.nodes.extend(authors.nodes);
                        commit.authors.page_info = authors.page_info;
                        if seen.len() > 100 {
                            bail!("GitHub author list is too large; no partial totals were used");
                        }
                    }
                }
                snapshot.commits.extend(history.nodes);
                if !history.page_info.has_next_page {
                    break;
                }
                variables["cursor"] = json!(next_cursor(&history.page_info, &mut cursors)?);
            }
        }
        snapshot.since = snapshot.since.min(bounds.since);
        snapshot.until = snapshot.until.max(bounds.until);
        let mut seen = HashSet::new();
        snapshot.commits.retain(|c| seen.insert(c.oid.clone()));
        // Complete pages only; atomic replacement also makes concurrent readers
        // safe. A racing writer can lose an optimization, never mix totals.
        let _ = (|| -> Result<()> {
            let mut file = tempfile::NamedTempFile::new_in(&parent)?;
            serde_json::to_writer(&mut file, &snapshot)?;
            file.as_file().sync_all()?;
            file.persist(&path)?;
            Ok(())
        })();
        check(cancel, Instant::now() + API_TIMEOUT)?;
        let mut repository = self.repository(remote);
        repository.name = repo["nameWithOwner"]
            .as_str()
            .context("Missing repository name")?
            .into();
        let records = snapshot
            .commits
            .iter()
            .filter(|c| c.committed_date >= window.since && c.committed_date <= window.until)
            .map(|c| c.record(&repository, options))
            .collect();
        Ok(ScanData {
            records,
            warnings: vec![],
        })
    }
}

fn next_cursor(info: &PageInfo, seen: &mut HashSet<String>) -> Result<String> {
    let cursor = info
        .end_cursor
        .as_ref()
        .filter(|c| !c.is_empty())
        .context("GitHub pagination was incomplete")?;
    if !seen.insert(cursor.clone()) {
        bail!("GitHub pagination repeated a page; no partial totals were used");
    }
    Ok(cursor.clone())
}

impl ApiCommit {
    fn record(&self, repo: &Repository, options: &git::CollectOptions) -> CommitRecord {
        let identity_key = |a: &Actor| {
            let email = a.email.trim().to_lowercase();
            let name = if email.is_empty() {
                a.name.clone()
            } else {
                String::new()
            };
            (email, name)
        };
        let mut seen = HashSet::from([identity_key(&self.author)]);
        let coauthors: Vec<_> = self
            .authors
            .nodes
            .iter()
            .skip(1)
            .filter(|a| seen.insert(identity_key(a)))
            .map(|a| Identity {
                name: a.name.clone(),
                email: a.email.clone(),
            })
            .collect();
        let is_merge = self.parents.total_count > 1;
        // API merge diffs include branch changes already counted in ancestors.
        // Do not attribute those changes a second time or fabricate a baseline.
        let lines_known = !is_merge && self.changed_files_if_available.is_some();
        CommitRecord {
            commit_id: self.oid.clone(),
            author: self.author.name.clone(),
            email: self.author.email.clone(),
            date: self.committed_date.fixed_offset(),
            added: if lines_known { self.additions } else { 0 },
            removed: if lines_known { self.deletions } else { 0 },
            repo_id: repo.id.clone(),
            repo_name: repo.name.clone(),
            ai_assisted: git::is_ai(&self.author.email, &options.ai_identities)
                || coauthors
                    .iter()
                    .any(|a| git::is_ai(&a.email, &options.ai_identities)),
            coauthors,
            lines_known,
            landed: true,
            is_merge,
        }
    }
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

fn check(cancel: &AtomicBool, deadline: Instant) -> Result<()> {
    if cancel.load(Ordering::Relaxed) {
        bail!("Operation canceled");
    }
    if Instant::now() >= deadline {
        bail!("GitHub operation timed out; retry with R");
    }
    Ok(())
}

fn run_command(mut command: Command, cancel: &AtomicBool, timeout: Duration) -> Result<Vec<u8>> {
    let deadline = Instant::now() + timeout;
    check(cancel, deadline)?;
    // File-backed output prevents pipe deadlocks if a credential helper exits
    // late, and keeps API output off the alternate-screen terminal.
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
        .context("Cannot start GitHub tooling; install the GitHub CLI (gh)")?;
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
    fn metadata(head: &str, viewer: &str) -> Value {
        json!({"data":{"viewer":{"id":viewer},"repository":{
            "databaseId":1,"nameWithOwner":"org/project","isEmpty":false,
            "defaultBranchRef":{"target":{"oid":head}}
        }}})
    }
    fn commit(oid: &str, date: &str) -> Value {
        json!({"oid":oid,"committedDate":date,"additions":10,"deletions":3,
            "changedFilesIfAvailable":1,"author":{"name":"Alice","email":"alice@example.test"},
            "authors":{"nodes":[{"name":"Alice","email":"alice@example.test"}],
                       "pageInfo":{"hasNextPage":false,"endCursor":null}},
            "parents":{"totalCount":1}})
    }
    fn page(nodes: Vec<Value>, cursor: Option<&str>) -> Value {
        json!({"data":{"repository":{"object":{"history":{"nodes":nodes,
            "pageInfo":{"hasNextPage":cursor.is_some(),"endCursor":cursor}}}}}})
    }
    fn window(days: i64) -> Window {
        Window::new(days, Utc.with_ymd_and_hms(2026, 9, 26, 12, 0, 0).unwrap())
    }
    #[cfg(unix)]
    fn fake_graphql(root: &Path, responses: Vec<Value>) -> Client {
        use std::os::unix::fs::PermissionsExt;
        for (i, response) in responses.iter().enumerate() {
            fs::write(
                root.join(format!("response-{i}")),
                serde_json::to_vec(response).unwrap(),
            )
            .unwrap();
        }
        let script = root.join("gh");
        // Only the API command is accepted. All request variables are retained
        // for assertions; no network, credentials, or Git executable is needed.
        fs::write(&script, format!(r##"#!/bin/sh
set -eu
[ "$1" = api ] && [ "$2" = --hostname ] && [ "$3" = github.com ] && [ "$4" = graphql ] && [ "$5" = --input ] || exit 4
cd '{}'
n=0
if [ -f count ]; then read -r n < count; fi
printf '%s\n' "$((n+1))" > count
/bin/cat "$6" >> requests
printf '\n' >> requests
/bin/cat "response-$n"
"##, root.display())).unwrap();
        fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
        let mut c = client(root);
        c.gh_program = script;
        c
    }
    fn requests(root: &Path) -> Vec<Value> {
        fs::read_to_string(root.join("requests"))
            .unwrap()
            .lines()
            .map(|line| serde_json::from_str(line).unwrap())
            .collect()
    }
    fn load(c: &Client, days: i64) -> Result<ScanData> {
        c.scan_with_progress(
            &remote(1, "org/project", "main"),
            &git::CollectOptions::default(),
            window(days),
            &AtomicBool::new(false),
            &|_| {},
        )
    }

    #[cfg(unix)]
    #[test]
    fn summary_pagination_pins_head_and_filters_exact_window() {
        let root = tempfile::tempdir().unwrap();
        let c = fake_graphql(
            root.path(),
            vec![
                metadata("head-one", "alice"),
                page(
                    vec![
                        commit("recent", "2026-09-25T12:00:00Z"),
                        commit("early", "2026-09-12T01:00:00Z"),
                    ],
                    Some("page-two"),
                ),
                page(
                    vec![
                        commit("future", "2026-09-26T15:00:00Z"),
                        commit("older", "2026-09-14T00:00:00Z"),
                    ],
                    None,
                ),
            ],
        );
        let result = load(&c, 14).unwrap();
        assert_eq!(
            result
                .records
                .iter()
                .map(|r| r.commit_id.as_str())
                .collect::<Vec<_>>(),
            ["recent", "older"]
        );
        assert_eq!(
            (result.records[0].added, result.records[0].removed),
            (10, 3)
        );
        let req = requests(root.path());
        assert_eq!(req.len(), 3);
        for r in &req[1..] {
            assert_eq!(r["variables"]["head"], "head-one");
            assert_eq!(r["variables"]["since"], "2026-09-12T00:00:00+00:00");
            assert_eq!(r["variables"]["until"], "2026-09-27T00:00:00+00:00");
            assert!(!r["query"].as_str().unwrap().contains("message"));
        }
        assert_eq!(req[2]["variables"]["cursor"], "page-two");
        let path = c.repository(&remote(1, "org/project", "main")).path;
        assert_eq!(path.extension().unwrap(), "json");
        assert!(!root.path().join("cache/github.com/1.git").exists());
    }

    #[cfg(unix)]
    #[test]
    fn cache_reuses_coverage_extends_only_missing_dates_and_replaces_changed_head() {
        let root = tempfile::tempdir().unwrap();
        let c = fake_graphql(
            root.path(),
            vec![
                metadata("head-one", "alice"),
                page(vec![commit("recent", "2026-09-25T12:00:00Z")], None),
                metadata("head-one", "alice"), // smaller range: metadata only
                metadata("head-one", "alice"),
                page(vec![commit("older", "2026-09-05T12:00:00Z")], None), // extension
                metadata("head-two", "alice"),
                page(vec![commit("replacement", "2026-09-25T12:00:00Z")], None),
                metadata("head-two", "bob"),
                page(vec![commit("replacement", "2026-09-25T12:00:00Z")], None),
            ],
        );
        assert_eq!(load(&c, 14).unwrap().records.len(), 1);
        assert_eq!(load(&c, 7).unwrap().records.len(), 1);
        assert_eq!(requests(root.path()).len(), 3);
        assert_eq!(load(&c, 30).unwrap().records.len(), 2);
        let req = requests(root.path());
        assert_eq!(req[4]["variables"]["since"], "2026-08-27T00:00:00+00:00");
        assert_eq!(req[4]["variables"]["until"], "2026-09-12T00:00:00+00:00");
        let result = load(&c, 14).unwrap();
        assert_eq!(result.records.len(), 1);
        assert_eq!(result.records[0].commit_id, "replacement");
        load(&c, 14).unwrap(); // switching gh account must not reuse another account's summaries
        assert_eq!(requests(root.path()).len(), 9);
    }

    #[cfg(unix)]
    #[test]
    fn api_errors_never_publish_partial_or_stale_totals() {
        let root = tempfile::tempdir().unwrap();
        let mut error = page(vec![commit("partial", "2026-09-25T12:00:00Z")], None);
        error["errors"] = json!([{"message":"API rate limit exceeded"}]);
        let c = fake_graphql(
            root.path(),
            vec![
                metadata("one", "alice"),
                page(vec![commit("old", "2026-09-25T12:00:00Z")], None),
                metadata("two", "alice"),
                page(vec![], Some("next")),
                error,
                json!({"data":{"repository":null},"errors":[{"message":"Access denied"}]}),
            ],
        );
        load(&c, 14).unwrap();
        let path = c.repository(&remote(1, "org/project", "main")).path;
        let cached = fs::read(&path).unwrap();
        assert!(load(&c, 14).unwrap_err().to_string().contains("rate limit"));
        assert_eq!(fs::read(&path).unwrap(), cached);
        assert!(
            load(&c, 14)
                .unwrap_err()
                .to_string()
                .contains("Access denied")
        );
    }

    #[cfg(unix)]
    #[test]
    fn coauthor_pagination_and_unknown_merge_counts_preserve_credit() {
        let root = tempfile::tempdir().unwrap();
        let mut merge = commit("merge", "2026-09-25T12:00:00Z");
        merge["parents"]["totalCount"] = json!(2);
        merge["authors"]["pageInfo"] = json!({"hasNextPage":true,"endCursor":"more-authors"});
        let c = fake_graphql(
            root.path(),
            vec![
                metadata("head", "alice"),
                page(vec![merge], None),
                json!({"data":{"repository":{"object":{"authors":{
                "nodes":[{"name":"Claude","email":"noreply@anthropic.com"},
                         {"name":"Alice","email":"alice@example.test"}],
                "pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}),
            ],
        );
        let data = load(&c, 14).unwrap();
        let r = &data.records[0];
        assert!(r.is_merge && r.landed && r.ai_assisted);
        assert!(!r.lines_known);
        assert_eq!((r.added, r.removed), (0, 0));
        assert_eq!(r.coauthors.len(), 1);
        assert_eq!(requests(root.path())[2]["variables"]["head"], "merge");
        let mut unknown: ApiCommit =
            serde_json::from_value(commit("unknown", "2026-09-25T12:00:00Z")).unwrap();
        unknown.changed_files_if_available = None;
        assert!(
            !unknown
                .record(
                    &c.repository(&remote(1, "org/project", "main")),
                    &git::CollectOptions::default()
                )
                .lines_known
        );
    }

    #[cfg(unix)]
    #[test]
    fn repeated_cursor_missing_branch_and_empty_repository_are_distinct() {
        let root = tempfile::tempdir().unwrap();
        let mut empty = metadata("head", "alice");
        empty["data"]["repository"]["isEmpty"] = json!(true);
        empty["data"]["repository"]["defaultBranchRef"] = Value::Null;
        let mut missing = empty.clone();
        missing["data"]["repository"]["isEmpty"] = json!(false);
        let c = fake_graphql(
            root.path(),
            vec![
                empty,
                missing,
                metadata("head", "alice"),
                page(vec![], Some("same")),
                page(vec![], Some("same")),
            ],
        );
        assert!(load(&c, 14).unwrap().records.is_empty());
        assert!(
            load(&c, 14)
                .unwrap_err()
                .to_string()
                .contains("default branch")
        );
        assert!(load(&c, 14).unwrap_err().to_string().contains("repeated"));
        assert!(
            !c.repository(&remote(1, "org/project", "main"))
                .path
                .exists()
        );
    }

    #[test]
    #[ignore = "read-only network check using the current gh login"]
    fn live_public_summary_scan() {
        let root = tempfile::tempdir().unwrap();
        let mut c = client(root.path());
        if let Ok(program) = std::env::var("BIGBOARD_TEST_GH") {
            c.gh_program = program.into();
        }
        let cancel = AtomicBool::new(false);
        let remote: RemoteRepository =
            serde_json::from_slice(&c.api("repos/richhaase/bigboard", false, &cancel).unwrap())
                .unwrap();
        let window = Window::new(14, Utc::now());
        for label in ["cold", "warm"] {
            let start = Instant::now();
            let data = c
                .scan_with_progress(
                    &remote,
                    &git::CollectOptions::default(),
                    window,
                    &cancel,
                    &|stage| eprintln!("{stage}"),
                )
                .unwrap();
            eprintln!(
                "{label}: {} summaries, {:.3}s, {} cache bytes",
                data.records.len(),
                start.elapsed().as_secs_f64(),
                fs::metadata(c.repository(&remote).path).unwrap().len()
            );
            assert!(!data.records.is_empty());
            assert!(
                data.records
                    .iter()
                    .all(|r| r.date >= window.since && r.date <= window.until)
            );
        }
    }
}
