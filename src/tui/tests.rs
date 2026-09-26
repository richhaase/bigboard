use super::components::*;
use super::render::{heatmap, line_value, monthly_rows};
use super::*;
use crate::model::Identity;
use crate::stats::RepoContribution;
use chrono::{TimeZone, Utc};
use ratatui::{backend::TestBackend, text::Line};
use std::collections::BTreeMap;

fn repo(name: &str) -> Repository {
    Repository {
        id: format!("/repos/{name}"),
        path: format!("/repos/{name}").into(),
        name: name.into(),
    }
}
fn now() -> DateTime<FixedOffset> {
    Utc.with_ymd_and_hms(2026, 6, 2, 12, 0, 0)
        .unwrap()
        .fixed_offset()
}
fn record(name: &str, repo_name: &str, added: i64) -> CommitRecord {
    CommitRecord {
        commit_id: format!("{name}-{repo_name}-{added}"),
        author: name.into(),
        email: format!("{}@example.com", name.to_lowercase().replace(' ', "")),
        date: now() - Duration::days(1),
        added,
        removed: added / 10,
        repo_id: format!("/repos/{repo_name}"),
        repo_name: repo_name.into(),
        ai_assisted: false,
        coauthors: vec![],
        lines_known: true,
        landed: true,
        is_merge: false,
    }
}
fn empty() -> App {
    let mut app = App::new(
        vec![],
        SortField::Total,
        HashSet::new(),
        "test",
        6,
        AnalysisOptions::default(),
        "dark",
        IdentityStore::default(),
        PathBuf::from("/unused/test-identities.json"),
    );
    app.cutoff = now();
    app.width = 100;
    app.height = 40;
    app
}
fn populate(app: &mut App) {
    app.rebuild_contributors();
    app.recompute();
}
fn populated() -> App {
    let mut app = empty();
    app.loaded_repos = vec![repo("compiler"), repo("engine")];
    app.all_records = vec![
        record("Ada Lovelace", "engine", 100),
        record("Grace Hopper", "compiler", 50),
    ];
    populate(&mut app);
    app
}
fn key(app: &mut App, code: KeyCode) -> Action {
    app.key(KeyEvent::new(code, KeyModifiers::NONE))
}
fn ch(app: &mut App, c: char) -> Action {
    key(app, KeyCode::Char(c))
}
fn ctrl(app: &mut App, c: char) -> Action {
    app.key(KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL))
}
fn plain(lines: Vec<UiLine>) -> String {
    lines
        .iter()
        .map(|l| {
            l.spans
                .iter()
                .map(|s| s.content.as_ref())
                .collect::<String>()
        })
        .collect::<Vec<_>>()
        .join("\n")
}
fn draw(app: &mut App, width: u16, height: u16) -> String {
    let mut terminal = Terminal::new(TestBackend::new(width, height)).unwrap();
    terminal.draw(|frame| app.draw(frame)).unwrap();
    let buf = terminal.backend().buffer();
    (0..height)
        .map(|y| (0..width).map(|x| buf[(x, y)].symbol()).collect::<String>())
        .collect::<Vec<_>>()
        .join("\n")
}
fn name(app: &mut App, name: &str) {
    ctrl(app, 'u');
    for c in name.chars() {
        ch(app, c);
    }
}
fn begin_merge(app: &mut App) {
    ch(app, 'M');
    assert!(app.merge.is_some());
    key(app, KeyCode::Enter);
    assert!(app.merge.as_ref().unwrap().target.is_some());
}

#[test]
fn github_picker_key_does_not_interrupt_search_or_active_scans() {
    let mut app = populated();
    assert_eq!(ch(&mut app, 'g'), Action::Github);
    ch(&mut app, '/');
    assert_eq!(ch(&mut app, 'g'), Action::None);
    assert_eq!(app.filter_query, "g");
    key(&mut app, KeyCode::Esc);
    app.loading = true;
    assert_eq!(ch(&mut app, 'g'), Action::None);
}

#[test]
fn search_apply_clear_and_unicode_backspace() {
    let mut app = populated();
    ch(&mut app, '/');
    for c in "grace".chars() {
        ch(&mut app, c);
    }
    assert!(app.searching);
    assert_eq!(app.displayed_authors().len(), 1);
    assert_eq!(app.displayed_authors()[0].name, "Grace Hopper");
    key(&mut app, KeyCode::Enter);
    assert!(!app.searching);
    assert_eq!(ch(&mut app, 'q'), Action::None);
    assert!(app.filter_query.is_empty());
    ch(&mut app, '/');
    ch(&mut app, '日');
    ch(&mut app, '本');
    key(&mut app, KeyCode::Backspace);
    assert_eq!(app.filter_query, "日");
    key(&mut app, KeyCode::Esc);
    assert!(!app.searching);
    assert_eq!(key(&mut app, KeyCode::Esc), Action::Quit);
}

#[test]
fn board_paging_and_endpoints_follow_the_filtered_list_after_resize() {
    let mut app = empty();
    app.all_records = (0..100)
        .map(|i| record(&format!("Person {i:03}"), "engine", i + 1))
        .collect();
    populate(&mut app);
    draw(&mut app, 80, 24);
    let page = app.table_viewport();
    key(&mut app, KeyCode::PageDown);
    assert_eq!((app.selected, app.offset), (page, page));
    key(&mut app, KeyCode::PageUp);
    assert_eq!((app.selected, app.offset), (0, 0));
    key(&mut app, KeyCode::End);
    assert_eq!(app.selected, 99);
    let screen = draw(&mut app, 60, 20);
    assert!(
        screen
            .lines()
            .any(|line| line.contains('▸') && line.contains("Person 000")),
        "{screen}"
    );
    key(&mut app, KeyCode::Home);
    assert_eq!((app.selected, app.offset), (0, 0));
    app.filter_query = "Person 00".into();
    key(&mut app, KeyCode::End);
    assert_eq!(app.selected, 9);
    app.filter_query = "no matches".into();
    for code in [
        KeyCode::PageDown,
        KeyCode::PageUp,
        KeyCode::End,
        KeyCode::Home,
    ] {
        key(&mut app, code);
        assert_eq!(app.selected, 0);
    }
}

