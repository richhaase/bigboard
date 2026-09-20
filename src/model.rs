use chrono::{DateTime, FixedOffset};
use std::path::PathBuf;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Repository {
    pub id: String,
    pub path: PathBuf,
    pub name: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Identity {
    pub name: String,
    pub email: String,
}

/// Canonical mailmapped emails establish identity. Missing emails are scoped to
/// their repository and exact name, never joined by name across repositories.
pub fn identity_key(name: &str, email: &str, repo_id: &str) -> String {
    let email = email.trim().to_lowercase();
    if email.is_empty() {
        format!(
            "local:{}",
            serde_json::to_string(&(repo_id, name)).expect("identity strings serialize")
        )
    } else {
        format!("email:{email}")
    }
}

#[derive(Clone, Debug)]
pub struct CommitRecord {
    pub commit_id: String,
    pub author: String,
    pub email: String,
    pub date: DateTime<FixedOffset>,
    pub added: i64,
    pub removed: i64,
    pub repo_id: String,
    pub repo_name: String,
    pub ai_assisted: bool,
    pub coauthors: Vec<Identity>,
    pub lines_known: bool,
    pub landed: bool,
    pub is_merge: bool,
}

#[derive(Clone, Debug, Default)]
pub struct ScanData {
    pub records: Vec<CommitRecord>,
    pub warnings: Vec<String>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum HistoryScope {
    #[default]
    Landed,
    AllBranches,
}
impl HistoryScope {
    pub fn label(self) -> &'static str {
        match self {
            Self::Landed => "LANDED",
            Self::AllBranches => "ALL BRANCHES",
        }
    }
    pub fn toggle(self) -> Self {
        match self {
            Self::Landed => Self::AllBranches,
            Self::AllBranches => Self::Landed,
        }
    }
}

#[derive(Clone, Debug)]
pub struct AnalysisOptions {
    pub include_generated: bool,
    pub ai_identities: Vec<String>,
    pub bot_identities: Vec<String>,
    pub timezone: chrono_tz::Tz,
}
impl Default for AnalysisOptions {
    fn default() -> Self {
        Self {
            include_generated: false,
            ai_identities: Vec::new(),
            bot_identities: Vec::new(),
            timezone: chrono_tz::UTC,
        }
    }
}
