use super::components::*;
use super::merge::{wrap_help, wrapped};
use super::{App, View};
use crate::stats::{AuthorStats, RepoContribution, SortField};
use chrono::{Datelike, Duration, Months, NaiveDate};
use ratatui::text::{Line, Span};
use std::collections::BTreeMap;

impl App {
    pub(super) fn lines(&self) -> Vec<UiLine> {
        if self.loading {
            return self.loading_lines();
        }
        if let Some(error) = &self.error {
            let mut lines = banner(self.width as usize, self.height < 25, &self.palette);
            lines.extend([
                blank(),
                text_line("  ◈ ERROR — totals unavailable", self.palette.red),
                blank(),
            ]);
            lines.extend(wrapped(
                &format!("  {error}"),
                self.width as usize,
                self.palette.red,
            ));
            lines.extend(self.quality_lines());
            lines.extend([blank(), help(&[("q", "quit".into())], &self.palette)]);
            return lines;
        }
        if let Some(flow) = &self.merge {
            return self.merge_lines(flow);
        }
        match self.view {
            View::Aggregate => self.aggregate_lines(),
            View::Operative => self.detail_lines(),
            View::Repositories => self.overlay_lines(),
        }
    }

    fn loading_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let mut lines = banner(self.width as usize, self.height < 25, p);
        lines.extend([
            blank(),
            text_line("  ◈ SCANNING REPOSITORIES", p.dim_cyan),
            blank(),
        ]);
        let max_shown = (self.height as usize)
            .saturating_sub(lines.len() + 4)
            .clamp(1, 14);
        let start = self.boot_lines.len().saturating_sub(max_shown);
        if start > 0 {
            lines.push(text_line(format!("  … {start} earlier"), p.dim_white));
        }
        for (name, ok) in &self.boot_lines[start..] {
            let c = if *ok { p.green } else { p.red };
            lines.push(Line::from(vec![
                span("  ▸ ", c),
                span(display_text(name), p.bright),
                span(if *ok { "  ✓" } else { "  ✗ unreadable" }, c),
            ]));
        }
        lines.extend([
            blank(),
            text_line(
                format!(
                    "  ▐ {}/{} repos ▌",
                    self.boot_lines.len(),
                    self.boot_lines.len() + self.pending_remaining
                ),
                p.dim_cyan,
            ),
        ]);
        lines
    }

    pub(super) fn quality_lines(&self) -> Vec<UiLine> {
        let mut lines = Vec::new();
        let p = &self.palette;
        let width = self.width as usize;
        if !self.failed_repos.is_empty() {
            let names = self
                .failed_repos
                .iter()
                .take(3)
                .map(|n| display_text(n))
                .collect::<Vec<_>>()
                .join(", ");
            let more = if self.failed_repos.len() > 3 {
                format!(", +{} more", self.failed_repos.len() - 3)
            } else {
                String::new()
            };
            lines.push(text_line(
                truncate(
                    &format!(
                        "  ⚠ Partial history: {} unreadable — {names}{more}",
                        self.failed_repos.len()
                    ),
                    width,
                ),
                p.amber,
            ));
        }
        let warnings: Vec<_> = self
            .warnings
            .iter()
            .filter(|(id, _)| !self.excluded.contains(id))
            .map(|(_, warning)| warning)
            .chain(self.attribution_warnings.iter())
            .collect();
        for warning in warnings.iter().take(2) {
            lines.push(text_line(
                truncate(&format!("  ⚠ {warning}"), width),
                p.amber,
            ));
        }
        if warnings.len() > 2 {
            lines.push(text_line(
                format!(
                    "  ⚠ {} more data warnings; r → inspect details",
                    warnings.len() - 2
                ),
                p.amber,
            ));
        }
        lines
    }

    fn scope_line(&self) -> UiLine {
        text_line(
            format!(
                "  {} · {} · as of {}",
                self.scope.label(),
                self.options.timezone,
                self.cutoff
                    .with_timezone(&self.options.timezone)
                    .format("%Y-%m-%d %H:%M")
            ),
            self.palette.dim_cyan,
        )
    }

    fn aggregate_above(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let short = self.height < 30;
        let mut lines = banner(width, short, p);
        if !short {
            lines.push(blank());
        }
        lines.push(footer(
            self.loaded_repos.len(),
            self.excluded_count(),
            width,
            &self.version,
            p,
        ));
        lines.push(self.scope_line());
        lines.extend(self.quality_lines());
        lines.push(time_picker(self.time_index, p));
        if !short {
            lines.push(blank());
        }
        let unknown = self
            .authors
            .iter()
            .map(|a| a.unknown_line_commits)
            .sum::<i64>();
        lines.extend(qualified_stat_boxes(
            self.totals(),
            unknown,
            width,
            short,
            p,
        ));
        let coauthored = self
            .authors
            .iter()
            .map(|a| a.coauthored_commits)
            .sum::<i64>();
        let (_, added, removed, _) = self.totals();
        lines.push(text_line(
            format!(
                "  Lines changed: {} · Coauthored participation: {}",
                line_value(added + removed, unknown, false),
                format_number(coauthored)
            ),
            p.dim_cyan,
        ));
        if unknown > 0 {
            lines.push(text_line(
                format!("  ⚠ Known line subtotal; {unknown} authored commits have unknown lines"),
                p.amber,
            ));
        }
        if let Some(notice) = &self.notice {
            lines.push(text_line(
                truncate(&format!("  ✓ {notice}"), width),
                p.green,
            ));
        }
        if !short {
            lines.push(blank());
        }
        lines
    }

    fn aggregate_help(&self) -> Vec<UiLine> {
        wrap_help(
            &[
                ("↑↓", "nav".into()),
                ("↵", "detail".into()),
                ("←→", "time".into()),
                ("/", "find".into()),
                ("s", "sort".into()),
                ("S", "reverse".into()),
                (
                    "b",
                    format!("bots:{}", if self.hide_bots { "off" } else { "on" }),
                ),
                ("B", "history".into()),
                ("M", "merge".into()),
                ("r", "repos".into()),
                ("R", "refresh".into()),
                ("q", "quit".into()),
            ],
            self.width as usize,
            &self.palette,
        )
    }

    pub(super) fn table_viewport(&self) -> usize {
        (self.height as usize)
            .saturating_sub(self.aggregate_above().len() + 2 + 3 + self.aggregate_help().len())
            .max(1)
    }
    fn aggregate_lines(&self) -> Vec<UiLine> {
        let mut lines = self.aggregate_above();
        lines.extend(self.table_lines());
        lines.extend(self.aggregate_help());
        lines
    }

    pub(super) fn table_lines(&self) -> Vec<UiLine> {
        let authors = self.displayed_authors();
        let p = &self.palette;
        let width = self.width as usize;
        if authors.is_empty() {
            return vec![text_line(
                if self.searching || !self.filter_query.is_empty() {
                    format!(
                        "  ◈ NO MATCH for {:?} — esc to clear filter.",
                        display_text(&self.filter_query)
                    )
                } else {
                    "  ◈ NO SIGNAL — no commit data in range. Widen time or toggle B for all branches.".into()
                },
                p.amber,
            )];
        }
        let layout = TableLayout::new(width);
        let arrow = if self.sort_ascending { "↑" } else { "↓" };
        let label = |s: &str, field| {
            if self.sort_field == field {
                format!("{s}{arrow}")
            } else {
                s.into()
            }
        };
        let mut headers = vec![
            "  # ".to_string(),
            pad_right("CONTRIBUTOR", layout.name),
            pad_left(
                &label(
                    if width < 60 { "AUTH" } else { "AUTHORED" },
                    SortField::Commits,
                ),
                layout.count,
            ),
            pad_left(if width < 60 { "CO" } else { "COAUTH" }, layout.co_count),
        ];
        if layout.extra {
            headers.extend([
                pad_left(&label("ADDED", SortField::Added), 9),
                pad_left(&label("REMOVED", SortField::Removed), 9),
            ]);
        }
        if layout.net {
            headers.push(pad_left(&label("NET", SortField::Net), 9));
        }
        headers.push(pad_left(
            &label(
                if width < 60 { "LINES" } else { "LINES CHANGED" },
                SortField::Total,
            ),
            layout.lines,
        ));
        if layout.ai {
            headers.push(pad_left(
                &label(
                    if width < 80 { "AI%" } else { "DETECTED AI" },
                    SortField::AI,
                ),
                layout.ai_width,
            ));
        }
        let mut lines = vec![Line::from(bold(headers.join(" "), p.cyan)), rule(width, p)];
        let start = self.offset.min(authors.len());
        let end = (start + self.table_viewport()).min(authors.len());
        for (index, a) in authors.iter().enumerate().take(end).skip(start) {
            let rank = if self.sort_ascending {
                authors.len() - index
            } else {
                index + 1
            };
            let rank_color = match rank {
                1 => p.gold,
                2 => p.silver,
                3 => p.bronze,
                _ => p.dim_cyan,
            };
            let mut row = vec![
                bold(if index == self.selected { "▸ " } else { "  " }, p.cyan),
                bold(format!("{rank:02}"), rank_color),
                Span::raw(" "),
            ];
            if a.bot {
                let n = layout.name.saturating_sub(4);
                row.extend([
                    span(pad_right(&truncate(&a.name, n), n), p.bright),
                    span(" BOT", p.dim_cyan),
                ]);
            } else {
                row.push(span(
                    pad_right(&truncate(&a.name, layout.name), layout.name),
                    p.bright,
                ));
            }
            append_cell(&mut row, &format_number(a.commits), layout.count, p.green);
            append_cell(
                &mut row,
                &format_number(a.coauthored_commits),
                layout.co_count,
                p.cyan,
            );
            let unallocated = a.commits == 0 && a.coauthored_commits > 0;
            if layout.extra {
                append_cell(
                    &mut row,
                    &line_value(a.added, a.unknown_line_commits, unallocated),
                    9,
                    p.green,
                );
                append_cell(
                    &mut row,
                    &line_value(a.removed, a.unknown_line_commits, unallocated),
                    9,
                    p.magenta,
                );
            }
            if layout.net {
                append_cell(
                    &mut row,
                    &line_value(a.net, a.unknown_line_commits, unallocated),
                    9,
                    if a.net < 0 { p.red } else { p.green },
                );
            }
            append_cell(
                &mut row,
                &line_value(a.total_change, a.unknown_line_commits, unallocated),
                layout.lines,
                if a.unknown_line_commits > 0 {
                    p.amber
                } else {
                    p.green
                },
            );
            if layout.ai {
                append_cell(
                    &mut row,
                    &if a.commits > 0 {
                        percent_label(a.ai_commits, a.commits)
                    } else {
                        "—".into()
                    },
                    layout.ai_width,
                    p.amber,
                );
            }
            lines.push(Line::from(row).style(p.row(index == self.selected, index)));
        }
        if self.searching {
            lines.push(Line::from(vec![
                span("  /", p.cyan),
                span(display_text(&self.filter_query), p.bright),
                bold("▌", p.cyan),
                span("  enter apply · esc clear", p.dim_white),
            ]));
        } else {
            lines.push(text_line(
                truncate(
                    &format!(
                        "  showing {}–{end} of {} · sort: {} {}",
                        start + 1,
                        authors.len(),
                        self.sort_field.label(),
                        arrow
                    ),
                    width,
                ),
                p.dim_cyan,
            ));
        }
        lines.push(text_line(
            truncate(
                "  CO = coauthored participation · ? unknown lines · — unallocated",
                width,
            ),
            p.dim_white,
        ));
        if let Some(author) = authors.get(self.selected) {
            let mut emails: Vec<_> = author.emails.iter().map(|s| display_text(s)).collect();
            emails.sort();
            let identity = if emails.is_empty() {
                format!(
                    "repository-local · {}",
                    author
                        .per_repo
                        .keys()
                        .cloned()
                        .collect::<Vec<_>>()
                        .join(", ")
                )
            } else {
                emails.join(", ")
            };
            lines.push(text_line(
                truncate(&format!("  {identity}"), width),
                p.dim_white,
            ));
        }
        lines
    }

    fn overlay_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let mut lines = vec![section("REPOSITORY CONTROL", width, p), blank()];
        let selected = self.loaded_repos.get(self.overlay_cursor);
        let mut warning_lines = Vec::new();
        if let Some(repo) = selected {
            for (_, warning) in self.warnings.iter().filter(|(id, _)| id == &repo.id) {
                warning_lines.extend(wrapped(&format!("  ⚠ {warning}"), width, p.amber));
            }
        }
        if let Some(repo) = selected {
            let filtered = self.filtered_records();
            let commits: std::collections::HashSet<_> = filtered
                .iter()
                .filter(|record| record.repo_id == repo.id)
                .map(|record| record.commit_id.clone())
                .collect();
            let associated: Vec<_> = filtered
                .into_iter()
                .filter(|record| commits.contains(&record.commit_id))
                .collect();
            let warnings =
                crate::stats::attribution_warnings(&associated, &self.aggregate_options());
            if !warnings.is_empty() {
                warning_lines.push(text_line(
                    "  Attribution warnings for the current filters:",
                    p.amber,
                ));
                for warning in warnings {
                    warning_lines.extend(wrapped(&format!("  ⚠ {warning}"), width, p.amber));
                }
            }
        }
        let rows = (self.height as usize)
            .saturating_sub(lines.len() + warning_lines.len() + 5)
            .max(1);
        let start = self.overlay_cursor.saturating_add(1).saturating_sub(rows);
        for (i, repo) in self.loaded_repos.iter().enumerate().skip(start).take(rows) {
            let excluded = self.overlay_excluded.contains(&repo.id);
            let c = if excluded { p.dim_white } else { p.cyan };
            let has_warning = self.warnings.iter().any(|(id, _)| id == &repo.id);
            lines.push(
                Line::from(vec![
                    bold(
                        if i == self.overlay_cursor {
                            "  ▸ "
                        } else {
                            "    "
                        },
                        p.cyan,
                    ),
                    span(if excluded { "[ ] " } else { "[x] " }, c),
                    span(display_text(&repo.name), c),
                    span(if has_warning { "  ⚠" } else { "" }, p.amber),
                ])
                .style(p.row(i == self.overlay_cursor, i)),
            );
        }
        let excluded = self
            .loaded_repos
            .iter()
            .filter(|r| self.overlay_excluded.contains(&r.id))
            .count();
        lines.push(blank());
        lines.extend(warning_lines);
        lines.push(text_line(
            repo_count(self.loaded_repos.len(), excluded),
            p.dim_cyan,
        ));
        lines.push(blank());
        lines.extend(wrap_help(
            &[
                ("space", "toggle".into()),
                ("enter/esc", "done".into()),
                ("↑↓", "navigate".into()),
            ],
            width,
            p,
        ));
        lines
    }

    pub(super) fn detail_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let author = self.authors.iter().find(|a| a.id == self.active_id);
        let context = author.or_else(|| self.contributors.iter().find(|a| a.id == self.active_id));
        let name = context.map(|a| a.name.as_str()).unwrap_or("Contributor");
        let mut lines = banner(width, self.height < 40, p);
        lines.extend([
            blank(),
            footer(self.loaded_repos.len(), self.excluded_count(), width, "", p),
            self.scope_line(),
            time_picker(self.time_index, p),
            blank(),
            section(&format!("CONTRIBUTOR: {}", name.to_uppercase()), width, p),
        ]);
        if let Some(a) = context {
            let mut emails: Vec<_> = a.emails.iter().map(|e| display_text(e)).collect();
            emails.sort();
            lines.extend(wrapped(
                &format!("  {}", emails.join(", ")),
                width,
                p.dim_white,
            ));
        }
        if let Some(a) = author {
            let unallocated = a.commits == 0 && a.coauthored_commits > 0;
            lines.push(blank());
            if !unallocated {
                lines.extend(qualified_stat_boxes(
                    (a.commits, a.added, a.removed, a.ai_commits),
                    a.unknown_line_commits,
                    width,
                    self.height < 40,
                    p,
                ));
            }
            lines.push(text_line(
                format!(
                    "  Authored {} · Coauthored participation {} · Lines changed {}",
                    format_number(a.commits),
                    format_number(a.coauthored_commits),
                    line_value(a.total_change, a.unknown_line_commits, unallocated)
                ),
                p.cyan,
            ));
            if unallocated {
                lines.push(text_line(
                    "  Line changes are unallocated to coauthors; credited to the primary author.",
                    p.dim_cyan,
                ));
            }
            lines.extend(metrics(a, width, p));
            if !a.per_repo.is_empty() {
                lines.extend([blank(), section("REPO CONTRIBUTIONS", width, p)]);
                lines.extend(wrapped(
                    "  Repository associations can overlap; do not sum these rows.",
                    width,
                    p.dim_white,
                ));
                lines.extend(wrapped(
                    "  COAUTH = participation · — unallocated lines · ? unknown lines",
                    width,
                    p.dim_white,
                ));
                lines.extend(repo_breakdown(a, width, p));
            }
            if !a.monthly.is_empty() {
                lines.extend([blank(), section("ACTIVITY TIMELINE", width, p), blank()]);
                lines.extend(timeline(&a.monthly, width, p));
            }
            if !a.daily.is_empty() {
                lines.extend([blank(), section("ACTIVITY MATRIX", width, p), blank()]);
                lines.extend(heatmap(
                    &a.daily,
                    width,
                    self.cutoff
                        .with_timezone(&self.options.timezone)
                        .date_naive(),
                    p,
                ));
            }
            if a.unknown_line_commits > 0 {
                lines.extend(wrapped(
                    &format!(
                        "  ⚠ Known line subtotal; {} authored commits have unknown lines.",
                        a.unknown_line_commits
                    ),
                    width,
                    p.amber,
                ));
            }
        } else {
            lines.extend([blank(),text_line("  ◈ NO SIGNAL — no activity in this range. Change time or press B for all branches.",p.amber)]);
        }
        // Keep qualification and context visible even when the finite terminal
        // displays the final screenful of a long contributor view.
        lines.extend(self.quality_lines());
        lines.push(self.scope_line());
        lines.push(text_line(truncate(&format!("  {}", name), width), p.bright));
        if let Some(notice) = &self.notice {
            lines.push(text_line(
                truncate(&format!("  ✓ {notice}"), width),
                p.green,
            ));
        }
        lines.extend(wrap_help(
            &[
                ("↑↓", "prev/next".into()),
                ("esc", "back".into()),
                ("←→", "time".into()),
                ("B", "history".into()),
                ("M", "merge".into()),
                ("q", "quit".into()),
            ],
            width,
            p,
        ));
        lines
    }
}