#[test]
fn empty_states_offer_available_source_and_filter_controls() {
    let mut app = empty();
    app.github_source = true;
    let screen = draw(&mut app, 80, 24);
    assert!(screen.contains("choose repos"));
    assert!(!screen.contains("all branches"));
    app.view = View::Operative;
    let screen = draw(&mut app, 80, 24);
    assert!(!screen.contains("all branches"));
    app.view = View::Aggregate;
    app.github_source = false;
    assert!(draw(&mut app, 80, 24).contains("B for all branches"));
    app.hide_bots = true;
    assert!(draw(&mut app, 80, 24).contains("b to include bots"));
    app.loaded_repos.push(repo("engine"));
    app.excluded.insert("/repos/engine".into());
    assert!(draw(&mut app, 80, 24).contains("No repositories selected"));
}
#[test]
fn sort_cycle_reverse_and_metric_ranks() {
    let mut app = populated();
    ch(&mut app, 'S');
    assert!(app.sort_ascending);
    assert_eq!(app.authors[0].name, "Grace Hopper");
    let table = plain(app.table_lines());
    assert!(
        table
            .lines()
            .any(|l| l.contains("Grace Hopper") && l.contains("02"))
    );
    ch(&mut app, 's');
    assert_eq!(app.sort_field, SortField::Commits);
    for _ in 0..5 {
        ch(&mut app, 's');
    }
    assert_eq!(app.sort_field, SortField::Total);
}
#[test]
fn repository_changes_apply_on_escape_and_enter_and_gate_keys() {
    for finish in [KeyCode::Esc, KeyCode::Enter] {
        let mut app = populated();
        ch(&mut app, 'r');
        ch(&mut app, ' ');
        assert!(app.excluded.is_empty());
        let time = app.time_index;
        key(&mut app, KeyCode::Left);
        ch(&mut app, 'R');
        ch(&mut app, 'B');
        ch(&mut app, 'M');
        assert_eq!(app.time_index, time);
        assert!(!app.loading);
        assert!(app.merge.is_none());
        assert_eq!(app.scope, HistoryScope::Landed);
        key(&mut app, finish);
        assert!(app.excluded.contains("/repos/compiler"));
        assert_eq!(app.authors.len(), 1);
        assert_eq!(app.filtered_records().count(), 1);
    }
}
#[test]
fn landed_default_and_branch_toggle_change_both_board_and_detail() {
    let mut app = populated();
    app.all_records[1].landed = false;
    populate(&mut app);
    assert_eq!(app.scope, HistoryScope::Landed);
    assert_eq!(app.authors.len(), 1);
    ch(&mut app, 'B');
    assert_eq!(app.authors.len(), 2);
    ch(&mut app, 'j');
    key(&mut app, KeyCode::Enter);
    let id = app.active_id.clone();
    ch(&mut app, 'B');
    assert_eq!(app.active_id, id);
    assert!(plain(app.detail_lines()).contains("NO SIGNAL"));
    ch(&mut app, 'B');
    assert!(!plain(app.detail_lines()).contains("NO SIGNAL"));
}
#[test]
fn same_name_contributors_keep_distinct_ids_and_detail_selection() {
    let mut app = empty();
    let mut first = record("Alex", "engine", 100);
    let mut second = record("Alex", "compiler", 50);
    first.email = "alex.one@example.com".into();
    second.email = "alex.two@example.com".into();
    app.all_records = vec![first, second];
    populate(&mut app);
    assert_eq!(app.authors.len(), 2);
    assert!(plain(app.table_lines()).contains("alex.one@example.com"));
    ch(&mut app, 'j');
    assert!(plain(app.table_lines()).contains("alex.two@example.com"));
    let selected = app.selected_id().unwrap();
    key(&mut app, KeyCode::Enter);
    assert_eq!(app.active_id, selected);
    assert!(plain(app.detail_lines()).contains("alex.two@example.com"));
    ch(&mut app, 'k');
    assert_ne!(app.active_id, selected);
    assert!(plain(app.detail_lines()).contains("alex.one@example.com"));
}
#[test]
fn time_changes_retain_id_across_display_name_changes() {
    let mut app = empty();
    let mut old = record("A", "engine", 100);
    old.date = now() - Duration::days(100);
    old.email = "ada@example.com".into();
    let mut recent = record("Ada Lovelace", "engine", 50);
    recent.email = old.email.clone();
    app.all_records = vec![old, recent];
    populate(&mut app);
    key(&mut app, KeyCode::Enter);
    let id = app.active_id.clone();
    app.time_index = 2;
    app.recompute();
    assert_eq!(app.active_id, id);
    assert_eq!(app.authors[0].id, id);
    assert!(!plain(app.detail_lines()).contains("NO SIGNAL"));
    assert_eq!(app.authors[0].commits, 1);
}
#[test]
fn one_cutoff_applies_to_all_ranges_and_calendar() {
    let mut app = empty();
    let mut recent = record("Ada", "engine", 10);
    recent.date = app.cutoff;
    let mut future = recent.clone();
    future.commit_id = "future".into();
    future.date += Duration::seconds(1);
    app.all_records = vec![recent, future];
    populate(&mut app);
    assert_eq!(app.authors[0].commits, 1);
    let cutoff = app.cutoff;
    ch(&mut app, 'h');
    ch(&mut app, 'l');
    ch(&mut app, 'B');
    assert_eq!(app.cutoff, cutoff);
    assert_eq!(app.authors[0].commits, 1);
}
#[test]
fn timezone_maps_drive_daily_monthly_and_heatmap() {
    let mut app = empty();
    app.options.timezone = chrono_tz::America::Denver;
    let mut r = record("Ada", "engine", 100);
    r.date = Utc
        .with_ymd_and_hms(2026, 6, 1, 1, 0, 0)
        .unwrap()
        .fixed_offset();
    app.all_records = vec![r];
    populate(&mut app);
    let day = chrono::NaiveDate::from_ymd_opt(2026, 5, 31).unwrap();
    assert!(app.authors[0].daily.contains_key(&day));
    assert!(app.authors[0].monthly.contains_key("2026-05"));
    key(&mut app, KeyCode::Enter);
    let text = plain(app.detail_content());
    assert!(plain(app.detail_lines()).contains("America/Denver"));
    assert!(text.contains("May 2026"));
    assert!(text.contains('█'));
}
#[test]
fn unknown_and_coauthored_lines_are_not_rendered_as_zero() {
    let mut app = empty();
    let mut r = record("Ada", "engine", 0);
    r.lines_known = false;
    r.coauthors.push(Identity {
        name: "Grace".into(),
        email: "grace@example.com".into(),
    });
    app.all_records = vec![r];
    populate(&mut app);
    assert_eq!(app.totals().0, 1);
    let table = plain(app.table_lines());
    assert!(table.lines().any(|l| l.contains("Ada") && l.contains('?')));
    assert!(
        table
            .lines()
            .any(|l| l.contains("Grace") && l.contains('—'))
    );
    assert!(plain(app.lines()).contains("Known line subtotal"));
    app.active_id = "email:grace@example.com".into();
    app.view = View::Operative;
    let detail = plain(app.detail_content());
    assert!(detail.contains("Coauthored participation 1"));
    assert!(detail.contains("unallocated"));
    assert!(detail.contains('○'));
    assert!(detail.contains("Removed/added ratio: N/A"));
    assert!(plain(app.detail_lines()).contains("— coauthor lines unallocated"));
}
#[test]
fn scan_failures_remain_visible_without_per_commit_warnings() {
    let mut app = populated();
    app.failed_repos = vec!["broken".into()];
    app.warnings = vec![("/repos/engine".into(), "engine: shallow history".into())];
    let screen = draw(&mut app, 100, 28);
    assert!(screen.contains("1 unreadable"), "{screen}");
    assert!(!screen.contains("1 warning"), "{screen}");
    assert!(!screen.contains("shallow history"), "{screen}");
    key(&mut app, KeyCode::Enter);
    let screen = draw(&mut app, 80, 24);
    assert!(screen.contains("1 unreadable"), "{screen}");
    assert!(!screen.contains("1 warning"), "{screen}");
    assert!(!screen.contains("shallow history"), "{screen}");
    assert!(screen.contains("LANDED"));
    key(&mut app, KeyCode::Esc);
    ch(&mut app, 'r');
    ch(&mut app, 'j');
    assert!(!plain(app.lines()).contains("shallow history"));
    ch(&mut app, 'j');
    let inspector = draw(&mut app, 80, 24);
    assert!(inspector.contains("SCAN FAILED"), "{inspector}");
}

