//! Known outcomes through real Git collection, filtering, aggregation and persistence.
use bigboard::{
    git::{self, CollectOptions},
    identity::IdentityStore,
    model::{CommitRecord, HistoryScope},
    stats::{self, AggregateOptions},
};
use std::{collections::HashSet, path::Path, process::Command, sync::atomic::AtomicBool};

fn git_command(repo: &Path, args: &[&str]) -> String {
    let output = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_AUTHOR_DATE", "2026-01-15T10:00:00Z")
        .env("GIT_COMMITTER_DATE", "2026-01-15T10:00:00Z")
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8_lossy(&output.stdout).trim().into()
}
fn init(repo: &Path) {
    std::fs::create_dir_all(repo).unwrap();
    git_command(repo, &["init", "-b", "main"]);
    git_command(repo, &["config", "user.name", "Fixture"]);
    git_command(repo, &["config", "user.email", "fixture@example.test"]);
}
fn commit(repo: &Path, name: &str, email: &str, file: &str, text: &str, message: &str) {
    std::fs::write(repo.join(file), text).unwrap();
    git_command(repo, &["add", "-A"]);
    git_command(
        repo,
        &[
            "-c",
            &format!("user.name={name}"),
            "-c",
            &format!("user.email={email}"),
            "commit",
            "-m",
            message,
        ],
    );
}
fn scan(paths: &[std::path::PathBuf]) -> Vec<CommitRecord> {
    git::new_repositories(paths)
        .iter()
        .flat_map(|repo| {
            git::scan_repository(repo, &CollectOptions::default(), &AtomicBool::new(false))
                .unwrap()
                .records
        })
        .collect()
}

#[test]
fn scope_duplicates_and_global_merge_work_together() {
    let dir = tempfile::tempdir().unwrap();
    let source = dir.path().join("source");
    let copy = dir.path().join("copy");
    init(&source);
    commit(
        &source,
        "Mike",
        "mike@work.test",
        "one",
        "a\nb\nc\n",
        "first",
    );
    commit(
        &source,
        "Alex",
        "alex@work.test",
        "two",
        "d\n",
        "pairing\n\nCo-authored-by: mbiggly <mbiggly@home.test>",
    );
    commit(
        &source,
        "Alex",
        "other-alex@work.test",
        "three",
        "e\n",
        "another person",
    );
    commit(
        &source,
        "Human Employee",
        "human@openai.com",
        "four",
        "f\n",
        "human work",
    );
    commit(
        &source,
        "Alex",
        "alex@work.test",
        "five",
        "g\n",
        "assisted\n\nCo-authored-by: Claude <noreply@anthropic.com>",
    );
    git_command(&source, &["checkout", "-b", "feature"]);
    commit(
        &source,
        "Unlanded",
        "feature@work.test",
        "six",
        "h\ni\n",
        "ongoing",
    );
    git_command(&source, &["checkout", "main"]);
    git_command(
        dir.path(),
        &[
            "clone",
            "--no-local",
            source.to_str().unwrap(),
            copy.to_str().unwrap(),
        ],
    );
    let records = scan(&[source.clone(), copy.clone()]);
    let landed = stats::filter_by_scope(&records, HistoryScope::Landed);
    let options = AggregateOptions::default();
    let authors = stats::aggregate(&landed, &options);
    assert_eq!(authors.iter().map(|a| a.commits).sum::<i64>(), 5);
    assert_eq!(authors.iter().map(|a| a.added).sum::<i64>(), 7);
    assert_eq!(authors.iter().filter(|a| a.name == "Alex").count(), 2);
    assert_eq!(
        authors
            .iter()
            .find(|a| a.name == "Human Employee")
            .unwrap()
            .ai_commits,
        0
    );
    assert_eq!(authors.iter().map(|a| a.ai_commits).sum::<i64>(), 1);
    assert_eq!(authors.iter().map(|a| a.coauthored_commits).sum::<i64>(), 1);
    assert!(!authors.iter().any(|a| a.name == "Claude"));
    assert!(!authors.iter().any(|a| a.name == "Unlanded"));
    assert_eq!(
        authors
            .iter()
            .flat_map(|a| a.per_repo.values())
            .map(|r| r.commits)
            .sum::<i64>(),
        10
    );
    let all = stats::aggregate(
        &stats::filter_by_scope(&records, HistoryScope::AllBranches),
        &options,
    );
    assert_eq!(all.iter().map(|a| a.commits).sum::<i64>(), 6);
    let path = dir.path().join("preferences/identities.json");
    let (_, merged_id) = IdentityStore::merge_and_save(
        &path,
        "email:mike@work.test",
        "email:mbiggly@home.test",
        "Mike Biggly",
    )
    .unwrap();
    let merged_options = AggregateOptions {
        identities: IdentityStore::load(&path).unwrap(),
        ..options
    };
    let merged = stats::aggregate(&landed, &merged_options);
    let mike = merged.iter().find(|a| a.id == merged_id).unwrap();
    assert_eq!(
        (mike.commits, mike.coauthored_commits, mike.added),
        (1, 1, 3)
    );
    assert_eq!(mike.name, "Mike Biggly");
    assert_eq!(merged.iter().map(|a| a.commits).sum::<i64>(), 5);
    // A different repository selection still uses the saved global mapping.
    let source_id = git::new_repositories(&[source])[0].id.clone();
    let copy_only = stats::filter_by_repo(&landed, &HashSet::from([source_id]));
    let scoped = stats::aggregate(&copy_only, &merged_options);
    assert_eq!(
        scoped
            .iter()
            .find(|a| a.id == merged_id)
            .unwrap()
            .coauthored_commits,
        1
    );
}

