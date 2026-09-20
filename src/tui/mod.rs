//! Ratatui presentation and keyboard state, retaining the Go dashboard's behavior.
mod components;
mod render;
#[cfg(test)]
mod tests;
mod theme;

use crate::model::{AnalysisOptions, CommitRecord, Repository};
use crate::scan::{self, ScanResult, ScanSession};
use crate::stats::{self, AggregateOptions, AuthorStats, SortField};
use chrono::Duration;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::{Frame, Terminal, backend::CrosstermBackend};
use std::{collections::HashSet, io, time::Duration as StdDuration};

use components::Palette;

pub const DEFAULT_TIME_INDEX: usize = 2;
pub const TIME_PRESETS: [(&str, i64); 7] = [
    ("1d", 1),
    ("7d", 7),
    ("14d", 14),
    ("30d", 30),
    ("90d", 90),
    ("1y", 365),
    ("ALL", 0),
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum View {
    Aggregate,
    Operative,
    Repositories,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Action {
    None,
    Quit,
    Refresh,
}

struct App {
    all_records: Vec<CommitRecord>,
    authors: Vec<AuthorStats>,
    repositories: Vec<Repository>,
    loaded_repos: Vec<Repository>,
    failed_repos: Vec<String>,
    excluded: HashSet<String>,
    overlay_excluded: HashSet<String>,
    overlay_cursor: usize,
    view: View,
    selected: usize,
    offset: usize,
    filter_query: String,
    searching: bool,
    sort_ascending: bool,
    hide_bots: bool,
    active_operative: String,
    sort_field: SortField,
    time_index: usize,
    version: String,
    width: u16,
    height: u16,
    loading: bool,
    error: Option<String>,
    options: AnalysisOptions,
    pending_records: Vec<CommitRecord>,
    pending_repos: Vec<Repository>,
    pending_failed: Vec<String>,
    pending_remaining: usize,
    boot_lines: Vec<(String, bool)>,
    palette: Palette,
}

impl App {
    fn new(
        repositories: Vec<Repository>,
        initial_sort: SortField,
        excluded: HashSet<String>,
        version: &str,
        initial_time_index: usize,
        options: AnalysisOptions,
        theme: &str,
    ) -> Self {
        let mut normalized = excluded.clone();
        for repo in &repositories {
            let base = repo.path.file_name().unwrap_or_default().to_string_lossy();
            if excluded.contains(&repo.name) || excluded.contains(base.as_ref()) {
                normalized.insert(repo.id.clone());
            }
        }
        for repo in &repositories {
            if repo.name != repo.id {
                normalized.remove(&repo.name);
            }
            let base = repo.path.file_name().unwrap_or_default().to_string_lossy();
            if base != repo.id {
                normalized.remove(base.as_ref());
            }
        }
        let mut app = Self {
            all_records: vec![],
            authors: vec![],
            repositories,
            loaded_repos: vec![],
            failed_repos: vec![],
            excluded: normalized,
            overlay_excluded: HashSet::new(),
            overlay_cursor: 0,
            view: View::Aggregate,
            selected: 0,
            offset: 0,
            filter_query: String::new(),
            searching: false,
            sort_ascending: false,
            hide_bots: false,
            active_operative: String::new(),
            sort_field: initial_sort,
            time_index: if initial_time_index < TIME_PRESETS.len() {
                initial_time_index
            } else {
                DEFAULT_TIME_INDEX
            },
            version: version.into(),
            width: 80,
            height: 24,
            loading: true,
            error: None,
            options,
            pending_records: vec![],
            pending_repos: vec![],
            pending_failed: vec![],
            pending_remaining: 0,
            boot_lines: vec![],
            palette: Palette::for_theme(theme),
        };
        app.reset_pending();
        app
    }

    fn reset_pending(&mut self) {
        self.loading = true;
        self.pending_remaining = self.repositories.len();
        self.pending_records.clear();
        self.pending_repos.clear();
        self.pending_failed.clear();
        self.boot_lines.clear();
        if self.pending_remaining == 0 {
            self.finalize_load();
        }
    }

    fn loaded(&mut self, result: ScanResult) {
        let ok = result.error.is_none();
        self.boot_lines.push((result.repository.name.clone(), ok));
        if ok {
            self.pending_records.extend(result.records);
            self.pending_repos.push(result.repository);
        } else {
            self.pending_failed.push(result.repository.name);
        }
        self.pending_remaining = self.pending_remaining.saturating_sub(1);
        if self.pending_remaining == 0 {
            self.finalize_load();
        }
    }

    fn finalize_load(&mut self) {
        self.pending_repos.sort_by(|a, b| a.name.cmp(&b.name));
        self.pending_failed.sort();
        self.all_records = std::mem::take(&mut self.pending_records);
        self.loaded_repos = std::mem::take(&mut self.pending_repos);
        self.failed_repos = std::mem::take(&mut self.pending_failed);
        self.error = if self.loaded_repos.is_empty() && !self.failed_repos.is_empty() {
            Some(format!(
                "all {} repositories failed to scan",
                self.failed_repos.len()
            ))
        } else {
            None
        };
        self.loading = false;
        self.recompute();
    }

    fn filtered_records(&self) -> Vec<CommitRecord> {
        stats::filter_by_time(
            &stats::filter_by_repo(&self.all_records, &self.excluded),
            Duration::days(TIME_PRESETS[self.time_index].1),
        )
    }

    fn recompute(&mut self) {
        self.authors = stats::aggregate(
            &self.filtered_records(),
            &AggregateOptions {
                fuzzy_matching: self.options.fuzzy_matching,
                bot_identities: self.options.bot_identities.clone(),
            },
        );
        if self.hide_bots {
            self.authors.retain(|a| !a.bot);
        }
        self.sort_authors();
        self.clamp_scroll();
    }

    fn sort_authors(&mut self) {
        stats::sort(&mut self.authors, self.sort_field);
        if self.sort_ascending {
            self.authors.reverse();
        }
    }

    fn displayed_authors(&self) -> Vec<&AuthorStats> {
        let query = self.filter_query.to_lowercase();
        self.authors
            .iter()
            .filter(|a| a.name.to_lowercase().contains(&query))
            .collect()
    }

    fn clamp_scroll(&mut self) {
        let n = self.displayed_authors().len();
        self.selected = self.selected.min(n.saturating_sub(1));
        let visible = self.table_viewport();
        self.offset = self.offset.min(self.selected);
        if self.selected >= self.offset + visible {
            self.offset = self.selected + 1 - visible;
        }
        self.offset = self.offset.min(n.saturating_sub(visible));
    }

    fn step_operative(&mut self, delta: isize) {
        let list = self.displayed_authors();
        if list.is_empty() {
            return;
        }
        // Deliberately retain Go's name-based selection, including its behavior when
        // aggregation changes the preferred name after switching the time range.
        let idx = match list.iter().position(|a| a.name == self.active_operative) {
            Some(idx) => idx.saturating_add_signed(delta),
            None => self.selected,
        }
        .min(list.len() - 1);
        let name = list[idx].name.clone();
        self.active_operative = name;
        self.selected = idx;
        self.clamp_scroll();
    }

    fn key(&mut self, key: KeyEvent) -> Action {
        if key.kind == KeyEventKind::Release {
            return Action::None;
        }
        if key.modifiers.contains(KeyModifiers::CONTROL) && key.code == KeyCode::Char('c') {
            return Action::Quit;
        }
        if self.searching {
            match key.code {
                KeyCode::Enter => self.searching = false,
                KeyCode::Esc => {
                    self.searching = false;
                    self.filter_query.clear();
                }
                KeyCode::Backspace => {
                    self.filter_query.pop();
                }
                KeyCode::Char(c)
                    if !key
                        .modifiers
                        .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT) =>
                {
                    self.filter_query.push(c)
                }
                _ => {}
            }
            self.selected = 0;
            self.offset = 0;
            return Action::None;
        }
        if key
            .modifiers
            .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT)
        {
            return Action::None;
        }
        match key.code {
            KeyCode::Char('q') => {
                if self.view == View::Aggregate && !self.filter_query.is_empty() {
                    self.filter_query.clear();
                    self.clamp_scroll();
                } else {
                    return Action::Quit;
                }
            }
            KeyCode::Char('R') if self.view == View::Aggregate && !self.loading => {
                self.reset_pending();
                return Action::Refresh;
            }
            KeyCode::Char('/') if self.view == View::Aggregate => {
                self.searching = true;
                self.filter_query.clear();
                self.selected = 0;
                self.offset = 0;
            }
            KeyCode::Esc => match self.view {
                View::Repositories => self.close_overlay(),
                View::Operative => {
                    self.view = View::Aggregate;
                    self.active_operative.clear();
                }
                View::Aggregate if !self.filter_query.is_empty() => {
                    self.filter_query.clear();
                    self.clamp_scroll();
                }
                View::Aggregate => return Action::Quit,
            },
            KeyCode::Up | KeyCode::Char('k') => match self.view {
                View::Repositories => self.overlay_cursor = self.overlay_cursor.saturating_sub(1),
                View::Operative => self.step_operative(-1),
                View::Aggregate => {
                    self.selected = self.selected.saturating_sub(1);
                    self.clamp_scroll();
                }
            },
            KeyCode::Down | KeyCode::Char('j') => match self.view {
                View::Repositories => {
                    self.overlay_cursor =
                        (self.overlay_cursor + 1).min(self.loaded_repos.len().saturating_sub(1))
                }
                View::Operative => self.step_operative(1),
                View::Aggregate => {
                    self.selected =
                        (self.selected + 1).min(self.displayed_authors().len().saturating_sub(1));
                    self.clamp_scroll();
                }
            },
            KeyCode::Left | KeyCode::Char('h') if self.view != View::Repositories => {
                if self.time_index > 0 {
                    self.time_index -= 1;
                    self.recompute();
                }
            }
            KeyCode::Right | KeyCode::Char('l') if self.view != View::Repositories => {
                if self.time_index + 1 < TIME_PRESETS.len() {
                    self.time_index += 1;
                    self.recompute();
                }
            }
            KeyCode::Char('s') if self.view == View::Aggregate => {
                self.sort_field = self.sort_field.next();
                self.sort_authors();
                self.selected = 0;
                self.offset = 0;
            }
            KeyCode::Char('S') if self.view == View::Aggregate => {
                self.sort_ascending = !self.sort_ascending;
                self.sort_authors();
                self.selected = 0;
                self.offset = 0;
            }
            KeyCode::Char('b') if self.view == View::Aggregate => {
                self.hide_bots = !self.hide_bots;
                self.recompute();
                self.selected = 0;
                self.offset = 0;
            }
            KeyCode::Char('r') if self.view == View::Aggregate => {
                self.overlay_excluded = self.excluded.clone();
                self.overlay_cursor = 0;
                self.view = View::Repositories;
            }
            KeyCode::Char(' ') if self.view == View::Repositories => {
                if let Some(repo) = self.loaded_repos.get(self.overlay_cursor)
                    && !self.overlay_excluded.remove(&repo.id)
                {
                    self.overlay_excluded.insert(repo.id.clone());
                }
            }
            KeyCode::Enter => match self.view {
                View::Repositories => self.close_overlay(),
                View::Aggregate => {
                    if let Some(author) = self.displayed_authors().get(self.selected) {
                        self.active_operative = author.name.clone();
                        self.view = View::Operative;
                    }
                }
                View::Operative => {}
            },
            _ => {}
        }
        Action::None
    }

    fn close_overlay(&mut self) {
        // Both Enter and Escape apply repository selection in the original app.
        self.excluded = std::mem::take(&mut self.overlay_excluded);
        self.view = View::Aggregate;
        self.recompute();
    }

    fn totals(&self) -> (i64, i64, i64, i64) {
        self.authors.iter().fold((0, 0, 0, 0), |t, a| {
            (
                t.0 + a.commits,
                t.1 + a.added,
                t.2 + a.removed,
                t.3 + a.ai_commits,
            )
        })
    }

    fn excluded_count(&self) -> usize {
        self.loaded_repos
            .iter()
            .filter(|r| self.excluded.contains(&r.id))
            .count()
    }

    fn draw(&mut self, frame: &mut Frame<'_>) {
        self.width = frame.area().width;
        self.height = frame.area().height;
        self.clamp_scroll();
        let mut lines = self.lines();
        // Bubble Tea's renderer retains the final screenful when a view exceeds
        // terminal height. Preserve that behavior for long contributor details.
        let overflow = lines.len().saturating_sub(self.height as usize);
        lines.drain(..overflow);
        frame.render_widget(ratatui::widgets::Paragraph::new(lines), frame.area());
    }
}