#[test]
fn unavailable_landed_history_is_actionable_only_when_it_affects_the_board() {
    let mut app = populated();
    app.warnings = vec![(
        "/repos/engine".into(),
        "engine: No available default branch could be identified; landed history is unknown. All locally available branches remain available in All branches.".into(),
    )];

    let board = plain(app.quality_lines());
    assert!(board.contains("1 without landed history"), "{board}");
    assert!(!board.contains("No available default branch"), "{board}");

    app.scope = HistoryScope::AllBranches;
    assert!(app.quality_lines().is_empty());

    app.scope = HistoryScope::Landed;
    app.excluded.insert("/repos/engine".into());
    assert!(app.quality_lines().is_empty());
}
#[test]
fn merged_commit_associations_not_summed_in_board() {
    let mut app = empty();
    let r = record("Ada", "engine", 100);
    let mut duplicate = r.clone();
    duplicate.repo_id = "/repos/compiler".into();
    duplicate.repo_name = "compiler".into();
    app.all_records = vec![r, duplicate];
    populate(&mut app);
    assert_eq!(app.totals(), (1, 100, 10, 0));
    assert_eq!(app.authors[0].per_repo.len(), 2);
    key(&mut app, KeyCode::Enter);
    assert!(plain(app.detail_content()).contains("Repository associations can overlap"));
}
#[test]
fn merge_picker_searches_all_loaded_dates_branches_repositories_and_emails() {
    let mut app = populated();
    app.all_records[1].date = now() - Duration::days(100);
    app.all_records[1].landed = false;
    app.excluded.insert("/repos/compiler".into());
    app.time_index = 0;
    populate(&mut app);
    app.time_index = 2;
    app.recompute();
    assert_eq!(app.authors.len(), 1);
    ch(&mut app, 'M');
    assert_eq!(app.merge.as_ref().unwrap().candidates.len(), 1);
    for c in "compiler".chars() {
        ch(&mut app, c);
    }
    assert!(plain(app.lines()).contains("Grace Hopper"));
    ctrl(&mut app, 'u');
    for c in "gracehopper@example.com".chars() {
        ch(&mut app, c);
    }
    key(&mut app, KeyCode::Enter);
    assert_eq!(
        app.merge.as_ref().unwrap().target.as_ref().unwrap().name,
        "Grace Hopper"
    );
    let preview = plain(app.lines());
    assert!(preview.contains("adalovelace@example.com"));
    assert!(preview.contains("gracehopper@example.com"));
    assert!(preview.contains("across all repositories"));
}
#[test]
fn merge_escape_never_writes_or_mutates_identity() {
    let temp = tempfile::tempdir().unwrap();
    for naming in [false, true] {
        let mut app = populated();
        app.identity_path = temp.path().join("identities.json");
        ch(&mut app, 'M');
        if naming {
            key(&mut app, KeyCode::Enter);
            name(&mut app, "Combined");
        }
        key(&mut app, KeyCode::Esc);
        assert!(app.merge.is_none());
        assert_eq!(app.authors.len(), 2);
        assert!(!app.identity_path.exists());
    }
}
#[test]
fn merge_saves_global_mapping_reaggregates_and_keeps_stable_detail() {
    let temp = tempfile::tempdir().unwrap();
    let mut app = populated();
    app.identity_path = temp.path().join("identities.json");
    key(&mut app, KeyCode::Enter);
    begin_merge(&mut app);
    name(&mut app, "Combined Person");
    key(&mut app, KeyCode::Enter);
    assert!(app.merge.is_none());
    assert_eq!(app.authors.len(), 1);
    assert_eq!(app.authors[0].name, "Combined Person");
    assert_eq!(app.authors[0].commits, 2);
    assert_eq!(app.active_id, app.authors[0].id);
    assert_eq!(app.selected_id().unwrap(), app.active_id);
    let saved = IdentityStore::load(&app.identity_path).unwrap();
    assert_eq!(
        saved.canonical_id("email:adalovelace@example.com"),
        saved.canonical_id("email:gracehopper@example.com")
    );
    let mut next = populated();
    next.identities = saved;
    populate(&mut next);
    assert_eq!(next.authors.len(), 1);
    assert_eq!(next.authors[0].name, "Combined Person");
}
#[test]
fn failed_merge_is_visible_and_leaves_memory_unchanged() {
    let temp = tempfile::tempdir().unwrap();
    let obstacle = temp.path().join("not-a-directory");
    std::fs::write(&obstacle, "obstacle").unwrap();
    let mut app = populated();
    app.identity_path = obstacle.join("identities.json");
    begin_merge(&mut app);
    name(&mut app, "Combined");
    key(&mut app, KeyCode::Enter);
    assert!(app.merge.is_some());
    assert!(plain(app.lines()).contains("Could not save mapping"));
    assert_eq!(app.authors.len(), 2);
    assert_ne!(
        app.identities.canonical_id("email:adalovelace@example.com"),
        app.identities.canonical_id("email:gracehopper@example.com")
    );
}
#[test]
fn empty_merge_name_cannot_be_saved() {
    let mut app = populated();
    begin_merge(&mut app);
    name(&mut app, "");
    key(&mut app, KeyCode::Enter);
    assert!(plain(app.lines()).contains("nonempty"));
    assert_eq!(app.authors.len(), 2);
}
#[test]
fn all_contributors_remain_reachable_after_resize() {
    let mut app = empty();
    app.all_records = (0..40)
        .map(|i| record(&format!("Person {i:02}"), "engine", 100 - i))
        .collect();
    populate(&mut app);
    for (w, h) in [(120, 40), (100, 24), (80, 24), (60, 20), (40, 12)] {
        draw(&mut app, w, h);
        for _ in 0..50 {
            ch(&mut app, 'j');
        }
        let screen = draw(&mut app, w, h);
        assert_eq!(app.selected, 39);
        if (w, h) == (40, 12) {
            assert!(screen.contains("LOW RESOLUTION"), "{w}x{h}:\n{screen}");
            let restored = draw(&mut app, 80, 24);
            assert_eq!(app.selected, 39);
            assert!(restored.contains("Person 39"), "{restored}");
        } else {
            assert!(screen.contains("Person 39"), "{w}x{h}:\n{screen}");
        }
        for _ in 0..50 {
            ch(&mut app, 'k');
        }
    }
}
#[test]
fn table_labels_bot_toggle_and_detected_ai() {
    let mut app = populated();
    let mut bot = record("dependabot[bot]", "engine", 200);
    bot.ai_assisted = true;
    app.all_records.push(bot);
    populate(&mut app);
    app.width = 144;
    let table = plain(app.table_lines());
    for text in ["AUTHORED", "COAUTH", "LINES CHANGED", "DETECTED AI", "BOT"] {
        assert!(table.contains(text), "missing {text}:\n{table}");
    }
    ch(&mut app, 'b');
    assert_eq!(app.authors.len(), 2);
    ch(&mut app, 'b');
    assert_eq!(app.authors.len(), 3);
    assert!(!plain(app.lines()).contains("IMPACT"));
}
#[test]
fn streaming_load_qualifies_partial_failures_and_refreshes_cutoff() {
    let repos = vec![repo("engine"), repo("broken")];
    let mut app = App::new(
        repos.clone(),
        SortField::Total,
        HashSet::new(),
        "test",
        6,
        AnalysisOptions::default(),
        "dark",
        IdentityStore::default(),
        "/unused".into(),
    );
    app.cutoff = now();
    app.loaded(ScanResult {
        repository: repos[0].clone(),
        records: vec![record("Ada", "engine", 100)],
        warnings: vec!["shallow history".into()],
        error: None,
    });
    assert!(app.loading);
    app.loaded(ScanResult {
        repository: repos[1].clone(),
        records: vec![],
        warnings: vec![],
        error: Some("unreadable".into()),
    });
    assert!(!app.loading);
    assert_eq!(app.authors.len(), 1);
    let summary = plain(app.lines());
    assert!(summary.contains("1 unreadable"), "{summary}");
    assert!(!summary.contains("1 warning"), "{summary}");
    assert!(!summary.contains("shallow history"), "{summary}");
    let old = app.cutoff;
    assert_eq!(ch(&mut app, 'R'), Action::Refresh);
    assert_ne!(app.cutoff, old);
    assert_eq!(app.authors.len(), 1);
    for r in repos {
        app.loaded(ScanResult {
            repository: r,
            records: vec![],
            warnings: vec![],
            error: Some("failed".into()),
        });
    }
    assert!(plain(app.lines()).contains("totals unavailable"));
}
#[test]
fn monthly_gaps_and_heatmap_unknown_participation_markers() {
    let p = Palette::for_theme("dark");
    let months = BTreeMap::from([
        (
            "2025-01".into(),
            RepoContribution {
                commits: 2,
                ..Default::default()
            },
        ),
        (
            "2025-04".into(),
            RepoContribution {
                commits: 3,
                ..Default::default()
            },
        ),
    ]);
    let rows = monthly_rows(&months);
    assert_eq!(rows.len(), 4);
    assert_eq!(rows[1].1.commits, 0);
    assert_eq!(rows[2].1.commits, 0);
    assert_eq!(rows[3].1.commits, 3);
    let today = now().date_naive();
    let daily = BTreeMap::from([
        (
            today - Duration::days(1),
            RepoContribution {
                commits: 1,
                unknown_line_commits: 1,
                ..Default::default()
            },
        ),
        (
            today - Duration::days(2),
            RepoContribution {
                coauthored_commits: 1,
                ..Default::default()
            },
        ),
    ]);
    let heat = plain(heatmap(&daily, 100, today, &p));
    assert!(heat.contains('?'));
    assert!(heat.contains('○'));
    assert!(heat.contains("unallocated"));
    assert_eq!(line_value(0, 1, false), "?");
    assert_eq!(line_value(0, 0, true), "—");
}
#[test]
fn quit_and_merge_controls_remain_safe() {
    let mut app = populated();
    begin_merge(&mut app);
    assert_eq!(ctrl(&mut app, 'c'), Action::Quit);
    key(&mut app, KeyCode::Esc);
    for view in [View::Aggregate, View::Operative, View::Repositories] {
        app.view = view;
        assert_eq!(ch(&mut app, 'q'), Action::Quit);
    }
    let mut released = KeyEvent::new(KeyCode::Char('q'), KeyModifiers::NONE);
    released.kind = KeyEventKind::Release;
    assert_eq!(app.key(released), Action::None);
}
#[test]
fn long_repository_overlay_keeps_cursor_visible() {
    let mut app = populated();
    app.loaded_repos = (0..50).map(|i| repo(&format!("repo-{i:02}"))).collect();
    ch(&mut app, 'r');
    for _ in 0..60 {
        ch(&mut app, 'j');
    }
    assert!(draw(&mut app, 60, 20).contains("repo-49"));
    ch(&mut app, ' ');
    key(&mut app, KeyCode::Esc);
    assert!(app.excluded.contains("/repos/repo-49"));
}
#[test]
fn text_safety_numbers_width_and_gradients() {
    assert_eq!(format_number(i64::MIN), "-9,223,372,036,854,775,808");
    let hostile = "\u{1b}]52;c;data\u{7}Alice\n\u{1b}[31mRed\u{1b}[0m\u{7f}";
    assert_eq!(display_text(hostile), "Alice Red ");
    assert_eq!(truncate("hello world foo", 10), "hello w...");
    assert_eq!(truncate("日本語", 2), "日");
    for width in 0..15 {
        assert!(
            unicode_width::UnicodeWidthStr::width(truncate("日本語テスト名前", width).as_str())
                <= width
        );
    }
    let p = Palette::for_theme("dark");
    let bar = plain(vec![Line::from(impact_bar(80, 20, 100, 20, &p))]);
    assert!(bar.contains("▓▒░"));
    assert_eq!(bar.chars().count(), 20);
}
#[test]
fn stat_boxes_fit_and_use_detected_attribution() {
    let p = Palette::for_theme("dark");
    for width in [40, 50, 60, 78, 80, 96, 120] {
        assert!(
            stat_boxes((1234567, 9876543, 1234567, 4321), width, false, &p)
                .iter()
                .all(|l| l.width() <= width)
        );
    }
    let boxes = plain(stat_boxes((1000, 0, 0, 3), 120, false, &p));
    assert!(boxes.contains("<1% (3)"));
    assert!(boxes.contains("DETECTED AI"));
    assert!(!boxes.contains("CO-AUTHORED"));
}
#[test]
fn loading_shows_heartbeat_elapsed_stage_and_completed_counts() {
    use crate::progress::{ScanProgress, ScanStage};
    let mut app = populated();
    app.github_source = true;
    app.repositories = vec![repo("engine")];
    app.reset_pending();
    app.scan_progress.insert(
        "/repos/engine".into(),
        ScanProgress {
            repository_id: "/repos/engine".into(),
            repository_name: "org/engine".into(),
            stage: ScanStage::CountingChanges {
                done: 128,
                total: 400,
            },
        },
    );
    for (width, height) in [(50, 12), (80, 24), (140, 36)] {
        app.loading_tick = 31;
        let screen = draw(&mut app, width, height);
        assert!(screen.contains("3s elapsed"), "{screen}");
        assert!(screen.contains("q cancels"), "{screen}");
        assert!(screen.contains("org/engine"), "{screen}");
        assert!(screen.contains("128/400 commits"), "{screen}");
        assert!(screen.contains("0/1 repos complete"), "{screen}");
        app.loading_tick += 1;
        assert_ne!(screen, draw(&mut app, width, height));
    }
    app.loaded(ScanResult {
        repository: repo("engine"),
        records: vec![],
        error: None,
        warnings: vec![],
    });
    assert!(!app.loading);
    assert!(app.scan_progress.is_empty());
    app.reset_pending();
    assert_eq!(app.loading_tick, 0);
    app.filter_query = "old search".into();
    app.searching = true;
    assert_eq!(ch(&mut app, 'q'), Action::Quit);
}