#[test]
fn shallow_unknown_counts_are_recovered_from_an_identical_full_copy() {
    let dir = tempfile::tempdir().unwrap();
    let source = dir.path().join("source");
    let shallow = dir.path().join("shallow");
    init(&source);
    commit(
        &source,
        "Alice",
        "alice@work.test",
        "one",
        "a\nb\nc\n",
        "first",
    );
    commit(&source, "Alice", "alice@work.test", "two", "d\n", "second");
    git_command(
        dir.path(),
        &[
            "clone",
            "--depth",
            "1",
            &format!("file://{}", source.display()),
            shallow.to_str().unwrap(),
        ],
    );
    let repo = git::new_repositories(std::slice::from_ref(&shallow)).remove(0);
    let result =
        git::scan_repository(&repo, &CollectOptions::default(), &AtomicBool::new(false)).unwrap();
    assert!(!result.warnings.is_empty());
    let partial = stats::aggregate(&result.records, &AggregateOptions::default());
    assert_eq!(partial[0].commits, 1);
    assert_eq!(partial[0].unknown_line_commits, 1);
    assert_eq!(partial[0].removed_added_ratio(), None);
    let combined = stats::aggregate(&scan(&[source, shallow]), &AggregateOptions::default());
    assert_eq!(combined[0].commits, 2);
    assert_eq!(combined[0].added, 4);
    assert_eq!(combined[0].unknown_line_commits, 0);
}

#[test]
fn mailmapped_primary_and_coauthor_do_not_double_credit_the_same_person() {
    let dir = tempfile::tempdir().unwrap();
    let repo = dir.path().join("source");
    init(&repo);
    commit(
        &repo,
        "Work Alias",
        "work@team.test",
        "one",
        "one\n",
        "pairing\n\nCo-authored-by: Home Alias <home@team.test>",
    );
    // An uncommitted mailmap is intentionally honored by Git's canonical fields.
    std::fs::write(repo.join(".mailmap"), "Canonical Person <canonical@team.test> Work Alias <work@team.test>\nCanonical Person <canonical@team.test> Home Alias <home@team.test>\n").unwrap();
    let records = scan(&[repo]);
    assert_eq!(records[0].email, "canonical@team.test");
    assert_eq!(records[0].coauthors[0].email, "canonical@team.test");
    let authors = stats::aggregate(&records, &AggregateOptions::default());
    assert_eq!(authors.len(), 1);
    assert_eq!(authors[0].name, "Canonical Person");
    assert_eq!(
        (
            authors[0].commits,
            authors[0].coauthored_commits,
            authors[0].added
        ),
        (1, 0, 1)
    );
}
