use super::components::*;
use super::{Action, App, View};
use crate::{identity::IdentityStore, stats::AuthorStats};
use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::text::Line;
use unicode_width::UnicodeWidthStr;

pub(super) struct MergeFlow {
    pub source: AuthorStats,
    pub candidates: Vec<AuthorStats>,
    pub target: Option<AuthorStats>,
    pub query: String,
    pub cursor: usize,
    pub name: String,
    pub error: Option<String>,
}
impl MergeFlow {
    fn choices(&self) -> Vec<&AuthorStats> {
        let query = self.query.to_lowercase();
        self.candidates
            .iter()
            .filter(|a| {
                a.name.to_lowercase().contains(&query)
                    || a.emails.iter().any(|e| e.to_lowercase().contains(&query))
                    || a.per_repo.keys().any(|r| r.to_lowercase().contains(&query))
                    || a.aliases.iter().any(|n| n.to_lowercase().contains(&query))
            })
            .collect()
    }
}

impl App {
    pub(super) fn open_merge(&mut self) {
        let id = if self.view == View::Operative {
            Some(self.active_id.clone())
        } else {
            self.selected_id()
        };
        let Some(source) = id
            .and_then(|id| self.contributors.iter().find(|a| a.id == id))
            .cloned()
        else {
            return;
        };
        let candidates = self
            .contributors
            .iter()
            .filter(|a| a.id != source.id)
            .cloned()
            .collect();
        self.merge = Some(MergeFlow {
            name: source.name.clone(),
            source,
            candidates,
            target: None,
            query: String::new(),
            cursor: 0,
            error: None,
        });
        self.notice = None;
    }

    pub(super) fn merge_key(&mut self, key: KeyEvent) -> Action {
        let Some(mut flow) = self.merge.take() else {
            return Action::None;
        };
        if key.code == KeyCode::Esc {
            return Action::None;
        }
        if key.modifiers.contains(KeyModifiers::CONTROL) && key.code == KeyCode::Char('u') {
            if flow.target.is_some() {
                flow.name.clear()
            } else {
                flow.query.clear();
                flow.cursor = 0;
            }
        } else if !key
            .modifiers
            .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT)
        {
            if let Some(target) = &flow.target {
                match key.code {
                    KeyCode::Backspace => {
                        flow.name.pop();
                        flow.error = None;
                    }
                    KeyCode::Char(c) if !c.is_control() => {
                        flow.name.push(c);
                        flow.error = None;
                    }
                    KeyCode::Enter => {
                        if flow.name.trim().is_empty() {
                            flow.error = Some("Choose a nonempty combined display name.".into());
                        } else {
                            match IdentityStore::merge_and_save(
                                &self.identity_path,
                                &flow.source.id,
                                &target.id,
                                flow.name.trim(),
                            ) {
                                Ok((identities, id)) => {
                                    self.identities = identities;
                                    self.invalidate_contributors();
                                    // A saved rename can no longer match the old search text.
                                    // Clear it so the merged identity remains selected and visible.
                                    self.filter_query.clear();
                                    self.recompute();
                                    self.select_id(&id);
                                    if self.view == View::Operative {
                                        self.active_id = id;
                                    }
                                    self.notice = Some(format!(
                                        "Identity mapping saved: {}",
                                        flow.name.trim()
                                    ));
                                    return Action::None;
                                }
                                Err(error) => {
                                    flow.error = Some(format!("Could not save mapping: {error:#}"))
                                }
                            }
                        }
                    }
                    _ => {}
                }
            } else {
                match key.code {
                    KeyCode::Up => flow.cursor = flow.cursor.saturating_sub(1),
                    KeyCode::Down => {
                        flow.cursor = (flow.cursor + 1).min(flow.choices().len().saturating_sub(1))
                    }
                    KeyCode::Backspace => {
                        flow.query.pop();
                        flow.cursor = 0;
                    }
                    KeyCode::Char(c) if !c.is_control() => {
                        flow.query.push(c);
                        flow.cursor = 0;
                    }
                    KeyCode::Enter => {
                        flow.target = flow.choices().get(flow.cursor).map(|a| (*a).clone());
                    }
                    _ => {}
                }
            }
        }
        self.merge = Some(flow);
        Action::None
    }

    pub(super) fn merge_lines(&self, flow: &MergeFlow) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let mut lines = vec![section("MERGE CONTRIBUTOR IDENTITIES", width, p), blank()];
        if let Some(target) = &flow.target {
            lines.extend(preview("Source", &flow.source, width, p));
            lines.extend(preview("Target", target, width, p));
            lines.push(blank());
            lines.extend(wrapped(
                "  These identities will be combined across all repositories and future sessions.",
                width,
                p.dim_cyan,
            ));
            lines.push(blank());
            lines.push(text_line("  Combined display name", p.cyan));
            lines.push(Line::from(vec![
                span("  ", p.bright),
                span(display_text(&flow.name), p.bright),
                bold("▌", p.cyan),
            ]));
            if let Some(error) = &flow.error {
                lines.extend(wrapped(&format!("  ⚠ {error}"), width, p.red));
            }
            lines.push(blank());
            lines.extend(wrap_help(
                &[
                    ("enter", "save merge".into()),
                    ("ctrl+u", "clear name".into()),
                    ("esc", "cancel".into()),
                ],
                width,
                p,
            ));
        } else {
            lines.extend(preview("Merge", &flow.source, width, p));
            lines.extend(wrapped("  Select another contributor. Includes all loaded repositories, branches and dates.",width,p.dim_cyan));
            lines.push(Line::from(vec![
                span("  Find: ", p.cyan),
                span(display_text(&flow.query), p.bright),
                bold("▌", p.cyan),
            ]));
            lines.push(blank());
            let choices = flow.choices();
            let available = (self.height as usize)
                .saturating_sub(lines.len() + 4)
                .max(2);
            let visible = (available / 2).max(1);
            let start = flow.cursor.saturating_add(1).saturating_sub(visible);
            for (index, author) in choices.iter().enumerate().skip(start).take(visible) {
                let selected = index == flow.cursor;
                lines.push(
                    text_line(
                        format!(
                            "  {}{}",
                            if selected { "▸ " } else { "  " },
                            display_text(&author.name)
                        ),
                        p.bright,
                    )
                    .style(p.row(selected, index)),
                );
                lines.push(
                    text_line(
                        truncate(
                            &format!("    {} · {}", email_list(author), repo_list(author)),
                            width,
                        ),
                        p.dim_cyan,
                    )
                    .style(p.row(selected, index)),
                );
            }
            if choices.is_empty() {
                lines.push(text_line("  No other contributors match.", p.amber));
            }
            lines.push(blank());
            lines.extend(wrap_help(
                &[
                    ("↑↓", "choose".into()),
                    ("enter", "set display name".into()),
                    ("esc", "cancel".into()),
                ],
                width,
                p,
            ));
        }
        lines
    }
}

