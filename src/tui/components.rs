use super::TIME_PRESETS;
use ratatui::{
    style::{Color, Modifier, Style},
    text::{Line, Span},
};
use unicode_width::{UnicodeWidthChar, UnicodeWidthStr};

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
            dim_cyan: c(0x5E8590, 0x005566),
            dim_white: c(0x888888, 0x555555),
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
        return vec![Line::from(bold("  ░▒▓█  B I G   B O A R D  █▓▒░", p.cyan))];
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

pub(super) fn footer(
    repos: usize,
    excluded: usize,
    width: usize,
    version: &str,
    p: &Palette,
) -> UiLine {
    let left = repo_count(repos, excluded);
    let target = width.saturating_sub(2).max(left.width());
    let ver = truncate(version, target.saturating_sub(left.width() + 2));
    let padding = if ver.is_empty() {
        String::new()
    } else {
        " ".repeat(target.saturating_sub(left.width() + ver.width()).max(2))
    };
    text_line(format!("{left}{padding}{ver}"), p.dim_cyan)
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
        let mut spans = vec![
            span("  AUTHORED ", p.dim_cyan),
            bold(format_number(commits), p.cyan),
        ];
        let mut used = 10 + format_number(commits).width();
        for (value, label, color) in values.iter().skip(1) {
            let val = if *label == "DETECTED AI" {
                format!("Detected AI {value}")
            } else {
                value.clone()
            };
            if used + 5 + val.width() > width {
                break;
            }
            used += 5 + val.width();
            spans.push(span("  ·  ", p.dim_cyan));
            spans.push(bold(val, *color));
        }
        return vec![Line::from(spans)];
    }
    let boxes: Vec<Vec<UiLine>> = values
        .into_iter()
        .map(|(value, label, color)| {
            let inner = value.width().max(label.width()) + 6;
            vec![
                text_line(format!("┏{}┓", "━".repeat(inner)), p.dim_cyan),
                Line::from(vec![
                    span("┃", p.dim_cyan),
                    bold(center(&value, inner), color),
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
        return boxes
            .into_iter()
            .flatten()
            .map(|mut l| {
                l.spans.insert(0, Span::raw("  "));
                l
            })
            .collect();
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
            span(*key, p.cyan),
            span("▌", p.dim_cyan),
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
    s.chars()
        .take_while(|c| {
            used += c.width().unwrap_or(0);
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
