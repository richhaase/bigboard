use super::TIME_PRESETS;
use ratatui::{
    style::{Color, Modifier, Style},
    text::{Line, Span},
};
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

pub(super) type UiLine = Line<'static>;
pub(super) type UiSpan = Span<'static>;

#[derive(Clone)]
pub(super) struct Palette {
    pub cyan: Color,
    pub magenta: Color,
    pub green: Color,
    pub amber: Color,
    pub red: Color,
    pub cyan_mid: Color,
    pub cyan_dim: Color,
    pub magenta_mid: Color,
    pub magenta_dim: Color,
    pub dim_cyan: Color,
    pub dim_white: Color,
    pub bright: Color,
    pub row_even: Color,
    pub row_odd: Color,
    pub row_selected: Color,
    pub gold: Color,
    pub silver: Color,
    pub bronze: Color,
    pub banner: [Color; 7],
}

fn rgb(value: u32) -> Color {
    Color::Rgb((value >> 16) as u8, (value >> 8) as u8, value as u8)
}
impl Palette {
    pub fn for_theme(theme: &str) -> Self {
        let theme = theme.to_lowercase();
        let light = theme == "light";
        let c = |l, d| rgb(if light { l } else { d });
        Self {
            cyan: c(0x006B86, 0x00FFFF),
            magenta: c(0x9C27B0, 0xFF00FF),
            green: c(0x1B7F3B, 0x00FF88),
            amber: c(0xB25900, 0xFFB000),
            red: c(0xC8002A, 0xFF0040),
            cyan_mid: c(0x3F8CA0, 0x00BBDD),
            cyan_dim: c(0x7CAEB8, 0x005577),
            magenta_mid: c(0xB449C4, 0xCC00CC),
            magenta_dim: c(0xD29ED9, 0x660066),
            dim_cyan: c(0x486E7D, 0x4B99A8),
            dim_white: c(0x64677B, 0x8991A8),
            bright: c(0x1A1A1A, 0xE0E0E0),
            row_even: c(0xF0F2F8, 0x0A0A1A),
            row_odd: c(0xFAFAFA, 0x070714),
            row_selected: c(0xC5E0EC, 0x0C2030),
            gold: c(0xB8860B, 0xFFD700),
            silver: c(0x6E6E6E, 0xC0C0C0),
            bronze: c(0x8B5A2B, 0xCD7F32),
            banner: [
                c(0x003D55, 0x00FFFF),
                c(0x00536F, 0x00EEFF),
                c(0x006782, 0x00CCDD),
                c(0x00789A, 0x00AACC),
                c(0x008CB0, 0x0088AA),
                c(0x009AC2, 0x006688),
                c(0x00ACDD, 0x005577),
            ],
        }
    }
    pub fn row(&self, selected: bool, index: usize) -> Style {
        let style = Style::default().bg(if selected {
            self.row_selected
        } else if index.is_multiple_of(2) {
            self.row_even
        } else {
            self.row_odd
        });
        if selected {
            style.fg(self.cyan).add_modifier(Modifier::BOLD)
        } else {
            style
        }
    }
}

pub(super) fn span(text: impl Into<String>, color: Color) -> UiSpan {
    Span::styled(text.into(), Style::default().fg(color))
}
pub(super) fn bold(text: impl Into<String>, color: Color) -> UiSpan {
    span(text, color).style(Style::default().fg(color).add_modifier(Modifier::BOLD))
}
pub(super) fn blank() -> UiLine {
    Line::default()
}
pub(super) fn text_line(text: impl Into<String>, color: Color) -> UiLine {
    Line::from(span(text, color))
}
pub(super) fn rule(width: usize, p: &Palette) -> UiLine {
    text_line(
        format!("  {}", "━".repeat(width.saturating_sub(4))),
        p.dim_cyan,
    )
}

