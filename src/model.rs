use chrono::{DateTime, FixedOffset};
use std::path::PathBuf;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Repository {
    pub id: String,
    pub path: PathBuf,
    pub name: String,
}

#[derive(Clone, Debug)]
pub struct CommitRecord {
    pub author: String,
    pub email: String,
    pub date: DateTime<FixedOffset>,
    pub added: i64,
    pub removed: i64,
    pub repo_id: String,
    pub repo_name: String,
    pub ai_assisted: bool,
}

#[derive(Clone, Debug, Default)]
pub struct AnalysisOptions {
    pub fuzzy_matching: bool,
    pub include_generated: bool,
    pub ai_identities: Vec<String>,
    pub bot_identities: Vec<String>,
}
