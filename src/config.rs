//! Terminal CLI and user preferences.
use crate::{model::Repository, stats::SortField};
use anyhow::{Context, Result, bail};
use serde::{
    Deserialize, Deserializer,
    de::{self, MapAccess, Visitor},
};
use std::{
    collections::{BTreeMap, HashSet},
    env, fs,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Config {
    pub paths: Vec<String>,
    pub exclude: Vec<String>,
    pub sort: String,
    pub since: String,
    pub theme: String,
    pub timezone: String,
    // Accept legacy false values; true is rejected with a migration message.
    pub fuzzy: bool,
    pub all_files: bool,
    pub depth: i64,
    pub groups: BTreeMap<String, Option<Vec<String>>>,
    pub ai_identities: Vec<String>,
    pub bot_identities: Vec<String>,
}

// Go's decoder accepts null string elements as the empty string. Duplicate
// scalar fields keep their previous value when the later value is null.
#[derive(Default)]
struct ConfigString(String);
impl<'de> Deserialize<'de> for ConfigString {
    fn deserialize<D: Deserializer<'de>>(d: D) -> std::result::Result<Self, D::Error> {
        Ok(Self(Option::<String>::deserialize(d)?.unwrap_or_default()))
    }
}

fn string_list<'de, A: MapAccess<'de>>(
    map: &mut A,
    backing: &mut Vec<String>,
) -> std::result::Result<Vec<String>, A::Error> {
    let values = map
        .next_value::<Option<Vec<Option<String>>>>()?
        .unwrap_or_default();
    if values.is_empty() {
        backing.clear();
        return Ok(Vec::new());
    }
    // encoding/json reuses a nonempty slice's backing array across repeated
    // fields; a null element leaves the old slot intact, even after shrinking.
    backing.resize(backing.len().max(values.len()), String::new());
    let length = values.len();
    for (slot, value) in backing.iter_mut().zip(values) {
        if let Some(value) = value {
            *slot = value;
        }
    }
    Ok(backing[..length].to_vec())
}

// Config keys are ASCII, but Go's EqualFold also folds the long s and Kelvin
// sign into ASCII. Arbitrary names inside groups remain case-sensitive.
fn folded_config_key(key: &str) -> String {
    key.chars()
        .map(|c| match c {
            'ſ' => 's',
            'K' => 'k',
            _ => c.to_ascii_lowercase(),
        })
        .collect()
}

impl<'de> Deserialize<'de> for Config {
    fn deserialize<D: Deserializer<'de>>(d: D) -> std::result::Result<Self, D::Error> {
        struct ConfigVisitor;
        impl<'de> Visitor<'de> for ConfigVisitor {
            type Value = Config;
            fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
                formatter.write_str("a configuration object or null")
            }
            fn visit_unit<E: de::Error>(self) -> std::result::Result<Config, E> {
                Ok(Config::default())
            }
            fn visit_map<A: MapAccess<'de>>(
                self,
                mut map: A,
            ) -> std::result::Result<Config, A::Error> {
                let mut config = Config::default();
                let mut list_buffers: BTreeMap<&str, Vec<String>> = BTreeMap::new();
                while let Some(key) = map.next_key::<String>()? {
                    match folded_config_key(&key).as_str() {
                        "paths" => {
                            config.paths =
                                string_list(&mut map, list_buffers.entry("paths").or_default())?
                        }
                        "exclude" => {
                            config.exclude =
                                string_list(&mut map, list_buffers.entry("exclude").or_default())?
                        }
                        "ai_identities" => {
                            config.ai_identities = string_list(
                                &mut map,
                                list_buffers.entry("ai_identities").or_default(),
                            )?
                        }
                        "bot_identities" => {
                            config.bot_identities = string_list(
                                &mut map,
                                list_buffers.entry("bot_identities").or_default(),
                            )?
                        }
                        "sort" => {
                            if let Some(value) = map.next_value::<Option<String>>()? {
                                config.sort = value
                            }
                        }
                        "since" => {
                            if let Some(value) = map.next_value::<Option<String>>()? {
                                config.since = value
                            }
                        }
                        "theme" => {
                            if let Some(value) = map.next_value::<Option<String>>()? {
                                config.theme = value
                            }
                        }
                        "timezone" => {
                            if let Some(value) = map.next_value::<Option<String>>()? {
                                config.timezone = value;
                            }
                        }
                        "fuzzy" => {
                            if let Some(value) = map.next_value::<Option<bool>>()? {
                                config.fuzzy = value
                            }
                        }
                        "all_files" => {
                            if let Some(value) = map.next_value::<Option<bool>>()? {
                                config.all_files = value
                            }
                        }
                        "depth" => {
                            if let Some(value) = map.next_value::<Option<i64>>()? {
                                config.depth = value
                            }
                        }
                        "groups" => {
                            type Groups = BTreeMap<String, Option<Vec<ConfigString>>>;
                            if let Some(groups) = map.next_value::<Option<Groups>>()? {
                                for (name, paths) in groups {
                                    config.groups.insert(
                                        name,
                                        paths.map(|paths| {
                                            paths.into_iter().map(|value| value.0).collect()
                                        }),
                                    );
                                }
                            } else {
                                config.groups.clear();
                            }
                        }
                        _ => return Err(de::Error::custom(format!("json: unknown field {key:?}"))),
                    }
                }
                Ok(config)
            }
        }
        d.deserialize_any(ConfigVisitor)
    }
}

