//! Contributor identity, unique-commit accounting, and reporting-calendar totals.

use std::cmp::Ordering;
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};

use anyhow::{Result, bail};
use chrono::{DateTime, Duration, FixedOffset, NaiveDate, SecondsFormat, TimeZone, Utc};
use chrono_tz::Tz;
use serde::{Serialize, Serializer};

use crate::identity::IdentityStore;
use crate::model::{CommitRecord, HistoryScope, identity_key};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum SortField {
    #[default]
    Total,
    Commits,
    Added,
    Removed,
    Net,
    AI,
}

impl SortField {
    pub fn parse(value: &str) -> Result<Self> {
        match value.to_lowercase().as_str() {
            "total" | "impact" | "lines changed" => Ok(Self::Total),
            "commits" => Ok(Self::Commits),
            "added" => Ok(Self::Added),
            "removed" => Ok(Self::Removed),
            "net" => Ok(Self::Net),
            "ai" | "detected ai" => Ok(Self::AI),
            _ => bail!("invalid sort {value:?} (want commits|added|removed|net|ai|total)"),
        }
    }

    pub fn next(self) -> Self {
        match self {
            Self::Total => Self::Commits,
            Self::Commits => Self::Added,
            Self::Added => Self::Removed,
            Self::Removed => Self::Net,
            Self::Net => Self::AI,
            Self::AI => Self::Total,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Self::Total => "LINES CHANGED",
            Self::Commits => "COMMITS",
            Self::Added => "ADDED",
            Self::Removed => "REMOVED",
            Self::Net => "NET",
            Self::AI => "DETECTED AI",
        }
    }
}

#[derive(Clone)]
pub struct AggregateOptions {
    pub identities: IdentityStore,
    pub timezone: Tz,
    pub bot_identities: Vec<String>,
}

impl Default for AggregateOptions {
    fn default() -> Self {
        Self {
            identities: IdentityStore::default(),
            timezone: chrono_tz::UTC,
            bot_identities: Vec::new(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct AuthorStats {
    pub id: String,
    pub name: String,
    pub emails: HashSet<String>,
    pub member_ids: HashSet<String>,
    /// Unique commits credited to this contributor as primary author.
    pub commits: i64,
    /// Human participation on commits authored by somebody else.
    pub coauthored_commits: i64,
    pub added: i64,
    pub removed: i64,
    pub net: i64,
    pub total_change: i64,
    pub ai_commits: i64,
    /// Authored commits for which no selected repository supplied a known diff.
    pub unknown_line_commits: i64,
    pub bot: bool,
    #[serde(serialize_with = "serialize_date")]
    pub first_commit: DateTime<FixedOffset>,
    #[serde(serialize_with = "serialize_date")]
    pub last_commit: DateTime<FixedOffset>,
    pub active_days: i64,
    /// Repository associations overlap when clones/forks share a commit.
    pub per_repo: BTreeMap<String, RepoContribution>,
    pub daily: BTreeMap<NaiveDate, RepoContribution>,
    pub monthly: BTreeMap<String, RepoContribution>,
    #[serde(skip)]
    pub aliases: HashSet<String>,
}

impl Default for AuthorStats {
    fn default() -> Self {
        Self {
            id: String::new(),
            name: String::new(),
            emails: HashSet::new(),
            member_ids: HashSet::new(),
            commits: 0,
            coauthored_commits: 0,
            added: 0,
            removed: 0,
            net: 0,
            total_change: 0,
            ai_commits: 0,
            unknown_line_commits: 0,
            bot: false,
            first_commit: zero_time(),
            last_commit: zero_time(),
            active_days: 0,
            per_repo: BTreeMap::new(),
            daily: BTreeMap::new(),
            monthly: BTreeMap::new(),
            aliases: HashSet::new(),
        }
    }
}

impl AuthorStats {
    pub fn removed_added_ratio(&self) -> Option<f64> {
        (self.added != 0 && self.unknown_line_commits == 0)
            .then(|| self.removed as f64 / self.added as f64)
    }

    /// Rounded down for display only; sorting compares the exact ratios.
    pub fn ai_percent(&self) -> i64 {
        if self.commits == 0 {
            0
        } else {
            (i128::from(self.ai_commits) * 100 / i128::from(self.commits)) as i64
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize)]
pub struct RepoContribution {
    pub commits: i64,
    pub coauthored_commits: i64,
    pub added: i64,
    pub removed: i64,
    pub net: i64,
    pub total_change: i64,
    pub ai_commits: i64,
    pub unknown_line_commits: i64,
}

impl RepoContribution {
    fn add(&mut self, contribution: &Self) {
        self.commits += contribution.commits;
        self.coauthored_commits += contribution.coauthored_commits;
        self.added += contribution.added;
        self.removed += contribution.removed;
        self.net += contribution.net;
        self.total_change += contribution.total_change;
        self.ai_commits += contribution.ai_commits;
        self.unknown_line_commits += contribution.unknown_line_commits;
    }
}

fn zero_time() -> DateTime<FixedOffset> {
    Utc.with_ymd_and_hms(1, 1, 1, 0, 0, 0)
        .single()
        .expect("the zero timestamp is valid")
        .fixed_offset()
}

fn serialize_date<S>(date: &DateTime<FixedOffset>, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(&date.to_rfc3339_opts(SecondsFormat::AutoSi, true))
}

pub fn filter_by_time(records: &[CommitRecord], duration: Duration) -> Vec<CommitRecord> {
    filter_by_time_at(records, duration, Utc::now().fixed_offset())
}

/// Apply a single query clock: (now-duration, now], or all history through now.
pub fn filter_by_time_at(
    records: &[CommitRecord],
    duration: Duration,
    now: DateTime<FixedOffset>,
) -> Vec<CommitRecord> {
    let cutoff = now - duration;
    records
        .iter()
        .filter(|record| record.date <= now && (duration.is_zero() || record.date > cutoff))
        .cloned()
        .collect()
}

pub fn filter_by_repo(records: &[CommitRecord], excluded: &HashSet<String>) -> Vec<CommitRecord> {
    records
        .iter()
        .filter(|record| {
            !excluded.contains(&record.repo_id) && !excluded.contains(&record.repo_name)
        })
        .cloned()
        .collect()
}

pub fn filter_by_scope(records: &[CommitRecord], scope: HistoryScope) -> Vec<CommitRecord> {
    records
        .iter()
        .filter(|record| scope == HistoryScope::AllBranches || record.landed)
        .cloned()
        .collect()
}

#[derive(PartialEq, Eq, PartialOrd, Ord)]
enum CommitKey<'a> {
    Object(&'a str),
    // Missing IDs must never accidentally collapse unrelated input records.
    Missing(usize),
}

#[derive(Clone, Copy)]
struct IdentityRef<'a> {
    name: &'a str,
    email: &'a str,
    repo_id: &'a str,
}

impl IdentityRef<'_> {
    fn member_id(self) -> String {
        identity_key(self.name, self.email, self.repo_id)
    }
}

struct Participant<'a> {
    primary: bool,
    preferred_name: &'a str,
    identities: Vec<IdentityRef<'a>>,
}

fn participants<'a>(
    record: &'a CommitRecord,
    copies: &[&'a CommitRecord],
    options: &AggregateOptions,
) -> BTreeMap<String, Participant<'a>> {
    let primary = IdentityRef {
        name: &record.author,
        email: &record.email,
        repo_id: &record.repo_id,
    };
    let id = options.identities.canonical_id(&primary.member_id());
    let mut participants = BTreeMap::from([(
        id,
        Participant {
            primary: true,
            preferred_name: primary.name,
            identities: vec![primary],
        },
    )]);
    // The Git collector has already removed detected AI coauthors using its
    // built-in identities and the configured AI overrides.
    for coauthor in &record.coauthors {
        if is_bot_identity(&coauthor.name, &coauthor.email, &options.bot_identities) {
            continue;
        }
        let identity = IdentityRef {
            name: &coauthor.name,
            email: &coauthor.email,
            repo_id: &record.repo_id,
        };
        let id = options.identities.canonical_id(&identity.member_id());
        let participant = participants.entry(id).or_insert_with(|| Participant {
            primary: false,
            preferred_name: identity.name,
            identities: vec![],
        });
        if !participant.primary && prefer_name(identity.name, participant.preferred_name) {
            participant.preferred_name = identity.name;
        }
        participant.identities.push(identity);
    }
    // Preserve observed aliases from other clones, without inferring merges
    // when repositories have conflicting mailmap rules for the same object.
    for copy in copies {
        // A sole primary identity is already present. Keep the reconciliation
        // pass for coauthors, whose saved mappings can affect identity metadata.
        if std::ptr::eq(*copy, record) && record.coauthors.is_empty() {
            continue;
        }
        let primary = IdentityRef {
            name: &copy.author,
            email: &copy.email,
            repo_id: &copy.repo_id,
        };
        let others = copy.coauthors.iter().map(|identity| IdentityRef {
            name: &identity.name,
            email: &identity.email,
            repo_id: &copy.repo_id,
        });
        for identity in std::iter::once(primary).chain(others) {
            let id = options.identities.canonical_id(&identity.member_id());
            if let Some(participant) = participants.get_mut(&id) {
                participant.identities.push(identity);
            }
        }
    }
    participants
}

fn commit_groups<'a>(
    records: impl IntoIterator<Item = &'a CommitRecord>,
) -> BTreeMap<CommitKey<'a>, Vec<&'a CommitRecord>> {
    let mut commits = BTreeMap::new();
    for (index, record) in records.into_iter().enumerate() {
        let key = if record.commit_id.is_empty() {
            CommitKey::Missing(index)
        } else {
            CommitKey::Object(&record.commit_id)
        };
        commits.entry(key).or_insert_with(Vec::new).push(record);
    }
    commits
}