#[test]
fn views_render_at_tiny_and_large_sizes() {
    let mut app = populated();
    for (w, h) in [(1, 1), (10, 3), (40, 12), (80, 24), (120, 80)] {
        for view in [View::Aggregate, View::Operative, View::Repositories] {
            app.view = view;
            app.active_id = "email:adalovelace@example.com".into();
            draw(&mut app, w, h);
        }
        app.view = View::Aggregate;
        ch(&mut app, 'M');
        draw(&mut app, w, h);
        key(&mut app, KeyCode::Enter);
        draw(&mut app, w, h);
        key(&mut app, KeyCode::Esc);
        app.loading = true;
        draw(&mut app, w, h);
        app.loading = false;
        app.error = Some("failure".into());
        draw(&mut app, w, h);
        app.error = None;
    }
}

#[test]
fn roomy_dashboard_preserves_full_content_and_separates_major_sections() {
    let mut app = populated();
    let roomy = draw(&mut app, 180, 50);
    let lines: Vec<_> = roomy.lines().collect();
    let activity = lines
        .iter()
        .position(|line| line.contains("ACTIVITY"))
        .unwrap();
    let range = lines.iter().position(|line| line.contains("14d")).unwrap();
    let contributors = lines
        .iter()
        .position(|line| line.contains("CONTRIBUTORS"))
        .unwrap();
    let controls = lines
        .iter()
        .position(|line| line.contains("select") && line.contains("detail"))
        .unwrap();

    assert!(
        range < activity,
        "range must precede the data it filters:\n{roomy}"
    );
    assert!(lines[range - 1].trim().is_empty(), "{roomy}");
    assert!(lines[range - 2].contains("LANDED"), "{roomy}");
    assert!(lines[range - 3].trim().is_empty(), "{roomy}");
    assert!(lines[range].contains("RANGE"), "{roomy}");
    assert!(lines[range + 1].trim().is_empty(), "{roomy}");
    for section in [activity, contributors, controls] {
        assert!(
            lines[section - 1].trim().is_empty(),
            "section at row {section} has no breathing room:\n{roomy}"
        );
    }
    for label in [
        "AUTHORED",
        "ADDED",
        "REMOVED",
        "Lines changed",
        "COAUTHORED",
        "Detected AI",
        "Ada Lovelace",
        "Grace Hopper",
        "history",
        "merge",
        "repos",
        "quit",
    ] {
        assert!(roomy.contains(label), "missing {label}:\n{roomy}");
    }
    assert!(
        lines[activity + 1].contains("AUTHORED")
            && lines[activity + 1].contains("ADDED")
            && lines[activity + 1].contains("REMOVED")
    );
    assert!(
        lines[activity + 2].contains("Lines changed")
            && lines[activity + 2].contains("COAUTHORED")
            && lines[activity + 2].contains("Detected AI")
    );
    let dividers = |line: &str| {
        line.chars()
            .enumerate()
            .filter_map(|(column, character)| (character == '│').then_some(column))
            .collect::<Vec<_>>()
    };
    assert_eq!(
        dividers(lines[activity + 1]),
        dividers(lines[activity + 2]),
        "activity grid dividers must align:\n{roomy}"
    );

    let compact = draw(&mut app, 120, 24);
    let compact_lines: Vec<_> = compact.lines().collect();
    let compact_activity = compact_lines
        .iter()
        .position(|line| line.contains("ACTIVITY"))
        .unwrap();
    assert!(
        !compact_lines[compact_activity - 1].trim().is_empty(),
        "compact fallback spent a contributor row on spacing:\n{compact}"
    );
}

