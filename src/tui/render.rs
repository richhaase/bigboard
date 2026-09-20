use super::components::*;
use super::{App, View};
use crate::{
    model::CommitRecord,
    stats::{self, AuthorStats, SortField},
};
use chrono::{DateTime, Datelike, Duration, Local, Months, NaiveDate, TimeZone};
use ratatui::text::{Line, Span};
use std::collections::{BTreeMap, HashMap};

impl App {
    pub(super) fn lines(&self) -> Vec<UiLine> {
        if self.loading {
            return self.loading_lines();
        }
        if let Some(error) = &self.error {
            let mut lines = banner(self.width as usize, self.height < 25, &self.palette);
            lines.extend([
                blank(),
                text_line("  ◈ ERROR", self.palette.red),
                blank(),
                text_line(format!("  {}", display_text(error)), self.palette.red),
                blank(),
                help(&[("q", "quit".into())], &self.palette),
            ]);
            return lines;
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

    fn aggregate_above(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let very_short = self.height < 20;
        let mut lines = banner(width, self.height < 30, p);
        if !very_short {
            lines.push(blank());
        }
        lines.push(footer(
            self.loaded_repos.len(),
            self.excluded_count(),
            width,
            &self.version,
            p,
        ));
        if !very_short {
            lines.push(rule(width, p));
        }
        if !self.failed_repos.is_empty() {
            let names = self
                .failed_repos
                .iter()
                .take(3)
                .map(|n| display_text(n))
                .collect::<Vec<_>>()
                .join(", ");
            let suffix = if self.failed_repos.len() > 3 {
                format!(", +{} more", self.failed_repos.len() - 3)
            } else {
                String::new()
            };
            lines.push(text_line(
                truncate(
                    &format!(
                        "  ⚠ {} repo(s) unreadable: {names}{suffix}",
                        self.failed_repos.len()
                    ),
                    width,
                ),
                p.amber,
            ));
        }
        if !very_short {
            lines.push(blank());
        }
        lines.push(time_picker(self.time_index, p));
        if !very_short {
            lines.push(blank());
        }
        lines.extend(stat_boxes(self.totals(), width, self.height < 30, p));
        if !very_short {
            lines.push(blank());
        }
        lines
    }

    fn aggregate_help(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let bindings = [
            ("↑↓", "nav".into()),
            ("↵", "detail".into()),
            ("←→", "time".into()),
            ("/", "find".into()),
            (
                "s",
                format!("sort:{}", self.sort_field.label().to_lowercase()),
            ),
            (
                "b",
                format!("bots:{}", if self.hide_bots { "off" } else { "on" }),
            ),
            ("r", "repos".into()),
            ("R", "refresh".into()),
            ("q", "quit".into()),
        ];
        let full = help(&bindings, p);
        if full.width() <= self.width as usize {
            vec![full]
        } else {
            vec![help(&bindings[..4], p), help(&bindings[4..], p)]
        }
    }

    pub(super) fn table_viewport(&self) -> usize {
        let header_rows = if self.height < 20 { 2 } else { 4 };
        (self.height as usize)
            .saturating_sub(
                self.aggregate_above().len() + header_rows + 1 + 1 + self.aggregate_help().len(),
            )
            .max(1)
    }

    fn aggregate_lines(&self) -> Vec<UiLine> {
        let mut lines = self.aggregate_above();
        lines.extend(self.table_lines());
        lines.push(blank());
        lines.extend(self.aggregate_help());
        lines
    }

    pub(super) fn table_lines(&self) -> Vec<UiLine> {
        let authors = self.displayed_authors();
        let p = &self.palette;
        if authors.is_empty() {
            return vec![text_line(
                if self.searching || !self.filter_query.is_empty() {
                    format!(
                        "  ◈ NO MATCH for {:?} — esc to clear filter.",
                        display_text(&self.filter_query)
                    )
                } else {
                    "  ◈ NO SIGNAL — no commit data in range. Widen the time range with ←/→.".into()
                },
                p.amber,
            )];
        }
        let layout = TableLayout::new(self.width as usize);
        let arrow = if self.sort_ascending { " ▲" } else { " ▼" };
        let label = |s: &str, field| {
            if self.sort_field == field {
                format!("{s}{arrow}")
            } else {
                s.to_owned()
            }
        };
        let mut cells = vec![
            "  # ".to_string(),
            pad_right("CONTRIBUTOR", layout.name),
            pad_left(&label("COMMITS", SortField::Commits), layout.num),
        ];
        if layout.added_removed {
            cells.extend([
                pad_left(&label("ADDED", SortField::Added), layout.num),
                pad_left(&label("REMOVED", SortField::Removed), layout.num),
            ]);
        }
        cells.extend([
            pad_left(&label("NET", SortField::Net), layout.num),
            pad_left(&label("AI%", SortField::AI), 5),
        ]);
        if layout.bar > 0 {
            cells.push(pad_right(
                &label("+/- IMPACT", SortField::Total),
                layout.bar,
            ));
        }
        let mut lines = if self.height < 20 {
            vec![]
        } else {
            vec![rule(self.width as usize, p), blank()]
        };
        lines.extend([
            Line::from(bold(cells.join(" "), p.cyan)),
            text_line(format!("  {}", "━".repeat(layout.row_width())), p.dim_cyan),
        ]);
        let maximum = authors.iter().map(|a| a.total_change).max().unwrap_or(0);
        let start = self.offset.min(authors.len());
        let end = (start + self.table_viewport()).min(authors.len());
        for (i, a) in authors.iter().enumerate().take(end).skip(start) {
            let rank = if self.sort_ascending {
                authors.len() - i
            } else {
                i + 1
            };
            let rank_color = match rank {
                1 => p.gold,
                2 => p.silver,
                3 => p.bronze,
                _ => p.dim_cyan,
            };
            let mut row = vec![
                bold(if i == self.selected { "▸ " } else { "  " }, p.cyan),
                bold(format!("{rank:02}"), rank_color),
                Span::raw(" "),
            ];
            if a.bot {
                let name_width = layout.name.saturating_sub(4);
                row.extend([
                    span(
                        pad_right(&truncate(&a.name, name_width), name_width),
                        p.bright,
                    ),
                    span(" BOT", p.dim_cyan),
                ]);
            } else {
                row.push(span(
                    pad_right(&truncate(&a.name, layout.name), layout.name),
                    p.bright,
                ));
            }
            append_number(&mut row, a.commits, layout.num, p.green);
            if layout.added_removed {
                append_number(&mut row, a.added, layout.num, p.green);
                append_number(&mut row, a.removed, layout.num, p.green);
            }
            append_number(
                &mut row,
                a.net,
                layout.num,
                if a.net < 0 { p.red } else { p.green },
            );
            row.push(span(
                format!(
                    " {}",
                    pad_left(
                        &if a.ai_commits > 0 {
                            percent_label(a.ai_commits, a.commits)
                        } else {
                            String::new()
                        },
                        5
                    )
                ),
                p.amber,
            ));
            if layout.bar > 0 {
                row.push(Span::raw(" "));
                row.extend(impact_bar(a.added, a.removed, maximum, layout.bar, p));
            }
            lines.push(Line::from(row).style(p.row(i == self.selected, i)));
        }
        let status = if self.searching {
            Line::from(vec![
                span("  /", p.cyan),
                span(display_text(&self.filter_query), p.bright),
                bold("▌", p.cyan),
                span("   (enter to apply · esc to clear)", p.dim_white),
            ])
        } else {
            let range = format!("showing {}–{end} of {}", start + 1, authors.len());
            text_line(
                if self.filter_query.is_empty() {
                    format!("  {range}")
                } else {
                    format!(
                        "  filter {:?} · {range} · esc clears",
                        display_text(&self.filter_query)
                    )
                },
                p.dim_cyan,
            )
        };
        lines.push(status);
        lines
    }

    fn overlay_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let mut lines = banner(self.width as usize, self.height < 30, p);
        lines.extend([
            blank(),
            section("REPOSITORY CONTROL", self.width as usize, p),
            blank(),
        ]);
        let rows = (self.height as usize)
            .saturating_sub(lines.len() + 5)
            .max(1);
        // Retain repository order while keeping the selected checkbox on screen.
        let start = self.overlay_cursor.saturating_add(1).saturating_sub(rows);
        for (i, repo) in self.loaded_repos.iter().enumerate().skip(start).take(rows) {
            let excluded = self.overlay_excluded.contains(&repo.id);
            let color = if excluded { p.dim_white } else { p.cyan };
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
                    span(if excluded { "[ ] " } else { "[x] " }, color),
                    span(
                        display_text(&repo.name),
                        if excluded { p.dim_white } else { p.bright },
                    ),
                ])
                .style(p.row(i == self.overlay_cursor, i)),
            );
        }
        let excluded = self
            .loaded_repos
            .iter()
            .filter(|r| self.overlay_excluded.contains(&r.id))
            .count();
        lines.extend([
            blank(),
            text_line(repo_count(self.loaded_repos.len(), excluded), p.dim_cyan),
            blank(),
            help(
                &[
                    ("space", "toggle".into()),
                    ("enter/esc", "done".into()),
                    ("↑↓", "navigate".into()),
                ],
                p,
            ),
        ]);
        lines
    }

    pub(super) fn detail_lines(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let author = self
            .authors
            .iter()
            .find(|a| a.name == self.active_operative);
        let records = self.filtered_records();
        let records = author_records(
            &records,
            author,
            &self.active_operative,
            self.options.fuzzy_matching,
        );
        let mut lines = banner(width, false, p);
        lines.extend([
            blank(),
            footer(self.loaded_repos.len(), self.excluded_count(), width, "", p),
            time_picker(self.time_index, p),
            blank(),
            section(
                &format!("CONTRIBUTOR: {}", self.active_operative.to_uppercase()),
                width,
                p,
            ),
        ]);
        if author.is_none() && records.is_empty() {
            lines.extend([
                blank(),
                text_line(
                    "  ◈ NO SIGNAL — no commit data in range. Widen the time range with ←/→.",
                    p.amber,
                ),
            ]);
        } else {
            if let Some(author) = author {
                lines.push(blank());
                lines.extend(stat_boxes(
                    (
                        author.commits,
                        author.added,
                        author.removed,
                        author.ai_commits,
                    ),
                    width,
                    false,
                    p,
                ));
                lines.extend([blank(), metrics(author, p)]);
                if !author.per_repo.is_empty() {
                    lines.extend([blank(), section("REPO CONTRIBUTIONS", width, p), blank()]);
                    lines.extend(repo_breakdown(author, width, p));
                }
            }
            if !records.is_empty() {
                lines.extend([blank(), section("ACTIVITY TIMELINE", width, p), blank()]);
                lines.extend(timeline(&records, width, p));
                lines.extend([blank(), section("ACTIVITY MATRIX", width, p), blank()]);
                lines.extend(heatmap(&records, width, Local::now(), p));
            }
        }
        lines.extend([
            blank(),
            help(
                &[
                    ("↑↓", "prev/next".into()),
                    ("esc", "back".into()),
                    ("←→", "time".into()),
                    ("q", "quit".into()),
                ],
                p,
            ),
        ]);
        lines
    }
}

