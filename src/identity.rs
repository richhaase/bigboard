//! User-confirmed identity mappings, shared by every Big Board repository view.
use anyhow::{Context, Result, bail};
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::{
    collections::BTreeSet,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
};

fn version_one() -> u32 {
    1
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct IdentityMerge {
    id: String,
    name: String,
    members: BTreeSet<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct IdentityStore {
    #[serde(default = "version_one")]
    version: u32,
    #[serde(default)]
    merges: Vec<IdentityMerge>,
}

impl Default for IdentityStore {
    fn default() -> Self {
        Self {
            version: 1,
            merges: Vec::new(),
        }
    }
}

/// Independent of --config and repo groups: explicit merges are user-global.
pub fn default_global_path() -> PathBuf {
    crate::config::default_config_path().with_file_name("identities.json")
}

fn valid_key(key: &str) -> bool {
    !key.chars().any(char::is_control)
        && (key
            .strip_prefix("email:")
            .is_some_and(|s| !s.trim().is_empty())
            || key.strip_prefix("local:").is_some_and(|s| !s.is_empty()))
}

fn validate_name(name: &str) -> Result<()> {
    if name.trim().is_empty() || name.chars().any(char::is_control) {
        bail!("choose a nonempty display name without control characters");
    }
    Ok(())
}

impl IdentityStore {
    pub fn load(path: &Path) -> Result<Self> {
        let bytes = match fs::read(path) {
            Ok(bytes) => bytes,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self::default());
            }
            Err(error) => {
                return Err(error)
                    .with_context(|| format!("reading identity mappings {}", path.display()));
            }
        };
        let store: Self = serde_json::from_slice(&bytes)
            .with_context(|| format!("invalid identity mappings in {}", path.display()))?;
        store.validate()?;
        Ok(store)
    }

    fn validate(&self) -> Result<()> {
        if self.version != 1 {
            bail!("unsupported identity mapping version {}", self.version);
        }
        let mut seen = BTreeSet::new();
        for group in &self.merges {
            validate_name(&group.name)?;
            if group.members.len() < 2 || !group.members.contains(&group.id) {
                bail!("identity mapping must contain its ID and at least two members");
            }
            for member in &group.members {
                if !valid_key(member) || !seen.insert(member) {
                    bail!("invalid or overlapping identity mapping member");
                }
            }
        }
        Ok(())
    }

    pub fn canonical_id(&self, key: &str) -> String {
        self.merges
            .iter()
            .find(|group| group.members.contains(key))
            .map_or_else(|| key.to_owned(), |group| group.id.clone())
    }

    pub fn display_name(&self, id: &str) -> Option<&str> {
        self.merges
            .iter()
            .find(|group| group.id == id)
            .map(|group| group.name.as_str())
    }

    /// Pure in-memory operation; the TUI uses merge_and_save before applying it.
    pub fn merge(
        &mut self,
        source_id: &str,
        target_id: &str,
        display_name: &str,
    ) -> Result<String> {
        validate_name(display_name)?;
        if !valid_key(source_id) || !valid_key(target_id) {
            bail!("invalid contributor identity");
        }
        let source = self.canonical_id(source_id);
        let target = self.canonical_id(target_id);
        let mut members = BTreeSet::from([source.clone(), target.clone()]);
        for group in &self.merges {
            if group.id == source || group.id == target {
                members.extend(group.members.iter().cloned());
            }
        }
        if members.len() < 2 {
            bail!("choose two distinct contributor identities");
        }
        // The anchor is itself a member, so older/stale selected IDs continue
        // resolving even when two existing groups are joined.
        let id = members.first().expect("nonempty identity group").clone();
        self.merges
            .retain(|group| group.id != source && group.id != target);
        self.merges.push(IdentityMerge {
            id: id.clone(),
            name: display_name.trim().to_owned(),
            members,
        });
        self.merges.sort_by(|a, b| a.id.cmp(&b.id));
        Ok(id)
    }

    pub fn merge_and_save(
        path: &Path,
        source_id: &str,
        target_id: &str,
        display_name: &str,
    ) -> Result<(Self, String)> {
        let parent = path
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or(Path::new("."));
        fs::create_dir_all(parent)
            .with_context(|| format!("creating identity directory {}", parent.display()))?;
        let lock_path = path.with_extension("lock");
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(&lock_path)
            .context("opening identity mapping lock")?;
        FileExt::try_lock_exclusive(&lock)
            .context("identity mappings are busy; retry the merge")?;
        // Reload inside the lock: another running dashboard may have saved a
        // different merge since this dashboard loaded its snapshot.
        let mut store = Self::load(path)?;
        let id = store.merge(source_id, target_id, display_name)?;
        store.validate()?;
        let mut temporary =
            tempfile::NamedTempFile::new_in(parent).context("creating identity mapping file")?;
        serde_json::to_writer_pretty(&mut temporary, &store)?;
        writeln!(&mut temporary)?;
        temporary
            .as_file()
            .sync_all()
            .context("flushing identity mappings")?;
        temporary
            .persist(path)
            .map_err(|error| error.error)
            .context("saving identity mappings")?;
        Ok((store, id))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn only_explicit_merges_link_distinct_emails_and_survive_restart() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("identities.json");
        assert_eq!(
            IdentityStore::load(&path)
                .unwrap()
                .canonical_id("email:mike@work.test"),
            "email:mike@work.test"
        );
        let (_, id) = IdentityStore::merge_and_save(
            &path,
            "email:mike@work.test",
            "email:mbiggly@home.test",
            "Mike",
        )
        .unwrap();
        let store = IdentityStore::load(&path).unwrap();
        assert_eq!(store.canonical_id("email:mike@work.test"), id);
        assert_eq!(store.canonical_id("email:mbiggly@home.test"), id);
        assert_eq!(store.display_name(&id), Some("Mike"));
        assert_eq!(
            store.canonical_id("email:other-mike@work.test"),
            "email:other-mike@work.test"
        );
    }
    #[test]
    fn joining_groups_and_stale_ids_preserves_every_member() {
        let mut store = IdentityStore::default();
        let a = store.merge("email:a@test", "email:b@test", "A").unwrap();
        let b = store.merge("email:c@test", "email:d@test", "B").unwrap();
        let id = store.merge(&a, &b, "Together").unwrap();
        for raw in [
            "email:a@test",
            "email:b@test",
            "email:c@test",
            "email:d@test",
        ] {
            assert_eq!(store.canonical_id(raw), id);
        }
        let id2 = store.merge(&b, "email:e@test", "Together").unwrap();
        assert_eq!(store.canonical_id("email:d@test"), id2);
        assert_eq!(store.canonical_id("email:e@test"), id2);
        store.validate().unwrap();
    }
    #[test]
    fn separate_saves_reload_existing_mappings() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("identities.json");
        IdentityStore::merge_and_save(&path, "email:a@test", "email:b@test", "One").unwrap();
        let (store, _) =
            IdentityStore::merge_and_save(&path, "email:c@test", "email:d@test", "Two").unwrap();
        assert_eq!(
            store.display_name(&store.canonical_id("email:a@test")),
            Some("One")
        );
        assert_eq!(
            store.display_name(&store.canonical_id("email:d@test")),
            Some("Two")
        );
    }
    #[test]
    fn failed_merges_preserve_disk_contents_and_lock_contention_is_reported() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("identities.json");
        IdentityStore::merge_and_save(&path, "email:a@test", "email:b@test", "One").unwrap();
        let before = fs::read(&path).unwrap();
        assert!(
            IdentityStore::merge_and_save(&path, "email:a@test", "email:c@test", " \n ").is_err()
        );
        assert_eq!(before, fs::read(&path).unwrap());
        let lock = OpenOptions::new()
            .read(true)
            .write(true)
            .open(path.with_extension("lock"))
            .unwrap();
        FileExt::lock_exclusive(&lock).unwrap();
        assert!(
            IdentityStore::merge_and_save(&path, "email:c@test", "email:d@test", "Two").is_err()
        );
        assert_eq!(before, fs::read(&path).unwrap());
    }
    #[test]
    fn invalid_mapping_files_are_never_overwritten() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("identities.json");
        for content in [
            "not json",
            r#"{"version":2,"merges":[]}"#,
            r#"{"version":1,"merges":[{"id":"email:a","name":"A","members":["email:a","email:b"]},{"id":"email:b","name":"B","members":["email:b","email:c"]}]}"#,
        ] {
            fs::write(&path, content).unwrap();
            assert!(IdentityStore::load(&path).is_err());
            assert!(IdentityStore::merge_and_save(&path, "email:x", "email:y", "X").is_err());
            assert_eq!(fs::read_to_string(&path).unwrap(), content);
        }
    }
}