fn qualified_stat_boxes(
    totals: (i64, i64, i64, i64),
    unknown: i64,
    width: usize,
    compact: bool,
    p: &Palette,
) -> Vec<UiLine> {
    if unknown == 0 {
        return stat_boxes(totals, width, compact, p);
    }
    let (commits, added, removed, ai) = totals;
    let mut lines = wrapped(
        &format!(
            "  Authored {} · Detected AI {} ({})",
            format_number(commits),
            percent_label(ai, commits),
            format_number(ai)
        ),
        width,
        p.cyan,
    );
    lines.extend(wrapped(
        &format!(
            "  Known line subtotal: added {} · removed {}",
            line_value(added, unknown, false),
            line_value(removed, unknown, false)
        ),
        width,
        p.amber,
    ));
    lines
}

struct TableLayout {
    name: usize,
    count: usize,
    co_count: usize,
    lines: usize,
    extra: bool,
    net: bool,
    ai: bool,
    ai_width: usize,
}
impl TableLayout {
    fn new(width: usize) -> Self {
        if width >= 120 {
            Self {
                name: 22,
                count: 8,
                co_count: 7,
                lines: 14,
                extra: true,
                net: true,
                ai: true,
                ai_width: 11,
            }
        } else if width >= 80 {
            Self {
                name: 20,
                count: 8,
                co_count: 6,
                lines: 14,
                extra: false,
                net: true,
                ai: true,
                ai_width: 11,
            }
        } else if width >= 60 {
            Self {
                name: 18,
                count: 8,
                co_count: 6,
                lines: 14,
                extra: false,
                net: false,
                ai: true,
                ai_width: 5,
            }
        } else {
            Self {
                name: width.saturating_sub(25).clamp(6, 16),
                count: 4,
                co_count: 2,
                lines: 7,
                extra: false,
                net: false,
                ai: width >= 44,
                ai_width: 4,
            }
        }
    }
}
fn append_cell(row: &mut Vec<UiSpan>, value: &str, width: usize, color: ratatui::style::Color) {
    row.push(span(format!(" {}", pad_left(value, width)), color));
}
pub(super) fn line_value(value: i64, unknown: i64, unallocated: bool) -> String {
    if unallocated {
        "—".into()
    } else if unknown > 0 {
        if value == 0 {
            "?".into()
        } else {
            format!("{} ?", format_number(value))
        }
    } else {
        format_number(value)
    }
}
fn metrics(a: &AuthorStats, width: usize, p: &Palette) -> Vec<UiLine> {
    let ratio = if a.unknown_line_commits > 0 {
        "N/A (unknown lines)".into()
    } else {
        a.removed_added_ratio()
            .map(|r| format!("{r:.2}"))
            .unwrap_or_else(|| "N/A".into())
    };
    let mut parts = vec![format!("Active {} days", a.active_days)];
    if a.first_commit.year() != 1 {
        parts.extend([
            format!("First {}", a.first_commit.format("%Y-%m-%d")),
            format!("Last {}", a.last_commit.format("%Y-%m-%d")),
        ]);
    }
    let mut lines = wrapped(&format!("  {}", parts.join(" · ")), width, p.dim_cyan);
    lines.extend(wrapped(
        &format!(
            "  Removed/added ratio: {ratio} · Detected AI: {}",
            if a.commits > 0 {
                percent_label(a.ai_commits, a.commits)
            } else {
                "N/A".into()
            }
        ),
        width,
        p.dim_cyan,
    ));
    lines
}
fn repo_breakdown(author: &AuthorStats, width: usize, p: &Palette) -> Vec<UiLine> {
    let mut repos: Vec<_> = author.per_repo.iter().collect();
    repos.sort_by(|a, b| {
        b.1.total_change
            .cmp(&a.1.total_change)
            .then_with(|| a.0.cmp(b.0))
    });
    let name_width = if width >= 80 { 28 } else { 18 };
    let mut lines = vec![
        Line::from(bold(
            format!(
                "  {:name_width$} {:>8} {:>7} {:>14}",
                "REPO", "AUTHORED", "COAUTH", "LINES CHANGED"
            ),
            p.cyan,
        )),
        rule(width, p),
    ];
    for (i, (name, repo)) in repos.into_iter().enumerate() {
        let unallocated = repo.commits == 0 && repo.coauthored_commits > 0;
        let mut row = vec![span(
            format!("  {}", pad_right(&truncate(name, name_width), name_width)),
            p.magenta,
        )];
        append_cell(&mut row, &format_number(repo.commits), 8, p.green);
        append_cell(&mut row, &format_number(repo.coauthored_commits), 7, p.cyan);
        append_cell(
            &mut row,
            &line_value(repo.total_change, repo.unknown_line_commits, unallocated),
            14,
            if repo.unknown_line_commits > 0 {
                p.amber
            } else {
                p.green
            },
        );
        if width >= 100 {
            row.push(span(
                format!(
                    "  +{} / -{}",
                    line_value(repo.added, repo.unknown_line_commits, unallocated),
                    line_value(repo.removed, repo.unknown_line_commits, unallocated)
                ),
                p.dim_cyan,
            ));
        }
        let ai_label = if repo.ai_commits > 0 {
            format!(
                "  Detected AI {}",
                percent_label(repo.ai_commits, repo.commits)
            )
        } else {
            String::new()
        };
        let fits = row.iter().map(|s| s.width()).sum::<usize>() + ai_label.len() <= width;
        if fits && !ai_label.is_empty() {
            row.push(span(ai_label.clone(), p.amber));
        }
        lines.push(Line::from(row).style(p.row(false, i)));
        if !fits && !ai_label.is_empty() {
            lines.push(text_line(format!("    {}", ai_label.trim()), p.amber));
        }
    }
    lines
}

