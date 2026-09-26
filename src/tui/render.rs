use super::components::*;
use super::merge::{wrap_help, wrapped};
use super::{App, TIME_PRESETS, View};
use crate::stats::{AuthorStats, RepoContribution, SortField};
use chrono::{Datelike, Duration, Months, NaiveDate};
use ratatui::text::{Line, Span};
use std::collections::BTreeMap;
use unicode_width::UnicodeWidthStr;

impl App {
    fn spacious_commands(&self) -> bool {
        self.width >= 106 && self.height >= 30
    }

    fn roomy_aggregate(&self) -> bool {
        self.width >= 82
            && self.height >= 40
            && self.authors.len() <= (self.height as usize).saturating_sub(26)
    }

    fn spacious_header(&self) -> bool {
        self.roomy_aggregate() && self.authors.len() <= (self.height as usize).saturating_sub(28)
    }

    pub(super) fn lines(&self) -> Vec<UiLine> {
        if self.loading {
            return self.loading_lines();
        }
        if self.view == View::Repositories {
            return self.repository_lines();
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
            lines.extend([
                blank(),
                help(
                    &[
                        ("g", "GitHub".into()),
                        ("R", "retry".into()),
                        ("q", "quit".into()),
                    ],
                    &self.palette,
                ),
            ]);
            return lines;
        }
        if let Some(flow) = &self.merge {
            return self.merge_lines(flow);
        }
        match self.view {
            View::Aggregate => self.aggregate_lines(),
            View::Operative => self.detail_lines(),
            View::Repositories => self.repository_lines(),
        }
    }