pub(super) fn banner(width: usize, compact: bool, p: &Palette) -> Vec<UiLine> {
    if width < 82 || compact {
        let title = if width >= 31 {
            "B I G   B O A R D"
        } else {
            "BIG BOARD"
        };
        return vec![clip_line(
            Line::from(vec![
                span("  ░▒", p.cyan_dim),
                bold("▓█  ", p.cyan),
                bold(title, p.cyan),
                bold("  █▓", p.magenta),
                span("▒░", p.magenta_dim),
            ]),
            width,
        )];
    }
    [
        "████████  ████  ██████      ████████   ███████     ███    ████████  ████████",
        "██     ██  ██  ██    ██     ██     ██ ██     ██   ██ ██   ██     ██ ██     ██",
        "██     ██  ██  ██           ██     ██ ██     ██  ██   ██  ██     ██ ██     ██",
        "████████   ██  ██   ████    ████████  ██     ██ ██     ██ ████████  ██     ██",
        "██     ██  ██  ██    ██     ██     ██ ██     ██ █████████ ██   ██   ██     ██",
        "██     ██  ██  ██    ██     ██     ██ ██     ██ ██     ██ ██    ██  ██     ██",
        "████████  ████  ██████      ████████   ███████  ██     ██ ██     ██ ████████",
    ]
    .iter()
    .enumerate()
    .map(|(i, s)| text_line(format!("  {s}"), p.banner[i]))
    .collect()
}

pub(super) fn repo_count(total: usize, excluded: usize) -> String {
    if excluded > 0 {
        format!("  {}/{} repos", total.saturating_sub(excluded), total)
    } else {
        format!("  {total} repos")
    }
}

pub(super) fn time_picker(active: usize, p: &Palette) -> UiLine {
    let mut spans = vec![Span::raw("  ")];
    for (i, (label, _)) in TIME_PRESETS.iter().enumerate() {
        if i > 0 {
            spans.push(Span::raw(" "));
        }
        if i == active {
            spans.extend([
                span("▐", p.dim_cyan),
                bold(*label, p.cyan),
                span("▌", p.dim_cyan),
            ]);
        } else {
            spans.push(span(format!("  {label}  "), p.dim_white));
        }
    }
    Line::from(spans)
}

pub(super) fn percent_label(part: i64, whole: i64) -> String {
    if whole <= 0 {
        return "0%".into();
    }
    let pct = part * 100 / whole;
    if pct == 0 && part > 0 {
        "<1%".into()
    } else {
        format!("{pct}%")
    }
}
fn ai_value(commits: i64, ai: i64) -> String {
    if commits <= 0 {
        format_number(ai)
    } else {
        format!("{} ({ai})", percent_label(ai, commits))
    }
}

pub(super) fn stat_boxes(
    (commits, added, removed, ai): (i64, i64, i64, i64),
    width: usize,
    compact: bool,
    p: &Palette,
) -> Vec<UiLine> {
    let mut values = vec![
        (format_number(commits), "AUTHORED", p.cyan),
        (format!("+{}", format_number(added)), "ADDED", p.green),
        (format!("-{}", format_number(removed)), "REMOVED", p.magenta),
    ];
    if ai > 0 {
        values.push((ai_value(commits, ai), "DETECTED AI", p.amber));
    }
    if width < 78 || compact {
        return compact_stat_lines(&values, width, p);
    }
    let boxes: Vec<Vec<UiLine>> = values
        .iter()
        .map(|(value, label, color)| {
            let inner = value.width().max(label.width()) + 6;
            vec![
                text_line(format!("┏{}┓", "━".repeat(inner)), p.dim_cyan),
                Line::from(vec![
                    span("┃", p.dim_cyan),
                    bold(center(value, inner), *color),
                    span("┃", p.dim_cyan),
                ]),
                text_line(format!("┃{}┃", center(label, inner)), p.dim_cyan),
                text_line(format!("┗{}┛", "━".repeat(inner)), p.dim_cyan),
            ]
        })
        .collect();
    let full_width: usize =
        2 + boxes.iter().map(|b| b[0].width()).sum::<usize>() + boxes.len().saturating_sub(1);
    if full_width > width {
        // Stacking four cards uses sixteen rows. Fall back to a compact,
        // width-bounded summary so large counts do not consume the dashboard.
        return compact_stat_lines(&values, width, p);
    }
    (0..4)
        .map(|row| {
            let mut spans = vec![Span::raw("  ")];
            for (i, b) in boxes.iter().enumerate() {
                if i > 0 {
                    spans.push(Span::raw(" "));
                }
                spans.extend(b[row].spans.clone());
            }
            Line::from(spans)
        })
        .collect()
}