#[derive(Default, Debug, PartialEq, Eq)]
pub struct Cli {
    pub version: bool,
    pub github: bool,
    pub group: String,
    pub config: String,
    pub paths: Vec<String>,
    pub help: bool,
}

impl Cli {
    /// Source selection depends on explicit local inputs, not the working
    /// directory or legacy configured paths. --github remains a compatible alias.
    pub fn github_mode(&self) -> Result<bool> {
        let local = !self.paths.is_empty() || !self.group.is_empty();
        if self.github && local {
            bail!("--github selects remote repositories; use paths or --group for a local board");
        }
        Ok(!local)
    }

    /// Like Go's flag package, stop parsing flags at the first positional arg.
    pub fn parse(args: impl IntoIterator<Item = String>) -> Result<Self> {
        let mut result = Self::default();
        let mut args = args.into_iter();
        while let Some(arg) = args.next() {
            if arg == "--" {
                result.paths.extend(args);
                break;
            }
            if !arg.starts_with('-') || arg == "-" {
                result.paths.push(arg);
                result.paths.extend(args);
                break;
            }
            let flag = arg.strip_prefix("--").unwrap_or_else(|| &arg[1..]);
            if flag.is_empty() || flag.starts_with(['-', '=']) {
                bail!("bad flag syntax: {arg}");
            }
            let (key, supplied) = flag
                .split_once('=')
                .map_or((flag, None), |(k, v)| (k, Some(v)));
            match key {
                "h" | "help" => {
                    result.help = true;
                    break;
                }
                "export" => bail!("--export has been removed; use the interactive dashboard"),
                "version" | "github" => {
                    let value = match supplied.unwrap_or("true") {
                        "1" | "t" | "T" | "TRUE" | "true" | "True" => true,
                        "0" | "f" | "F" | "FALSE" | "false" | "False" => false,
                        v => bail!("invalid boolean value {v:?} for -{key}"),
                    };
                    if key == "github" {
                        result.github = value;
                    } else {
                        result.version = value;
                    }
                }
                "group" | "config" => {
                    let value = supplied
                        .map(str::to_owned)
                        .or_else(|| args.next())
                        .with_context(|| format!("flag needs an argument: -{key}"))?;
                    if key == "group" {
                        result.group = value
                    } else {
                        result.config = value
                    }
                }
                _ => bail!("flag provided but not defined: -{key}"),
            }
        }
        Ok(result)
    }
}

pub fn default_config_path() -> PathBuf {
    if let Some(dir) = env::var_os("XDG_CONFIG_HOME").filter(|s| !s.is_empty()) {
        return PathBuf::from(dir).join("bigboard/config.json");
    }
    env::var_os("HOME")
        .filter(|home| !home.is_empty())
        .map(|home| PathBuf::from(home).join(".config/bigboard/config.json"))
        .unwrap_or_default()
}

pub fn load_config(path: &Path, explicit: bool) -> Result<Config> {
    let data = match fs::read(path) {
        Ok(data) => data,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound && !explicit => {
            return Ok(Config::default());
        }
        Err(e) => return Err(e.into()),
    };
    Ok(serde_json::from_slice::<Option<Config>>(&data)?.unwrap_or_default())
}