#[test]
fn wide_dashboard_commands_align_and_have_room_with_many_contributors() {
    let mut app = empty();
    app.loaded_repos = vec![repo("engine")];
    app.all_records = (0..14)
        .map(|index| record(&format!("Contributor {index:02}"), "engine", 100))
        .collect();
    populate(&mut app);
    let screen = draw(&mut app, 160, 40);
    let lines: Vec<_> = screen.lines().collect();
    let activity = lines
        .iter()
        .position(|line| line.contains("ACTIVITY"))
        .unwrap();
    let contributors = lines
        .iter()
        .position(|line| line.contains("CONTRIBUTORS"))
        .unwrap();
    assert!(lines[activity - 1].trim().is_empty(), "{screen}");
    let range = lines
        .iter()
        .position(|line| line.contains("RANGE"))
        .unwrap();
    assert_eq!(range + 2, activity, "{screen}");
    assert!(lines[range - 1].contains("LANDED"), "{screen}");
    assert!(lines[activity + 1].contains("AUTHORED"), "{screen}");
    assert!(lines[activity + 2].contains("COAUTHORED"), "{screen}");
    assert!(lines[contributors - 1].trim().is_empty(), "{screen}");
    let command_rows: Vec<_> = lines
        .iter()
        .enumerate()
        .filter(|(_, line)| line.contains("▐↑↓▌") || line.contains("▐M▌"))
        .collect();
    assert_eq!(command_rows.len(), 2, "{screen}");
    let keycaps = |line: &str| {
        line.chars()
            .enumerate()
            .filter_map(|(column, character)| (character == '▐').then_some(column))
            .collect::<Vec<_>>()
    };
    let first = keycaps(command_rows[0].1);
    let second = keycaps(command_rows[1].1);
    assert_eq!(first.len(), 6, "{screen}");
    assert_eq!(second.len(), 6, "{screen}");
    assert_eq!(first, second, "{screen}");
    assert!(second[5] < 105, "commands spread too far: {screen}");
    assert_eq!(command_rows[1].0, command_rows[0].0 + 1, "{screen}");
    assert!(lines[command_rows[0].0 - 1].trim().is_empty(), "{screen}");
    for index in 0..14 {
        assert!(
            screen.contains(&format!("Contributor {index:02}")),
            "contributor {index} was hidden:\n{screen}"
        );
    }
    for label in [
        "sort/reverse",
        "merge",
        "history",
        "bots:on",
        "refresh",
        "quit",
    ] {
        assert!(screen.contains(label), "missing {label}:\n{screen}");
    }

    let compact = draw(&mut app, 80, 24);
    assert!(compact.contains("▐M▌ merge"), "{compact}");
    assert!(compact.contains("▐q▌ quit"), "{compact}");
    for (width, height) in [(104, 30), (120, 35), (160, 40)] {
        let resized = draw(&mut app, width, height);
        assert!(!resized.contains("LOW RESOLUTION"), "{resized}");
        assert!(
            app.lines()
                .iter()
                .all(|line| line.width() <= width as usize),
            "command grid overflowed {width}×{height}"
        );
    }
    app.all_records[0].lines_known = false;
    populate(&mut app);
    let incomplete = draw(&mut app, 160, 40);
    assert!(incomplete.contains("? unknown lines"), "{incomplete}");
    assert!(incomplete.contains("▐q▌ quit"), "{incomplete}");
    assert!(incomplete.contains("Contributor 00"), "{incomplete}");
}

#[test]
fn conflicting_attribution_is_not_rendered_and_counts_follow_repository_filters() {
    let mut app = populated();
    let mut duplicate = app.all_records[0].clone();
    duplicate.repo_id = "/repos/compiler".into();
    duplicate.repo_name = "compiler".into();
    duplicate.author = "Different Person".into();
    duplicate.email = "different@example.com".into();
    app.all_records.push(duplicate);
    populate(&mut app);
    assert_eq!(app.totals().0, 2);
    let summary = plain(app.lines());
    assert!(!summary.contains("1 warning"), "{summary}");
    assert!(
        !summary.contains("conflicting contributor mappings"),
        "{summary}"
    );
    ch(&mut app, 'r');
    let inspector = plain(app.lines())
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ");
    assert!(
        !inspector.contains("conflicting contributor mappings"),
        "{inspector}"
    );
    key(&mut app, KeyCode::Esc);
    app.excluded.insert("/repos/compiler".into());
    app.recompute();
    assert_eq!(app.authors[0].name, "Ada Lovelace");
    assert_eq!(app.totals(), (1, 100, 10, 0));
}

