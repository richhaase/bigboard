//! Resolve the terminal background once, before Crossterm owns its input stream.
use std::{
    io::{self, IsTerminal},
    time::Duration,
};

const QUERY_TIMEOUT: Duration = Duration::from_millis(200);

pub(super) fn resolve(requested: &str) -> &'static str {
    let fallback = std::env::var("COLORFGBG").ok();
    resolve_with(requested, fallback.as_deref(), || {
        if !io::stdout().is_terminal() {
            return None;
        }
        let mut options = terminal_colorsaurus::QueryOptions::default();
        options.timeout = QUERY_TIMEOUT;
        // Colorsaurus owns raw-mode setup/restoration, including errors/unwinding.
        // Run before the TUI event reader so the response cannot become user input.
        let color = terminal_colorsaurus::background_color(options).ok()?;
        Some([color.r, color.g, color.b])
    })
}

fn resolve_with(
    requested: &str,
    fallback: Option<&str>,
    query: impl FnOnce() -> Option<[u16; 3]>,
) -> &'static str {
    match requested.to_ascii_lowercase().as_str() {
        "light" => return "light",
        "dark" => return "dark",
        _ => {}
    }
    let color = query().or_else(|| fallback.and_then(ansi_background));
    if color.is_some_and(is_light) {
        "light"
    } else {
        "dark"
    }
}

fn is_light([r, g, b]: [u16; 3]) -> bool {
    // termenv.HasDarkBackground uses HSL lightness < 0.5. Using the same
    // threshold (rather than perceptual lightness) preserves colorful themes.
    u32::from(r.max(g).max(b)) + u32::from(r.min(g).min(b)) >= u32::from(u16::MAX)
}

fn ansi_background(value: &str) -> Option<[u16; 3]> {
    // Match termenv's last COLORFGBG field and xterm palette fallback.
    if !value.contains(';') {
        return None;
    }
    let index = value.rsplit(';').next()?.parse::<u8>().ok()?;
    let color = match index {
        0..=15 => [
            [0, 0, 0],
            [128, 0, 0],
            [0, 128, 0],
            [128, 128, 0],
            [0, 0, 128],
            [128, 0, 128],
            [0, 128, 128],
            [192, 192, 192],
            [128, 128, 128],
            [255, 0, 0],
            [0, 255, 0],
            [255, 255, 0],
            [0, 0, 255],
            [255, 0, 255],
            [0, 255, 255],
            [255, 255, 255],
        ][index as usize],
        16..=231 => {
            let cube = (index - 16) as usize;
            let levels = [0, 95, 135, 175, 215, 255];
            [levels[cube / 36], levels[(cube / 6) % 6], levels[cube % 6]]
        }
        _ => [8 + 10 * u16::from(index - 232); 3],
    };
    Some(color.map(|channel| channel * 257))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn explicit_themes_bypass_query_and_environment() {
        assert_eq!(
            resolve_with("LIGHT", Some("15;0"), || panic!(
                "explicit theme queried terminal"
            )),
            "light"
        );
        assert_eq!(
            resolve_with("dark", Some("0;15"), || panic!(
                "explicit theme queried terminal"
            )),
            "dark"
        );
    }
    #[test]
    fn auto_queries_before_fallback_and_uses_go_hsl_threshold() {
        assert_eq!(
            resolve_with("auto", Some("15;0"), || Some([65535; 3])),
            "light"
        );
        assert_eq!(resolve_with("auto", Some("0;15"), || Some([0; 3])), "dark");
        assert_eq!(resolve_with("auto", None, || Some([65535, 0, 0])), "light");
        assert_eq!(resolve_with("auto", None, || Some([32767; 3])), "dark");
        assert_eq!(resolve_with("auto", None, || Some([32768; 3])), "light");
    }
    #[test]
    fn failed_queries_use_colorfgbg_then_dark() {
        for env in ["0;7", "0;15", "0;8", "0;9", "0;255"] {
            assert_eq!(resolve_with("auto", Some(env), || None), "light", "{env}");
        }
        for env in [
            None,
            Some("15;0"),
            Some("15;16"),
            Some("15;232"),
            Some("invalid"),
            Some("15"),
        ] {
            assert_eq!(resolve_with("auto", env, || None), "dark", "{env:?}");
        }
    }
}