pub fn time_index_for_since(s: &str) -> Result<usize> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(2);
    }
    ["1d", "7d", "14d", "30d", "90d", "1y", "all"]
        .iter()
        .position(|p| p.eq_ignore_ascii_case(s))
        .with_context(|| format!("invalid since {s:?} (want 1d|7d|14d|30d|90d|1y|all)"))
}

pub fn reporting_timezone(config: &Config) -> Result<chrono_tz::Tz> {
    let timezone = config.timezone.trim();
    if timezone.is_empty() {
        return Ok(chrono_tz::UTC);
    }
    timezone.parse().with_context(|| format!("invalid reporting timezone {timezone:?}; use an IANA name such as UTC or America/Denver"))
}

pub fn validate_preferences(config: &Config) -> Result<(SortField, usize)> {
    if config.fuzzy {
        bail!(
            "automatic name merging (fuzzy) has been removed; set fuzzy to false or remove it, then use M in the dashboard to merge contributors"
        );
    }
    reporting_timezone(config)?;
    match config.theme.to_lowercase().as_str() {
        "" | "auto" | "dark" | "light" => {}
        _ => bail!(
            "invalid theme {:?} in config (want auto|light|dark)",
            config.theme
        ),
    }
    let sort = SortField::parse(if config.sort.is_empty() {
        "total"
    } else {
        &config.sort
    })?;
    Ok((sort, time_index_for_since(&config.since)?))
}

pub fn expand_home(s: &str) -> PathBuf {
    if (s == "~" || s.starts_with("~/"))
        && let Some(home) = env::var_os("HOME").filter(|home| !home.is_empty())
    {
        return if s == "~" {
            PathBuf::from(home)
        } else {
            join_home(&PathBuf::from(home), &s[2..])
        };
    }
    PathBuf::from(s)
}

fn join_home(home: &Path, suffix: &str) -> PathBuf {
    // filepath.Join cleans dot segments and does not let an extra slash after
    // ~/ discard the home directory as PathBuf::join would.
    let joined = home.join(suffix.trim_start_matches(std::path::MAIN_SEPARATOR));
    let mut clean = PathBuf::new();
    for component in joined.components() {
        match component {
            std::path::Component::CurDir => {}
            std::path::Component::ParentDir => {
                if clean.file_name().is_some_and(|name| name != "..") {
                    clean.pop();
                } else if !clean.has_root() {
                    clean.push("..");
                }
            }
            component => clean.push(component.as_os_str()),
        }
    }
    if clean.as_os_str().is_empty() {
        clean.push(".");
    }
    clean
}

pub fn scan_paths(cli: &Cli, config: &Config) -> Result<Vec<PathBuf>> {
    let selected = if !cli.group.is_empty() {
        config
            .groups
            .get(&cli.group)
            .with_context(|| format!("unknown group {:?}", cli.group))?
            .as_deref()
            .unwrap_or(&[])
    } else if !cli.paths.is_empty() {
        &cli.paths
    } else if !config.paths.is_empty() {
        &config.paths
    } else {
        return Ok(vec![PathBuf::from(".")]);
    };
    Ok(selected.iter().map(|p| expand_home(p)).collect())
}

pub fn validate_scan_paths(paths: &[PathBuf]) -> Result<()> {
    for path in paths {
        let info = fs::metadata(path).with_context(|| format!("scan path {:?}", path))?;
        if !info.is_dir() {
            bail!("scan path {:?} is not a directory", path)
        }
    }
    Ok(())
}

pub fn build_exclude_set(repos: &[Repository], patterns: &[String]) -> Result<HashSet<String>> {
    let mut excluded = HashSet::new();
    for pattern in patterns {
        let mut exact = false;
        for repo in repos {
            if repo.name == *pattern
                || repo
                    .path
                    .file_name()
                    .is_some_and(|name| name == pattern.as_str())
            {
                exact = true;
                excluded.insert(repo.id.clone());
            }
        }
        if let Err(error) = file_pattern_matches(pattern, "") {
            if exact {
                continue;
            }
            bail!("pattern {pattern:?}: {error}");
        }
        for repo in repos {
            let basename = repo.path.file_name().unwrap_or_default().to_string_lossy();
            if file_pattern_matches(pattern, &basename).unwrap_or(false)
                || file_pattern_matches(pattern, &repo.name).unwrap_or(false)
            {
                excluded.insert(repo.id.clone());
            }
        }
    }
    Ok(excluded)
}

