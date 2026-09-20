//! Bounded repository selection with pageable paths and scan failures.
use super::App;
use super::components::*;
use super::merge::{wrap_help, wrapped};
use ratatui::text::Line;

struct RepositoryChoice<'a> {
    id: &'a str,
    name: &'a str,
    path: Option<&'a std::path::Path>,
    failed: bool,
}

struct RepositoryLayout {
    list_rows: usize,
    detail_rows: usize,
    help: Vec<UiLine>,
}

impl App {
    fn repository_choices(&self) -> Vec<RepositoryChoice<'_>> {
        let mut choices: Vec<_> = self
            .loaded_repos
            .iter()
            .map(|repo| RepositoryChoice {
                id: &repo.id,
                name: &repo.name,
                path: Some(&repo.path),
                failed: false,
            })
            .collect();
        for name in &self.failed_repos {
            // Names are presentation labels; only a recorded failure ID can
            // identify its error when multiple repositories share a label.
            let eligible = |repo: &&crate::model::Repository| {
                &repo.name == name && !choices.iter().any(|choice| choice.id == repo.id)
            };
            let repo = self
                .repositories
                .iter()
                .filter(eligible)
                .find(|repo| self.failure_details.iter().any(|(id, _)| id == &repo.id))
                .or_else(|| {
                    // Legacy/incomplete scan state may have only a failed name.
                    // Show a path only when the remaining candidate is unambiguous.
                    let mut remaining = self.repositories.iter().filter(eligible);
                    let only = remaining.next()?;
                    remaining.next().is_none().then_some(only)
                });
            choices.push(RepositoryChoice {
                id: repo.map_or(name.as_str(), |repo| &repo.id),
                name,
                path: repo.map(|repo| repo.path.as_path()),
                failed: true,
            });
        }
        choices
    }

    fn repository_layout(&self) -> RepositoryLayout {
        let help = wrap_help(
            &[
                ("↑↓/jk", "repo".into()),
                ("space", "toggle".into()),
                ("PgUp/PgDn", "details".into()),
                ("enter/esc", "apply".into()),
                ("q", "quit".into()),
            ],
            self.width as usize,
            &self.palette,
        );
        // Preserve controls and at least one repository row. Healthy nodes
        // need only their path; return unused detail space to the list.
        let available = (self.height as usize).saturating_sub(help.len() + 5);
        let choices = self.repository_choices();
        let failed = choices
            .get(self.overlay_cursor)
            .is_some_and(|repo| repo.failed);
        let detail_limit = if failed {
            available.saturating_sub((available / 3).clamp(1, 8))
        } else {
            (available / 3).max(1).min(available.saturating_sub(1))
        };
        let detail_rows = self.repository_details().len().min(detail_limit);
        RepositoryLayout {
            list_rows: available
                .saturating_sub(detail_rows)
                .min(choices.len().max(1)),
            detail_rows,
            help,
        }
    }

    fn repository_details(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = (self.width as usize).saturating_sub(4).max(1);
        let choices = self.repository_choices();
        let Some(repo) = choices.get(self.overlay_cursor) else {
            return wrapped("No repositories available.", width, p.dim_white);
        };
        let mut lines = if let Some(path) = repo.path {
            wrapped(&path.to_string_lossy(), width, p.dim_white)
        } else {
            wrapped("Repository path unavailable.", width, p.dim_white)
        };
        if repo.failed {
            let failure = self.failure_details.iter().find(|(id, _)| id == repo.id);
            let detail = failure.map_or(
                "Scan failed; this repository is absent from the totals.",
                |(_, error)| error.as_str(),
            );
            lines.extend(wrapped(&format!("SCAN FAILED // {detail}"), width, p.red));
        } else if self.repository_history_unavailable(repo.id) {
            lines.extend(wrapped(
                "Default branch unavailable locally; use B on the board for all branches.",
                width,
                p.amber,
            ));
        }
        lines
    }

    pub(super) fn step_repository(&mut self, delta: isize) {
        let next = self
            .overlay_cursor
            .saturating_add_signed(delta)
            .min(self.repository_choices().len().saturating_sub(1));
        if next != self.overlay_cursor {
            self.overlay_cursor = next;
            self.repository_diagnostic_offset = 0;
        }
    }

    pub(super) fn toggle_repository(&mut self) {
        let id = self
            .repository_choices()
            .get(self.overlay_cursor)
            .filter(|repo| !repo.failed)
            .map(|repo| repo.id.to_owned());
        if let Some(id) = id
            && !self.overlay_excluded.remove(&id)
        {
            self.overlay_excluded.insert(id);
        }
    }

    pub(super) fn page_repository_diagnostics(&mut self, forward: bool) {
        let rows = self.repository_layout().detail_rows.max(1);
        let last = self.repository_details().len().saturating_sub(rows);
        self.repository_diagnostic_offset = if forward {
            self.repository_diagnostic_offset
                .saturating_add(rows)
                .min(last)
        } else {
            self.repository_diagnostic_offset
                .min(last)
                .saturating_sub(rows)
        };
    }

    pub(super) fn repository_lines(&self) -> Vec<UiLine> {
        let width = self.width as usize;
        let height = self.height as usize;
        let p = &self.palette;
        let layout = self.repository_layout();
        let choices = self.repository_choices();
        let active = self
            .loaded_repos
            .iter()
            .filter(|repo| !self.overlay_excluded.contains(&repo.id))
            .count();
        let mut lines = vec![panel_header("REPOSITORY MATRIX", width, p)];
        lines.push(panel_row(
            Line::from(vec![
                bold(
                    format!("{active}/{} ACTIVE", self.loaded_repos.len()),
                    p.cyan,
                ),
                span(
                    format!("  //  {} FAILED", self.failed_repos.len()),
                    if self.failed_repos.is_empty() {
                        p.dim_white
                    } else {
                        p.red
                    },
                ),
            ]),
            width,
            p,
        ));
        let start = self
            .overlay_cursor
            .saturating_add(1)
            .saturating_sub(layout.list_rows);
        for index in start..start + layout.list_rows {
            let content = if let Some(repo) = choices.get(index) {
                let selected = index == self.overlay_cursor;
                let excluded = self.overlay_excluded.contains(repo.id);
                let color = if repo.failed {
                    p.red
                } else if excluded {
                    p.dim_white
                } else {
                    p.cyan
                };
                Line::from(vec![
                    bold(if selected { "▸ " } else { "  " }, p.magenta),
                    span(
                        if repo.failed {
                            "[!] "
                        } else if excluded {
                            "[ ] "
                        } else {
                            "[x] "
                        },
                        color,
                    ),
                    span(display_text(repo.name), color),
                ])
                .style(p.row(selected, index))
            } else if choices.is_empty() && index == 0 {
                text_line("No repositories", p.dim_white)
            } else {
                blank()
            };
            lines.push(panel_row(content, width, p));
        }
        lines.push(panel_header("REPOSITORY DETAILS", width, p));
        let details = self.repository_details();
        let last = details.len().saturating_sub(layout.detail_rows);
        let offset = self.repository_diagnostic_offset.min(last);
        for index in offset..offset + layout.detail_rows {
            lines.push(panel_row(
                details.get(index).cloned().unwrap_or_else(blank),
                width,
                p,
            ));
        }
        let end = (offset + layout.detail_rows).min(details.len());
        let pager = if layout.detail_rows == 0 {
            "Resize to read details".into()
        } else {
            format!(
                "LINES {}–{} / {}  // PgUp PgDn",
                offset + 1,
                end,
                details.len()
            )
        };
        lines.push(panel_row(text_line(pager, p.magenta), width, p));
        lines.push(panel_footer(width, p));
        lines.extend(layout.help);
        lines.truncate(height);
        lines
    }
}