fn compact_stat_lines(values: &[(String, &str, Color)], width: usize, p: &Palette) -> Vec<UiLine> {
    if width == 0 {
        return vec![blank()];
    }
    let indent = " ".repeat(width.min(2));
    let mut lines = Vec::new();
    let mut line = Line::from(indent.clone());
    for (value, label, color) in values {
        let item = Line::from(vec![
            span(format!("{label} "), p.dim_cyan),
            bold(value, *color),
        ]);
        let populated = line.spans.len() > 1;
        let separator_width = if populated { 3 } else { 0 };
        if populated && line.width() + separator_width + item.width() > width {
            lines.push(line);
            line = Line::from(indent.clone());
        }
        if line.spans.len() > 1 {
            line.spans.push(span(" · ", p.dim_cyan));
        }
        let available = width.saturating_sub(line.width());
        line.spans.extend(clip_line(item, available).spans);
    }
    lines.push(line);
    lines
}

/// A full-width neon panel edge. Width includes the border; no margin is added.
pub(super) fn panel_header(label: &str, width: usize, p: &Palette) -> UiLine {
    if width < 8 {
        return panel_edge("┏", "┓", width, p);
    }
    let label = truncate(label, width - 8);
    Line::from(vec![
        span("┏━╸ ", p.cyan),
        bold(label.clone(), p.magenta),
        span(
            format!(" ╺{}", "━".repeat(width - label.width() - 7)),
            p.cyan_mid,
        ),
        span("┓", p.magenta),
    ])
}

pub(super) fn panel_footer(width: usize, p: &Palette) -> UiLine {
    panel_edge("┗", "┛", width, p)
}

fn panel_edge(left: &str, right: &str, width: usize, p: &Palette) -> UiLine {
    if width == 0 {
        return blank();
    }
    if width == 1 {
        return text_line(left, p.cyan);
    }
    let inner = width - 2;
    let cyan_width = inner * 2 / 3;
    Line::from(vec![
        span(left, p.cyan),
        span("━".repeat(cyan_width), p.cyan_mid),
        span("━".repeat(inner - cyan_width), p.magenta_mid),
        span(right, p.magenta),
    ])
}

/// Frame an existing styled row. The content area is width - 4 cells, with one
/// space between each edge and its content. Wide graphemes are never split.
pub(super) fn panel_row(content: UiLine, width: usize, p: &Palette) -> UiLine {
    if width < 4 {
        return match width {
            0 => blank(),
            1 => text_line("┃", p.cyan),
            2 => Line::from(vec![span("┃", p.cyan), span("┃", p.magenta)]),
            _ => Line::from(vec![span("┃ ", p.cyan), span("┃", p.magenta)]),
        };
    }
    let inner_width = width - 4;
    let content_style = content.style;
    let content = clip_line(content, inner_width);
    let used = content.width();
    let mut spans = vec![span("┃ ", p.cyan_mid)];
    spans.extend(content.spans);
    spans.push(Span::styled(" ".repeat(inner_width - used), content_style));
    spans.push(span(" ┃", p.magenta_mid));
    Line::from(spans)
}

fn clip_line(line: UiLine, width: usize) -> UiLine {
    let mut remaining = width;
    let mut clipped = Vec::new();
    for item in line.spans {
        let clean = display_text(&item.content);
        let text = cut_width(&clean, remaining);
        let used = text.width();
        let complete = text.len() == clean.len();
        clipped.push(Span::styled(text, line.style.patch(item.style)));
        remaining -= used;
        if !complete || remaining == 0 {
            break;
        }
    }
    Line::from(clipped)
}

