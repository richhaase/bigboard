//! Ratatui presentation and keyboard state, retaining the Go dashboard's behavior.
mod components;
mod detail;
mod github;
mod merge;
mod render;
mod repositories;
#[cfg(test)]
mod tests;
mod theme;

use crate::identity::IdentityStore;
use crate::model::{AnalysisOptions, CommitRecord, HistoryScope, Repository};
use crate::progress::ScanProgress;
use crate::scan::{self, ScanResult, ScanSession};
use crate::stats::{self, AggregateOptions, AuthorStats, SortField};
use chrono::{DateTime, Duration, FixedOffset, Utc};
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::{Frame, Terminal, backend::CrosstermBackend};
use std::{
    collections::{BTreeMap, HashSet},
    io,
    path::PathBuf,
    time::{Duration as StdDuration, Instant},
};

use components::Palette;
use merge::MergeFlow;

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
    Github,
}

struct App {
    github_source: bool,
    all_records: Vec<CommitRecord>,
    authors: Vec<AuthorStats>,
    contributors: Vec<AuthorStats>,
    identities: IdentityStore,
    identity_path: PathBuf,
    merge: Option<MergeFlow>,
    notice: Option<String>,
    scope: HistoryScope,
    cutoff: DateTime<FixedOffset>,
    warnings: Vec<(String, String)>,
    attribution_warnings: Vec<String>,
    pending_warnings: Vec<(String, String)>,
    repositories: Vec<Repository>,
    loaded_repos: Vec<Repository>,
    failed_repos: Vec<String>,
    failure_details: Vec<(String, String)>,
    excluded: HashSet<String>,
    overlay_excluded: HashSet<String>,
    overlay_cursor: usize,
    repository_diagnostic_offset: usize,
    view: View,
    selected: usize,
    offset: usize,
    detail_offset: usize,
    filter_query: String,
    searching: bool,
    sort_ascending: bool,
    hide_bots: bool,
    active_id: String,
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
    pending_failure_details: Vec<(String, String)>,
    pending_remaining: usize,
    boot_lines: Vec<(String, bool)>,
    scan_progress: BTreeMap<String, ScanProgress>,
    loading_started: Instant,
    loading_tick: u128,
    palette: Palette,
}

