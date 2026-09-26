//! GitHub repository selection, kept responsive while discovery runs.
use super::{components::*, merge::wrap_help};
use crate::github::{Catalog, Client, Discovery, RemoteRepository};
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::{Terminal, backend::CrosstermBackend, text::Line, widgets::Paragraph};
use std::{collections::HashSet, io, time::Duration};

pub(super) enum Choice {
    Repositories(Client, Vec<RemoteRepository>),
    Local,
    Cancel,
    Quit,
}
#[derive(Debug, PartialEq, Eq)]
enum PickerAction {
    None,
    Apply,
    Refresh,
    Local,
    Cancel,
    Quit,
}

struct Picker {
    client: Option<Client>,
    login: String,
    repositories: Vec<RemoteRepository>,
    selected: HashSet<u64>,
    query: String,
    searching: bool,
    owner: Option<String>,
    cursor: usize,
    offset: usize,
    loading: bool,
    error: Option<String>,
    palette: Palette,
}
impl Picker {
    fn new(palette: Palette, selected: HashSet<u64>) -> Self {
        Self {
            client: None,
            login: String::new(),
            repositories: vec![],
            selected,
            query: String::new(),
            searching: false,
            owner: None,
            cursor: 0,
            offset: 0,
            loading: true,
            error: None,
            palette,
        }
    }
    fn visible(&self) -> Vec<usize> {
        let query = self.query.to_lowercase();
        self.repositories
            .iter()
            .enumerate()
            .filter_map(|(index, repo)| {
                (self
                    .owner
                    .as_deref()
                    .is_none_or(|owner| repo.owner() == owner)
                    && repo.full_name.to_lowercase().contains(&query))
                .then_some(index)
            })
            .collect()
    }
    fn ready(&mut self, catalog: Catalog) {
        self.login = catalog.login;
        self.repositories = catalog.repositories;
        let available: HashSet<_> = self.repositories.iter().map(|repo| repo.id).collect();
        self.selected.retain(|id| available.contains(id));
        self.loading = false;
        self.cursor = 0;
        self.offset = 0;
    }
    fn key(&mut self, key: KeyEvent, can_local: bool, page: usize) -> PickerAction {
        if key.kind == KeyEventKind::Release {
            return PickerAction::None;
        }
        if key.modifiers.contains(KeyModifiers::CONTROL) && key.code == KeyCode::Char('c') {
            return PickerAction::Quit;
        }
        if key
            .modifiers
            .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT)
        {
            return PickerAction::None;
        }
        if self.searching {
            match key.code {
                KeyCode::Esc => {
                    self.query.clear();
                    self.searching = false;
                }
                KeyCode::Enter => self.searching = false,
                KeyCode::Backspace => {
                    self.query.pop();
                }
                KeyCode::Char(c) => self.query.push(c),
                _ => {}
            }
            self.cursor = 0;
            self.offset = 0;
            return PickerAction::None;
        }
        match key.code {
            KeyCode::Char('q') => return PickerAction::Quit,
            KeyCode::Esc => return PickerAction::Cancel,
            KeyCode::Char('l') if can_local => return PickerAction::Local,
            KeyCode::Char('R') => return PickerAction::Refresh,
            _ if self.loading => return PickerAction::None,
            KeyCode::Char('/') => {
                self.searching = true;
                self.query.clear();
                self.cursor = 0;
            }
            KeyCode::Up | KeyCode::Char('k') => self.cursor = self.cursor.saturating_sub(1),
            KeyCode::Down | KeyCode::Char('j') => {
                self.cursor = (self.cursor + 1).min(self.visible().len().saturating_sub(1))
            }
            KeyCode::PageUp => self.cursor = self.cursor.saturating_sub(page),
            KeyCode::PageDown => {
                self.cursor = (self.cursor + page).min(self.visible().len().saturating_sub(1))
            }
            KeyCode::Home => self.cursor = 0,
            KeyCode::End => self.cursor = self.visible().len().saturating_sub(1),
            KeyCode::Char(' ') => {
                if let Some(&index) = self.visible().get(self.cursor) {
                    let id = self.repositories[index].id;
                    if !self.selected.remove(&id) {
                        self.selected.insert(id);
                    }
                }
            }
            KeyCode::Char('a') => {
                let ids: Vec<_> = self
                    .visible()
                    .into_iter()
                    .map(|i| self.repositories[i].id)
                    .collect();
                let all_selected = ids.iter().all(|id| self.selected.contains(id));
                for id in ids {
                    if all_selected {
                        self.selected.remove(&id);
                    } else {
                        self.selected.insert(id);
                    }
                }
            }
            KeyCode::Char('o') => {
                let mut owners: Vec<_> = self
                    .repositories
                    .iter()
                    .map(|repo| repo.owner().to_owned())
                    .collect();
                owners.sort();
                owners.dedup();
                self.owner = match self
                    .owner
                    .as_ref()
                    .and_then(|owner| owners.iter().position(|o| o == owner))
                {
                    Some(index) => owners.get(index + 1).cloned(),
                    None => owners.first().cloned(),
                };
                self.cursor = 0;
                self.offset = 0;
            }
            KeyCode::Enter => {
                if !self.selected.is_empty() {
                    return PickerAction::Apply;
                }
                self.error =
                    Some("Select at least one repository with Space, then press Enter".into());
            }
            _ => {}
        }
        PickerAction::None
    }
    fn lines(&mut self, width: usize, height: usize, can_local: bool) -> Vec<UiLine> {
        let p = &self.palette;
        if height < 12 || width < 50 {
            return vec![
                text_line("  GITHUB // resize to at least 50×12", p.cyan),
                text_line("  Esc back · q quit", p.dim_white),
            ];
        }
        let mut bindings = vec![
            ("↑↓", "select".into()),
            ("space", "toggle".into()),
            ("enter", "analyze".into()),
            ("/", "find".into()),
            ("o", "owner".into()),
            ("a", "toggle visible".into()),
            ("R", "reload list".into()),
        ];
        if can_local {
            bindings.push(("l", "local repos".into()));
        }
        bindings.extend([("esc", "back".into()), ("q", "quit".into())]);
        let help = wrap_help(&bindings, width, p);
        let mut lines = banner(width, true, p);
        lines.push(blank());
        let account = self.client.as_ref().map_or_else(
            || "GitHub".into(),
            |c| {
                format!(
                    "{} · {}",
                    c.host,
                    if self.login.is_empty() {
                        "connecting"
                    } else {
                        &self.login
                    }
                )
            },
        );
        lines.push(text_line(
            truncate(&format!("  GITHUB // {}", display_text(&account)), width),
            p.cyan,
        ));
        lines.push(text_line(
            truncate(
                &format!(
                    "  Owner: {} · {} selected · {}{}",
                    self.owner.as_deref().unwrap_or("ALL"),
                    self.selected.len(),
                    if self.searching { "Find: " } else { "Filter: " },
                    display_text(&self.query)
                ),
                width,
            ),
            p.dim_white,
        ));
        lines.push(panel_header("REPOSITORIES", width, p));
        let visible = self.visible();
        let help_gap = usize::from(height >= 16);
        let row_count = height
            .saturating_sub(lines.len() + help.len() + 2 + help_gap)
            .max(1);
        self.cursor = self.cursor.min(visible.len().saturating_sub(1));
        self.offset = self.offset.min(self.cursor);
        if self.cursor >= self.offset + row_count {
            self.offset = self.cursor + 1 - row_count;
        }
        self.offset = self.offset.min(visible.len().saturating_sub(row_count));
        if self.loading {
            lines.push(text_line(
                "  Discovering accessible repositories…",
                p.dim_cyan,
            ));
        } else if visible.is_empty() {
            lines.push(text_line("  No matching repositories", p.dim_white));
        } else {
            for (position, &index) in visible.iter().enumerate().skip(self.offset).take(row_count) {
                let repo = &self.repositories[index];
                let marker = if self.selected.contains(&repo.id) {
                    "x"
                } else {
                    " "
                };
                let tags = format!(
                    "{}{}{}",
                    if repo.private { "  PRIVATE" } else { "" },
                    if repo.archived { "  ARCHIVED" } else { "" },
                    if repo.fork { "  FORK" } else { "" }
                );
                let label = truncate(
                    &format!("  [{}] {}{}", marker, display_text(&repo.full_name), tags),
                    width,
                );
                lines.push(Line::from(label).style(p.row(position == self.cursor, position)));
            }
        }
        lines.push(panel_footer(width, p));
        let status = self.error.as_deref().map_or_else(
            || "API summaries for the selected range · no repositories cloned.".into(),
            display_text,
        );
        lines.push(text_line(
            truncate(&format!("  {status}"), width),
            if self.error.is_some() {
                p.amber
            } else {
                p.dim_cyan
            },
        ));
        if help_gap > 0 {
            lines.push(blank());
        }
        lines.extend(help);
        lines
    }
}