#[derive(Debug)]
enum PatternTerm {
    Literal(u8),
    Any,
    Class {
        negated: bool,
        ranges: Vec<(char, char)>,
    },
}

// Go filepath.Match has no recursive ** operator, uses ^ for class negation,
// and supports backslash escaping on Unix. Parse one star-delimited section
// at a time, retaining Go's validation order for malformed trailing sections.
fn pattern_section(pattern: &str) -> (bool, &str, &str) {
    let section = pattern.trim_start_matches('*');
    let star = section.len() != pattern.len();
    let mut escaped = false;
    let mut in_class = false;
    for (index, character) in section.char_indices() {
        if escaped {
            escaped = false;
            continue;
        }
        match character {
            '\\' if !cfg!(windows) => escaped = true,
            '[' => in_class = true,
            ']' => in_class = false,
            '*' if !in_class => return (star, &section[..index], &section[index..]),
            _ => {}
        }
    }
    (star, section, "")
}

fn class_character(chars: &[char], index: &mut usize) -> Result<char> {
    let mut character = *chars.get(*index).context("syntax error in pattern")?;
    if matches!(character, '-' | ']') {
        bail!("syntax error in pattern");
    }
    *index += 1;
    if character == '\\' && !cfg!(windows) {
        character = *chars.get(*index).context("syntax error in pattern")?;
        *index += 1;
    }
    if *index == chars.len() {
        bail!("syntax error in pattern");
    }
    Ok(character)
}

fn parse_pattern_section(section: &str) -> Result<Vec<PatternTerm>> {
    let chars: Vec<_> = section.chars().collect();
    let mut terms = Vec::new();
    let mut index = 0;
    while index < chars.len() {
        let mut character = chars[index];
        index += 1;
        match character {
            '?' => terms.push(PatternTerm::Any),
            '[' => {
                let negated = chars.get(index) == Some(&'^');
                index += usize::from(negated);
                let mut ranges = Vec::new();
                loop {
                    if chars.get(index) == Some(&']') && !ranges.is_empty() {
                        index += 1;
                        break;
                    }
                    let lo = class_character(&chars, &mut index)?;
                    let hi = if chars[index] == '-' {
                        index += 1;
                        class_character(&chars, &mut index)?
                    } else {
                        lo
                    };
                    ranges.push((lo, hi));
                }
                terms.push(PatternTerm::Class { negated, ranges });
            }
            _ => {
                if character == '\\' && !cfg!(windows) {
                    character = *chars.get(index).context("syntax error in pattern")?;
                    index += 1;
                }
                let mut encoded = [0; 4];
                terms.extend(
                    character
                        .encode_utf8(&mut encoded)
                        .bytes()
                        .map(PatternTerm::Literal),
                );
            }
        }
    }
    Ok(terms)
}

fn leading_rune(bytes: &[u8]) -> (char, usize) {
    let width = match bytes[0] {
        0..=0x7f => 1,
        0xc2..=0xdf => 2,
        0xe0..=0xef => 3,
        0xf0..=0xf4 => 4,
        _ => return ('\u{fffd}', 1),
    };
    bytes
        .get(..width)
        .and_then(|bytes| std::str::from_utf8(bytes).ok())
        .and_then(|text| text.chars().next())
        .map(|character| (character, width))
        .unwrap_or(('\u{fffd}', 1))
}

fn match_pattern_section(terms: &[PatternTerm], mut name: &[u8]) -> Option<usize> {
    let original = name.len();
    for term in terms {
        if name.is_empty() {
            return None;
        }
        let width = match term {
            PatternTerm::Literal(byte) => {
                if name[0] != *byte {
                    return None;
                }
                1
            }
            PatternTerm::Any => {
                if name[0] == std::path::MAIN_SEPARATOR as u8 {
                    return None;
                }
                leading_rune(name).1
            }
            PatternTerm::Class { negated, ranges } => {
                let (character, width) = leading_rune(name);
                let matched = ranges
                    .iter()
                    .any(|&(lo, hi)| lo <= character && character <= hi);
                if matched == *negated {
                    return None;
                }
                width
            }
        };
        name = &name[width..];
    }
    Some(original - name.len())
}