impl App {
    #[allow(clippy::too_many_arguments)]
    fn new(
        repositories: Vec<Repository>,
        initial_sort: SortField,
        excluded: HashSet<String>,
        version: &str,
        initial_time_index: usize,
        options: AnalysisOptions,
        theme: &str,
        identities: IdentityStore,
        identity_path: PathBuf,
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
            github_source: false,
            all_records: vec![],
            authors: vec![],
            contributors: vec![],
            identities,
            identity_path,
            merge: None,
            notice: None,
            scope: HistoryScope::Landed,
            cutoff: Utc::now().fixed_offset(),
            warnings: vec![],
            attribution_warnings: vec![],
            pending_warnings: vec![],
            repositories,
            loaded_repos: vec![],
            failed_repos: vec![],
            failure_details: vec![],
            excluded: normalized,
            overlay_excluded: HashSet::new(),
            overlay_cursor: 0,
            repository_diagnostic_offset: 0,
            view: View::Aggregate,
            selected: 0,
            offset: 0,
            detail_offset: 0,
            filter_query: String::new(),
            searching: false,
            sort_ascending: false,
            hide_bots: false,
            active_id: String::new(),
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
            pending_failure_details: vec![],
            pending_remaining: 0,
            boot_lines: vec![],
            scan_progress: BTreeMap::new(),
            loading_started: Instant::now(),
            loading_tick: 0,
            palette: Palette::for_theme(theme),
        };
        app.reset_pending();
        app
    }

    fn reset_pending(&mut self) {
        self.scan_progress.clear();
        self.loading_started = Instant::now();
        self.loading_tick = 0;
        self.loading = true;
        self.pending_remaining = self.repositories.len();
        self.pending_records.clear();
        self.pending_repos.clear();
        self.pending_failed.clear();
        self.pending_failure_details.clear();
        self.pending_warnings.clear();
        self.cutoff = Utc::now().fixed_offset();
        self.boot_lines.clear();
        if self.pending_remaining == 0 {
            self.finalize_load();
        }
    }

    fn loaded(&mut self, result: ScanResult) {
        self.scan_progress.remove(&result.repository.id);
        let ok = result.error.is_none();
        self.pending_warnings
            .extend(result.warnings.into_iter().map(|warning| {
                (
                    result.repository.id.clone(),
                    format!("{}: {warning}", result.repository.name),
                )
            }));
        self.boot_lines.push((result.repository.name.clone(), ok));
        if ok {
            self.pending_records.extend(result.records);
            self.pending_repos.push(result.repository);
        } else {
            if let Some(error) = result.error {
                self.pending_failure_details
                    .push((result.repository.id, error));
            }
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
        self.failure_details = std::mem::take(&mut self.pending_failure_details);
        self.warnings = std::mem::take(&mut self.pending_warnings);
        self.warnings.sort();
        self.error = if self.loaded_repos.is_empty() && !self.failed_repos.is_empty() {
            Some(format!(
                "all {} repositories failed to scan",
                self.failed_repos.len()
            ))
        } else {
            None
        };
        self.loading = false;
        self.rebuild_contributors();
        self.recompute();
    }

    fn aggregate_options(&self) -> AggregateOptions {
        AggregateOptions {
            identities: self.identities.clone(),
            timezone: self.options.timezone,
            bot_identities: self.options.bot_identities.clone(),
        }
    }

    fn filtered_records(&self) -> Vec<CommitRecord> {
        stats::filter_by_time_at(
            &stats::filter_by_scope(
                &stats::filter_by_repo(&self.all_records, &self.excluded),
                self.scope,
            ),
            Duration::days(TIME_PRESETS[self.time_index].1),
            self.cutoff,
        )
    }

    fn rebuild_contributors(&mut self) {
        // Merge choices include every loaded repository and branch, independent
        // of view filters. Future-dated records remain excluded by the same cutoff.
        self.contributors = stats::identity_catalog(
            &stats::filter_by_time_at(&self.all_records, Duration::zero(), self.cutoff),
            &self.aggregate_options(),
        );
        self.contributors
            .sort_by(|a, b| a.name.cmp(&b.name).then_with(|| a.id.cmp(&b.id)));
    }

    fn selected_id(&self) -> Option<String> {
        self.displayed_authors()
            .get(self.selected)
            .map(|a| a.id.clone())
    }

    fn select_id(&mut self, id: &str) {
        let canonical = self.identities.canonical_id(id);
        if let Some(index) = self
            .displayed_authors()
            .iter()
            .position(|a| a.id == canonical)
        {
            self.selected = index;
        }
        self.clamp_scroll();
    }

    fn recompute(&mut self) {
        self.detail_offset = 0;
        let selected_id = self.selected_id();
        let records = self.filtered_records();
        let options = self.aggregate_options();
        self.attribution_warnings = stats::attribution_warnings(&records, &options);
        self.authors = stats::aggregate(&records, &options);
        if self.hide_bots {
            self.authors.retain(|a| !a.bot);
        }
        self.sort_authors();
        if let Some(id) = selected_id {
            self.select_id(&id);
        }
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
        let idx = match list.iter().position(|a| a.id == self.active_id) {
            Some(idx) => idx.saturating_add_signed(delta),
            None => self.selected,
        }
        .min(list.len() - 1);
        let next_id = list[idx].id.clone();
        if self.active_id != next_id {
            self.detail_offset = 0;
        }
        self.active_id = next_id;
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
        if self.loading
            && !key
                .modifiers
                .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT)
        {
            return if matches!(key.code, KeyCode::Char('q') | KeyCode::Esc) {
                Action::Quit
            } else {
                Action::None
            };
        }
        if self.merge.is_some() {
            return self.merge_key(key);
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
            KeyCode::Char('g') if self.view == View::Aggregate && !self.loading => {
                return Action::Github;
            }
            KeyCode::Char('M') if self.view != View::Repositories && !self.loading => {
                self.open_merge();
            }
            KeyCode::Char('B') if self.view != View::Repositories => {
                self.scope = self.scope.toggle();
                self.recompute();
            }
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
                    self.active_id.clear();
                }
                View::Aggregate if !self.filter_query.is_empty() => {
                    self.filter_query.clear();
                    self.clamp_scroll();
                }
                View::Aggregate => return Action::Quit,
            },
            KeyCode::Up | KeyCode::Char('k') => match self.view {
                View::Repositories => self.step_repository(-1),
                View::Operative => self.step_operative(-1),
                View::Aggregate => {
                    self.selected = self.selected.saturating_sub(1);
                    self.clamp_scroll();
                }
            },
            KeyCode::Down | KeyCode::Char('j') => match self.view {
                View::Repositories => self.step_repository(1),
                View::Operative => self.step_operative(1),
                View::Aggregate => {
                    self.selected =
                        (self.selected + 1).min(self.displayed_authors().len().saturating_sub(1));
                    self.clamp_scroll();
                }
            },
            KeyCode::PageUp if self.view == View::Operative => self.page_detail(false),
            KeyCode::PageDown if self.view == View::Operative => self.page_detail(true),
            KeyCode::Home if self.view == View::Operative => self.detail_offset = 0,
            KeyCode::End if self.view == View::Operative => {
                self.detail_offset = self.detail_max_offset();
            }
            KeyCode::PageUp if self.view == View::Repositories => {
                self.page_repository_diagnostics(false);
            }
            KeyCode::PageDown if self.view == View::Repositories => {
                self.page_repository_diagnostics(true);
            }
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
                self.repository_diagnostic_offset = 0;
                self.view = View::Repositories;
            }
            KeyCode::Char(' ') if self.view == View::Repositories => {
                self.toggle_repository();
            }
            KeyCode::Enter => match self.view {
                View::Repositories => self.close_overlay(),
                View::Aggregate => {
                    if let Some(author) = self.displayed_authors().get(self.selected) {
                        self.active_id = author.id.clone();
                        self.view = View::Operative;
                        self.detail_offset = 0;
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

    fn repository_history_unavailable(&self, id: &str) -> bool {
        self.warnings.iter().any(|(repo_id, warning)| {
            repo_id == id && warning.contains("No available default branch could be identified;")
        })
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
        if self.view == View::Operative && self.merge.is_none() {
            self.clamp_detail_scroll();
        }
        let mut lines = self.lines();
        // Detail screens compose a bounded viewport with fixed context and
        // controls. Other legacy screens retain their existing overflow fallback.
        if self.view != View::Operative {
            let overflow = lines.len().saturating_sub(self.height as usize);
            lines.drain(..overflow);
        }
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

#[allow(clippy::too_many_arguments)]
pub fn run(
    repositories: Vec<Repository>,
    initial_sort: SortField,
    excluded: HashSet<String>,
    version: &str,
    initial_time_idx: usize,
    options: AnalysisOptions,
    theme: &str,
    identities: IdentityStore,
    identity_path: PathBuf,
    github_start: bool,
) -> anyhow::Result<()> {
    let theme = theme::resolve(theme);
    let local_repositories = repositories.clone();
    let mut local_excluded = excluded.clone();
    let mut app = App::new(
        repositories,
        initial_sort,
        excluded,
        version,
        initial_time_idx,
        options,
        theme,
        identities,
        identity_path,
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
    let mut open_github = github_start || app.repositories.is_empty();
    let mut source: Option<(crate::github::Client, Vec<crate::github::RemoteRepository>)> = None;
    let mut session: Option<ScanSession> = if app.loading && !open_github {
        Some(scan::start_scan(
            app.repositories.clone(),
            app.options.clone(),
        ))
    } else {
        None
    };
    let mut dirty = true;
    loop {
        if open_github {
            let choice = github::choose(
                &mut terminal,
                app.palette.clone(),
                source.as_ref().map(|(_, repos)| repos.as_slice()),
                !local_repositories.is_empty(),
            )?;
            match choice {
                github::Choice::Quit => break,
                github::Choice::Cancel if app.repositories.is_empty() => break,
                github::Choice::Cancel => {}
                github::Choice::Local => {
                    source = None;
                    app.github_source = false;
                    app.repositories = local_repositories.clone();
                    app.excluded = local_excluded.clone();
                    app.reset_pending();
                }
                github::Choice::Repositories(client, repositories) => {
                    if !app.github_source {
                        local_excluded = app.excluded.clone();
                    }
                    app.github_source = true;
                    app.repositories = repositories.iter().map(|r| client.repository(r)).collect();
                    app.excluded.clear();
                    source = Some((client, repositories));
                    app.reset_pending();
                }
            }
            if app.loading {
                app.filter_query.clear();
                app.selected = 0;
                app.offset = 0;
                app.notice = None;
                session = Some(start_source_scan(&app, &source));
            }
            open_github = false;
            dirty = true;
        }
        if let Some(scan) = &session {
            while let Ok(progress) = scan.progress.try_recv() {
                app.scan_progress
                    .insert(progress.repository_id.clone(), progress);
                dirty = true;
            }
            while let Ok(result) = scan.receiver.try_recv() {
                app.loaded(result);
                dirty = true;
            }
        }
        if !app.loading {
            session = None;
        }
        if app.loading {
            let tick = app.loading_started.elapsed().as_millis() / 100;
            if tick != app.loading_tick {
                app.loading_tick = tick;
                dirty = true;
            }
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
                        session = Some(start_source_scan(&app, &source));
                    }
                    Action::Github => open_github = true,
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

fn start_source_scan(
    app: &App,
    source: &Option<(crate::github::Client, Vec<crate::github::RemoteRepository>)>,
) -> ScanSession {
    match source {
        Some((client, repos)) => {
            scan::start_github_scan(client.clone(), repos.clone(), app.options.clone())
        }
        None => scan::start_scan(app.repositories.clone(), app.options.clone()),
    }
}
