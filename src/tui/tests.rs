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
        assert_eq!(app.filtered_records().len(), 1);
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
    ch(&mut app, 'j');
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
    let text = plain(app.detail_lines());
    assert!(text.contains("America/Denver"));
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
    let detail = plain(app.detail_lines());
    assert!(detail.contains("Coauthored participation 1"));
    assert!(detail.contains("unallocated"));
    assert!(detail.contains('○'));
    assert!(detail.contains("Removed/added ratio: N/A"));
}
#[test]
fn incomplete_scan_warnings_visible_in_board_detail_and_repository() {
    let mut app = populated();
    app.failed_repos = vec!["broken".into()];
    app.warnings = vec![("/repos/engine".into(), "engine: shallow history".into())];
    let screen = draw(&mut app, 100, 28);
    assert!(screen.contains("Partial history"));
    assert!(screen.contains("shallow history"));
    key(&mut app, KeyCode::Enter);
    let screen = draw(&mut app, 80, 24);
    assert!(screen.contains("Partial history"));
    assert!(screen.contains("shallow history"));
    assert!(screen.contains("LANDED"));
    key(&mut app, KeyCode::Esc);
    ch(&mut app, 'r');
    ch(&mut app, 'j');
    assert!(plain(app.lines()).contains("shallow history"));
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
    assert!(plain(app.detail_lines()).contains("Repository associations can overlap"));
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
        assert!(screen.contains("Person 39"), "{w}x{h}:\n{screen}");
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
    assert!(plain(app.lines()).contains("shallow history"));
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
fn attribution_conflicts_are_visible_and_change_with_repository_filters() {
    let mut app = populated();
    let mut duplicate = app.all_records[0].clone();
    duplicate.repo_id = "/repos/compiler".into();
    duplicate.repo_name = "compiler".into();
    duplicate.author = "Different Person".into();
    duplicate.email = "different@example.com".into();
    app.all_records.push(duplicate);
    populate(&mut app);
    assert!(!app.attribution_warnings.is_empty());
    let warning = app.attribution_warnings[0].clone();
    assert!(plain(app.lines()).contains(&truncate(&format!("  ⚠ {warning}"), app.width as usize)));
    ch(&mut app, 'r');
    assert!(plain(app.lines()).contains("Attribution warnings for the current filters"));
    key(&mut app, KeyCode::Esc);
    app.excluded.insert("/repos/compiler".into());
    app.recompute();
    assert!(app.attribution_warnings.is_empty());
}

#[test]
fn unknown_headers_are_qualified_and_detected_ai_remains_in_detail_breakdowns() {
    let mut app = empty();
    let mut unknown = record("Ada", "engine", 0);
    unknown.lines_known = false;
    app.all_records = vec![unknown];
    populate(&mut app);
    let overview = plain(app.lines());
    assert!(overview.contains("Known line subtotal: added ? · removed ?"));
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
    let detail = plain(app.detail_lines());
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
    assert_eq!(app.attribution_warnings.len(), 1);
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
    assert!(app.attribution_warnings.is_empty());
    assert_eq!(app.contributors.len(), 1);
    assert_eq!(app.authors.len(), 1);
    assert_eq!(app.authors[0].name, "Resolved Person");
    assert_eq!(app.totals(), (1, 100, 10, 0));
}