    fn loading_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let mut lines = banner(self.width as usize, self.height < 25, p);
        let title = if self.github_source {
            "LOADING GITHUB ACTIVITY"
        } else {
            "SCANNING REPOSITORIES"
        };
        lines.extend([
            blank(),
            text_line(format!("  {} {title}", spinner(self.loading_tick)), p.cyan),
            text_line(
                format!("  {}s elapsed · q cancels", self.loading_tick / 10),
                p.dim_cyan,
            ),
            blank(),
        ]);
        let available = (self.height as usize).saturating_sub(lines.len() + 3);
        let active = self.scan_progress.len().min(available / 2);
        for progress in self.scan_progress.values().take(active) {
            lines.push(text_line(
                truncate(
                    &format!("  ▸ {}", display_text(&progress.repository_name)),
                    self.width as usize,
                ),
                p.bright,
            ));
            lines.push(text_line(
                truncate(&format!("    {}", progress.stage), self.width as usize),
                p.dim_cyan,
            ));
        }
        let remaining = available.saturating_sub(active * 2);
        let start = self.boot_lines.len().saturating_sub(remaining.min(8));
        for (name, ok) in &self.boot_lines[start..] {
            lines.push(text_line(
                truncate(
                    &format!("  {} {}", if *ok { "✓" } else { "✗" }, display_text(name)),
                    self.width as usize,
                ),
                if *ok { p.green } else { p.red },
            ));
        }
        lines.extend([
            blank(),
            text_line(
                format!(
                    "  ▐ {}/{} repos complete ▌",
                    self.boot_lines.len(),
                    self.boot_lines.len() + self.pending_remaining
                ),
                p.dim_cyan,
            ),
        ]);
        lines
    }

    pub(super) fn quality_lines(&self) -> Vec<UiLine> {
        let mut status = Vec::new();
        if !self.failed_repos.is_empty() {
            status.push(format!("{} unreadable", self.failed_repos.len()));
        }
        if self.scope == crate::model::HistoryScope::Landed {
            let missing = self
                .loaded_repos
                .iter()
                .filter(|repo| {
                    !self.excluded.contains(&repo.id)
                        && self.repository_history_unavailable(&repo.id)
                })
                .count();
            if missing > 0 {
                status.push(format!("{missing} without landed history"));
            }
        }
        if status.is_empty() {
            return vec![];
        }
        let inspect = if self.view == View::Operative {
            "esc → r"
        } else {
            "r"
        };
        vec![text_line(
            truncate(
                &format!("  ⚠ Repos: {} · [{inspect}] details", status.join(" · ")),
                self.width as usize,
            ),
            self.palette.amber,
        )]
    }

    pub(super) fn api_basis(&self) -> UiLine {
        text_line(
            "  Commit date · all files · merges: ? lines",
            self.palette.dim_cyan,
        )
    }

    pub(super) fn scope_line(&self) -> UiLine {
        text_line(
            format!(
                "  {}{} · {} · as of {}",
                if self.github_source {
                    "GITHUB API · "
                } else {
                    ""
                },
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
        let roomy = self.roomy_aggregate();
        // Expand the logo only when every contributor still fits. Resizing a
        // busy board must never trade contributor rows for extra decoration.
        let full_banner =
            self.height >= 40 && self.authors.len() <= (self.height as usize).saturating_sub(22);
        let mut lines = banner(width, !full_banner, p);
        if self.spacious_header() {
            lines.push(blank());
        }
        let left = format!(
            "  {}{} · {} · {} · {}",
            if self.github_source {
                "GITHUB API · "
            } else {
                ""
            },
            repo_count(self.loaded_repos.len(), self.excluded_count()).trim(),
            self.scope.label(),
            self.options.timezone,
            self.cutoff
                .with_timezone(&self.options.timezone)
                .format("%Y-%m-%d %H:%M"),
        );
        let right = format!("v{}  ", self.version);
        let context = if left.width() + right.width() + 2 <= width {
            format!(
                "{left}{}{right}",
                " ".repeat(width - left.width() - right.width())
            )
        } else {
            truncate(&left, width)
        };
        lines.push(text_line(context, p.dim_cyan));
        if self.github_source {
            lines.push(self.api_basis());
        }
        if self.spacious_header() {
            lines.push(blank());
        }
        if width >= 70 {
            if width >= 82 {
                let mut spans = vec![bold("  RANGE", p.cyan)];
                spans.extend(time_picker(self.time_index, p).spans);
                lines.push(Line::from(spans));
            } else {
                lines.push(time_picker(self.time_index, p));
            }
        } else {
            lines.push(text_line(
                truncate(
                    &format!(
                        "  ◂ RANGE ▐{}▌ ▸  [←→] change",
                        TIME_PRESETS[self.time_index].0
                    ),
                    width,
                ),
                p.cyan,
            ));
        }
        if roomy {
            lines.push(blank());
        }
        lines.push(panel_header("ACTIVITY", width, p));
        let inner = width.saturating_sub(4);
        let (commits, added, removed, ai) = self.totals();
        let unknown = self
            .authors
            .iter()
            .map(|a| a.unknown_line_commits)
            .sum::<i64>();
        let coauthored = self
            .authors
            .iter()
            .map(|a| a.coauthored_commits)
            .sum::<i64>();
        let metrics = [
            ("AUTHORED", format_number(commits), p.cyan),
            ("ADDED", line_value(added, unknown, false), p.green),
            ("REMOVED", line_value(removed, unknown, false), p.magenta),
            (
                "Lines changed",
                line_value(added + removed, unknown, false),
                p.cyan,
            ),
            ("COAUTHORED", format_number(coauthored), p.cyan),
            (
                "Detected AI",
                format!("{} ({ai})", percent_label(ai, commits)),
                p.amber,
            ),
        ];
        if roomy {
            let mut columns = [0; 3];
            for (index, (label, value, _)) in metrics.iter().enumerate() {
                columns[index % 3] = columns[index % 3].max(label.width() + value.width() + 1);
            }
            let required = columns.iter().sum::<usize>() + 6;
            if required <= inner {
                let spare = inner - required;
                for (index, column) in columns.iter_mut().enumerate() {
                    *column += spare / 3 + usize::from(index < spare % 3);
                }
                for row in metrics.chunks(3) {
                    lines.push(activity_metric_row(row, width, p, Some(&columns)));
                }
            } else {
                let mut start = 0;
                while start < metrics.len() {
                    let mut end = start + 1;
                    let mut used = metrics[start].0.width() + metrics[start].1.width() + 1;
                    while end < metrics.len() && end - start < 3 {
                        let next = metrics[end].0.width() + metrics[end].1.width() + 1;
                        if used + 3 + next > inner {
                            break;
                        }
                        used += 3 + next;
                        end += 1;
                    }
                    lines.push(activity_metric_row(&metrics[start..end], width, p, None));
                    start = end;
                }
            }
        } else {
            let mut row = Vec::new();
            let mut used = 0;
            for (label, value, color) in metrics {
                let size = label.width() + value.width() + 1;
                if !row.is_empty() && used + 3 + size > inner {
                    lines.push(panel_row(Line::from(std::mem::take(&mut row)), width, p));
                    used = 0;
                }
                if !row.is_empty() {
                    row.push(span(" │ ", p.dim_cyan));
                    used += 3;
                }
                row.extend([span(format!("{label} "), p.dim_cyan), bold(value, color)]);
                used += size;
            }
            if !row.is_empty() {
                lines.push(panel_row(Line::from(row), width, p));
            }
        }
        if unknown > 0 {
            let qualifier =
                format!("Known line subtotal · {unknown} authored commits have unknown lines");
            lines.push(panel_row(
                text_line(truncate(&qualifier, inner), p.amber),
                width,
                p,
            ));
        }
        lines.push(panel_footer(width, p));
        lines.extend(self.quality_lines());
        if let Some(notice) = &self.notice {
            lines.push(text_line(
                truncate(&format!("  ✓ {notice}"), width),
                p.green,
            ));
        }
        if roomy {
            lines.push(blank());
        }
        lines
    }

    fn aggregate_help(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let mut bindings = vec![
            ("↑↓", "select".to_owned()),
            ("↵", "detail".to_owned()),
            ("←→", "range".to_owned()),
            ("/", "find".to_owned()),
            ("s/S", "sort/reverse".to_owned()),
            (
                "g",
                if self.github_source {
                    "choose repos"
                } else {
                    "GitHub"
                }
                .to_owned(),
            ),
            ("M", "merge".to_owned()),
            ("B", "history".to_owned()),
            (
                "b",
                format!("bots:{}", if self.hide_bots { "off" } else { "on" }),
            ),
            ("r", "repos".to_owned()),
            ("R", "refresh".to_owned()),
            ("q", "quit".to_owned()),
        ];
        if self.github_source {
            bindings.retain(|(key, _)| *key != "B");
        }
        if self.spacious_commands() {
            return command_grid(&bindings, width, p);
        }
        wrap_help(&bindings[..6], width, p)
            .into_iter()
            .chain(wrap_help(&bindings[6..], width, p))
            .collect()
    }

    fn table_legend(&self) -> Option<String> {
        let authors = self.displayed_authors();
        let mut markers = Vec::new();
        if authors.iter().any(|a| a.unknown_line_commits > 0) {
            markers.push("? unknown lines");
        }
        if authors
            .iter()
            .any(|a| a.commits == 0 && a.coauthored_commits > 0)
        {
            markers.push("— coauthor lines unallocated");
        }
        (!markers.is_empty()).then(|| markers.join(" · "))
    }

    fn selected_identity(&self) -> Option<String> {
        let authors = self.displayed_authors();
        let author = authors.get(self.selected)?;
        if authors
            .iter()
            .filter(|other| other.name == author.name)
            .count()
            < 2
        {
            return None;
        }
        let mut emails: Vec<_> = author.emails.iter().map(|s| display_text(s)).collect();
        emails.sort();
        Some(if emails.is_empty() {
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
        })
    }

    pub(super) fn table_viewport(&self) -> usize {
        let footer = 1
            + usize::from(self.table_legend().is_some())
            + usize::from(self.selected_identity().is_some());
        (self.height as usize)
            .saturating_sub(
                self.aggregate_above().len()
                    + 3
                    + footer
                    + self.aggregate_help().len()
                    + usize::from(self.roomy_aggregate() || self.spacious_commands()),
            )
            .max(1)
    }

    fn aggregate_lines(&self) -> Vec<UiLine> {
        let mut lines = self.aggregate_above();
        lines.extend(self.table_lines());
        if self.roomy_aggregate() || self.spacious_commands() {
            lines.push(blank());
        }
        lines.extend(self.aggregate_help());
        if lines.len() > self.height as usize || self.width < 60 {
            // Never silently tail-clip the board's scope or quality context.
            // A terminal that cannot fit even one row needs an explicit prompt.
            let required = lines.len();
            let required_width = self.width.max(60);
            let width = self.width as usize;
            let p = &self.palette;
            let mut compact = banner(width, true, p);
            compact.push(text_line(
                truncate(&self.scope_line().to_string(), width),
                p.dim_cyan,
            ));
            compact.extend(self.quality_lines());
            compact.extend(wrapped(
                &format!("  ◈ LOW RESOLUTION · resize to at least {required_width}×{required} to display the board"),
                width, p.amber,
            ));
            compact.push(help(&[("q", "quit".into())], p));
            compact.truncate(self.height as usize);
            return compact;
        }
        lines
    }

    pub(super) fn table_lines(&self) -> Vec<UiLine> {
        let authors = self.displayed_authors();
        let p = &self.palette;
        let width = self.width as usize;
        if authors.is_empty() {
            return vec![text_line(
                truncate(
                    &if self.searching || !self.filter_query.is_empty() {
                        format!(
                            "  ◈ NO MATCH for {:?} — esc to clear filter.",
                            display_text(&self.filter_query)
                        )
                    } else {
                        "  ◈ NO SIGNAL — no commit data in range. Widen time or toggle B for all branches.".into()
                    },
                    width,
                ),
                p.amber,
            )];
        }
        let layout = TableLayout::new(width.saturating_sub(4), &authors);
        let arrow = if self.sort_ascending { "↑" } else { "↓" };
        let mut header = vec![bold(
            format!(
                "  {}  {}",
                pad_left("#", layout.rank),
                pad_right(&truncate("CONTRIBUTOR", layout.name), layout.name)
            ),
            p.cyan,
        )];
        for column in &layout.columns {
            let selected = column.field.sort_field() == Some(self.sort_field);
            let label = format!("{}{}", column.label, if selected { arrow } else { " " });
            header.push(bold(
                format!("  {}", pad_left(&label, column.width)),
                if selected { p.magenta } else { p.cyan },
            ));
        }
        let mut lines = vec![
            panel_header("CONTRIBUTORS", width, p),
            panel_row(Line::from(header), width, p),
        ];
        let start = self.offset.min(authors.len());
        let end = (start + self.table_viewport()).min(authors.len());
        for (index, author) in authors.iter().enumerate().take(end).skip(start) {
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
                bold(pad_left(&format!("{rank:02}"), layout.rank), rank_color),
                Span::raw("  "),
            ];
            if author.bot && layout.name >= 8 {
                let name_width = layout.name - 4;
                let name = truncate(&author.name, name_width);
                let padding = name_width.saturating_sub(name.width());
                row.extend([
                    span(name, p.bright),
                    span(" BOT", p.dim_cyan),
                    Span::raw(" ".repeat(padding)),
                ]);
            } else {
                row.push(span(
                    pad_right(&truncate(&author.name, layout.name), layout.name),
                    p.bright,
                ));
            }
            for column in &layout.columns {
                let value = column.field.value(author);
                let color = match column.field {
                    TableField::Added => p.green,
                    TableField::Removed => p.magenta,
                    TableField::Lines | TableField::Net if author.unknown_line_commits > 0 => {
                        p.amber
                    }
                    TableField::Net if author.net < 0 => p.red,
                    TableField::Net | TableField::Lines => p.green,
                    TableField::AI => p.amber,
                    _ => p.cyan,
                };
                row.push(span(format!("  {}", pad_left(&value, column.width)), color));
            }
            lines.push(panel_row(
                Line::from(row).style(p.row(index == self.selected, index)),
                width,
                p,
            ));
        }
        lines.push(panel_footer(width, p));
        if self.searching {
            lines.push(text_line(
                truncate(
                    &format!(
                        "  /{}▌  enter apply · esc clear",
                        display_text(&self.filter_query)
                    ),
                    width,
                ),
                p.cyan,
            ));
        } else {
            lines.push(text_line(
                truncate(
                    &format!(
                        "  showing {}–{end} of {} · sort: {} {arrow}",
                        start + 1,
                        authors.len(),
                        self.sort_field.label()
                    ),
                    width,
                ),
                p.dim_cyan,
            ));
        }
        if let Some(legend) = self.table_legend() {
            lines.push(text_line(
                truncate(&format!("  {legend}"), width),
                p.dim_white,
            ));
        }
        if let Some(identity) = self.selected_identity() {
            lines.push(text_line(
                truncate(&format!("  ID {identity}"), width),
                p.dim_white,
            ));
        }
        lines
    }

    pub(super) fn detail_content(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let author = self.authors.iter().find(|a| a.id == self.active_id);
        let context = author.or_else(|| self.contributors.iter().find(|a| a.id == self.active_id));
        let mut lines = Vec::new();
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
        } else {
            lines.extend([blank(),text_line("  ◈ NO SIGNAL — no activity in this range. Change time or press B for all branches.",p.amber)]);
        }

        lines
    }
}