struct TableLayout {
    name: usize,
    num: usize,
    bar: usize,
    added_removed: bool,
}
impl TableLayout {
    fn new(width: usize) -> Self {
        let available = width.saturating_sub(4);
        if width >= 96 {
            Self {
                name: 22,
                num: 10,
                bar: available.saturating_sub(78).clamp(10, 16),
                added_removed: true,
            }
        } else if width >= 78 {
            Self {
                name: 22,
                num: 10,
                bar: available.saturating_sub(56).clamp(8, 16),
                added_removed: false,
            }
        } else {
            let num = if width < 55 { 7 } else { 10 };
            Self {
                name: available.saturating_sub(13 + num * 2).clamp(8, 22),
                num,
                bar: 0,
                added_removed: false,
            }
        }
    }
    fn row_width(&self) -> usize {
        5 + self.name
            + 1
            + self.num
            + if self.added_removed {
                2 * (1 + self.num)
            } else {
                0
            }
            + 1
            + self.num
            + 1
            + 5
            + if self.bar > 0 { 1 + self.bar } else { 0 }
    }
}
fn append_number(row: &mut Vec<UiSpan>, value: i64, width: usize, color: ratatui::style::Color) {
    row.push(span(
        format!(" {}", pad_left(&format_number(value), width)),
        color,
    ));
}

