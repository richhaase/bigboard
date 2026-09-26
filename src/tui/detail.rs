//! Contributor analytics scroll independently of their context and controls.
use super::components::*;
use super::merge::wrap_help;
use super::{App, TIME_PRESETS};
use ratatui::text::{Line, Span};
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

impl App {
    fn detail_header(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let author = self
            .authors
            .iter()
            .chain(&self.contributors)
            .find(|author| author.id == self.active_id);
        let name = author.map_or("Contributor", |author| author.name.as_str());
        let mut lines = vec![
            panel_header(&format!("CONTRIBUTOR: {}", name.to_uppercase()), width, p),
            text_line(truncate(&self.scope_line().to_string(), width), p.dim_cyan),
            if width >= 60 {
                time_picker(self.time_index, p)
            } else {
                text_line(
                    truncate(
                        &format!("  RANGE ▐{}▌", TIME_PRESETS[self.time_index].0),
                        width,
                    ),
                    p.cyan,
                )
            },
        ];
        if self.github_source {
            lines.insert(2, self.api_basis());
        }
        lines
    }

    fn detail_footer(&self) -> Vec<UiLine> {
        let p = &self.palette;
        let width = self.width as usize;
        let mut lines = Vec::new();
        if let Some(author) = self
            .authors
            .iter()
            .find(|author| author.id == self.active_id)
        {
            let mut markers = Vec::new();
            if author.unknown_line_commits > 0 {
                markers.push("? incomplete line counts");
            }
            if author.commits == 0 && author.coauthored_commits > 0 {
                markers.push("— coauthor lines unallocated");
            }
            if !markers.is_empty() {
                lines.push(text_line(
                    truncate(&format!("  {}", markers.join(" · ")), width),
                    p.amber,
                ));
            }
        }
        lines.extend(self.quality_lines());
        if let Some(notice) = &self.notice {
            lines.push(text_line(
                truncate(&format!("  ✓ {notice}"), width),
                p.green,
            ));
        }
        for mut bindings in [
            vec![
                ("PgUp/PgDn", "scroll".into()),
                ("Home/End", "top/bottom".into()),
                ("esc", "back".into()),
            ],
            vec![
                ("↑↓", "prev/next".into()),
                ("←→", "time".into()),
                ("B", "history".into()),
                ("M", "merge".into()),
                ("q", "quit".into()),
            ],
        ] {
            if self.github_source {
                bindings.retain(|(key, _)| *key != "B");
            }
            lines.extend(wrap_help(&bindings, width, p));
        }
        lines
    }

    fn detail_body(&self) -> Vec<UiLine> {
        self.detail_content()
            .into_iter()
            .flat_map(|line| wrap_line(line, self.width as usize))
            .collect()
    }

    fn detail_viewport(&self) -> usize {
        (self.height as usize)
            .saturating_sub(self.detail_header().len() + self.detail_footer().len() + 1)
    }

    pub(super) fn detail_max_offset(&self) -> usize {
        self.detail_body()
            .len()
            .saturating_sub(self.detail_viewport().max(1))
    }

    pub(super) fn clamp_detail_scroll(&mut self) {
        self.detail_offset = self.detail_offset.min(self.detail_max_offset());
    }

    pub(super) fn page_detail(&mut self, forward: bool) {
        let page = self.detail_viewport().saturating_sub(1).max(1);
        let last = self.detail_max_offset();
        self.detail_offset = if forward {
            self.detail_offset.saturating_add(page).min(last)
        } else {
            self.detail_offset.min(last).saturating_sub(page)
        };
    }

    pub(super) fn detail_lines(&self) -> Vec<UiLine> {
        let width = self.width as usize;
        let height = self.height as usize;
        let p = &self.palette;
        let mut lines = self.detail_header();
        let footer = self.detail_footer();
        let visible = height.saturating_sub(lines.len() + footer.len() + 1);
        if visible == 0 || width < 40 {
            lines.push(text_line(
                truncate("  ◈ Resize terminal to read contributor details", width),
                p.amber,
            ));
            lines.push(help(&[("esc", "back".into()), ("q", "quit".into())], p));
            lines.truncate(height);
            return lines;
        }
        let body = self.detail_body();
        let start = self.detail_offset.min(body.len().saturating_sub(visible));
        let end = (start + visible).min(body.len());
        lines.extend(body[start..end].iter().cloned());
        lines.resize_with(height - footer.len() - 1, blank);
        let pager = format!(
            "  LINES {}–{end} / {}",
            if body.is_empty() { 0 } else { start + 1 },
            body.len()
        );
        lines.push(text_line(truncate(&pager, width), p.magenta));
        lines.extend(footer);
        lines
    }
}

// Preserve colors and exact values when a detail row exceeds the terminal
// width. Paging then reaches every wrapped line instead of clipping its tail.
fn wrap_line(line: UiLine, width: usize) -> Vec<UiLine> {
    if width == 0 {
        return vec![blank()];
    }
    if line.width() <= width {
        return vec![line];
    }
    let mut lines = Vec::new();
    let mut row = Vec::new();
    let mut used = 0;
    for item in line.spans {
        let style = line.style.patch(item.style);
        let mut chunk = String::new();
        let clean = display_text(&item.content);
        for grapheme in clean.graphemes(true) {
            let cells = grapheme.width();
            if used + cells > width && used > 0 {
                if !chunk.is_empty() {
                    row.push(Span::styled(std::mem::take(&mut chunk), style));
                }
                lines.push(Line::from(std::mem::take(&mut row)));
                used = 0;
            }
            chunk.push_str(grapheme);
            used += cells;
        }
        if !chunk.is_empty() {
            row.push(Span::styled(chunk, style));
        }
    }
    if !row.is_empty() {
        lines.push(Line::from(row));
    }
    lines
}
