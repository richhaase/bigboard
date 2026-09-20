//! Contributor aggregation and identity policy, preserved from the Go revision.

use std::collections::{BTreeMap, HashMap, HashSet};

use anyhow::{Result, bail};
use chrono::{DateTime, Duration, FixedOffset, Local, SecondsFormat, TimeZone, Utc};
use serde::{Serialize, Serializer};

use crate::model::CommitRecord;

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
        match lowercase(value).as_str() {
            "total" | "impact" => Ok(Self::Total),
            "commits" => Ok(Self::Commits),
            "added" => Ok(Self::Added),
            "removed" => Ok(Self::Removed),
            "net" => Ok(Self::Net),
            "ai" => Ok(Self::AI),
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
            Self::Total => "IMPACT",
            Self::Commits => "COMMITS",
            Self::Added => "ADDED",
            Self::Removed => "REMOVED",
            Self::Net => "NET",
            Self::AI => "AI",
        }
    }
}

#[derive(Clone, Debug, Default)]
pub struct AggregateOptions {
    pub fuzzy_matching: bool,
    pub bot_identities: Vec<String>,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct AuthorStats {
    pub name: String,
    pub commits: i64,
    pub added: i64,
    pub removed: i64,
    pub net: i64,
    pub total_change: i64,
    pub ai_commits: i64,
    pub bot: bool,
    #[serde(serialize_with = "serialize_date")]
    pub first_commit: DateTime<FixedOffset>,
    #[serde(serialize_with = "serialize_date")]
    pub last_commit: DateTime<FixedOffset>,
    pub active_days: i64,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub per_repo: BTreeMap<String, RepoContribution>,
    #[serde(skip)]
    pub aliases: HashSet<String>,
}

impl Default for AuthorStats {
    fn default() -> Self {
        Self {
            name: String::new(),
            commits: 0,
            added: 0,
            removed: 0,
            net: 0,
            total_change: 0,
            ai_commits: 0,
            bot: false,
            first_commit: zero_time(),
            last_commit: zero_time(),
            active_days: 0,
            per_repo: BTreeMap::new(),
            aliases: HashSet::new(),
        }
    }
}

impl AuthorStats {
    /// The existing metric is removed / added, with zero for no additions.
    pub fn churn_ratio(&self) -> f64 {
        if self.added == 0 {
            0.0
        } else {
            self.removed as f64 / self.added as f64
        }
    }