pub(super) fn choose(
    terminal: &mut Terminal<CrosstermBackend<io::Stdout>>,
    palette: Palette,
    current: Option<&[RemoteRepository]>,
    can_local: bool,
) -> anyhow::Result<Choice> {
    let mut picker = Picker::new(
        palette,
        current.unwrap_or_default().iter().map(|r| r.id).collect(),
    );
    let mut discovery = None;
    let mut restart = true;
    let mut first = true;
    let mut page = 1;
    let mut dirty = true;
    loop {
        if restart {
            discovery = None;
            picker.error = None;
            picker.loading = true;
            match Client::from_env() {
                Ok(client) => {
                    discovery = Some(Discovery::start(client.clone()));
                    picker.client = Some(client);
                }
                Err(error) => {
                    picker.error = Some(format!("{error:#}"));
                    picker.loading = false;
                }
            }
            restart = false;
            dirty = true;
        }
        if let Some(worker) = &discovery {
            match worker.receiver.try_recv() {
                Ok(Ok(catalog)) => {
                    if first && current.is_none() {
                        match picker
                            .client
                            .as_ref()
                            .unwrap()
                            .saved_selection(&catalog.login)
                        {
                            Ok(selected) => picker.selected = selected,
                            Err(error) => picker.error = Some(format!("{error:#}")),
                        }
                    }
                    picker.ready(catalog);
                    first = false;
                    discovery = None;
                    dirty = true;
                }
                Ok(Err(error)) => {
                    picker.loading = false;
                    picker.error = Some(error);
                    discovery = None;
                    dirty = true;
                }
                Err(std::sync::mpsc::TryRecvError::Disconnected) => {
                    picker.loading = false;
                    picker.error = Some("GitHub discovery stopped; press R to retry".into());
                    discovery = None;
                    dirty = true;
                }
                Err(std::sync::mpsc::TryRecvError::Empty) => {}
            }
        }
        if dirty {
            terminal.draw(|frame| {
                let area = frame.area();
                page = (area.height as usize).saturating_sub(12).max(1);
                frame.render_widget(
                    Paragraph::new(picker.lines(
                        area.width as usize,
                        area.height as usize,
                        can_local,
                    )),
                    area,
                );
            })?;
            dirty = false;
        }
        if !event::poll(Duration::from_millis(100))? {
            continue;
        }
        let input = event::read()?;
        dirty = matches!(input, Event::Key(_) | Event::Resize(_, _));
        if let Event::Key(key) = input {
            match picker.key(key, can_local, page) {
                PickerAction::None => {}
                PickerAction::Refresh => restart = true,
                PickerAction::Cancel => return Ok(Choice::Cancel),
                PickerAction::Quit => return Ok(Choice::Quit),
                PickerAction::Local => return Ok(Choice::Local),
                PickerAction::Apply => {
                    let repositories: Vec<_> = picker
                        .repositories
                        .iter()
                        .filter(|r| picker.selected.contains(&r.id))
                        .cloned()
                        .collect();
                    let Some(client) = picker.client.as_ref() else {
                        continue;
                    };
                    match client.save_selection(&picker.login, &repositories) {
                        Ok(()) => return Ok(Choice::Repositories(client.clone(), repositories)),
                        Err(error) => {
                            picker.error = Some(format!("Unable to save selection: {error:#}"))
                        }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn repo(id: u64, name: &str) -> RemoteRepository {
        RemoteRepository {
            id,
            full_name: name.into(),
            default_branch: Some("main".into()),
            private: false,
            archived: false,
            fork: false,
            disabled: false,
        }
    }
    fn key(picker: &mut Picker, code: KeyCode) -> PickerAction {
        picker.key(KeyEvent::new(code, KeyModifiers::NONE), true, 5)
    }
    #[test]
    fn selection_survives_search_and_owner_filters() {
        let mut picker = Picker::new(Palette::for_theme("dark"), HashSet::new());
        picker.ready(Catalog {
            login: "me".into(),
            repositories: vec![
                repo(1, "alpha/one"),
                repo(2, "beta/two"),
                repo(3, "beta/three"),
            ],
        });
        key(&mut picker, KeyCode::Char(' '));
        key(&mut picker, KeyCode::Char('o'));
        assert_eq!(picker.visible(), [0]);
        key(&mut picker, KeyCode::Char('o'));
        assert_eq!(picker.visible(), [1, 2]);
        key(&mut picker, KeyCode::Char('a'));
        assert_eq!(picker.selected.len(), 3);
        key(&mut picker, KeyCode::Char('/'));
        for c in "three".chars() {
            key(&mut picker, KeyCode::Char(c));
        }
        assert_eq!(picker.visible(), [2]);
        assert_eq!(key(&mut picker, KeyCode::Enter), PickerAction::None);
        assert_eq!(key(&mut picker, KeyCode::Enter), PickerAction::Apply);
        key(&mut picker, KeyCode::Char('a'));
        assert_eq!(picker.selected, HashSet::from([1, 2]));
    }
    #[test]
    fn picker_pages_without_hiding_controls_and_can_cancel_discovery() {
        let mut picker = Picker::new(Palette::for_theme("dark"), HashSet::new());
        assert_eq!(key(&mut picker, KeyCode::Esc), PickerAction::Cancel);
        assert_eq!(key(&mut picker, KeyCode::Enter), PickerAction::None);
        picker.ready(Catalog {
            login: "me".into(),
            repositories: (1..60)
                .map(|id| repo(id, &format!("org/repo-{id}")))
                .collect(),
        });
        for (w, h) in [(80, 24), (140, 40), (50, 12)] {
            key(&mut picker, KeyCode::End);
            let lines = picker.lines(w, h, true);
            assert!(lines.len() <= h, "{} rows at {w}×{h}", lines.len());
            let screen = lines
                .iter()
                .map(ToString::to_string)
                .collect::<Vec<_>>()
                .join("\n");
            assert!(screen.contains("repo-59"), "{screen}");
            assert!(screen.contains("quit"), "{screen}");
        }
    }
}