#[cfg(test)]
mod tests {
    use super::super::{Action, View};
    use super::*;
    use crate::{
        identity::IdentityStore,
        model::{AnalysisOptions, Repository},
        stats::SortField,
    };
    use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
    use std::collections::HashSet;

    fn repo(index: usize) -> Repository {
        Repository {
            id: format!("/repos/node-{index:02}"),
            name: format!("node-{index:02}"),
            path: format!("/repos/node-{index:02}").into(),
        }
    }
    fn app(width: u16, height: u16) -> App {
        let mut app = App::new(
            vec![],
            SortField::Total,
            HashSet::new(),
            "test",
            6,
            AnalysisOptions::default(),
            "dark",
            IdentityStore::default(),
            "/unused/identities.json".into(),
        );
        app.width = width;
        app.height = height;
        app.loaded_repos = (0..40).map(repo).collect();
        app.repositories = app.loaded_repos.clone();
        app.view = View::Repositories;
        app
    }
    fn text(lines: &[UiLine]) -> String {
        lines
            .iter()
            .map(|line| {
                line.spans
                    .iter()
                    .map(|span| span.content.as_ref())
                    .collect::<String>()
            })
            .collect::<Vec<_>>()
            .join("\n")
    }
    fn key(app: &mut App, code: KeyCode) -> Action {
        app.key(KeyEvent::new(code, KeyModifiers::NONE))
    }

