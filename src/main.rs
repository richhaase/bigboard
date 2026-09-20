use anyhow::{Context, Result, bail};
use bigboard::{
    config::{self, Cli},
    git,
    model::{AnalysisOptions, Repository},
    scan,
    stats::{self, SortField},
};
use std::{
    collections::{HashMap, HashSet},
    io::{self, Write},
    process::ExitCode,
};

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
        "Usage: bigboard [flags] [paths...]\n  -config string\n        Config file path (default ~/.config/bigboard/config.json)\n  -export\n        Print contributor stats as JSON and exit\n  -group string\n        Use a named repo group from the config file\n  -version\n        Print version and exit"
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
fn export(
    repos: Vec<Repository>,
    excluded: HashSet<String>,
    sort: SortField,
    options: AnalysisOptions,
) -> Result<()> {
    let session = scan::start_scan(repos.clone(), options.clone());
    let mut results = HashMap::new();
    while let Ok(result) = session.receiver.recv() {
        results.insert(result.repository.id.clone(), result);
    }
    let mut all = Vec::new();
    let mut failed = 0;
    for repo in &repos {
        let result = results
            .remove(&repo.id)
            .context("repository scan ended without a result")?;
        if let Some(error) = result.error {
            eprintln!(
                "warning: skipping {}: {}",
                diagnostic(&repo.path.to_string_lossy()),
                diagnostic(&error)
            );
            failed += 1;
        } else {
            all.extend(result.records)
        }
    }
    if failed > 0 && failed == repos.len() {
        bail!("all {} repositories failed to scan", failed)
    }
    let filtered = stats::filter_by_repo(&all, &excluded);
    let mut authors = stats::aggregate(
        &filtered,
        &stats::AggregateOptions {
            fuzzy_matching: options.fuzzy_matching,
            bot_identities: options.bot_identities,
        },
    );
    stats::sort(&mut authors, sort);
    let stdout = io::stdout();
    let mut out = stdout.lock();
    // Go's empty aggregation is a non-nil slice and encodes as [].
    serde_json::to_writer_pretty(&mut out, &authors)?;
    writeln!(&mut out)?;
    Ok(())
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
    let paths = config::scan_paths(&cli, &cfg)?;
    config::validate_scan_paths(&paths)?;
    let found = git::discover_repos_depth(&paths, cfg.depth.max(1) as usize);
    if found.is_empty() {
        bail!("no git repositories found in {:?}", paths)
    }
    let repos = git::new_repositories(&found);
    let excluded = config::build_exclude_set(&repos, &cfg.exclude).context("invalid exclusion")?;
    let options = AnalysisOptions {
        fuzzy_matching: cfg.fuzzy,
        include_generated: cfg.all_files,
        ai_identities: cfg.ai_identities,
        bot_identities: cfg.bot_identities,
    };
    if cli.export {
        export(repos, excluded, sort, options)
    } else {
        bigboard::tui::run(
            repos,
            sort,
            excluded,
            option_env!("BIGBOARD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")),
            time_idx,
            options,
            &cfg.theme,
        )
    }
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