    /// Integer truncation is also retained for sorting compatibility.
    pub fn ai_percent(&self) -> i64 {
        if self.commits == 0 {
            0
        } else {
            self.ai_commits * 100 / self.commits
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize)]
pub struct RepoContribution {
    pub commits: i64,
    pub added: i64,
    pub removed: i64,
    pub net: i64,
    pub total_change: i64,
    pub ai_commits: i64,
}

fn zero_time() -> DateTime<FixedOffset> {
    Utc.with_ymd_and_hms(1, 1, 1, 0, 0, 0)
        .single()
        .expect("the Go zero timestamp is valid")
        .fixed_offset()
}

fn serialize_date<S>(date: &DateTime<FixedOffset>, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(&date.to_rfc3339_opts(SecondsFormat::AutoSi, true))
}

pub fn filter_by_time(records: &[CommitRecord], duration: Duration) -> Vec<CommitRecord> {
    filter_by_time_at(records, duration, Local::now().fixed_offset())
}

/// Match the existing exclusive lower cutoff, including future-dated records.
pub fn filter_by_time_at(
    records: &[CommitRecord],
    duration: Duration,
    now: DateTime<FixedOffset>,
) -> Vec<CommitRecord> {
    if duration.is_zero() {
        return records.to_vec();
    }
    let cutoff = now - duration;
    records
        .iter()
        .filter(|record| record.date > cutoff)
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

type IdentityKey = (String, String);

fn key_for_record(record: &CommitRecord) -> IdentityKey {
    (record.author.clone(), lowercase(record.email.trim()))
}

struct DisjointSet(Vec<usize>);

impl DisjointSet {
    fn new(size: usize) -> Self {
        Self((0..size).collect())
    }

    fn find(&mut self, mut index: usize) -> usize {
        let mut root = index;
        while self.0[root] != root {
            root = self.0[root];
        }
        while self.0[index] != index {
            let parent = self.0[index];
            self.0[index] = root;
            index = parent;
        }
        root
    }

    fn union(&mut self, a: usize, b: usize) {
        let a = self.find(a);
        let b = self.find(b);
        if a < b {
            self.0[b] = a;
        } else if b < a {
            self.0[a] = b;
        }
    }
}

fn resolve_canonical_names(records: &[CommitRecord], fuzzy: bool) -> HashMap<IdentityKey, String> {
    let mut pair_index = HashMap::new();
    let mut pairs = Vec::new();
    for record in records {
        let key = key_for_record(record);
        if !pair_index.contains_key(&key) {
            pair_index.insert(key.clone(), pairs.len());
            pairs.push(key);
        }
    }

    let mut set = DisjointSet::new(pairs.len());
    let mut by_email = HashMap::new();
    let mut by_name = BTreeMap::new();
    for (index, (author, email)) in pairs.iter().enumerate() {
        if !email.is_empty() {
            if let Some(&previous) = by_email.get(email) {
                set.union(index, previous);
            } else {
                by_email.insert(email.clone(), index);
            }
        }
        let name = normalized_name(author);
        if name.is_empty() {
            continue;
        }
        if let Some(&previous) = by_name.get(&name) {
            set.union(index, previous);
        } else {
            by_name.insert(name, index);
        }
    }

    if fuzzy {
        let names: Vec<_> = by_name.keys().collect();
        for (index, name) in names.iter().enumerate() {
            for candidate in &names[index + 1..] {
                if similar_normalized_names(name, candidate) {
                    set.union(by_name[*name], by_name[*candidate]);
                }
            }
        }
    }

    let mut name_counts: HashMap<usize, BTreeMap<String, i64>> = HashMap::new();
    for record in records {
        let root = set.find(pair_index[&key_for_record(record)]);
        *name_counts
            .entry(root)
            .or_default()
            .entry(record.author.clone())
            .or_default() += 1;
    }
    let mut canonical_by_root = HashMap::new();
    for (root, counts) in name_counts {
        let mut best = String::new();
        let mut best_count = -1;
        for (name, count) in counts {
            if count > best_count || (count == best_count && prefer_canonical(&name, &best)) {
                best = name;
                best_count = count;
            }
        }
        canonical_by_root.insert(root, best);
    }
    pairs
        .into_iter()
        .enumerate()
        .map(|(index, pair)| (pair, canonical_by_root[&set.find(index)].clone()))
        .collect()
}

/// Resolve identities and return totals in deterministic canonical-name order.
pub fn aggregate(records: &[CommitRecord], options: &AggregateOptions) -> Vec<AuthorStats> {
    let canonical = resolve_canonical_names(records, options.fuzzy_matching);
    let mut by_name: BTreeMap<String, AuthorStats> = BTreeMap::new();
    let mut active_days: HashMap<String, HashSet<String>> = HashMap::new();
    for record in records {
        let name = &canonical[&key_for_record(record)];
        let author = by_name.entry(name.clone()).or_insert_with(|| AuthorStats {
            name: name.clone(),
            ..AuthorStats::default()
        });
        if !author.bot && is_bot_identity(&record.author, &record.email, &options.bot_identities) {
            author.bot = true;
        }
        author.commits += 1;
        author.added += record.added;
        author.removed += record.removed;
        author.net += record.added - record.removed;
        author.total_change += record.added + record.removed;
        author.aliases.insert(record.author.clone());
        if author.first_commit == zero_time() || record.date < author.first_commit {
            author.first_commit = record.date;
        }
        if record.date > author.last_commit {
            author.last_commit = record.date;
        }
        active_days
            .entry(name.clone())
            .or_default()
            .insert(record.date.format("%Y-%m-%d").to_string());
        if record.ai_assisted {
            author.ai_commits += 1;
        }
        let repo = author.per_repo.entry(record.repo_name.clone()).or_default();
        repo.commits += 1;
        repo.added += record.added;
        repo.removed += record.removed;
        repo.net += record.added - record.removed;
        repo.total_change += record.added + record.removed;
        if record.ai_assisted {
            repo.ai_commits += 1;
        }
    }
    by_name
        .into_values()
        .map(|mut author| {
            author.active_days = active_days[&author.name].len() as i64;
            author
        })
        .collect()
}

fn metric_value(author: &AuthorStats, field: SortField) -> i64 {
    match field {
        SortField::Total => author.total_change,
        SortField::Commits => author.commits,
        SortField::Added => author.added,
        SortField::Removed => author.removed,
        SortField::Net => author.net,
        SortField::AI => author.ai_percent(),
    }
}

pub fn sort(authors: &mut [AuthorStats], field: SortField) {
    authors.sort_by(|a, b| {
        metric_value(b, field)
            .cmp(&metric_value(a, field))
            .then_with(|| b.total_change.cmp(&a.total_change))
            .then_with(|| b.commits.cmp(&a.commits))
            .then_with(|| a.name.cmp(&b.name))
    });
}

pub fn names_match(a: &str, b: &str, fuzzy: bool) -> bool {
    let a = normalized_name(a);
    let b = normalized_name(b);
    a == b || (fuzzy && similar_normalized_names(&a, &b))
}

fn similar_normalized_names(a: &str, b: &str) -> bool {
    a == b || (a.chars().count() > 5 && b.contains(a)) || (b.chars().count() > 5 && a.contains(b))
}

fn prefer_canonical(a: &str, b: &str) -> bool {
    let a_length = a.chars().count();
    let b_length = b.chars().count();
    if a_length != b_length {
        a_length > b_length
    } else {
        a < b
    }
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

fn normalized_name(value: &str) -> String {
    lowercase(value)
        .chars()
        .filter(|character| !character.is_whitespace() && !matches!(character, '-' | '_' | '.'))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record(
        author: &str,
        email: &str,
        date: &str,
        added: i64,
        removed: i64,
        repo: &str,
    ) -> CommitRecord {
        CommitRecord {
            author: author.into(),
            email: email.into(),
            date: DateTime::parse_from_rfc3339(date).unwrap(),
            added,
            removed,
            repo_id: format!("/repos/{repo}"),
            repo_name: repo.into(),
            ai_assisted: false,
        }
    }

    fn simple(author: &str, email: &str) -> CommitRecord {
        record(author, email, "2026-09-20T12:00:00Z", 1, 0, "r")
    }

    #[test]
    fn aggregates_totals_aliases_dates_and_per_repo() {
        let mut records = vec![
            record(
                "Alice Smith",
                "alice@example.com",
                "2026-09-02T12:00:00-06:00",
                100,
                20,
                "a",
            ),
            record(
                "asmith",
                " ALICE@EXAMPLE.COM ",
                "2026-09-01T12:00:00-06:00",
                50,
                10,
                "a",
            ),
            record(
                "Alice Smith",
                "alice@personal.com",
                "2026-09-02T13:00:00-06:00",
                30,
                5,
                "b",
            ),
            record(
                "Bob",
                "bob@example.com",
                "2026-09-02T12:00:00Z",
                200,
                80,
                "a",
            ),
        ];
        records[0].ai_assisted = true;
        records[2].ai_assisted = true;
        let authors = aggregate(&records, &AggregateOptions::default());
        assert_eq!(authors.len(), 2);
        let alice = &authors[0];
        assert_eq!(alice.name, "Alice Smith");
        assert_eq!(
            (
                alice.commits,
                alice.added,
                alice.removed,
                alice.net,
                alice.total_change
            ),
            (3, 180, 35, 145, 215)
        );
        assert_eq!(
            (alice.active_days, alice.ai_commits, alice.ai_percent()),
            (2, 2, 66)
        );
        assert_eq!(alice.first_commit, records[1].date);
        assert_eq!(alice.last_commit, records[2].date);
        assert_eq!(
            alice.aliases,
            HashSet::from(["Alice Smith".into(), "asmith".into()])
        );
        assert_eq!(
            alice.per_repo["a"],
            RepoContribution {
                commits: 2,
                added: 150,
                removed: 30,
                net: 120,
                total_change: 180,
                ai_commits: 1
            }
        );
        assert_eq!(alice.per_repo["b"].ai_commits, 1);
        assert!(!alice.bot);
    }

    #[test]
    fn default_identity_keeps_similar_names_but_merges_same_names() {
        let records = vec![
            simple("Daniel", "one@x"),
            simple("Daniela", "two@x"),
            simple("Martin", "three@x"),
            simple("Martinez", "four@x"),
        ];
        assert_eq!(aggregate(&records, &AggregateOptions::default()).len(), 4);
        let homonyms = vec![simple("Alex Lee", "one@x"), simple("Alex Lee", "two@x")];
        let authors = aggregate(&homonyms, &AggregateOptions::default());
        assert_eq!(authors.len(), 1);
        assert_eq!(authors[0].commits, 2);
    }

    #[test]
    fn name_normalization_and_fuzzy_character_count_match_go() {
        for (a, b) in [
            ("Alice Smith", "alice-smith"),
            ("Alice_  Smith", "alice.smith"),
            ("ΟΣ", "οσ"),
            ("İ", "i"),
        ] {
            assert!(names_match(a, b, false), "{a:?} vs {b:?}");
        }
        assert!(!names_match("Alice S", "Alice Smith", false));
        assert!(names_match("Alice S", "Alice Smith", true));
        assert!(!names_match("東京", "東京都", true));
        assert!(!names_match("Al", "Alice", true));
    }

    #[test]
    fn aggregation_is_independent_of_all_720_identity_permutations() {
        let mut records = vec![
            simple("A", "one@x"),
            simple("Alice", "one@x"),
            simple("A", "two@x"),
            simple("Aaron", "two@x"),
            simple("Anne", "three@x"),
            simple("Anna", "three@x"),
        ];
        let options = AggregateOptions::default();
        let expected = aggregate(&records, &options);
        fn permutations(
            records: &mut [CommitRecord],
            index: usize,
            expected: &[AuthorStats],
            options: &AggregateOptions,
        ) -> usize {
            if index == records.len() {
                assert_eq!(aggregate(records, options), expected);
                return 1;
            }
            let mut count = 0;
            for next in index..records.len() {
                records.swap(index, next);
                count += permutations(records, index + 1, expected, options);
                records.swap(index, next);
            }
            count
        }
        assert_eq!(permutations(&mut records, 0, &expected, &options), 720);
    }

    #[test]
    fn fuzzy_union_is_transitive_and_chooses_longest_then_lexical_name() {
        let mut records = vec![simple("Andrew", "one@x")];
        for _ in 0..5 {
            records.push(simple("Andrews", "two@x"));
            records.push(simple("xAndrew", "three@x"));
        }
        let options = AggregateOptions {
            fuzzy_matching: true,
            ..AggregateOptions::default()
        };
        let result = aggregate(&records, &options);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "Andrews");
        assert_eq!(result[0].commits, 11);
        records.reverse();
        assert_eq!(aggregate(&records, &options), result);
    }

    #[test]
    fn bot_roster_and_config_entries_tag_merged_identity() {
        for name in [
            "dependabot",
            "dependabot-preview",
            "renovate",
            "github-actions",
            "snyk-bot",
            "greenkeeper",
            "imgbot",
            "mergify",
            "allcontributors",
            "pre-commit-ci",
            "codecov",
            "Renovate Bot",
            "worker[bot]",
        ] {
            assert!(is_bot_identity(name, "someone@example.com", &[]), "{name}");
        }
        assert!(is_bot_identity(
            "Worker",
            "123+agent[bot]@users.noreply.github.com",
            &[]
        ));
        assert!(!is_bot_identity("Robotics Lab", "robots@example.com", &[]));
        assert!(is_bot_identity(
            "Worker",
            " <RUNNER@AGENTS.EXAMPLE.COM> ",
            &[" @agents.example.com ".into()]
        ));
        assert!(is_bot_identity(
            "Worker",
            "runner@example.com",
            &["RUNNER@EXAMPLE.COM".into()]
        ));
        assert!(is_bot_identity(
            "Fleet Runner",
            "other@example.com",
            &[" fleet runner ".into()]
        ));
        assert!(!is_bot_identity(
            "Worker",
            "someone@example.com",
            &["".into(), "other".into()]
        ));
        let authors = aggregate(
            &[simple("Person", "one@x"), simple("Worker[bot]", "one@x")],
            &AggregateOptions::default(),
        );
        assert_eq!(authors.len(), 1);
        assert!(authors[0].bot);
    }

    #[test]
    fn time_filter_preserves_exclusive_boundary_and_future_inclusion() {
        let now = DateTime::parse_from_rfc3339("2026-09-20T12:00:00Z").unwrap();
        let mut records = vec![
            simple("Recent", "a@x"),
            simple("Boundary", "b@x"),
            simple("Old", "c@x"),
            simple("Future", "d@x"),
        ];
        records[1].date = now - Duration::days(1);
        records[2].date = now - Duration::days(2);
        records[3].date = now + Duration::days(1000);
        let result = filter_by_time_at(&records, Duration::days(1), now);
        assert_eq!(
            result
                .iter()
                .map(|record| record.author.as_str())
                .collect::<Vec<_>>(),
            ["Recent", "Future"]
        );
        assert_eq!(filter_by_time_at(&records, Duration::zero(), now).len(), 4);
    }

    #[test]
    fn repo_filters_support_stable_ids_and_display_names() {
        let mut records = vec![simple("A", "a@x"), simple("B", "b@x")];
        records[0].repo_id = "/org-a/api".into();
        records[1].repo_id = "/org-b/api".into();
        records[0].repo_name = "api".into();
        records[1].repo_name = "api".into();
        let remaining = filter_by_repo(&records, &HashSet::from(["/org-a/api".into()]));
        assert_eq!(remaining.len(), 1);
        assert_eq!(remaining[0].repo_id, "/org-b/api");
        assert!(filter_by_repo(&records, &HashSet::from(["api".into()])).is_empty());
        assert_eq!(filter_by_repo(&records, &HashSet::new()).len(), 2);
    }

    #[test]
    fn preserves_truncated_ai_sort_and_other_tiebreakers() {
        let mut authors = vec![
            AuthorStats {
                name: "HigherShare".into(),
                commits: 101,
                ai_commits: 1,
                total_change: 1,
                ..AuthorStats::default()
            },
            AuthorStats {
                name: "LowerShare".into(),
                commits: 1000,
                ai_commits: 1,
                total_change: 1000,
                ..AuthorStats::default()
            },
        ];
        sort(&mut authors, SortField::AI);
        assert_eq!(authors[0].name, "LowerShare");
        authors = vec![
            AuthorStats {
                name: "Charlie".into(),
                commits: 5,
                total_change: 10,
                ..AuthorStats::default()
            },
            AuthorStats {
                name: "Bob".into(),
                commits: 5,
                total_change: 30,
                ..AuthorStats::default()
            },
            AuthorStats {
                name: "Alice".into(),
                commits: 5,
                total_change: 30,
                ..AuthorStats::default()
            },
        ];
        sort(&mut authors, SortField::Commits);
        assert_eq!(
            authors
                .iter()
                .map(|author| author.name.as_str())
                .collect::<Vec<_>>(),
            ["Alice", "Bob", "Charlie"]
        );
        let mut authors = vec![
            AuthorStats {
                name: "HighCount".into(),
                commits: 100,
                ai_commits: 10,
                ..AuthorStats::default()
            },
            AuthorStats {
                name: "HighPercent".into(),
                commits: 4,
                ai_commits: 4,
                ..AuthorStats::default()
            },
        ];
        sort(&mut authors, SortField::AI);
        assert_eq!(authors[0].name, "HighPercent");
    }

    #[test]
    fn preserves_author_local_active_days_and_deletion_only_churn() {
        let authors = aggregate(
            &[
                record("A", "a@x", "2026-01-01T23:30:00Z", 0, 50, "r"),
                record("A", "a@x", "2026-01-02T00:30:00+01:00", 0, 50, "r"),
            ],
            &AggregateOptions::default(),
        );
        assert_eq!(authors[0].active_days, 2);
        assert_eq!(authors[0].churn_ratio(), 0.0);
        assert_eq!(AuthorStats::default().ai_percent(), 0);
        assert_eq!(
            AuthorStats {
                added: 10,
                removed: 4,
                ..AuthorStats::default()
            }
            .churn_ratio(),
            0.4
        );
    }

    #[test]
    fn canonical_name_is_recomputed_from_each_filtered_input() {
        let mut records = vec![
            simple("Alice Smith", "a@x"),
            simple("Alice Smith", "a@x"),
            simple("asmith", "a@x"),
        ];
        records[0].date -= Duration::days(60);
        records[1].date -= Duration::days(60);
        let options = AggregateOptions::default();
        assert_eq!(aggregate(&records, &options)[0].name, "Alice Smith");
        let recent = filter_by_time_at(&records, Duration::days(7), records[2].date);
        assert_eq!(aggregate(&recent, &options)[0].name, "asmith");
    }

    #[test]
    fn json_preserves_go_field_names_timestamp_offsets_and_omissions() {
        let records = vec![record("A", "a@x", "2026-09-20T12:00:00Z", 2, 1, "r")];
        let authors = aggregate(&records, &AggregateOptions::default());
        let value = serde_json::to_value(&authors[0]).unwrap();
        assert_eq!(value["first_commit"], "2026-09-20T12:00:00Z");
        assert_eq!(value["last_commit"], "2026-09-20T12:00:00Z");
        assert_eq!(value["per_repo"]["r"]["total_change"], 3);
        assert_eq!(value["bot"], false);
        assert!(value.get("aliases").is_none());
        let empty = serde_json::to_value(AuthorStats::default()).unwrap();
        assert!(empty.get("per_repo").is_none());
        assert_eq!(empty["first_commit"], "0001-01-01T00:00:00Z");
        let mut author = authors[0].clone();
        author.first_commit = DateTime::parse_from_rfc3339("2026-09-20T12:00:00-06:00").unwrap();
        assert_eq!(
            serde_json::to_value(author).unwrap()["first_commit"],
            "2026-09-20T12:00:00-06:00"
        );
        assert!(aggregate(&[], &AggregateOptions::default()).is_empty());
    }

    #[test]
    fn sort_field_parser_labels_and_cycle_match_the_tui() {
        let fields = [
            SortField::Total,
            SortField::Commits,
            SortField::Added,
            SortField::Removed,
            SortField::Net,
            SortField::AI,
        ];
        for (index, field) in fields.iter().enumerate() {
            assert_eq!(SortField::parse(field.label()).unwrap(), *field);
            assert_eq!(field.next(), fields[(index + 1) % fields.len()]);
        }
        assert_eq!(SortField::parse("total").unwrap(), SortField::Total);
        assert!(SortField::parse("commit").is_err());
        assert!(SortField::parse(" total ").is_err());
    }
}
