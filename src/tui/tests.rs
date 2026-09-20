use super::components::*;
use super::render::{aggregate_months, author_records, heatmap};
use super::*;
use chrono::{TimeZone, Utc};
use ratatui::{backend::TestBackend, text::Line};
use std::path::PathBuf;

fn repo(name: &str) -> Repository {
    Repository {
        id: format!("/repos/{name}"),
        path: PathBuf::from(format!("/repos/{name}")),
        name: name.into(),
    }
}
fn record(name: &str, repo_name: &str, added: i64) -> CommitRecord {
    CommitRecord {
        author: name.into(),
        email: format!("{name}@example.com"),
        date: Utc::now().fixed_offset(),
        added,
        removed: added / 10,
        repo_id: format!("/repos/{repo_name}"),
        repo_name: repo_name.into(),
        ai_assisted: false,
    }
}
fn populated() -> App {
    let mut app = App::new(
        vec![],
        SortField::Total,
        HashSet::new(),
        "test",
        6,
        AnalysisOptions::default(),
        "dark",
    );
    app.width = 100;
    app.height = 40;
    app.loaded_repos = vec![repo("compiler"), repo("engine")];
    app.all_records = vec![
        record("Ada Lovelace", "engine", 100),
        record("Grace Hopper", "compiler", 50),
    ];
    app.recompute();
    app
}
fn key(app: &mut App, code: KeyCode) -> Action {
    app.key(KeyEvent::new(code, KeyModifiers::NONE))
}
fn ch(app: &mut App, c: char) -> Action {
    key(app, KeyCode::Char(c))
}
fn plain(lines: Vec<UiLine>) -> String {
    lines
        .iter()
        .map(|line| {
            line.spans
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
    let buffer = terminal.backend().buffer();
    (0..height)
        .map(|y| {
            (0..width)
                .map(|x| buffer[(x, y)].symbol())
                .collect::<String>()
        })
        .collect::<Vec<_>>()
        .join("\n")
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
    assert_eq!(app.filter_query, "grace");
    assert_eq!(ch(&mut app, 'q'), Action::None);
    assert!(app.filter_query.is_empty());
    ch(&mut app, '/');
    ch(&mut app, '日');
    ch(&mut app, '本');
    key(&mut app, KeyCode::Backspace);
    assert_eq!(app.filter_query, "日");
    key(&mut app, KeyCode::Esc);
    assert!(!app.searching);
    assert!(app.filter_query.is_empty());
    assert_eq!(key(&mut app, KeyCode::Esc), Action::Quit);
}

#[test]
fn sort_cycle_reverse_and_metric_ranks() {
    let mut app = populated();
    assert_eq!(app.authors[0].name, "Ada Lovelace");
    ch(&mut app, 'S');
    assert!(app.sort_ascending);
    assert_eq!(app.authors[0].name, "Grace Hopper");
    let table = plain(app.table_lines());
    assert!(
        table
            .lines()
            .any(|l| l.contains("Grace Hopper") && l.contains("02"))
    );
    assert!(
        table
            .lines()
            .any(|l| l.contains("Ada Lovelace") && l.contains("01"))
    );
    ch(&mut app, 's');
    assert_eq!(app.sort_field, SortField::Commits);
    for _ in 0..5 {
        ch(&mut app, 's');
    }
    assert_eq!(app.sort_field, SortField::Total);
}

#[test]
fn repository_changes_apply_on_both_escape_and_enter_and_gate_other_keys() {
    for finish in [KeyCode::Esc, KeyCode::Enter] {
        let mut app = populated();
        ch(&mut app, 'r');
        assert_eq!(app.view, View::Repositories);
        ch(&mut app, ' ');
        assert!(app.excluded.is_empty());
        assert!(app.overlay_excluded.contains("/repos/compiler"));
        let time = app.time_index;
        key(&mut app, KeyCode::Left);
        ch(&mut app, 'R');
        ch(&mut app, 's');
        ch(&mut app, 'b');
        assert_eq!(app.time_index, time);
        assert!(!app.loading);
        assert!(!app.hide_bots);
        key(&mut app, finish);
        assert_eq!(app.view, View::Aggregate);
        assert!(app.excluded.contains("/repos/compiler"));
        assert_eq!(app.authors.len(), 1);
        assert_eq!(app.authors[0].name, "Ada Lovelace");
        assert_eq!(app.filtered_records().len(), 1);
    }
}

#[test]
fn exclusions_normalize_names_and_basename_to_repository_ids() {
    let repositories = vec![
        Repository {
            id: "/x/foo".into(),
            path: "/x/foo".into(),
            name: "x/foo".into(),
        },
        Repository {
            id: "/y/foo".into(),
            path: "/y/foo".into(),
            name: "y/foo".into(),
        },
    ];
    let app = App::new(
        repositories,
        SortField::Total,
        HashSet::from(["foo".into()]),
        "",
        99,
        AnalysisOptions::default(),
        "dark",
    );
    assert_eq!(
        app.excluded,
        HashSet::from(["/x/foo".into(), "/y/foo".into()])
    );
    assert_eq!(app.time_index, DEFAULT_TIME_INDEX);
}

#[test]
fn detail_navigation_and_disappearing_names_retain_go_selection_behavior() {
    let mut app = populated();
    key(&mut app, KeyCode::Enter);
    assert_eq!(app.view, View::Operative);
    assert_eq!(app.active_operative, "Ada Lovelace");
    ch(&mut app, 'j');
    assert_eq!(app.active_operative, "Grace Hopper");
    ch(&mut app, 'k');
    assert_eq!(app.active_operative, "Ada Lovelace");
    app.active_operative = "Vanished".into();
    app.selected = 1;
    ch(&mut app, 'j');
    assert_eq!(app.active_operative, "Grace Hopper");
    app.active_operative = "Vanished".into();
    assert!(plain(app.detail_lines()).contains("NO SIGNAL"));
    key(&mut app, KeyCode::Esc);
    assert_eq!(app.view, View::Aggregate);
}

#[test]
fn time_filter_changes_clamp_selection_and_work_in_details() {
    let mut app = populated();
    app.all_records[1].date -= Duration::days(100);
    app.recompute();
    ch(&mut app, 'j');
    assert_eq!(app.selected, 1);
    app.time_index = 2;
    key(&mut app, KeyCode::Left);
    assert_eq!(app.time_index, 1);
    assert_eq!(app.authors.len(), 1);
    assert_eq!(app.selected, 0);
    key(&mut app, KeyCode::Enter);
    key(&mut app, KeyCode::Right);
    assert_eq!(app.time_index, 2);
    assert_eq!(app.view, View::Operative);
}

#[test]
fn all_contributors_remain_reachable_after_resize() {
    let mut app = populated();
    app.all_records = (0..40)
        .map(|i| record(&format!("Person {i:02}"), "engine", 100 - i))
        .collect();
    app.recompute();
    for (w, h) in [(120, 40), (100, 24), (80, 24), (60, 20), (40, 12)] {
        draw(&mut app, w, h);
        for _ in 0..50 {
            ch(&mut app, 'j');
        }
        let screen = draw(&mut app, w, h);
        assert_eq!(app.selected, 39, "{w}x{h}");
        assert!(app.selected >= app.offset && app.selected < app.offset + app.table_viewport());
        assert!(
            screen.contains("Person 39"),
            "last contributor invisible at {w}x{h}:\n{screen}"
        );
        for _ in 0..50 {
            ch(&mut app, 'k');
        }
        assert_eq!(app.selected, 0);
    }
}

#[test]
fn table_tiers_ai_percentage_and_bot_badge() {
    let mut app = populated();
    app.all_records
        .push(record("dependabot[bot]", "engine", 200));
    app.recompute();
    app.authors[0].commits = 300;
    app.authors[0].ai_commits = 1;
    for width in [60, 80, 120] {
        app.width = width;
        let table = plain(app.table_lines());
        assert!(table.contains("CONTRIBUTOR"));
        assert!(table.contains("NET"));
        assert!(table.contains("AI%"));
        assert!(table.contains("BOT"));
        assert!(table.contains("<1%"));
        assert_eq!(table.contains("ADDED"), width >= 96);
        assert_eq!(table.contains("IMPACT"), width >= 78);
    }
    ch(&mut app, 'b');
    assert_eq!(app.authors.len(), 2);
    ch(&mut app, 'b');
    assert_eq!(app.authors.len(), 3);
}

#[test]
fn streaming_load_partial_failure_refresh_and_total_failure() {
    let repos = vec![repo("engine"), repo("broken")];
    let mut app = App::new(
        repos.clone(),
        SortField::Total,
        HashSet::new(),
        "test",
        6,
        AnalysisOptions::default(),
        "dark",
    );
    assert!(plain(app.lines()).contains("0/2 repos"));
    app.loaded(ScanResult {
        repository: repos[0].clone(),
        records: vec![record("Ada", "engine", 100)],
        error: None,
    });
    assert!(app.loading);
    assert!(app.authors.is_empty());
    assert!(plain(app.lines()).contains("1/2 repos"));
    app.loaded(ScanResult {
        repository: repos[1].clone(),
        records: vec![],
        error: Some("unreadable".into()),
    });
    assert!(!app.loading);
    assert_eq!(app.authors.len(), 1);
    assert!(app.error.is_none());
    assert!(draw(&mut app, 100, 30).contains("broken"));
    assert_eq!(ch(&mut app, 'R'), Action::Refresh);
    assert!(app.loading);
    assert_eq!(
        app.authors.len(),
        1,
        "refresh retains old data until all results arrive"
    );
    for r in repos {
        app.loaded(ScanResult {
            repository: r,
            records: vec![],
            error: Some("failed".into()),
        });
    }
    assert!(!app.loading);
    assert!(app.authors.is_empty());
    assert_eq!(
        app.error.as_deref(),
        Some("all 2 repositories failed to scan")
    );
    assert!(plain(app.lines()).contains("ERROR"));
}

#[test]
fn quit_works_during_loading_search_and_every_view() {
    for view in [View::Aggregate, View::Operative, View::Repositories] {
        let mut app = populated();
        app.view = view;
        app.loading = true;
        assert_eq!(ch(&mut app, 'q'), Action::Quit);
        app.searching = true;
        assert_eq!(
            app.key(KeyEvent::new(KeyCode::Char('c'), KeyModifiers::CONTROL)),
            Action::Quit
        );
    }
    let mut app = populated();
    let mut released = KeyEvent::new(KeyCode::Char('q'), KeyModifiers::NONE);
    released.kind = KeyEventKind::Release;
    assert_eq!(app.key(released), Action::None);
}

#[test]
fn long_repository_overlay_keeps_selected_checkbox_visible() {
    let mut app = populated();
    app.loaded_repos = (0..50).map(|i| repo(&format!("repo-{i:02}"))).collect();
    ch(&mut app, 'r');
    for _ in 0..60 {
        ch(&mut app, 'j');
    }
    let screen = draw(&mut app, 60, 20);
    assert!(screen.contains("repo-49"));
    ch(&mut app, ' ');
    key(&mut app, KeyCode::Esc);
    assert!(app.excluded.contains("/repos/repo-49"));
}

#[test]
fn detail_has_all_metrics_repositories_month_gaps_and_heatmap() {
    let mut app = populated();
    app.height = 80;
    let mut jan = record("Ada Lovelace", "engine", 100);
    jan.date = Utc
        .with_ymd_and_hms(2025, 1, 15, 12, 0, 0)
        .unwrap()
        .fixed_offset();
    let mut apr = jan.clone();
    apr.date = Utc
        .with_ymd_and_hms(2025, 4, 15, 12, 0, 0)
        .unwrap()
        .fixed_offset();
    apr.ai_assisted = true;
    app.all_records = vec![jan, apr];
    app.recompute();
    key(&mut app, KeyCode::Enter);
    let screen = draw(&mut app, 120, 80);
    for text in [
        "CONTRIBUTOR: ADA LOVELACE",
        "ACTIVE 2 days",
        "CHURN 0.10",
        "AI 50%",
        "REPO CONTRIBUTIONS",
        "ACTIVITY TIMELINE",
        "Feb 2025",
        "Mar 2025",
        "ACTIVITY MATRIX",
        "Sun",
        "Sat",
        "less",
        "more",
    ] {
        assert!(screen.contains(text), "missing {text}:\n{screen}");
    }
    let records: Vec<_> = app.all_records.iter().collect();
    let months = aggregate_months(&records);
    assert_eq!(months.len(), 4);
    assert_eq!(months[1].commits, 0);
    assert_eq!(months[2].commits, 0);
    assert_eq!(months[3].commits, 1);
    assert_eq!(months[3].ai, 1);
}

#[test]
fn detail_filters_by_exact_aliases_or_explicit_fuzzy_policy() {
    let records = vec![
        record("Alice S", "engine", 10),
        record("unrelated", "engine", 20),
    ];
    assert_eq!(author_records(&records, None, "Alice Smith", true).len(), 1);
    assert_eq!(
        author_records(&records, None, "Alice Smith", false).len(),
        0
    );
    let mut author = AuthorStats::default();
    author.aliases.insert("unrelated".into());
    assert_eq!(
        author_records(&records, Some(&author), "Alice Smith", true)[0].author,
        "unrelated"
    );
}

#[test]
fn heatmap_keeps_original_local_calendar_and_intensity() {
    let now = Utc.with_ymd_and_hms(2026, 6, 2, 12, 0, 0).unwrap();
    let mut hot = record("A", "engine", 100);
    hot.date = (now - Duration::days(1)).fixed_offset();
    let mut cold = record("A", "engine", 5);
    cold.date = (now - Duration::days(8)).fixed_offset();
    let text = plain(heatmap(
        &[&hot, &cold],
        100,
        now,
        &Palette::for_theme("dark"),
    ));
    assert!(text.contains('█'));
    assert!(text.contains('░'));
    assert!(text.contains("Sun"));
    assert!(text.contains("Sat"));
    assert_eq!(text.lines().count(), 9);
}

#[test]
fn text_safety_width_number_format_and_gradients() {
    assert_eq!(format_number(i64::MIN), "-9,223,372,036,854,775,808");
    assert_eq!(format_number(1_234_567), "1,234,567");
    let hostile = "\u{1b}]52;c;Y2xpcGJvYXJk\u{7}Alice\n\u{1b}[31mRed\u{1b}[0m\u{7f}";
    assert_eq!(display_text(hostile), "Alice Red ");
    assert_eq!(truncate("hello world foo", 10), "hello w...");
    assert_eq!(truncate("日本語", 2), "日");
    assert_eq!(truncate("日本語", 0), "");
    for width in 0..15 {
        assert!(
            unicode_width::UnicodeWidthStr::width(truncate("日本語テスト名前", width).as_str())
                <= width
        );
    }
    let p = Palette::for_theme("dark");
    assert_eq!(
        plain(vec![Line::from(impact_bar(1, 0, 1_000_000, 20, &p))])
            .chars()
            .filter(|c| *c == '█')
            .count(),
        1
    );
    let gradient = plain(vec![Line::from(impact_bar(80, 20, 100, 20, &p))]);
    assert!(gradient.contains("▓▒░"));
    assert_eq!(gradient.chars().count(), 20);
    assert_ne!(p.cyan, Palette::for_theme("light").cyan);
}

#[test]
fn stat_boxes_fit_supported_widths_and_show_small_ai_shares() {
    let p = Palette::for_theme("dark");
    for width in [40, 50, 60, 78, 80, 96, 120] {
        let lines = stat_boxes((1_234_567, 9_876_543, 1_234_567, 4321), width, false, &p);
        assert!(
            lines.iter().all(|l| l.width() <= width),
            "overflow at width {width}"
        );
    }
    let stats = plain(stat_boxes((1000, 0, 0, 3), 120, false, &p));
    assert!(stats.contains("<1% (3)"));
}

#[test]
fn every_view_renders_at_tiny_and_large_terminal_sizes() {
    let mut app = populated();
    for (width, height) in [(1, 1), (10, 3), (40, 12), (80, 24), (120, 80)] {
        for view in [View::Aggregate, View::Operative, View::Repositories] {
            app.view = view;
            app.active_operative = "Ada Lovelace".into();
            draw(&mut app, width, height);
        }
        app.loading = true;
        draw(&mut app, width, height);
        app.loading = false;
        app.error = Some("failure".into());
        draw(&mut app, width, height);
        app.error = None;
    }
}

#[test]
fn oversized_detail_preserves_go_tail_clipping_and_visible_help() {
    let mut app = populated();
    key(&mut app, KeyCode::Enter);
    let screen = draw(&mut app, 80, 24);
    assert!(app.detail_lines().len() > 24);
    assert!(screen.contains("ACTIVITY MATRIX"), "{screen}");
    assert!(screen.contains("Sun") && screen.contains("Sat"), "{screen}");
    assert!(screen.lines().last().unwrap().contains("quit"), "{screen}");
    let expected = plain(
        app.detail_lines()
            .into_iter()
            .rev()
            .take(24)
            .collect::<Vec<_>>()
            .into_iter()
            .rev()
            .collect(),
    );
    for (actual, expected) in screen.lines().zip(expected.lines()) {
        let expected = expected.chars().take(80).collect::<String>();
        assert_eq!(actual.trim_end(), expected.trim_end());
    }
}