fn file_pattern_matches(mut pattern: &str, name: &str) -> Result<bool> {
    let mut name = name.as_bytes();
    while !pattern.is_empty() {
        let (star, section, rest) = pattern_section(pattern);
        if star && section.is_empty() {
            return Ok(!name.contains(&(std::path::MAIN_SEPARATOR as u8)));
        }
        let terms = parse_pattern_section(section)?;
        let limit = if star {
            name.iter()
                .position(|byte| *byte == std::path::MAIN_SEPARATOR as u8)
                .unwrap_or(name.len())
        } else {
            0
        };
        let matched = (0..=limit).find_map(|skip| {
            let consumed = match_pattern_section(&terms, &name[skip..])?;
            let end = skip + consumed;
            (!rest.is_empty() || end == name.len()).then_some(end)
        });
        let Some(end) = matched else {
            return Ok(false);
        };
        name = &name[end..];
        pattern = rest;
    }
    Ok(name.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn startup_defaults_to_github_unless_local_inputs_are_explicit() {
        for args in [
            vec![],
            vec!["--github"],
            vec!["--config", "custom.json"],
            vec!["--github=false"],
        ] {
            let cli = Cli::parse(args.into_iter().map(str::to_owned)).unwrap();
            assert!(cli.github_mode().unwrap());
        }
        for args in [
            vec!["."],
            vec!["one", "two"],
            vec!["--group", "team"],
            vec!["--config", "custom.json", "."],
        ] {
            let cli = Cli::parse(args.into_iter().map(str::to_owned)).unwrap();
            assert!(!cli.github_mode().unwrap());
        }
        for args in [vec!["--github", "."], vec!["--github", "--group", "team"]] {
            let cli = Cli::parse(args.into_iter().map(str::to_owned)).unwrap();
            assert!(cli.github_mode().is_err());
        }
    }

    #[test]
    fn cli_github_entry_preserves_local_flags_and_export_is_removed() {
        assert!(Cli::parse(["--github".into()]).unwrap().github);
        assert!(!Cli::parse(["--github=false".into()]).unwrap().github);
        assert!(Cli::parse(["--github=invalid".into()]).is_err());
        let cli = Cli::parse(
            ["--config=x.json", "--group", "backend", "repo", "--version"].map(String::from),
        )
        .unwrap();
        assert!(!cli.version);
        assert_eq!(cli.config, "x.json");
        assert_eq!(cli.paths, ["repo", "--version"]);
        for flag in ["--export", "-export", "--export=false"] {
            assert!(
                Cli::parse([flag.into()])
                    .unwrap_err()
                    .to_string()
                    .contains("removed")
            );
        }
    }
    #[test]
    fn timezone_defaults_and_legacy_name_merging_validation() {
        let mut cfg = Config::default();
        assert_eq!(reporting_timezone(&cfg).unwrap(), chrono_tz::UTC);
        cfg.timezone = "America/Denver".into();
        assert_eq!(
            reporting_timezone(&cfg).unwrap(),
            chrono_tz::America::Denver
        );
        cfg.timezone = "Invalid/Zone".into();
        assert!(validate_preferences(&cfg).is_err());
        cfg.timezone.clear();
        cfg.fuzzy = true;
        assert!(
            validate_preferences(&cfg)
                .unwrap_err()
                .to_string()
                .contains("use M")
        );
        cfg.fuzzy = false;
        assert!(validate_preferences(&cfg).is_ok());
        let decoded: Config = serde_json::from_str(r#"{"timezone":"Asia/Tokyo"}"#).unwrap();
        assert_eq!(
            reporting_timezone(&decoded).unwrap(),
            chrono_tz::Asia::Tokyo
        );
    }
    #[test]
    fn config_is_strict_but_accepts_go_nulls() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("config.json");
        assert!(load_config(&p, false).is_ok());
        assert!(load_config(&p, true).is_err());
        for body in [r#"{"typo":true}"#, r#"{} {}"#] {
            fs::write(&p, body).unwrap();
            assert!(load_config(&p, true).is_err());
        }
        fs::write(
            &p,
            r#"{"paths":null,"fuzzy":null,"sort":null,"groups":{"empty":null}}"#,
        )
        .unwrap();
        let c = load_config(&p, true).unwrap();
        assert!(!c.fuzzy);
        assert!(c.paths.is_empty());
        assert!(
            scan_paths(
                &Cli {
                    group: "empty".into(),
                    ..Cli::default()
                },
                &c
            )
            .unwrap()
            .is_empty()
        );
    }
    #[test]
    fn presets_are_validated() {
        assert_eq!(time_index_for_since("").unwrap(), 2);
        assert_eq!(time_index_for_since(" ALL ").unwrap(), 6);
        assert!(time_index_for_since("21d").is_err());
    }
    #[test]
    fn duplicate_repo_labels_and_literal_patterns() {
        let repos = crate::git::new_repositories(&[
            "/src/org-a/api".into(),
            "/src/org-b/api".into(),
            "/src/[archive".into(),
        ]);
        let ex = build_exclude_set(&repos, &["org-a/*".into(), "[archive".into()]).unwrap();
        assert!(ex.contains(&repos[0].id));
        assert!(!ex.contains(&repos[1].id));
        assert!(ex.contains(&repos[2].id));
        assert!(build_exclude_set(&repos, &["[".into()]).is_err());
    }
    #[test]
    fn json_keys_duplicates_and_nulls_follow_go_decoding() {
        let config: Config = serde_json::from_str(
            r#"{
            "PATHS":["first","second"],"Paths":["updated"],"paths":[null,null],
            "Sort":"net","SORT":null,"FUZZY":true,"fuzzy":null,
            "depth":2,"Depth":null,"all_fileſ":true,
            "groups":{"one":["a"]},"GROUPS":{"two":[null],"one":null}
        }"#,
        )
        .unwrap();
        assert_eq!(config.paths, ["updated", "second"]);
        assert_eq!(config.sort, "net");
        assert!(config.fuzzy);
        assert!(config.all_files);
        assert_eq!(config.depth, 2);
        assert_eq!(config.groups["one"], None);
        assert_eq!(config.groups["two"], Some(vec![String::new()]));
        let config: Config = serde_json::from_str(
            r#"{
            "paths":["before"],"paths":null,"paths":[null],
            "groups":{"old":["a"]},"groups":null,"groups":{"new":[]},
            "exclude":[null],"AI_IDENTITIES":[null],"BOT_IDENTITIES":null
        }"#,
        )
        .unwrap();
        assert_eq!(config.paths, [""]);
        assert_eq!(config.exclude, [""]);
        assert_eq!(config.ai_identities, [""]);
        assert!(config.bot_identities.is_empty());
        assert_eq!(config.groups.len(), 1);
        assert_eq!(config.groups["new"], Some(vec![]));
        assert_eq!(
            serde_json::from_str::<Config>("null").unwrap(),
            Config::default()
        );
        for invalid in [
            r#"{"fuzzy":1}"#,
            r#"{"depth":1.5}"#,
            r#"{"paths":[42]}"#,
            r#"{"groups":{"team":false}}"#,
            r#"{"UNKNOWN":null}"#,
            r#"[]"#,
        ] {
            assert!(
                serde_json::from_str::<Config>(invalid).is_err(),
                "{invalid}"
            );
        }
    }

    #[test]
    fn cli_boolean_values_repetition_help_and_terminators() {
        for value in ["1", "t", "T", "TRUE", "true", "True"] {
            assert!(Cli::parse([format!("--version={value}")]).unwrap().version);
        }
        for value in ["0", "f", "F", "FALSE", "false", "False"] {
            assert!(!Cli::parse([format!("--version={value}")]).unwrap().version);
        }
        assert!(Cli::parse(["--version=TrUe".into()]).is_err());
        assert!(Cli::parse(["---version".into()]).is_err());
        assert!(Cli::parse(["--=version".into()]).is_err());
        assert!(Cli::parse(["--group".into()]).is_err());
        let cli = Cli::parse(
            [
                "-version",
                "--version=false",
                "--group=old",
                "-group",
                "--",
                "repo",
            ]
            .map(String::from),
        )
        .unwrap();
        assert!(!cli.version);
        assert_eq!(cli.group, "--");
        assert_eq!(cli.paths, ["repo"]);
        let cli = Cli::parse(["--", "--export", "repo"].map(String::from)).unwrap();
        assert_eq!(cli.paths, ["--export", "repo"]);
        assert!(
            Cli::parse(["--help=false".into(), "--invalid".into()])
                .unwrap()
                .help
        );
    }

    #[test]
    fn group_paths_take_precedence_and_names_remain_case_sensitive() {
        let config = Config {
            paths: vec!["configured".into()],
            groups: BTreeMap::from([
                ("Team".into(), Some(vec!["grouped".into()])),
                ("empty".into(), None),
            ]),
            ..Config::default()
        };
        let mut cli = Cli {
            paths: vec!["positional".into()],
            ..Cli::default()
        };
        assert_eq!(
            scan_paths(&cli, &config).unwrap(),
            [PathBuf::from("positional")]
        );
        cli.group = "Team".into();
        assert_eq!(
            scan_paths(&cli, &config).unwrap(),
            [PathBuf::from("grouped")]
        );
        cli.group = "team".into();
        assert!(scan_paths(&cli, &config).is_err());
        cli.group = "empty".into();
        assert!(scan_paths(&cli, &config).unwrap().is_empty());
        assert_eq!(
            scan_paths(&Cli::default(), &config).unwrap(),
            [PathBuf::from("configured")]
        );
        assert_eq!(
            scan_paths(&Cli::default(), &Config::default()).unwrap(),
            [PathBuf::from(".")]
        );
    }

    #[cfg(unix)]
    #[test]
    fn exclusion_patterns_follow_filepath_match_not_recursive_globs() {
        for (pattern, name, expected) in [
            ("**/api", "org/api", true),
            ("**/api", "parent/org/api", false),
            ("org-**/api", "org-a/api", true),
            ("[!a]", "!", true),
            ("[!a]", "a", true),
            ("[!a]", "b", false),
            ("[^a]", "b", true),
            ("[^a]", "a", false),
            (r"a\*b", "a*b", true),
            (r"[\-]", "-", true),
            ("[z-a]", "m", false),
            ("[/]", "/", true),
            ("*", ".hidden", true),
            ("?", "東", true),
            ("*??", "東", true),
            ("?", "/", false),
        ] {
            assert_eq!(
                file_pattern_matches(pattern, name).unwrap(),
                expected,
                "{pattern:?} vs {name:?}"
            );
        }
        for pattern in ["[", "[^", "[]a]", "[-]", "[x-]", "[-x]", "[a-b-c]", "\\"] {
            assert!(file_pattern_matches(pattern, "").is_err(), "{pattern:?}");
        }
        // The original validation does not inspect later star-delimited
        // sections after an earlier section fails to match.
        assert!(!file_pattern_matches("x*[", "").unwrap());
        assert!(file_pattern_matches("x*[", "x").is_err());
        let repos = vec![
            Repository {
                id: "one".into(),
                path: "/repos/a*b".into(),
                name: "a*b".into(),
            },
            Repository {
                id: "two".into(),
                path: "/repos/api".into(),
                name: "parent/org/api".into(),
            },
            Repository {
                id: "three".into(),
                path: "/repos/api".into(),
                name: "org/api".into(),
            },
        ];
        let excluded = build_exclude_set(&repos, &[r"a\*b".into(), "**/api".into()]).unwrap();
        assert_eq!(excluded, HashSet::from(["one".into(), "three".into()]));
    }

    #[cfg(unix)]
    #[test]
    fn home_join_cleans_segments_without_discarding_home() {
        let home = Path::new("/Users/test");
        assert_eq!(join_home(home, "/repo"), Path::new("/Users/test/repo"));
        assert_eq!(
            join_home(home, "src/../repo"),
            Path::new("/Users/test/repo")
        );
        assert_eq!(join_home(home, "../repo"), Path::new("/Users/repo"));
        assert_eq!(join_home(home, "../../../repo"), Path::new("/repo"));
        assert_eq!(expand_home("~someone/repo"), Path::new("~someone/repo"));
        assert_eq!(expand_home("src/../repo"), Path::new("src/../repo"));
    }
}