pub(super) fn impact_bar(
    added: i64,
    removed: i64,
    max: i64,
    width: usize,
    p: &Palette,
) -> Vec<UiSpan> {
    let total = added + removed;
    if total <= 0 || max <= 0 || width == 0 {
        return vec![Span::raw(" ".repeat(width))];
    }
    let filled = (total as i128 * width as i128 / max as i128).clamp(1, width as i128) as usize;
    let added_fill =
        (added as i128 * filled as i128 / total as i128).clamp(0, filled as i128) as usize;
    let mut spans = gradient(added_fill, p.cyan, p.cyan_mid, p.cyan_dim);
    spans.extend(gradient(
        filled - added_fill,
        p.magenta,
        p.magenta_mid,
        p.magenta_dim,
    ));
    spans.push(Span::raw(" ".repeat(width - filled)));
    spans
}
fn gradient(width: usize, bright: Color, mid: Color, dim: Color) -> Vec<UiSpan> {
    let tail = if width < 6 { 0 } else { 3 };
    let mut spans = vec![span("█".repeat(width - tail), bright)];
    if tail > 0 {
        spans.extend([span("▓", mid), span("▒░", dim)]);
    }
    spans
}

pub(super) fn section(label: &str, width: usize, p: &Palette) -> UiLine {
    let label = display_text(label);
    let fill = width.saturating_sub(10 + label.width()).max(2);
    Line::from(vec![
        span("  ──╸ ", p.dim_cyan),
        span(label, p.cyan),
        span(format!(" ╺{}", "─".repeat(fill)), p.dim_cyan),
    ])
}
pub(super) fn help(bindings: &[(&str, String)], p: &Palette) -> UiLine {
    let mut spans = vec![Span::raw("  ")];
    for (i, (key, desc)) in bindings.iter().enumerate() {
        if i > 0 {
            spans.push(Span::raw("  "));
        }
        spans.extend([
            span("▐", p.dim_cyan),
            bold(*key, p.cyan),
            span("▌ ", p.dim_cyan),
            span(desc.clone(), p.dim_white),
        ]);
    }
    Line::from(spans)
}

pub(super) fn format_number(n: i64) -> String {
    let s = n.to_string();
    let (sign, digits) = if let Some(n) = s.strip_prefix('-') {
        ("-", n)
    } else {
        ("", s.as_str())
    };
    let mut out = sign.to_string();
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    out
}

/// Remove terminal escape sequences and replace remaining controls. Repository
/// names and Git author names are untrusted text, never terminal instructions.
pub(super) fn display_text(s: &str) -> String {
    let mut out = String::new();
    let mut chars = s.chars().peekable();
    while let Some(c) = chars.next() {
        match c {
            '\u{1b}' => match chars.next() {
                Some('[') => {
                    for c in chars.by_ref() {
                        if ('@'..='~').contains(&c) {
                            break;
                        }
                    }
                }
                Some(']') | Some('P') | Some('X') | Some('^') | Some('_') => {
                    while let Some(c) = chars.next() {
                        if c == '\u{7}' || (c == '\u{1b}' && chars.peek() == Some(&'\\')) {
                            if c == '\u{1b}' {
                                chars.next();
                            }
                            break;
                        }
                    }
                }
                Some(c) if (' '..='/').contains(&c) => {
                    for c in chars.by_ref() {
                        if ('0'..='~').contains(&c) {
                            break;
                        }
                    }
                }
                _ => {}
            },
            '\u{9b}' => {
                for c in chars.by_ref() {
                    if ('@'..='~').contains(&c) {
                        break;
                    }
                }
            }
            c if c.is_control() => out.push(' '),
            c => out.push(c),
        }
    }
    out
}
pub(super) fn truncate(s: &str, width: usize) -> String {
    let s = display_text(s);
    if s.width() <= width {
        return s;
    }
    if width <= 3 {
        cut_width(&s, width)
    } else {
        format!("{}...", cut_width(&s, width - 3))
    }
}
fn cut_width(s: &str, width: usize) -> String {
    let mut used = 0;
    s.graphemes(true)
        .take_while(|grapheme| {
            used += grapheme.width();
            used <= width
        })
        .collect()
}
pub(super) fn pad_right(s: &str, width: usize) -> String {
    format!("{s}{}", " ".repeat(width.saturating_sub(s.width())))
}
pub(super) fn pad_left(s: &str, width: usize) -> String {
    format!("{}{s}", " ".repeat(width.saturating_sub(s.width())))
}
fn center(s: &str, width: usize) -> String {
    let pad = width.saturating_sub(s.width());
    format!("{}{s}{}", " ".repeat(pad / 2), " ".repeat(pad - pad / 2))
}