    #[test]
    fn long_scan_errors_are_pageable_without_hiding_selection_or_help() {
        for (width, height) in [(40, 12), (60, 18), (80, 24), (140, 45)] {
            let mut app = app(width, height);
            app.loaded_repos.remove(0);
            app.failed_repos = vec!["node-00".into()];
            app.failure_details = vec![(
                "/repos/node-00".into(),
                (0..120)
                    .map(|index| format!("ERROR-{index:03}: unreadable object; "))
                    .collect(),
            )];
            app.overlay_cursor = app.loaded_repos.len();
            let expected: HashSet<_> = app
                .repository_details()
                .iter()
                .flat_map(|line| line.spans.iter())
                .filter(|span| span.style.fg == Some(app.palette.red))
                .map(|span| span.content.to_string())
                .collect();
            let mut seen = HashSet::new();
            loop {
                let lines = app.repository_lines();
                assert!(lines.len() <= height as usize);
                assert!(lines.iter().all(|line| line.width() <= width as usize));
                let screen = text(&lines);
                assert!(screen.contains("REPOSITORY MATRIX"));
                assert!(screen.contains("REPOSITORY DETAILS"));
                assert!(screen.contains("node-00"));
                assert!(screen.contains("PgUp/PgDn"));
                assert!(screen.contains("enter/esc"));
                seen.extend(
                    lines
                        .iter()
                        .flat_map(|line| line.spans.iter())
                        .filter(|span| span.style.fg == Some(app.palette.red))
                        .map(|span| span.content.to_string()),
                );
                let offset = app.repository_diagnostic_offset;
                key(&mut app, KeyCode::PageDown);
                if offset == app.repository_diagnostic_offset {
                    break;
                }
            }
            assert!(
                expected.is_subset(&seen),
                "missing error lines at {width}x{height}"
            );
            key(&mut app, KeyCode::PageUp);
            assert!(app.repository_diagnostic_offset < app.repository_details().len());
            key(&mut app, KeyCode::Up);
            assert_eq!(app.repository_diagnostic_offset, 0);
        }
    }

    #[test]
    fn healthy_repository_details_use_only_path_rows_and_ignore_warning_data() {
        let mut app = app(80, 24);
        app.warnings = vec![("/repos/node-00".into(), "RAW-COMMIT-WARNING".into())];
        app.attribution_warnings = vec!["RAW-ATTRIBUTION-WARNING".into()];
        let details = text(&app.repository_details());
        assert_eq!(details, "/repos/node-00");
        let screen = text(&app.repository_lines());
        assert!(!screen.contains("RAW-COMMIT"));
        assert!(!screen.contains("RAW-ATTRIBUTION"));
        assert!(!screen.contains("⚠"));
        assert!(!screen.contains("No scan"));
        assert_eq!(app.repository_layout().detail_rows, 1);
        assert!(app.repository_layout().list_rows >= 12);
    }