fn representative<'a>(copies: &[&'a CommitRecord]) -> &'a CommitRecord {
    // A full-history copy supplies the diff missing at a shallow boundary.
    // Repository/name ordering makes remaining choices input-order neutral.
    copies
        .iter()
        .copied()
        .min_by_key(|record| {
            (
                !record.lines_known,
                &record.repo_id,
                &record.repo_name,
                &record.email,
                &record.author,
                record.added,
                record.removed,
            )
        })
        .expect("each commit group contains a record")
}

fn resolved_attribution(
    record: &CommitRecord,
    options: &AggregateOptions,
) -> BTreeMap<String, bool> {
    participants(record, &[], options)
        .into_iter()
        .map(|(id, participant)| (id, participant.primary))
        .collect()
}

/// Qualify unique-object attribution when repository mailmaps disagree.
/// Call with the same filtered records and options supplied to `aggregate`.
pub fn attribution_warnings(records: &[CommitRecord], options: &AggregateOptions) -> Vec<String> {
    let mut warnings = Vec::new();
    for copies in commit_groups(records).into_values() {
        if copies.len() < 2 {
            continue;
        }
        let record = representative(&copies);
        let attribution = resolved_attribution(record, options);
        if copies
            .iter()
            .all(|copy| resolved_attribution(copy, options) == attribution)
        {
            continue;
        }
        let repositories: BTreeSet<_> = copies.iter().map(|copy| copy.repo_name.as_str()).collect();
        let primary = attribution
            .iter()
            .find(|(_, primary)| **primary)
            .map(|(id, _)| id.as_str())
            .expect("each commit has a primary author");
        let coauthors = attribution
            .iter()
            .filter(|(_, primary)| !**primary)
            .map(|(id, _)| id.as_str())
            .collect::<Vec<_>>();
        let chosen_name = options
            .identities
            .display_name(primary)
            .unwrap_or(&record.author);
        let chosen = if coauthors.is_empty() {
            format!("{chosen_name} ({primary}) with no human coauthors")
        } else {
            format!(
                "{chosen_name} ({primary}) with coauthors {}",
                coauthors.join(", ")
            )
        };
        let oid: String = record.commit_id.chars().take(12).collect();
        warnings.push(format!(
            "Shared commit {oid} has conflicting contributor mappings across {}; using {chosen} from {}. Review repository mailmaps or merge the identities explicitly.",
            repositories.into_iter().collect::<Vec<_>>().join(", "),
            record.repo_name,
        ));
    }
    warnings
}