#[cfg(test)]
mod component_tests {
    use super::*;

    fn plain(line: &UiLine) -> String {
        line.spans
            .iter()
            .map(|span| span.content.as_ref())
            .collect()
    }

    #[test]
    fn neon_panels_keep_exact_cell_width_and_styled_content() {
        for theme in ["light", "dark"] {
            let p = Palette::for_theme(theme);
            for width in [0, 1, 2, 3, 4, 7, 8, 20, 60, 120, 220] {
                let content = Line::from(vec![
                    bold("日本語 ", p.green),
                    span("👩‍💻 e\u{301} contributor", p.bright),
                ])
                .style(Style::default().bg(p.row_selected));
                let row = panel_row(content, width, &p);
                assert_eq!(row.width(), width, "row {theme} {width}");
                assert_eq!(
                    panel_header("CONTRIBUTION // 活動", width, &p).width(),
                    width
                );
                assert_eq!(panel_footer(width, &p).width(), width);
                if width >= 20 {
                    assert!(row.spans.iter().any(|s| {
                        s.content.starts_with("日本語")
                            && s.style.fg == Some(p.green)
                            && s.style.bg == Some(p.row_selected)
                    }));
                }
            }
        }
    }

    #[test]
    fn compact_banner_is_bounded_and_full_banner_remains_available() {
        let p = Palette::for_theme("dark");
        for width in [0, 1, 10, 20, 30, 31, 60, 120, 220] {
            let lines = banner(width, true, &p);
            assert_eq!(lines.len(), 1);
            assert!(lines[0].width() <= width);
        }
        assert_eq!(banner(120, false, &p).len(), 7);
        let compact = banner(120, true, &p);
        assert!(
            compact[0]
                .spans
                .iter()
                .any(|s| s.style.fg == Some(p.magenta))
        );
    }

    #[test]
    fn keycaps_have_space_before_description_and_keep_bindings() {
        let p = Palette::for_theme("dark");
        let line = help(&[("B", "history".into()), ("M", "merge".into())], &p);
        assert_eq!(plain(&line), "  ▐B▌ history  ▐M▌ merge");
    }

    #[test]
    fn card_overflow_uses_bounded_summary_instead_of_vertical_card_stack() {
        let p = Palette::for_theme("light");
        for width in [40, 60, 78, 80, 96, 120] {
            let lines = stat_boxes((1_234_567, 9_876_543, 1_234_567, 4_321), width, false, &p);
            assert!(lines.len() <= 4, "{width} cells used {} rows", lines.len());
            assert!(lines.iter().all(|line| line.width() <= width));
            let text = lines.iter().map(plain).collect::<Vec<_>>().join("\n");
            for value in ["1,234,567", "+9,876,543", "-1,234,567", "DETECTED AI"] {
                assert!(text.contains(value), "{width} cells dropped {value}");
            }
        }
        for width in [0, 1, 2, 3, 10, 20, 40, 78, 120] {
            let lines = stat_boxes((i64::MAX, i64::MAX, i64::MAX, 0), width, true, &p);
            assert!(lines.len() <= 3);
            assert!(lines.iter().all(|line| line.width() <= width));
        }
    }

    #[test]
    fn clipping_preserves_whole_unicode_graphemes() {
        assert_eq!(cut_width("👩‍💻作業", 2), "👩‍💻");
        assert_eq!(cut_width("e\u{301}quipe", 1), "e\u{301}");
        let p = Palette::for_theme("dark");
        let row = panel_row(text_line("👩‍💻作業", p.cyan), 6, &p);
        assert_eq!(plain(&row), "┃ 👩‍💻 ┃");
        assert_eq!(row.width(), 6);
    }
}