fn command_grid(bindings: &[(&str, String)], width: usize, p: &Palette) -> Vec<UiLine> {
    let rows = [&bindings[..6], &bindings[6..]];
    let mut columns = [18; 5];
    for row in rows {
        for (index, (key, description)) in row.iter().take(5).enumerate() {
            columns[index] = columns[index].max(key.width() + description.width() + 6);
        }
    }
    rows.into_iter()
        .map(|row| {
            let mut spans = vec![Span::raw("  ")];
            for (index, (key, description)) in row.iter().enumerate() {
                let content_width = key.width() + description.width() + 3;
                spans.extend([
                    span("▐", p.dim_cyan),
                    bold(*key, p.cyan),
                    span("▌ ", p.dim_cyan),
                    span(description.clone(), p.dim_white),
                ]);
                if index < 5 {
                    spans.push(Span::raw(" ".repeat(columns[index] - content_width)));
                }
            }
            let line = Line::from(spans);
            debug_assert!(line.width() <= width);
            line
        })
        .collect()
}

fn activity_metric_row(
    metrics: &[(&str, String, ratatui::style::Color)],
    width: usize,
    p: &Palette,
    columns: Option<&[usize]>,
) -> UiLine {
    let inner = width.saturating_sub(4);
    let content_width = metrics
        .iter()
        .map(|(label, value, _)| label.width() + value.width() + 1)
        .sum::<usize>()
        + metrics.len().saturating_sub(1) * 3;
    let spare = inner.saturating_sub(content_width);
    let mut row = Vec::new();
    for (index, (label, value, color)) in metrics.iter().enumerate() {
        row.extend([
            span(format!("{label} "), p.dim_cyan),
            bold(value.clone(), *color),
        ]);
        let size = label.width() + value.width() + 1;
        let padding = columns.map_or_else(
            || spare / metrics.len() + usize::from(index < spare % metrics.len()),
            |columns| columns[index].saturating_sub(size),
        );
        row.push(Span::raw(" ".repeat(padding)));
        if index + 1 < metrics.len() {
            row.push(span(" │ ", p.dim_cyan));
        }
    }
    panel_row(Line::from(row), width, p)
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

#[derive(Clone, Copy, PartialEq, Eq)]
enum TableField {
    Authored,
    Coauthored,
    Added,
    Removed,
    Net,
    Lines,
    AI,
}
impl TableField {
    fn sort_field(self) -> Option<SortField> {
        Some(match self {
            Self::Authored => SortField::Commits,
            Self::Added => SortField::Added,
            Self::Removed => SortField::Removed,
            Self::Net => SortField::Net,
            Self::Lines => SortField::Total,
            Self::AI => SortField::AI,
            Self::Coauthored => return None,
        })
    }
    fn value(self, author: &AuthorStats) -> String {
        let unallocated = author.commits == 0 && author.coauthored_commits > 0;
        match self {
            Self::Authored => format_number(author.commits),
            Self::Coauthored => format_number(author.coauthored_commits),
            Self::Added => line_value(author.added, author.unknown_line_commits, unallocated),
            Self::Removed => line_value(author.removed, author.unknown_line_commits, unallocated),
            Self::Net => line_value(author.net, author.unknown_line_commits, unallocated),
            Self::Lines => line_value(
                author.total_change,
                author.unknown_line_commits,
                unallocated,
            ),
            Self::AI if author.commits == 0 => "—".into(),
            Self::AI => percent_label(author.ai_commits, author.commits),
        }
    }
}
struct TableColumn {
    field: TableField,
    label: &'static str,
    width: usize,
}
struct TableLayout {
    name: usize,
    rank: usize,
    columns: Vec<TableColumn>,
}
impl TableLayout {
    fn new(width: usize, authors: &[&AuthorStats]) -> Self {
        let compact = width < 70;
        let fields = [
            (
                TableField::Authored,
                if compact { "AUTH" } else { "AUTHORED" },
            ),
            (
                TableField::Coauthored,
                if compact { "CO" } else { "COAUTH" },
            ),
            (TableField::Added, "ADDED"),
            (TableField::Removed, "REMOVED"),
            (TableField::Net, "NET"),
            (
                TableField::Lines,
                if compact { "LINES" } else { "LINES CHANGED" },
            ),
            (
                TableField::AI,
                if width < 110 { "AI%" } else { "DETECTED AI" },
            ),
        ];
        let rank = authors.len().to_string().len().max(2);
        let mut columns: Vec<_> = fields
            .into_iter()
            .map(|(field, label)| {
                let values = authors
                    .iter()
                    .map(|author| field.value(author).width())
                    .max()
                    .unwrap_or(0);
                // Every sortable heading reserves its arrow before allocating cells.
                TableColumn {
                    field,
                    label,
                    width: values.max(label.width() + 1),
                }
            })
            .collect();
        let fixed =
            |columns: &[TableColumn]| 4 + rank + columns.iter().map(|c| c.width + 2).sum::<usize>();
        let min_name = if width >= 70 { 16 } else { 12 };
        for remove in [
            TableField::Added,
            TableField::Removed,
            TableField::Net,
            TableField::AI,
            TableField::Coauthored,
            TableField::Lines,
            TableField::Authored,
        ] {
            if fixed(&columns) + min_name <= width {
                break;
            }
            columns.retain(|c| c.field != remove);
        }
        Self {
            name: width.saturating_sub(fixed(&columns)),
            rank,
            columns,
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