fn metrics(author: &AuthorStats, p: &Palette) -> UiLine {
    let mut parts = Vec::new();
    if author.active_days > 0 {
        parts.push(format!("ACTIVE {} days", author.active_days));
    }
    if author.first_commit.year() != 1 {
        parts.extend([
            format!("FIRST {}", author.first_commit.format("%Y-%m-%d")),
            format!("LAST {}", author.last_commit.format("%Y-%m-%d")),
        ]);
    }
    parts.push(format!("CHURN {:.2}", author.churn_ratio()));
    if author.ai_commits > 0 {
        parts.push(format!(
            "AI {}",
            percent_label(author.ai_commits, author.commits)
        ));
    }
    text_line(format!("  {}", parts.join("  ·  ")), p.dim_cyan)
}

fn repo_breakdown(author: &AuthorStats, width: usize, p: &Palette) -> Vec<UiLine> {
    let mut repos: Vec<_> = author.per_repo.iter().collect();
    repos.sort_by(|a, b| {
        b.1.total_change
            .cmp(&a.1.total_change)
            .then_with(|| a.0.cmp(b.0))
    });
    let maximum = repos.iter().map(|(_, r)| r.total_change).max().unwrap_or(0);
    let mut lines = vec![
        Line::from(bold(
            format!(
                "  {:30} {:>10} {:>10} {:>10} {:>10}",
                "REPO", "COMMITS", "ADDED", "REMOVED", "NET"
            ),
            p.cyan,
        )),
        rule(width, p),
    ];
    for (i, (name, repo)) in repos.into_iter().enumerate() {
        let mut row = vec![span(
            format!("  {}", pad_right(&truncate(name, 30), 30)),
            p.magenta,
        )];
        for val in [repo.commits, repo.added, repo.removed] {
            append_number(&mut row, val, 10, p.green);
        }
        append_number(
            &mut row,
            repo.net,
            10,
            if repo.net < 0 { p.red } else { p.green },
        );
        row.push(Span::raw("  "));
        row.extend(impact_bar(repo.added, repo.removed, maximum, 15, p));
        if repo.ai_commits > 0 && repo.commits > 0 {
            row.push(span(
                format!("  ai {}", percent_label(repo.ai_commits, repo.commits)),
                p.amber,
            ));
        }
        lines.push(Line::from(row).style(p.row(false, i)));
    }
    lines
}