/// Restore terminal modes on normal return, errors, and unwinding. The panic hook
/// restores before Rust prints the diagnostic, so it remains readable.
struct TerminalGuard;
impl Drop for TerminalGuard {
    fn drop(&mut self) {
        restore_terminal();
    }
}
fn restore_terminal() {
    let _ = crossterm::terminal::disable_raw_mode();
    let _ = crossterm::execute!(
        io::stdout(),
        crossterm::terminal::LeaveAlternateScreen,
        crossterm::cursor::Show
    );
}

pub fn run(
    repositories: Vec<Repository>,
    initial_sort: SortField,
    excluded: HashSet<String>,
    version: &str,
    initial_time_idx: usize,
    options: AnalysisOptions,
    theme: &str,
) -> anyhow::Result<()> {
    let theme = theme::resolve(theme);
    let mut app = App::new(
        repositories,
        initial_sort,
        excluded,
        version,
        initial_time_idx,
        options,
        theme,
    );
    crossterm::terminal::enable_raw_mode()?;
    let _guard = TerminalGuard;
    crossterm::execute!(io::stdout(), crossterm::terminal::EnterAlternateScreen)?;
    let previous_hook = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        restore_terminal();
        previous_hook(info);
    }));
    let mut terminal = Terminal::new(CrosstermBackend::new(io::stdout()))?;
    terminal.hide_cursor()?;
    let mut session: Option<ScanSession> = if app.loading {
        Some(scan::start_scan(
            app.repositories.clone(),
            app.options.clone(),
        ))
    } else {
        None
    };
    let mut dirty = true;
    loop {
        if let Some(scan) = &session {
            while let Ok(result) = scan.receiver.try_recv() {
                app.loaded(result);
                dirty = true;
            }
        }
        if !app.loading {
            session = None;
        }
        if dirty {
            terminal.draw(|frame| app.draw(frame))?;
            dirty = false;
        }
        if event::poll(StdDuration::from_millis(50))? {
            let event = event::read()?;
            dirty = matches!(event, Event::Key(_) | Event::Resize(_, _));
            match event {
                Event::Key(key) => match app.key(key) {
                    Action::Quit => {
                        if let Some(scan) = &session {
                            scan.cancel();
                        }
                        break;
                    }
                    Action::Refresh => {
                        session = Some(scan::start_scan(
                            app.repositories.clone(),
                            app.options.clone(),
                        ));
                    }
                    Action::None => {}
                },
                Event::Resize(width, height) => {
                    app.width = width;
                    app.height = height;
                    app.clamp_scroll();
                }
                _ => {}
            }
        }
    }
    Ok(())
}
