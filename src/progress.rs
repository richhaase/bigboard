//! Observable work stages; progress never participates in analytics.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ScanStage {
    Metadata,
    ReusingSummaries,
    FetchingSummaries { pages: usize, commits: usize },
    ReadingHistory,
    CountingChanges { done: usize, total: usize },
    CheckingMerges { done: usize, total: usize },
}

impl std::fmt::Display for ScanStage {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Metadata => f.write_str("Checking GitHub repository"),
            Self::ReusingSummaries => f.write_str("Default branch unchanged · reusing summaries"),
            Self::FetchingSummaries { pages, commits } => {
                write!(f, "Loading summaries · {pages} pages · {commits} commits")
            }
            Self::ReadingHistory => f.write_str("Reading commit history"),
            Self::CountingChanges { done, total } => {
                write!(f, "Counting changes {done}/{total} commits")
            }
            Self::CheckingMerges { done, total } => write!(f, "Checking merges {done}/{total}"),
        }
    }
}

pub(crate) type Reporter<'a> = dyn Fn(ScanStage) + Send + Sync + 'a;

#[derive(Clone, Debug)]
pub struct ScanProgress {
    pub repository_id: String,
    pub repository_name: String,
    pub stage: ScanStage,
}