#[test]
fn unknown_headers_are_qualified_and_detected_ai_remains_in_detail_breakdowns() {
    let mut app = empty();
    let mut unknown = record("Ada", "engine", 0);
    unknown.lines_known = false;
    app.all_records = vec![unknown];
    populate(&mut app);
    let overview = plain(app.lines());
    for qualifier in [
        "ADDED ?",
        "REMOVED ?",
        "Known line subtotal",
        "unknown lines",
    ] {
        assert!(
            overview.contains(qualifier),
            "missing {qualifier}:\n{overview}"
        );
    }
    assert!(
        !overview
            .split_whitespace()
            .any(|cell| matches!(cell, "+0" | "-0")),
        "unknown line totals must not include an unqualified zero stat cell"
    );
    key(&mut app, KeyCode::Enter);
    assert!(plain(app.detail_lines()).contains("Known line subtotal: added ? · removed ?"));
    app.all_records[0].lines_known = true;
    app.all_records[0].ai_assisted = true;
    app.all_records[0].added = 100;
    populate(&mut app);
    let detail = plain(app.detail_content());
    let repo = detail
        .split("REPO CONTRIBUTIONS")
        .nth(1)
        .unwrap()
        .split("ACTIVITY TIMELINE")
        .next()
        .unwrap();
    assert!(repo.contains("Detected AI 100%"));
    let timeline = detail
        .split("ACTIVITY TIMELINE")
        .nth(1)
        .unwrap()
        .split("ACTIVITY MATRIX")
        .next()
        .unwrap();
    assert!(timeline.contains("Detected AI 100%"));
}

#[test]
fn conflicting_clone_identity_remains_mergeable_when_representative_is_excluded() {
    let temp = tempfile::tempdir().unwrap();
    let mut app = empty();
    app.identity_path = temp.path().join("identities.json");
    app.loaded_repos = vec![repo("a-clone"), repo("b-clone")];
    let mut first = record("Local A", "a-clone", 100);
    first.email = "local-a@example.com".into();
    let mut alternate = first.clone();
    alternate.author = "Local B".into();
    alternate.email = "local-b@example.com".into();
    alternate.repo_id = "/repos/b-clone".into();
    alternate.repo_name = "b-clone".into();
    app.all_records = vec![first, alternate];
    populate(&mut app);
    assert_eq!(app.authors.len(), 1);
    assert_eq!(app.authors[0].id, "email:local-a@example.com");
    assert_eq!(app.contributors.len(), 2);
    app.excluded.insert("/repos/a-clone".into());
    app.recompute();
    assert_eq!(app.authors[0].id, "email:local-b@example.com");
    ch(&mut app, 'M');
    let flow = app
        .merge
        .as_ref()
        .expect("filtered alternate ID opens merge picker");
    assert_eq!(flow.source.id, "email:local-b@example.com");
    assert_eq!(flow.candidates.len(), 1);
    assert_eq!(flow.candidates[0].id, "email:local-a@example.com");
    key(&mut app, KeyCode::Enter);
    name(&mut app, "Resolved Person");
    key(&mut app, KeyCode::Enter);
    assert!(app.merge.is_none());
    app.excluded.clear();
    app.recompute();
    assert_eq!(app.contributors.len(), 1);
    assert_eq!(app.authors.len(), 1);
    assert_eq!(app.authors[0].name, "Resolved Person");
    assert_eq!(app.totals(), (1, 100, 10, 0));
}

fn warning_heavy_dashboard(theme: &str, unknown_lines: bool) -> App {
    let mut app = empty();
    app.palette = Palette::for_theme(theme);
    app.loaded_repos = (0..15).map(|i| repo(&format!("repo-{i:02}"))).collect();
    let mut first = record("Ada Lovelace", "repo-00", 12_345);
    first.ai_assisted = true;
    app.all_records = if unknown_lines {
        first.lines_known = false;
        first.coauthors.push(Identity {
            name: "Grace Hopper".into(),
            email: "gracehopper@example.com".into(),
        });
        vec![first]
    } else {
        vec![first, record("Grace Hopper", "repo-01", 6_789)]
    };
    app.warnings = (0..19)
        .map(|i| (
            format!("/repos/repo-{:02}", i % 15),
            format!("repo-{:02}: Merge {i:012} resolution line counts cannot be allocated reliably because the locally available history lacks a reconstructable baseline. Authored participation remains visible, but line totals are incomplete and the full explanation belongs in the repository inspector.", i % 15),
        ))
        .collect();
    populate(&mut app);
    app
}

#[test]
fn warning_heavy_dashboard_keeps_context_rows_and_controls_visible() {
    for theme in ["light", "dark"] {
        for width in [144, 80] {
            for unknown_lines in [false, true] {
                let mut app = warning_heavy_dashboard(theme, unknown_lines);
                let screen = draw(&mut app, width, 24);
                let context = format!("{theme} {width}x24 unknown={unknown_lines}:\n{screen}");
                assert!(!screen.contains("19 warnings"), "{context}");
                assert!(
                    !screen.contains("resolution line counts cannot"),
                    "{context}"
                );
                assert!(!screen.contains("reconstructable baseline"), "{context}");
                assert!(
                    !plain(app.table_lines()).contains("@example.com"),
                    "unambiguous rows should not add an identity footer: {context}"
                );
                for label in ["Ada Lovelace", "Grace Hopper", "LANDED", "UTC"] {
                    assert!(screen.contains(label), "missing {label}: {context}");
                }
                assert!(
                    screen
                        .lines()
                        .any(|line| line.contains('▸') && line.contains("Ada Lovelace")),
                    "selected contributor missing: {context}"
                );
                let lower = screen.to_lowercase();
                for label in ["history", "merge", "repos", "quit"] {
                    assert!(lower.contains(label), "missing control {label}: {context}");
                }
                for line in app.lines() {
                    assert!(
                        line.width() <= usize::from(width),
                        "line exceeds width: {} > {width}: {}",
                        line.width(),
                        plain(vec![line])
                    );
                }
                if unknown_lines {
                    assert!(screen.contains('?'), "{context}");
                    assert!(screen.contains('—'), "{context}");
                    assert!(lower.contains("known"), "{context}");
                    assert!(lower.contains("unknown"), "{context}");
                }
            }
        }
    }
}

#[test]
fn low_resolution_warning_keeps_context_and_resize_restores_selected_contributor() {
    let mut app = warning_heavy_dashboard("dark", true);
    ch(&mut app, 'j');
    let selected = app.selected_id().unwrap();
    let small = draw(&mut app, 60, 17);
    for label in ["B I G", "LOW RESOLUTION", "resize", "LANDED", "UTC", "quit"] {
        assert!(small.contains(label), "missing {label} at 60x17:\n{small}");
    }
    assert_eq!(app.selected_id().unwrap(), selected);
    assert!(app.lines().len() <= 17);
    assert!(app.lines().iter().all(|line| line.width() <= 60));
    assert!(!small.contains("resolution line counts cannot"));

    let restored = draw(&mut app, 80, 24);
    assert!(!restored.contains("LOW RESOLUTION"), "{restored}");
    assert_eq!(app.selected_id().unwrap(), selected);
    assert!(
        restored
            .lines()
            .any(|line| line.contains('▸') && line.contains("Grace Hopper")),
        "{restored}"
    );
    for label in ["LANDED", "UTC", "Known line subtotal", "unknown lines"] {
        assert!(
            restored.contains(label),
            "missing {label} after resize:\n{restored}"
        );
    }
    assert!(
        restored.contains('?') && restored.contains('—'),
        "{restored}"
    );
}

