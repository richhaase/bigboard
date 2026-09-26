use anyhow::{Context, Result, bail};
use bigboard::{
    config::{self, Cli},
    git,
    identity::{self, IdentityStore},
    model::AnalysisOptions,
};
use std::process::ExitCode;

fn version() -> String {
    format!(
        "bigboard {} (commit: {}, built: {})",
        option_env!("BIGBOARD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")),
        option_env!("BIGBOARD_COMMIT").unwrap_or("none"),
        option_env!("BIGBOARD_BUILD_DATE").unwrap_or("unknown")
    )
}
fn usage() {
    eprintln!(
        "Usage: bigboard [flags] [paths...]\n  No paths: open saved GitHub repos, or choose them on first launch.\n  With paths: analyze local repos (use . for the current directory).\n  -config string\n        Config file path (default ~/.config/bigboard/config.json)\n  -group string\n        Use a named repo group from the config file\n  -version\n        Print version and exit"
    );
}
fn diagnostic(s: &str) -> String {
    s.chars()
        .flat_map(|c| {
            if c.is_control() {
                c.escape_default().collect::<Vec<_>>()
            } else {
                vec![c]
            }
        })
        .collect()
}
fn run(cli: Cli) -> Result<()> {
    if cli.version {
        println!("{}", version());
        return Ok(());
    }
    let explicit = !cli.config.is_empty();
    let path = if explicit {
        cli.config.clone().into()
    } else {
        config::default_config_path()
    };
    let cfg = config::load_config(&path, explicit)
        .with_context(|| format!("reading config {}", path.display()))?;
    let (sort, time_idx) = config::validate_preferences(&cfg)?;
    let github_mode = cli.github_mode();
    let found = if github_mode {
        Vec::new()
    } else {
        let paths = config::scan_paths(&cli, &cfg)?;
        config::validate_scan_paths(&paths)?;
        git::discover_repos_depth(&paths, cfg.depth.max(1) as usize)
    };
    if !github_mode && found.is_empty() {
        bail!("No local Git repositories found in the selected paths or group");
    }
    let repos = git::new_repositories(&found);
    let excluded = config::build_exclude_set(&repos, &cfg.exclude).context("invalid exclusion")?;
    let options = AnalysisOptions {
        include_generated: cfg.all_files,
        ai_identities: cfg.ai_identities.clone(),
        bot_identities: cfg.bot_identities.clone(),
        timezone: config::reporting_timezone(&cfg)?,
    };
    let identity_path = identity::default_global_path();
    if !identity_path.is_absolute() {
        bail!("set HOME or an absolute XDG_CONFIG_HOME to store global contributor mappings");
    }
    let identities = IdentityStore::load(&identity_path)?;
    bigboard::tui::run(
        repos,
        sort,
        excluded,
        option_env!("BIGBOARD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")),
        time_idx,
        options,
        &cfg.theme,
        identities,
        identity_path,
        github_mode,
    )
}
fn main() -> ExitCode {
    let cli = match Cli::parse(std::env::args().skip(1)) {
        Ok(cli) => cli,
        Err(error) => {
            eprintln!("{error}");
            usage();
            return ExitCode::from(2);
        }
    };
    if cli.help {
        usage();
        return ExitCode::SUCCESS;
    }
    match run(cli) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("Error: {}", diagnostic(&format!("{error:#}")));
            ExitCode::FAILURE
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn empty_explicit_local_path_does_not_fall_back_to_github() {
        let dir = tempfile::tempdir().unwrap();
        let config = dir.path().join("config.json");
        std::fs::write(&config, "{}").unwrap();
        let error = run(Cli {
            config: config.to_string_lossy().into_owned(),
            paths: vec![dir.path().to_string_lossy().into_owned()],
            ..Default::default()
        })
        .unwrap_err();
        assert!(
            error
                .to_string()
                .contains("No local Git repositories found")
        );
    }
}