    #[test]
    fn unavailable_landed_history_shows_actionable_context_without_raw_log() {
        let mut app = app(80, 24);
        app.warnings = vec![("/repos/node-00".into(), "node-00: No available default branch could be identified; landed history is unknown. All locally available branches remain available in All branches.".into())];
        let details = text(&app.repository_details());
        assert!(details.contains("Default branch unavailable locally"));
        assert!(details.contains("use B on the board for all branches"));
        assert!(!details.contains("No available default branch could be identified"));
        assert!(!details.contains("WARNING //"));
    }

    #[test]
    fn failed_repositories_keep_original_error_and_cannot_be_toggled() {
        let mut app = app(60, 18);
        let broken = repo(40);
        app.repositories.push(broken.clone());
        app.loaded_repos.clear();
        app.pending_remaining = 1;
        app.loaded(crate::scan::ScanResult {
            repository: broken.clone(),
            records: vec![],
            error: Some("fatal: unreadable object DEADFACE".into()),
            warnings: vec![],
        });
        app.view = View::Repositories;
        let diagnostics = text(&app.repository_details());
        assert!(diagnostics.contains("DEADFACE"));
        assert!(text(&app.repository_lines()).contains("[!]"));
        key(&mut app, KeyCode::Char(' '));
        assert!(!app.overlay_excluded.contains(&broken.id));
        assert_eq!(key(&mut app, KeyCode::Char('q')), Action::Quit);
    }

    #[test]
    fn same_named_repositories_keep_their_own_failure_ids_paths_and_errors() {
        let mut app = app(80, 24);
        let named = |group: &str| Repository {
            id: format!("/repos/{group}/shared"),
            name: "shared".into(),
            path: format!("/repos/{group}/shared").into(),
        };
        let healthy = named("healthy");
        let first = named("broken-a");
        let second = named("broken-b");
        app.repositories = vec![healthy.clone(), first.clone(), second.clone()];
        app.loaded_repos = vec![healthy.clone()];
        app.failed_repos = vec!["shared".into(), "shared".into()];
        app.failure_details = vec![
            (second.id.clone(), "ERROR-B".into()),
            (first.id.clone(), "ERROR-A".into()),
        ];
        let ids: Vec<_> = app
            .repository_choices()
            .iter()
            .map(|choice| choice.id.to_owned())
            .collect();
        assert_eq!(ids, [healthy.id, first.id.clone(), second.id.clone()]);
        for (index, expected, error, other_error) in [
            (1, &first, "ERROR-A", "ERROR-B"),
            (2, &second, "ERROR-B", "ERROR-A"),
        ] {
            app.overlay_cursor = index;
            let diagnostics = text(&app.repository_details());
            assert!(diagnostics.contains(expected.path.to_str().unwrap()));
            assert!(diagnostics.contains(error));
            assert!(!diagnostics.contains(other_error));
            key(&mut app, KeyCode::Char(' '));
            assert!(!app.overlay_excluded.contains(&expected.id));
        }
    }

    #[test]
    fn navigation_keeps_selected_node_visible_and_applies_selection() {
        let mut app = app(40, 12);
        for _ in 0..50 {
            key(&mut app, KeyCode::Char('j'));
        }
        assert_eq!(app.overlay_cursor, 39);
        assert!(text(&app.repository_lines()).contains("node-39"));
        key(&mut app, KeyCode::Char(' '));
        key(&mut app, KeyCode::Esc);
        assert!(app.excluded.contains("/repos/node-39"));
        assert_eq!(app.view, View::Aggregate);
    }
}