pub(super) fn monthly_rows(
    months: &BTreeMap<String, RepoContribution>,
) -> Vec<(NaiveDate, RepoContribution)> {
    let entries: BTreeMap<_, _> = months
        .iter()
        .filter_map(|(month, data)| {
            NaiveDate::parse_from_str(&format!("{month}-01"), "%Y-%m-%d")
                .ok()
                .map(|date| (date, data.clone()))
        })
        .collect();
    let Some((&first, _)) = entries.first_key_value() else {
        return vec![];
    };
    let last = *entries.last_key_value().expect("nonempty months").0;
    let mut date = last
        .checked_sub_months(Months::new(11))
        .unwrap_or(first)
        .max(first);
    let mut rows = Vec::new();
    loop {
        rows.push((date, entries.get(&date).cloned().unwrap_or_default()));
        if date >= last {
            break;
        }
        let Some(next) = date.checked_add_months(Months::new(1)) else {
            break;
        };
        date = next;
    }
    rows
}
fn timeline(months: &BTreeMap<String, RepoContribution>, width: usize, p: &Palette) -> Vec<UiLine> {
    let rows = monthly_rows(months);
    let maximum = rows.iter().map(|(_, r)| r.total_change).max().unwrap_or(0);
    let mut lines = vec![text_line(
        "  MONTH       AUTH COAUTH  LINES CHANGED",
        p.dim_cyan,
    )];
    for (month, r) in rows {
        let unallocated = r.commits == 0 && r.coauthored_commits > 0;
        let ai_label = if r.ai_commits > 0 {
            format!(" Detected AI {}", percent_label(r.ai_commits, r.commits))
        } else {
            String::new()
        };
        let mut row = vec![span(
            format!(
                "  {:10} {:4} {:6} {:>12} ",
                month.format("%b %Y").to_string(),
                r.commits,
                r.coauthored_commits,
                line_value(r.total_change, r.unknown_line_commits, unallocated)
            ),
            p.green,
        )];
        let bar_width = width.saturating_sub(38 + ai_label.len()).min(50);
        if !unallocated && bar_width > 0 {
            row.extend(impact_bar(r.added, r.removed, maximum, bar_width, p));
        }
        if !ai_label.is_empty() {
            row.push(span(ai_label, p.amber));
        }
        lines.push(Line::from(row));
    }
    lines
}