fn email_list(author: &AuthorStats) -> String {
    let mut emails: Vec<_> = author.emails.iter().map(|s| display_text(s)).collect();
    emails.sort();
    if emails.is_empty() {
        "no email; repository-local identity".into()
    } else {
        emails.join(", ")
    }
}
fn repo_list(author: &AuthorStats) -> String {
    author
        .per_repo
        .keys()
        .map(|r| display_text(r))
        .collect::<Vec<_>>()
        .join(", ")
}
fn preview(label: &str, author: &AuthorStats, width: usize, p: &Palette) -> Vec<UiLine> {
    let mut lines = wrapped(
        &format!(
            "  {label}: {} ({} identities)",
            display_text(&author.name),
            author.member_ids.len()
        ),
        width,
        p.cyan,
    );
    lines.extend(wrapped(
        &format!("    {}", email_list(author)),
        width,
        p.bright,
    ));
    lines.extend(wrapped(
        &format!("    Repositories: {}", repo_list(author)),
        width,
        p.dim_cyan,
    ));
    lines
}

pub(super) fn wrapped(text: &str, width: usize, color: ratatui::style::Color) -> Vec<UiLine> {
    let text = display_text(text);
    if width == 0 {
        return vec![blank()];
    }
    let mut lines = Vec::new();
    let mut current = String::new();
    for c in text.chars() {
        let mut next = current.clone();
        next.push(c);
        if next.width() > width && !current.is_empty() {
            lines.push(text_line(std::mem::take(&mut current), color));
        }
        current.push(c);
    }
    lines.push(text_line(current, color));
    lines
}

pub(super) fn wrap_help(bindings: &[(&str, String)], width: usize, p: &Palette) -> Vec<UiLine> {
    let mut lines = Vec::new();
    let mut start = 0;
    for end in 1..=bindings.len() {
        if help(&bindings[start..end], p).width() > width && end > start + 1 {
            lines.push(help(&bindings[start..end - 1], p));
            start = end - 1;
        }
    }
    if start < bindings.len() {
        lines.push(help(&bindings[start..], p));
    }
    lines
}