#[test]
fn table_headings_and_large_values_fit_every_sort_and_supported_width() {
    let mut app = populated();
    for author in &mut app.authors {
        author.commits = 123_456;
        author.coauthored_commits = 23_456;
        author.added = 134_567_890;
        author.removed = 246_801_357;
        author.net = author.added - author.removed;
        author.total_change = author.added + author.removed;
        author.ai_commits = 12_345;
    }
    for width in [60, 80, 100, 120, 144] {
        app.width = width;
        app.height = 24;
        for field in [
            SortField::Total,
            SortField::Commits,
            SortField::Added,
            SortField::Removed,
            SortField::Net,
            SortField::AI,
        ] {
            app.sort_field = field;
            for ascending in [false, true] {
                app.sort_ascending = ascending;
                let table = app.table_lines();
                let text = plain(table.clone());
                assert!(text.contains("Ada"), "{width} {field:?}:\n{text}");
                assert!(
                    text.contains("123,456"),
                    "authored value truncated at {width} {field:?}:\n{text}"
                );
                let expected_sorted_value = match field {
                    SortField::Total => "381,369,247",
                    SortField::Commits => "123,456",
                    SortField::Added => "134,567,890",
                    SortField::Removed => "246,801,357",
                    SortField::Net => "-112,233,467",
                    SortField::AI => "9%",
                };
                assert!(
                    text.contains(expected_sorted_value),
                    "active sort value missing at {width} {field:?}:\n{text}"
                );
                let header = table
                    .iter()
                    .find(|line| {
                        line.spans.iter().any(|span| {
                            span.content
                                .split_whitespace()
                                .any(|label| label == "CONTRIBUTOR")
                        })
                    })
                    .unwrap();
                assert!(
                    header
                        .to_string()
                        .contains(if ascending { '↑' } else { '↓' }),
                    "active sort column missing at {width} {field:?}:\n{text}"
                );
                let row = table
                    .iter()
                    .find(|line| line.spans.iter().any(|span| span.content.contains("Ada")))
                    .unwrap();
                let endpoints = |line: &UiLine| {
                    let mut end = 0;
                    line.spans
                        .iter()
                        .map(|span| {
                            end += span.width();
                            (span.content.trim().to_owned(), end)
                        })
                        .collect::<Vec<_>>()
                };
                let cells = endpoints(row);
                let mut checked_columns = 0;
                for (heading, edge) in endpoints(header) {
                    let label = heading.trim_end_matches(['↑', '↓']).trim();
                    let expected = match label {
                        "AUTH" | "AUTHORED" => "123,456",
                        "CO" | "COAUTH" => "23,456",
                        "ADDED" => "134,567,890",
                        "REMOVED" => "246,801,357",
                        "NET" => "-112,233,467",
                        "LINES" | "LINES CHANGED" => "381,369,247",
                        "AI%" | "DETECTED AI" => "9%",
                        _ => continue,
                    };
                    checked_columns += 1;
                    let cell_edge = cells
                        .iter()
                        .find(|(value, _)| value == expected)
                        .unwrap_or_else(|| {
                            panic!("missing {label} value at {width} {field:?}:\n{text}")
                        })
                        .1;
                    assert_eq!(
                        edge, cell_edge,
                        "{label} heading/value right edges differ at {width} {field:?}:\n{text}"
                    );
                }
                assert!(
                    checked_columns >= 2,
                    "numeric column alignment was not exercised at {width} {field:?}:\n{text}"
                );
                for line in table {
                    assert!(
                        line.width() <= usize::from(width),
                        "{width} {field:?} ascending={ascending}, width {}: {}",
                        line.width(),
                        plain(vec![line])
                    );
                }
            }
        }
    }
}

#[test]
fn many_row_navigation_preserves_selection_across_banner_height_boundary() {
    let mut app = empty();
    app.all_records = (0..40)
        .map(|i| record(&format!("Person {i:02}"), "engine", 100 - i))
        .collect();
    populate(&mut app);
    for width in [80, 144] {
        for height in [29, 30, 29] {
            draw(&mut app, width, height);
            for _ in 0..50 {
                ch(&mut app, 'j');
            }
            let selected = app.selected_id().unwrap();
            let screen = draw(&mut app, width, height);
            assert_eq!(app.selected, 39);
            assert_eq!(app.selected_id().unwrap(), selected);
            assert!(
                screen
                    .lines()
                    .any(|line| line.contains('▸') && line.contains("Person 39")),
                "{width}x{height}:\n{screen}"
            );
            assert!(screen.contains("LANDED"), "{width}x{height}:\n{screen}");
            for _ in 0..50 {
                ch(&mut app, 'k');
            }
            let screen = draw(&mut app, width, height);
            assert_eq!(app.selected, 0);
            assert!(
                screen
                    .lines()
                    .any(|line| line.contains('▸') && line.contains("Person 00")),
                "{width}x{height}:\n{screen}"
            );
        }
    }
}

fn long_contributor_detail() -> App {
    let mut app = empty();
    app.loaded_repos = (0..20).map(|i| repo(&format!("project-{i:02}"))).collect();
    app.all_records = (0..20)
        .map(|i| {
            let mut entry = record("Ada Lovelace", &format!("project-{i:02}"), 100 + i);
            entry.lines_known = i != 0;
            entry
        })
        .collect();
    for offset in 0..11 {
        let month = 7 + offset;
        let (year, month) = if month > 12 {
            (2026, month - 12)
        } else {
            (2025, month)
        };
        let mut entry = record("Ada Lovelace", "project-00", 200 + offset as i64);
        entry.date = Utc
            .with_ymd_and_hms(year, month, 1, 12, 0, 0)
            .unwrap()
            .fixed_offset();
        app.all_records.push(entry);
    }
    app.all_records
        .push(record("Grace Hopper", "project-00", 1));
    app.failed_repos = vec!["broken".into()];
    app.warnings = vec![(
        "/repos/project-00".into(),
        "per-commit diagnostic must not be rendered".into(),
    )];
    populate(&mut app);
    assert_eq!(app.authors[0].per_repo.len(), 20);
    assert_eq!(app.authors[0].monthly.len(), 12);
    app
}

fn assert_detail_chrome(app: &App, screen: &str, width: u16, height: u16) {
    let header = screen.lines().take(6).collect::<Vec<_>>().join("\n");
    for label in ["CONTRIBUTOR:", "ADA LOVELACE", "LANDED", "UTC", "▐ALL▌"] {
        assert!(header.contains(label), "missing fixed {label}:\n{screen}");
    }
    let normalized = screen.split_whitespace().collect::<Vec<_>>().join(" ");
    let lower = normalized.to_lowercase();
    for label in [
        "? incomplete line counts",
        "1 unreadable",
        "pgup",
        "pgdn",
        "home",
        "end",
        "merge",
        "history",
        "back",
        "quit",
    ] {
        assert!(lower.contains(label), "missing fixed {label}:\n{screen}");
    }
    assert!(!lower.contains("warning"), "{screen}");
    assert!(!lower.contains("per-commit diagnostic"), "{screen}");
    let lines = app.detail_lines();
    assert!(lines.len() <= usize::from(height), "{screen}");
    for line in lines {
        assert!(
            line.width() <= usize::from(width),
            "detail line exceeds {width} columns: {}",
            plain(vec![line])
        );
    }
}