pub(super) fn author_records<'a>(
    records: &'a [CommitRecord],
    author: Option<&AuthorStats>,
    name: &str,
    fuzzy: bool,
) -> Vec<&'a CommitRecord> {
    records
        .iter()
        .filter(|r| {
            if let Some(a) = author.filter(|a| !a.aliases.is_empty()) {
                a.aliases.contains(&r.author)
            } else {
                stats::names_match(&r.author, name, fuzzy)
            }
        })
        .collect()
}

#[derive(Debug, Default)]
pub(super) struct MonthActivity {
    pub month: NaiveDate,
    pub commits: i64,
    pub added: i64,
    pub removed: i64,
    pub ai: i64,
}
pub(super) fn aggregate_months(records: &[&CommitRecord]) -> Vec<MonthActivity> {
    let mut months: BTreeMap<NaiveDate, MonthActivity> = BTreeMap::new();
    for record in records {
        if let Some(date) = NaiveDate::from_ymd_opt(record.date.year(), record.date.month(), 1) {
            let m = months.entry(date).or_insert_with(|| MonthActivity {
                month: date,
                ..Default::default()
            });
            m.commits += 1;
            m.added += record.added;
            m.removed += record.removed;
            m.ai += i64::from(record.ai_assisted);
        }
    }
    let Some((&first, _)) = months.first_key_value() else {
        return vec![];
    };
    let last = *months.last_key_value().expect("nonempty months").0;
    let mut date = first;
    let mut result = Vec::new();
    loop {
        result.push(months.remove(&date).unwrap_or_else(|| MonthActivity {
            month: date,
            ..Default::default()
        }));
        if date >= last {
            break;
        }
        let Some(next) = date.checked_add_months(Months::new(1)) else {
            break;
        };
        date = next;
    }
    result
}
fn timeline(records: &[&CommitRecord], width: usize, p: &Palette) -> Vec<UiLine> {
    let months = aggregate_months(records);
    let months = &months[months.len().saturating_sub(12)..];
    let maximum = months
        .iter()
        .map(|m| m.added + m.removed)
        .max()
        .unwrap_or(0);
    let bar_width = width.saturating_sub(22).clamp(10, 60);
    months
        .iter()
        .map(|m| {
            let mut spans = vec![
                span(
                    format!("  {:10}", m.month.format("%b %Y").to_string()),
                    p.dim_white,
                ),
                span(format!("{:4} ", m.commits), p.green),
            ];
            spans.extend(impact_bar(m.added, m.removed, maximum, bar_width, p));
            if m.ai > 0 {
                spans.push(span(format!(" ◆{}", m.ai), p.amber));
            }
            Line::from(spans)
        })
        .collect()
}