/// Merge-choice catalog containing every observed resolved identity, including
/// competing repository mailmaps for one object. Each row counts only copies in
/// which that identity participates; these rows must not be summed as board totals.
pub fn identity_catalog(records: &[CommitRecord], options: &AggregateOptions) -> Vec<AuthorStats> {
    identity_catalog_iter(records, options)
}

pub(crate) fn identity_catalog_iter<'a>(
    records: impl IntoIterator<Item = &'a CommitRecord>,
    options: &AggregateOptions,
) -> Vec<AuthorStats> {
    let mut identities: BTreeMap<String, BTreeMap<CommitKey<'_>, Vec<&CommitRecord>>> =
        BTreeMap::new();
    for (index, record) in records.into_iter().enumerate() {
        for id in participants(record, &[], options).into_keys() {
            let key = if record.commit_id.is_empty() {
                CommitKey::Missing(index)
            } else {
                CommitKey::Object(&record.commit_id)
            };
            identities
                .entry(id)
                .or_default()
                .entry(key)
                .or_default()
                .push(record);
        }
    }
    let mut catalog = Vec::with_capacity(identities.len());
    for (id, commits) in identities {
        catalog.extend(aggregate_groups(commits.into_values(), options, Some(&id)));
    }
    catalog.sort_by(|a, b| a.name.cmp(&b.name).then_with(|| a.id.cmp(&b.id)));
    catalog
}

/// Aggregate unique Git objects, retaining overlapping repository associations.
/// Callers apply repository, history-scope, and query-time filters first.
pub fn aggregate(records: &[CommitRecord], options: &AggregateOptions) -> Vec<AuthorStats> {
    aggregate_iter(records, options)
}

pub(crate) fn aggregate_iter<'a>(
    records: impl IntoIterator<Item = &'a CommitRecord>,
    options: &AggregateOptions,
) -> Vec<AuthorStats> {
    aggregate_groups(commit_groups(records).into_values(), options, None)
}

fn aggregate_groups<'a>(
    groups: impl IntoIterator<Item = Vec<&'a CommitRecord>>,
    options: &AggregateOptions,
    only_id: Option<&str>,
) -> Vec<AuthorStats> {
    let mut by_id: BTreeMap<String, AuthorStats> = BTreeMap::new();
    let mut name_counts: HashMap<String, BTreeMap<String, usize>> = HashMap::new();
    for copies in groups {
        let record = representative(&copies);
        let ai_assisted = copies.iter().any(|record| record.ai_assisted);
        let mut repositories = BTreeMap::new();
        for copy in &copies {
            let key = if copy.repo_id.is_empty() {
                &copy.repo_name
            } else {
                &copy.repo_id
            };
            repositories
                .entry(key)
                .and_modify(|label: &mut &String| {
                    if copy.repo_name < **label {
                        *label = &copy.repo_name;
                    }
                })
                .or_insert(&copy.repo_name);
        }
        let repository_names: BTreeSet<_> = repositories.into_values().collect();
        let local_date = record.date.with_timezone(&options.timezone);
        let day = local_date.date_naive();
        let month = local_date.format("%Y-%m").to_string();
        for (id, participant) in participants(record, &copies, options) {
            if only_id.is_some_and(|selected| selected != id) {
                continue;
            }
            let author = by_id.entry(id.clone()).or_insert_with(|| AuthorStats {
                id: id.clone(),
                ..AuthorStats::default()
            });
            for identity in participant.identities {
                author.aliases.insert(identity.name.to_owned());
                author.member_ids.insert(identity.member_id());
                let email = identity.email.trim().to_lowercase();
                if !email.is_empty() {
                    author.emails.insert(email);
                }
                author.bot |=
                    is_bot_identity(identity.name, identity.email, &options.bot_identities);
            }
            *name_counts
                .entry(id)
                .or_default()
                .entry(participant.preferred_name.to_owned())
                .or_default() += 1;
            let date = local_date.fixed_offset();
            if author.commits == 0 && author.coauthored_commits == 0 {
                author.first_commit = date;
                author.last_commit = date;
            } else {
                author.first_commit = author.first_commit.min(date);
                author.last_commit = author.last_commit.max(date);
            }
            let contribution = if participant.primary {
                let (added, removed) = if record.lines_known {
                    (record.added, record.removed)
                } else {
                    (0, 0)
                };
                RepoContribution {
                    commits: 1,
                    added,
                    removed,
                    net: added - removed,
                    total_change: added + removed,
                    ai_commits: i64::from(ai_assisted),
                    unknown_line_commits: i64::from(!record.lines_known),
                    ..RepoContribution::default()
                }
            } else {
                RepoContribution {
                    coauthored_commits: 1,
                    ..RepoContribution::default()
                }
            };
            author.commits += contribution.commits;
            author.coauthored_commits += contribution.coauthored_commits;
            author.added += contribution.added;
            author.removed += contribution.removed;
            author.net += contribution.net;
            author.total_change += contribution.total_change;
            author.ai_commits += contribution.ai_commits;
            author.unknown_line_commits += contribution.unknown_line_commits;
            author.daily.entry(day).or_default().add(&contribution);
            author
                .monthly
                .entry(month.clone())
                .or_default()
                .add(&contribution);
            for name in &repository_names {
                author
                    .per_repo
                    .entry((*name).clone())
                    .or_default()
                    .add(&contribution);
            }
        }
    }
    let mut authors: Vec<_> = by_id
        .into_values()
        .map(|mut author| {
            author.name = options
                .identities
                .display_name(&author.id)
                .map(str::to_owned)
                .unwrap_or_else(|| {
                    name_counts[&author.id]
                        .iter()
                        .max_by(|(name_a, count_a), (name_b, count_b)| {
                            count_a
                                .cmp(count_b)
                                .then_with(|| name_a.chars().count().cmp(&name_b.chars().count()))
                                .then_with(|| name_b.cmp(name_a))
                        })
                        .map(|(name, _)| name.clone())
                        .unwrap_or_default()
                });
            if author.name.is_empty() {
                author.name = author
                    .emails
                    .iter()
                    .min()
                    .cloned()
                    .unwrap_or_else(|| "Unknown contributor".into());
            }
            author.active_days = author.daily.len() as i64;
            author
        })
        .collect();
    authors.sort_by(|a, b| a.name.cmp(&b.name).then_with(|| a.id.cmp(&b.id)));
    authors
}