pub(super) fn heatmap(
    daily: &BTreeMap<NaiveDate, RepoContribution>,
    width: usize,
    today: NaiveDate,
    p: &Palette,
) -> Vec<UiLine> {
    let maximum = daily
        .values()
        .map(|r| r.total_change)
        .max()
        .unwrap_or(0)
        .max(1);
    let weeks = width.saturating_sub(10).clamp(12, 53);
    let first = today - Duration::days(7 * (weeks as i64 - 1));
    let start = first - Duration::days(first.weekday().num_days_from_sunday() as i64);
    let ramp = [
        ("·", p.dim_white),
        ("░", p.cyan_dim),
        ("▒", p.cyan_mid),
        ("▓", p.cyan),
        ("█", p.magenta),
    ];
    let mut lines = Vec::new();
    for (wd, label) in ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
        .iter()
        .enumerate()
    {
        let mut row = vec![span(format!("  {label:4}"), p.dim_white)];
        for col in 0..weeks {
            let date = start + Duration::days((col * 7 + wd) as i64);
            if date > today {
                row.push(Span::raw(" "));
                continue;
            }
            let value = daily.get(&date);
            if value.is_some_and(|r| r.unknown_line_commits > 0) {
                row.push(span("?", p.amber));
                continue;
            }
            if value.is_some_and(|r| r.commits == 0 && r.coauthored_commits > 0) {
                row.push(span("○", p.cyan));
                continue;
            }
            let value = value.map_or(0, |r| r.total_change);
            let level = if value > 0 {
                (1 + value * 3 / maximum).min(4) as usize
            } else {
                0
            };
            row.push(span(ramp[level].0, ramp[level].1));
        }
        lines.push(Line::from(row));
    }
    lines.push(blank());
    let mut legend = vec![span("  less ", p.dim_white)];
    legend.extend(ramp.iter().skip(1).map(|(s, c)| span(*s, *c)));
    legend.push(span(" more (Lines changed)", p.dim_white));
    lines.push(Line::from(legend));
    if daily.values().any(|r| r.unknown_line_commits > 0) {
        lines.push(text_line("  ? unknown line changes", p.amber));
    }
    if daily
        .values()
        .any(|r| r.commits == 0 && r.coauthored_commits > 0)
    {
        lines.push(text_line(
            "  ○ coauthored participation; lines unallocated",
            p.dim_cyan,
        ));
    }
    lines
}