pub(super) fn heatmap<Tz: TimeZone>(
    records: &[&CommitRecord],
    width: usize,
    now: DateTime<Tz>,
    p: &Palette,
) -> Vec<UiLine> {
    // Match the original: group by the commit's own date, then place cells using
    // the viewer's calendar. Timezone normalization belongs to the later audit fixes.
    let mut totals = HashMap::<NaiveDate, i64>::new();
    let mut maximum = 0;
    for r in records {
        let total = totals.entry(r.date.date_naive()).or_default();
        *total += r.added + r.removed;
        maximum = maximum.max(*total);
    }
    maximum = maximum.max(1);
    let weeks = width.saturating_sub(10).clamp(12, 53);
    let today = now.date_naive();
    let first_col = today - Duration::days(7 * (weeks as i64 - 1));
    let start = first_col - Duration::days(first_col.weekday().num_days_from_sunday() as i64);
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
        let mut spans = vec![span(format!("  {label:4}"), p.dim_white)];
        for col in 0..weeks {
            let date = start + Duration::days((col * 7 + wd) as i64);
            if date > today {
                spans.push(Span::raw(" "));
                continue;
            }
            let val = totals.get(&date).copied().unwrap_or(0);
            let level = if val > 0 {
                (1 + (val * 3) / maximum).min(4) as usize
            } else {
                0
            };
            spans.push(span(ramp[level].0, ramp[level].1));
        }
        lines.push(Line::from(spans));
    }
    lines.push(blank());
    let mut legend = vec![span("  less ", p.dim_white)];
    legend.extend(ramp.iter().skip(1).map(|(s, c)| span(*s, *c)));
    legend.push(span(" more", p.dim_white));
    lines.push(Line::from(legend));
    lines
}