fn prefer_name(a: &str, b: &str) -> bool {
    a.chars().count() > b.chars().count() || (a.chars().count() == b.chars().count() && a < b)
}

fn metric_value(author: &AuthorStats, field: SortField) -> i64 {
    match field {
        SortField::Total => author.total_change,
        SortField::Commits => author.commits,
        SortField::Added => author.added,
        SortField::Removed => author.removed,
        SortField::Net => author.net,
        SortField::AI => unreachable!("AI ordering compares exact ratios"),
    }
}

fn descending_ai_ratio(a: &AuthorStats, b: &AuthorStats) -> Ordering {
    let denominator_a = i128::from(a.commits.max(1));
    let denominator_b = i128::from(b.commits.max(1));
    (i128::from(b.ai_commits) * denominator_a).cmp(&(i128::from(a.ai_commits) * denominator_b))
}

pub fn sort(authors: &mut [AuthorStats], field: SortField) {
    authors.sort_by(|a, b| {
        let metric_order = if field == SortField::AI {
            descending_ai_ratio(a, b)
        } else {
            metric_value(b, field).cmp(&metric_value(a, field))
        };
        metric_order
            .then_with(|| b.total_change.cmp(&a.total_change))
            .then_with(|| b.commits.cmp(&a.commits))
            .then_with(|| a.name.cmp(&b.name))
            .then_with(|| a.id.cmp(&b.id))
    });
}

pub fn is_bot_identity(name: &str, email: &str, extra: &[String]) -> bool {
    let raw_name = lowercase(name.trim());
    let address = lowercase(email.trim().trim_matches(['<', '>', ' ']));
    for entry in extra {
        let entry = lowercase(entry.trim());
        if entry.is_empty() {
            continue;
        }
        let matched = if entry.starts_with('@') {
            address.ends_with(&entry)
        } else if entry.contains('@') {
            address == entry
        } else {
            raw_name == entry
        };
        if matched {
            return true;
        }
    }
    if raw_name.contains("[bot]") || address.split('@').next().unwrap_or("").contains("[bot]") {
        return true;
    }
    let base = raw_name.strip_suffix("[bot]").unwrap_or(&raw_name).trim();
    let base = base.strip_suffix(" bot").unwrap_or(base).trim();
    matches!(
        base,
        "dependabot"
            | "dependabot-preview"
            | "renovate"
            | "github-actions"
            | "snyk-bot"
            | "greenkeeper"
            | "imgbot"
            | "mergify"
            | "allcontributors"
            | "pre-commit-ci"
            | "codecov"
    )
}