#[test]
fn detail_paging_reaches_every_repository_month_and_section_with_fixed_context() {
    for (width, height) in [(80, 24), (144, 50)] {
        let mut app = long_contributor_detail();
        draw(&mut app, width, height);
        key(&mut app, KeyCode::Enter);
        let mut pages = Vec::new();
        let selected = app.active_id.clone();
        for _ in 0..100 {
            let screen = draw(&mut app, width, height);
            assert_detail_chrome(&app, &screen, width, height);
            assert_eq!(app.active_id, selected);
            pages.push(screen);
            let before = app.detail_offset;
            key(&mut app, KeyCode::PageDown);
            if app.detail_offset == before {
                break;
            }
        }
        assert!(pages.len() > 1, "fixture must exceed {width}x{height}");
        assert!(pages.len() < 100, "detail paging did not reach the end");
        let all_pages = pages.join("\n");
        for label in [
            "REPO CONTRIBUTIONS",
            "ACTIVITY TIMELINE",
            "ACTIVITY MATRIX",
            "Removed/added ratio:",
            "Repository associations can overlap",
        ] {
            assert!(all_pages.contains(label), "missing {label}:\n{all_pages}");
        }
        for i in 0..20 {
            let label = format!("project-{i:02}");
            assert!(all_pages.contains(&label), "missing {label}:\n{all_pages}");
        }
        for label in [
            "Jul 2025", "Aug 2025", "Sep 2025", "Oct 2025", "Nov 2025", "Dec 2025", "Jan 2026",
            "Feb 2026", "Mar 2026", "Apr 2026", "May 2026", "Jun 2026",
        ] {
            assert!(all_pages.contains(label), "missing {label}:\n{all_pages}");
        }
        key(&mut app, KeyCode::Home);
        let first = draw(&mut app, width, height);
        assert_eq!(app.detail_offset, 0);
        assert_eq!(first, pages[0], "Home must restore the original first page");
    }
}

#[test]
fn detail_home_end_page_bounds_and_resize_keep_header_visible() {
    let mut app = long_contributor_detail();
    draw(&mut app, 80, 24);
    key(&mut app, KeyCode::Enter);
    key(&mut app, KeyCode::End);
    let last = draw(&mut app, 80, 24);
    assert_detail_chrome(&app, &last, 80, 24);
    assert!(last.contains("more (Lines changed)"), "{last}");
    let bottom = app.detail_offset;
    assert!(bottom > 0);
    for code in [KeyCode::End, KeyCode::PageDown, KeyCode::PageDown] {
        key(&mut app, code);
        assert_eq!(app.detail_offset, bottom);
    }
    key(&mut app, KeyCode::PageUp);
    assert!(app.detail_offset < bottom);
    key(&mut app, KeyCode::Home);
    assert_eq!(app.detail_offset, 0);
    key(&mut app, KeyCode::PageUp);
    assert_eq!(app.detail_offset, 0);
    key(&mut app, KeyCode::End);
    let wider = draw(&mut app, 144, 50);
    assert_detail_chrome(&app, &wider, 144, 50);
    let resized_bottom = app.detail_offset;
    assert!(resized_bottom < bottom);
    key(&mut app, KeyCode::End);
    assert_eq!(
        app.detail_offset, resized_bottom,
        "resize must clamp to the new end"
    );
    let expanded = draw(&mut app, 144, 120);
    assert_detail_chrome(&app, &expanded, 144, 120);
    assert_eq!(app.detail_offset, 0, "all content fits after expansion");
    assert!(expanded.contains("REPO CONTRIBUTIONS"));
    assert!(expanded.contains("ACTIVITY MATRIX"));
}

#[test]
fn detail_navigation_resets_viewport_without_changing_existing_controls() {
    let mut app = long_contributor_detail();
    draw(&mut app, 80, 24);
    key(&mut app, KeyCode::Enter);
    let ada = app.active_id.clone();
    key(&mut app, KeyCode::End);
    assert!(app.detail_offset > 0);
    key(&mut app, KeyCode::Down);
    assert_eq!(app.detail_offset, 0);
    assert_ne!(app.active_id, ada);
    assert!(draw(&mut app, 80, 24).contains("GRACE HOPPER"));
    key(&mut app, KeyCode::Up);
    assert_eq!(app.active_id, ada);
    assert_eq!(app.detail_offset, 0);

    for code in [KeyCode::Left, KeyCode::Right] {
        key(&mut app, KeyCode::End);
        assert!(app.detail_offset > 0);
        let time = app.time_index;
        key(&mut app, code);
        assert_ne!(app.time_index, time);
        assert_eq!(app.detail_offset, 0);
        assert_eq!(app.active_id, ada);
    }
    for expected in [HistoryScope::AllBranches, HistoryScope::Landed] {
        key(&mut app, KeyCode::End);
        assert!(app.detail_offset > 0);
        ch(&mut app, 'B');
        assert_eq!(app.scope, expected);
        assert_eq!(app.detail_offset, 0);
        assert_eq!(app.active_id, ada);
    }
    key(&mut app, KeyCode::End);
    ch(&mut app, 'M');
    assert!(
        app.merge.is_some(),
        "merge remains available while detail is scrolled"
    );
    key(&mut app, KeyCode::Esc);
    assert!(app.merge.is_none());
    assert_eq!(app.active_id, ada);
    key(&mut app, KeyCode::End);
    assert!(app.detail_offset > 0);
    key(&mut app, KeyCode::Esc);
    assert_eq!(app.view, View::Aggregate);
    key(&mut app, KeyCode::Enter);
    assert_eq!(app.view, View::Operative);
    assert_eq!(app.detail_offset, 0);
    assert_eq!(app.active_id, ada);
    let screen = draw(&mut app, 80, 24);
    assert_detail_chrome(&app, &screen, 80, 24);
}

#[test]
fn github_range_loads_new_window_and_history_stays_on_default_branch() {
    let mut app = populated();
    app.github_source = true;
    app.repositories = app.loaded_repos.clone();
    app.time_index = 2;
    assert_eq!(ch(&mut app, 'B'), Action::None);
    assert_eq!(app.scope, HistoryScope::Landed);
    assert_eq!(key(&mut app, KeyCode::Right), Action::Refresh);
    assert_eq!(app.time_index, 3);
    assert!(app.loading);
    let mut local = populated();
    local.time_index = 2;
    assert_eq!(key(&mut local, KeyCode::Right), Action::None);
    assert!(!local.loading);
}

#[test]
fn github_board_labels_api_basis_and_omits_local_history_control() {
    let mut app = populated();
    app.github_source = true;
    for (width, height) in [(60, 24), (80, 24), (140, 40)] {
        let screen = draw(&mut app, width, height);
        assert!(screen.contains("GITHUB API"), "{screen}");
        assert!(screen.contains("Commit date · all files"), "{screen}");
        assert!(!screen.contains("history"), "{screen}");
        key(&mut app, KeyCode::Enter);
        let detail = draw(&mut app, width, height);
        assert!(detail.contains("Commit date · all files"), "{detail}");
        key(&mut app, KeyCode::Esc);
    }
}