// Go uses simple per-codepoint lowercase, not contextual/full string folding.
fn lowercase(value: &str) -> String {
    value
        .chars()
        .map(|character| character.to_lowercase().next().unwrap_or(character))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::Identity;

    fn record(id: &str, name: &str, email: &str, repo: &str) -> CommitRecord {
        CommitRecord {
            commit_id: id.into(),
            author: name.into(),
            email: email.into(),
            date: DateTime::parse_from_rfc3339("2026-09-20T12:00:00Z").unwrap(),
            added: 10,
            removed: 3,
            repo_id: format!("/repos/{repo}"),
            repo_name: repo.into(),
            ai_assisted: false,
            coauthors: Vec::new(),
            lines_known: true,
            landed: true,
            is_merge: false,
        }
    }

    fn coauthor(name: &str, email: &str) -> Identity {
        Identity {
            name: name.into(),
            email: email.into(),
        }
    }

    fn author<'a>(authors: &'a [AuthorStats], id: &str) -> &'a AuthorStats {
        authors.iter().find(|author| author.id == id).unwrap()
    }

    #[test]
    fn same_names_stay_separate_while_matching_emails_keep_aliases() {
        let records = vec![
            record("one", "Alex Lee", "alex-one@test", "a"),
            record("two", "Alex Lee", "alex-two@test", "a"),
            record("three", "alee", " ALEX-ONE@TEST ", "b"),
        ];
        let authors = aggregate(&records, &AggregateOptions::default());
        assert_eq!(authors.len(), 2);
        let first = author(&authors, "email:alex-one@test");
        assert_eq!((first.commits, first.added, first.removed), (2, 20, 6));
        assert_eq!(
            first.aliases,
            HashSet::from(["Alex Lee".into(), "alee".into()])
        );
        assert_eq!(first.emails, HashSet::from(["alex-one@test".into()]));
        assert_eq!(
            first.member_ids,
            HashSet::from(["email:alex-one@test".into()])
        );
        assert_eq!(author(&authors, "email:alex-two@test").commits, 1);
        assert_eq!(authors[0].name, authors[1].name);
        assert_ne!(authors[0].id, authors[1].id);
    }

    #[test]
    fn explicit_mapping_combines_emails_and_preserves_selection_id_across_filters() {
        let records = vec![
            record("one", "Alice Smith", "alice@work.test", "work"),
            record("two", "asmith", "alice@home.test", "personal"),
            record("three", "Alice Smith", "other-person@test", "work"),
        ];
        let mut options = AggregateOptions::default();
        let id = options
            .identities
            .merge("email:alice@work.test", "email:alice@home.test", "Alice")
            .unwrap();
        let authors = aggregate(&records, &options);
        assert_eq!(authors.len(), 2);
        let alice = author(&authors, &id);
        assert_eq!(
            (alice.name.as_str(), alice.commits, alice.total_change),
            ("Alice", 2, 26)
        );
        assert_eq!(alice.emails.len(), 2);
        assert_eq!(alice.member_ids.len(), 2);
        let filtered = filter_by_repo(&records, &HashSet::from(["work".into()]));
        let filtered_authors = aggregate(&filtered, &options);
        assert_eq!(filtered_authors[0].id, id);
        assert_eq!(filtered_authors[0].name, "Alice");
        assert_eq!(filtered_authors[0].commits, 1);
    }

    #[test]
    fn missing_email_identities_are_scoped_to_repository_and_exact_name() {
        let records = vec![
            record("one", "Alex", "", "a"),
            record("two", "Alex", "", "b"),
            record("three", "alex", "", "a"),
            record("four", "Alex", "", "a"),
        ];
        let authors = aggregate(&records, &AggregateOptions::default());
        assert_eq!(authors.len(), 3);
        assert_eq!(
            author(&authors, &identity_key("Alex", "", "/repos/a")).commits,
            2
        );
        assert_eq!(
            author(&authors, &identity_key("Alex", "", "/repos/b")).commits,
            1
        );
        assert_eq!(
            author(&authors, &identity_key("alex", "", "/repos/a")).commits,
            1
        );
    }

    #[test]
    fn merge_catalog_keeps_every_observed_conflicting_identity_without_changing_board_counts() {
        let mut a = record("shared", "Alice", "a@test", "a");
        a.coauthors = vec![
            coauthor("Pair", "p@test"),
            coauthor("Self", "a@test"),
            coauthor("worker[bot]", "worker@test"),
        ];
        let mut b = record("shared", "Alice", "b@test", "b");
        b.coauthors = vec![
            coauthor("Pair", "q@test"),
            coauthor("Configured Bot", "agent@bots.test"),
        ];
        let records = vec![a, b];
        let mut options = AggregateOptions {
            bot_identities: vec!["@bots.test".into()],
            ..Default::default()
        };
        let catalog = identity_catalog(&records, &options);
        assert_eq!(catalog.len(), 4);
        for (id, repo) in [("email:a@test", "a"), ("email:b@test", "b")] {
            let entry = author(&catalog, id);
            assert_eq!(
                (entry.commits, entry.coauthored_commits, entry.added),
                (1, 0, 10)
            );
            assert_eq!(entry.per_repo.len(), 1);
            assert!(entry.per_repo.contains_key(repo));
        }
        for (id, repo) in [("email:p@test", "a"), ("email:q@test", "b")] {
            let entry = author(&catalog, id);
            assert_eq!(
                (entry.commits, entry.coauthored_commits, entry.added),
                (0, 1, 0)
            );
            assert_eq!(entry.per_repo.len(), 1);
            assert!(entry.per_repo.contains_key(repo));
        }
        assert_eq!(
            catalog,
            identity_catalog(&[records[1].clone(), records[0].clone()], &options)
        );
        let board = aggregate(&records, &options);
        assert_eq!(board.iter().map(|entry| entry.commits).sum::<i64>(), 1);
        assert_eq!(board.len(), 2);
        // The identity visible after excluding the representative remains in the catalog.
        let filtered = filter_by_repo(&records, &HashSet::from(["a".into()]));
        for entry in aggregate(&filtered, &options) {
            assert!(catalog.iter().any(|candidate| candidate.id == entry.id));
        }
        options
            .identities
            .merge("email:a@test", "email:b@test", "Alice Combined")
            .unwrap();
        options
            .identities
            .merge("email:p@test", "email:q@test", "Pair Combined")
            .unwrap();
        let merged = identity_catalog(&records, &options);
        assert_eq!(merged.len(), 2);
        assert_eq!(author(&merged, "email:a@test").commits, 1);
        assert_eq!(author(&merged, "email:a@test").name, "Alice Combined");
        assert_eq!(author(&merged, "email:a@test").member_ids.len(), 2);
        assert_eq!(author(&merged, "email:a@test").per_repo.len(), 2);
        assert_eq!(author(&merged, "email:p@test").coauthored_commits, 1);
        assert_eq!(author(&merged, "email:p@test").member_ids.len(), 2);
        assert!(attribution_warnings(&records, &options).is_empty());
        assert_eq!(
            aggregate(&records, &options)
                .iter()
                .map(|entry| entry.commits)
                .sum::<i64>(),
            1
        );
        assert!(identity_catalog(&[], &options).is_empty());
    }

    #[test]
    fn conflicting_mailmaps_warn_without_inferred_identity_merges() {
        let mut a = record("shared-object", "Alice", "a@test", "a");
        a.coauthors = vec![coauthor("Pair", "p@test")];
        let mut b = record("shared-object", "Alice", "b@test", "b");
        b.coauthors = vec![coauthor("Pair", "q@test")];
        let records = vec![a.clone(), b.clone()];
        let options = AggregateOptions::default();
        let warnings = attribution_warnings(&records, &options);
        assert_eq!(warnings.len(), 1);
        assert!(warnings[0].contains("across a, b"));
        assert!(warnings[0].contains("Alice (email:a@test) with coauthors email:p@test from a"));
        let authors = aggregate(&records, &options);
        assert_eq!(authors.len(), 2);
        assert_eq!(author(&authors, "email:a@test").commits, 1);
        assert_eq!(author(&authors, "email:p@test").coauthored_commits, 1);
        assert_eq!(
            attribution_warnings(&[b.clone(), a.clone()], &options),
            warnings
        );
        a.lines_known = false;
        let partial = vec![a, b];
        let warnings = attribution_warnings(&partial, &options);
        assert!(warnings[0].contains("Alice (email:b@test) with coauthors email:q@test from b"));
        assert_eq!(
            author(&aggregate(&partial, &options), "email:b@test").commits,
            1
        );
        let selected = filter_by_repo(&records, &HashSet::from(["b".into()]));
        assert!(attribution_warnings(&selected, &options).is_empty());
        let mut merged = options.clone();
        merged
            .identities
            .merge("email:a@test", "email:b@test", "Alice")
            .unwrap();
        // Reconciling only the primary identity still leaves conflicting coauthors.
        assert_eq!(attribution_warnings(&records, &merged).len(), 1);
        merged
            .identities
            .merge("email:p@test", "email:q@test", "Pair")
            .unwrap();
        assert!(attribution_warnings(&records, &merged).is_empty());
    }

    #[test]
    fn attribution_comparison_uses_participant_roles_and_ignores_names_and_bots() {
        let mut a = record("one", "Alice", "a@test", "a");
        a.coauthors = vec![coauthor("Pair", "p@test")];
        let mut b = record("one", "Alias", " A@TEST ", "b");
        b.coauthors = vec![
            coauthor("Pair Alias", " P@TEST "),
            coauthor("Self", "a@test"),
            coauthor("worker[bot]", "worker@test"),
            coauthor("Custom Agent", "agent@bots.test"),
        ];
        let options = AggregateOptions {
            bot_identities: vec!["@bots.test".into()],
            ..Default::default()
        };
        assert!(attribution_warnings(&[a.clone(), b.clone()], &options).is_empty());
        b.email = "p@test".into();
        b.coauthors = vec![coauthor("Other Primary", "a@test")];
        assert_eq!(
            attribution_warnings(&[a.clone(), b.clone()], &options).len(),
            1
        );
        let mut merged = options;
        merged
            .identities
            .merge("email:a@test", "email:p@test", "Same person")
            .unwrap();
        assert!(attribution_warnings(&[a, b], &merged).is_empty());
    }

    #[test]
    fn shared_objects_count_once_but_keep_each_repository_association() {
        let a = record("shared", "A", "a@test", "a");
        let mut b = a.clone();
        b.repo_id = "/repos/b".into();
        b.repo_name = "b".into();
        b.ai_assisted = true;
        let records = vec![a.clone(), a, b, record("other", "A", "a@test", "b")];
        let authors = aggregate(&records, &AggregateOptions::default());
        let a = &authors[0];
        assert_eq!(
            (
                a.commits,
                a.added,
                a.removed,
                a.net,
                a.total_change,
                a.ai_commits
            ),
            (2, 20, 6, 14, 26, 1)
        );
        assert_eq!(a.per_repo["a"].commits, 1);
        assert_eq!(a.per_repo["b"].commits, 2);
        assert_eq!(a.daily.values().next().unwrap().commits, 2);
        assert_eq!(a.monthly["2026-09"].commits, 2);
        let excluded = filter_by_repo(&records, &HashSet::from(["/repos/b".into()]));
        let remaining = aggregate(&excluded, &AggregateOptions::default());
        assert_eq!(remaining[0].commits, 1);
        assert_eq!(remaining[0].per_repo.len(), 1);
    }

    #[test]
    fn full_history_diff_replaces_unknown_shallow_copy_without_counting_twice() {
        let full = record("shared", "A", "a@test", "z-full");
        let mut shallow = full.clone();
        shallow.repo_id = "/repos/a-shallow".into();
        shallow.repo_name = "a-shallow".into();
        shallow.lines_known = false;
        shallow.added = 1000;
        shallow.removed = 900;
        let options = AggregateOptions::default();
        let records = vec![shallow.clone(), full.clone()];
        let a = aggregate(&records, &options).remove(0);
        assert_eq!(
            (a.commits, a.added, a.removed, a.unknown_line_commits),
            (1, 10, 3, 0)
        );
        for repo in a.per_repo.values() {
            assert_eq!(
                (repo.commits, repo.added, repo.unknown_line_commits),
                (1, 10, 0)
            );
        }
        assert_eq!(aggregate(&[full, shallow.clone()], &options)[0], a);
        let unknown = aggregate(&[shallow], &options).remove(0);
        assert_eq!(
            (
                unknown.commits,
                unknown.added,
                unknown.removed,
                unknown.unknown_line_commits
            ),
            (1, 0, 0, 1)
        );
        assert_eq!(
            unknown.daily.values().next().unwrap().unknown_line_commits,
            1
        );
        assert_eq!(unknown.monthly["2026-09"].unknown_line_commits, 1);
    }

    #[test]
    fn removed_added_ratio_is_available_only_for_complete_nonzero_additions() {
        let known = record("known", "A", "a@test", "a");
        let mut unknown = record("unknown", "A", "a@test", "a");
        unknown.lines_known = false;
        let options = AggregateOptions::default();
        let known_stats = aggregate(std::slice::from_ref(&known), &options).remove(0);
        assert_eq!(known_stats.removed_added_ratio(), Some(0.3));
        let mixed = aggregate(&[known, unknown], &options).remove(0);
        assert_eq!(
            (mixed.added, mixed.removed, mixed.unknown_line_commits),
            (10, 3, 1)
        );
        assert_eq!(mixed.removed_added_ratio(), None);
        let mut deletion = record("delete", "A", "a@test", "a");
        deletion.added = 0;
        let deletion_stats = aggregate(&[deletion], &options).remove(0);
        assert_eq!(deletion_stats.removed_added_ratio(), None);
    }

    #[test]
    fn human_coauthors_receive_unique_participation_without_authored_or_line_credit() {
        let mut commit = record("shared", "Primary", "primary@test", "a");
        commit.ai_assisted = true;
        commit.coauthors = vec![
            coauthor("Pair", "pair@test"),
            coauthor("Pair Alias", " PAIR@TEST "),
            coauthor("Primary Alias", "primary@test"),
            coauthor("worker[bot]", "worker@test"),
            coauthor("Configured Bot", "bot@agents.test"),
        ];
        let mut clone = commit.clone();
        clone.repo_id = "/repos/b".into();
        clone.repo_name = "b".into();
        let options = AggregateOptions {
            bot_identities: vec!["@agents.test".into()],
            ..AggregateOptions::default()
        };
        let authors = aggregate(&[commit, clone], &options);
        assert_eq!(authors.len(), 2);
        assert_eq!(authors.iter().map(|a| a.commits).sum::<i64>(), 1);
        let primary = author(&authors, "email:primary@test");
        assert_eq!(
            (
                primary.commits,
                primary.coauthored_commits,
                primary.added,
                primary.ai_commits
            ),
            (1, 0, 10, 1)
        );
        assert!(primary.aliases.contains("Primary Alias"));
        let pair = author(&authors, "email:pair@test");
        assert_eq!(
            (
                pair.commits,
                pair.coauthored_commits,
                pair.added,
                pair.removed,
                pair.ai_commits,
                pair.unknown_line_commits
            ),
            (0, 1, 0, 0, 0, 0)
        );
        assert_eq!(pair.per_repo.len(), 2);
        assert_eq!(pair.per_repo["a"].coauthored_commits, 1);
        assert_eq!(pair.per_repo["b"].coauthored_commits, 1);
        assert_eq!(pair.daily.values().next().unwrap().coauthored_commits, 1);
        assert_eq!(pair.monthly["2026-09"].coauthored_commits, 1);
        assert_eq!(pair.active_days, 1);
    }

    #[test]
    fn merging_primary_with_coauthors_does_not_double_participation() {
        let mut commit = record("one", "Primary", "author@test", "a");
        commit.coauthors = vec![
            coauthor("Partner", "partner@test"),
            coauthor("Partner Alias", "alias@test"),
        ];
        let mut options = AggregateOptions::default();
        let id = options
            .identities
            .merge("email:author@test", "email:partner@test", "One person")
            .unwrap();
        let id = options
            .identities
            .merge(&id, "email:alias@test", "One person")
            .unwrap();
        let authors = aggregate(&[commit], &options);
        assert_eq!(authors.len(), 1);
        assert_eq!(authors[0].id, id);
        assert_eq!(
            (
                authors[0].commits,
                authors[0].coauthored_commits,
                authors[0].added
            ),
            (1, 0, 10)
        );
        assert_eq!(authors[0].member_ids.len(), 3);
    }

    #[test]
    fn merged_coauthor_aliases_count_once_and_unknowns_belong_only_to_primary() {
        let mut commit = record("one", "Primary", "author@test", "a");
        commit.lines_known = false;
        commit.coauthors = vec![
            coauthor("Partner", "partner@test"),
            coauthor("Partner Alias", "alias@test"),
        ];
        let mut options = AggregateOptions::default();
        let id = options
            .identities
            .merge("email:partner@test", "email:alias@test", "Partner")
            .unwrap();
        let authors = aggregate(&[commit], &options);
        assert_eq!(
            author(&authors, "email:author@test").unknown_line_commits,
            1
        );
        let pair = author(&authors, &id);
        assert_eq!((pair.coauthored_commits, pair.unknown_line_commits), (1, 0));
        assert_eq!(pair.member_ids.len(), 2);
    }

    #[test]
    fn reporting_timezone_controls_daily_monthly_and_visible_dates() {
        let mut january = record("january", "A", "a@test", "a");
        january.date = DateTime::parse_from_rfc3339("2026-02-01T00:30:00+01:00").unwrap();
        let mut february = record("february", "A", "a@test", "a");
        february.date = DateTime::parse_from_rfc3339("2026-02-01T00:30:00Z").unwrap();
        let records = vec![january, february];
        let utc = aggregate(&records, &AggregateOptions::default()).remove(0);
        assert_eq!(utc.active_days, 2);
        assert_eq!(utc.monthly["2026-01"].commits, 1);
        assert_eq!(utc.monthly["2026-02"].commits, 1);
        assert_eq!(utc.first_commit.to_rfc3339(), "2026-01-31T23:30:00+00:00");
        let denver = aggregate(
            &records,
            &AggregateOptions {
                timezone: chrono_tz::America::Denver,
                ..AggregateOptions::default()
            },
        )
        .remove(0);
        assert_eq!(denver.active_days, 1);
        assert_eq!(denver.monthly.len(), 1);
        assert_eq!(denver.monthly["2026-01"].commits, 2);
        assert_eq!(
            denver.daily[&NaiveDate::from_ymd_opt(2026, 1, 31).unwrap()].commits,
            2
        );
        assert_eq!(denver.last_commit.to_rfc3339(), "2026-01-31T17:30:00-07:00");
    }

    #[test]
    fn dst_fold_has_one_reporting_day_with_correct_first_and_last_offsets() {
        let mut first = record("first", "A", "a@test", "a");
        first.date = DateTime::parse_from_rfc3339("2026-11-01T07:30:00Z").unwrap();
        let mut last = record("last", "A", "a@test", "a");
        last.date = DateTime::parse_from_rfc3339("2026-11-01T08:30:00Z").unwrap();
        let stats = aggregate(
            &[first, last],
            &AggregateOptions {
                timezone: chrono_tz::America::Denver,
                ..AggregateOptions::default()
            },
        )
        .remove(0);
        assert_eq!(stats.active_days, 1);
        assert_eq!(stats.first_commit.to_rfc3339(), "2026-11-01T01:30:00-06:00");
        assert_eq!(stats.last_commit.to_rfc3339(), "2026-11-01T01:30:00-07:00");
    }

    #[test]
    fn future_records_are_excluded_including_all_history_and_cutoff_is_consistent() {
        let now = DateTime::parse_from_rfc3339("2026-09-20T12:00:00Z").unwrap();
        let mut records = vec![
            record("now", "A", "a@test", "a"),
            record("boundary", "A", "a@test", "a"),
            record("future", "A", "a@test", "a"),
            record("past-east", "A", "a@test", "a"),
        ];
        records[1].date = now - Duration::days(1);
        records[2].date = now + Duration::nanoseconds(1);
        records[3].date = DateTime::parse_from_rfc3339("2026-09-21T00:30:00+14:00").unwrap();
        let ids = |records: Vec<CommitRecord>| {
            records.into_iter().map(|r| r.commit_id).collect::<Vec<_>>()
        };
        assert_eq!(
            ids(filter_by_time_at(&records, Duration::days(1), now)),
            ["now", "past-east"]
        );
        assert_eq!(
            ids(filter_by_time_at(&records, Duration::zero(), now)),
            ["now", "boundary", "past-east"]
        );
        let stats = aggregate(
            &filter_by_time_at(&records, Duration::days(1), now),
            &AggregateOptions::default(),
        );
        assert_eq!(stats[0].active_days, 1);
        assert_eq!(
            stats[0].daily[&NaiveDate::from_ymd_opt(2026, 9, 20).unwrap()].commits,
            2
        );
    }

    #[test]
    fn landed_scope_excludes_unmerged_work_and_all_branches_includes_it_once() {
        let landed = record("landed", "A", "a@test", "a");
        let mut unmerged = record("unmerged", "A", "a@test", "a");
        unmerged.landed = false;
        let records = vec![landed.clone(), landed, unmerged];
        let options = AggregateOptions::default();
        assert_eq!(
            aggregate(&filter_by_scope(&records, HistoryScope::Landed), &options)[0].commits,
            1
        );
        assert_eq!(
            aggregate(
                &filter_by_scope(&records, HistoryScope::AllBranches),
                &options
            )[0]
            .commits,
            2
        );
    }

    #[test]
    fn exact_ai_sort_beats_line_tiebreakers_and_does_not_overflow_i64_products() {
        let mut authors = vec![
            AuthorStats {
                id: "high".into(),
                name: "High".into(),
                commits: 101,
                ai_commits: 1,
                total_change: 1,
                ..AuthorStats::default()
            },
            AuthorStats {
                id: "low".into(),
                name: "Low".into(),
                commits: 1000,
                ai_commits: 1,
                total_change: 1000,
                ..AuthorStats::default()
            },
            AuthorStats {
                id: "coauthor".into(),
                name: "Coauthor".into(),
                coauthored_commits: 100,
                ..AuthorStats::default()
            },
        ];
        sort(&mut authors, SortField::AI);
        assert_eq!(
            authors.iter().map(|a| a.id.as_str()).collect::<Vec<_>>(),
            ["high", "low", "coauthor"]
        );
        assert_eq!(authors[0].ai_percent(), 0);
        let high = AuthorStats {
            commits: i64::MAX,
            ai_commits: i64::MAX - 1,
            ..AuthorStats::default()
        };
        let low = AuthorStats {
            commits: i64::MAX,
            ai_commits: i64::MAX - 2,
            ..AuthorStats::default()
        };
        assert_eq!(descending_ai_ratio(&high, &low), Ordering::Less);
        assert_eq!(high.ai_percent(), 99);
    }

    #[test]
    fn deterministic_sort_uses_id_when_names_and_metrics_collide() {
        let mut authors = vec![
            AuthorStats {
                id: "b".into(),
                name: "Same".into(),
                commits: 3,
                ..AuthorStats::default()
            },
            AuthorStats {
                id: "a".into(),
                name: "Same".into(),
                commits: 3,
                ..AuthorStats::default()
            },
        ];
        sort(&mut authors, SortField::Commits);
        assert_eq!(authors[0].id, "a");
        assert_eq!(AuthorStats::default().removed_added_ratio(), None);
        assert_eq!(
            AuthorStats {
                added: 0,
                removed: 100,
                ..AuthorStats::default()
            }
            .removed_added_ratio(),
            None
        );
        assert_eq!(
            AuthorStats {
                added: 10,
                removed: 4,
                ..AuthorStats::default()
            }
            .removed_added_ratio(),
            Some(0.4)
        );
    }

    #[test]
    fn identity_deduplication_and_metadata_are_independent_of_input_order() {
        let mut full = record("shared", "A", "a@test", "a");
        full.coauthors = vec![coauthor("B", "b@test")];
        let mut shallow = full.clone();
        shallow.repo_id = "/repos/shallow".into();
        shallow.repo_name = "shallow".into();
        shallow.lines_known = false;
        shallow.added = 1000;
        let mut records = vec![
            full,
            shallow,
            record("second", "Alias", "a@test", "a"),
            record("third", "A", "other@test", "a"),
            record("fourth", "B", "b@test", "b"),
            record("fifth", "Bot[bot]", "bot@test", "a"),
        ];
        let options = AggregateOptions::default();
        let expected = aggregate(&records, &options);
        fn visit(
            records: &mut [CommitRecord],
            index: usize,
            options: &AggregateOptions,
            expected: &[AuthorStats],
        ) -> usize {
            if index == records.len() {
                assert_eq!(aggregate(records, options), expected);
                return 1;
            }
            let mut count = 0;
            for next in index..records.len() {
                records.swap(index, next);
                count += visit(records, index + 1, options, expected);
                records.swap(index, next);
            }
            count
        }
        assert_eq!(visit(&mut records, 0, &options, &expected), 720);
    }

    #[test]
    fn empty_input_is_empty_and_missing_object_ids_do_not_collapse() {
        assert!(aggregate(&[], &AggregateOptions::default()).is_empty());
        let records = vec![
            record("", "A", "a@test", "a"),
            record("", "A", "a@test", "a"),
        ];
        assert_eq!(
            aggregate(&records, &AggregateOptions::default())[0].commits,
            2
        );
    }

    #[test]
    fn primary_bots_are_tagged_and_sort_labels_describe_activity() {
        let authors = aggregate(
            &[record("one", "worker[bot]", "worker@test", "a")],
            &AggregateOptions::default(),
        );
        assert!(authors[0].bot);
        assert_eq!(SortField::Total.label(), "LINES CHANGED");
        assert_eq!(SortField::AI.label(), "DETECTED AI");
        for field in [
            SortField::Total,
            SortField::Commits,
            SortField::Added,
            SortField::Removed,
            SortField::Net,
            SortField::AI,
        ] {
            assert_eq!(SortField::parse(field.label()).unwrap(), field);
        }
        assert_eq!(SortField::AI.next(), SortField::Total);
    }
}
