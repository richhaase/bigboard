import Darwin
import Foundation

public enum BigboardCommandLine {
    public static func dispatch(_ arguments: [String]) -> Int32? {
        commandArguments(arguments).map(runBlocking)
    }
    static func commandArguments(_ arguments: [String]) -> [String]? {
        let name = (arguments.first.map { ($0 as NSString).lastPathComponent }) ?? "Bigboard"
        let rest = Array(arguments.dropFirst())
        if name == "Bigboard" {
            guard let first = rest.first, CommandLineTool.commands.contains(first) else { return nil }
        }
        return rest
    }
    private final class ExitStatus: @unchecked Sendable { var value: Int32 = 0 }
    private static func runBlocking(_ arguments: [String]) -> Int32 {
        let status = ExitStatus()
        let done = DispatchSemaphore(value: 0)
        Task.detached {
            status.value = await CommandLineTool(environment: .system()).run(arguments)
            done.signal()
        }
        done.wait()
        return status.value
    }
}

struct CommandLineEnvironment: Sendable {
    var storage: WorkspaceStorage
    var auth: GitHubCLIAuth
    var transport: any GitHubTransport = URLSessionGitHubTransport()
    var now: @Sendable () -> Date
    var openApplication: @Sendable () throws -> Void
    var refreshHistory: @Sendable (GitHistory, Repository) async throws -> Repository = {
        try await $0.refresh($1)
    }
    var write: @Sendable (String) -> Void = { FileHandle.standardOutput.write(Data($0.utf8)) }
    var writeError: @Sendable (String) -> Void = { FileHandle.standardError.write(Data($0.utf8)) }

    static func system() -> CommandLineEnvironment {
        let bundle = enclosingApplication()
        let identity =
            bundle.flatMap { Bundle(url: $0)?.bundleIdentifier }.flatMap(ApplicationIdentity.init(rawValue:))
            ?? .production
        let storage = WorkspaceStorage.system(identity: identity)
        return CommandLineEnvironment(
            storage: storage, auth: GitHubCLIAuth(storage: storage), now: Date.init,
            openApplication: {
                guard let bundle else {
                    throw BigboardError.message(
                        "Bigboard.app was not found next to this command. Open Bigboard from Applications.")
                }
                let process = Process()
                process.executableURL = URL(fileURLWithPath: "/usr/bin/open")
                process.arguments = [bundle.path]
                try process.run()
                process.waitUntilExit()
                guard process.terminationStatus == 0 else {
                    throw BigboardError.message("Could not open \(bundle.path).")
                }
            })
    }
    static func enclosingApplication() -> URL? {
        var size: UInt32 = 0
        _ = _NSGetExecutablePath(nil, &size)
        var buffer = [CChar](repeating: 0, count: Int(size) + 1)
        guard _NSGetExecutablePath(&buffer, &size) == 0 else { return nil }
        let bytes = buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }
        let executable = URL(fileURLWithPath: String(decoding: bytes, as: UTF8.self)).resolvingSymlinksInPath()
        let app = executable.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        guard app.pathExtension == "app",
            executable.deletingLastPathComponent().path.hasSuffix(".app/Contents/MacOS")
        else { return nil }
        return app
    }
}

struct CommandLineTool: Sendable {
    static let commands: Set<String> = ["open", "status", "activity", "prs", "refresh", "config", "help", "--help", "-h", "version", "--version"]
    var environment: CommandLineEnvironment

    func run(_ arguments: [String]) async -> Int32 {
        guard let command = arguments.first else {
            environment.write(Self.usage)
            return 0
        }
        let options = Array(arguments.dropFirst())
        if let help = Self.help[command], options.contains(where: { $0 == "--help" || $0 == "-h" }) {
            environment.write(help)
            return 0
        }
        do {
            switch command {
            case "open": return open()
            case "status": return try await status(try Options(options, flags: ["--json"]))
            case "activity":
                return try await activity(
                    try Options(options, flags: ["--json", "--include-bots", "--compare", "--commits", "--timeline"], values: ["--days", "--repo", "--group", "--top", "--people-sort", "--depth", "--path", "--since", "--until", "--person"]))
            case "prs":
                return try await pullRequests(
                    try Options(
                        options, flags: ["--json", "--files"],
                        values: [
                            "--repo", "--group", "--mine", "--author", "--review", "--checks", "--state", "--search",
                            "--sort", "--top",
                        ]))
            case "refresh": return try await refresh(try Options(options, values: ["--repo", "--group"]))
            case "config": return try await config(options)
            case "help", "--help", "-h":
                guard let topic = options.first else {
                    environment.write(Self.usage)
                    return 0
                }
                guard options.count == 1, let help = Self.help[topic] else {
                    throw UsageError("No help for: \(options.joined(separator: " "))")
                }
                environment.write(help)
                return 0
            case "version", "--version":
                let bundle = CommandLineEnvironment.enclosingApplication().flatMap { Bundle(url: $0) }
                let version = bundle?.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
                environment.write("bigboard \(version ?? "development")\n")
                return 0
            default:
                throw UsageError("Unknown command: \(command)")
            }
        } catch let error as UsageError {
            environment.writeError("bigboard: \(error.message)\n\n" + (Self.help[command] ?? Self.usage))
            return 64
        } catch {
            environment.writeError("bigboard: \(error.localizedDescription)\n")
            return 1
        }
    }

    static let reference = "https://github.com/richhaase/bigboard/blob/main/docs/reference.md#command-line"
    static let usage = """
        bigboard: recorded change across your GitHub clones

        Bigboard reads Git history from your own clones, in the source folders chosen
        in the app, and pull requests through the GitHub CLI. It describes recorded
        change, not productivity.

        Usage: bigboard <command> [options]

        Commands:
          status      Show the GitHub CLI account, data locations, and collected clones.
          activity    Summarize recorded change: totals, repositories, paths, and people.
          prs         List open pull requests, with what needs your attention.
          refresh     Fetch each clone's default branch with your Git setup.
          config      Show or change settings: sources, repositories, groups, and more.
          open        Open the Bigboard app.
          help        Show help for a command, for example: bigboard help activity
          version     Show the version.

        For scripts and agents:
          - Add --json to status, activity, prs, and config for stable output with
            snake_case keys.
          - status and activity never fetch history. They confirm the GitHub CLI
            account with GitHub, or use the last confirmed account when offline.
            refresh fetches from each clone's remote, and prs asks GitHub for open
            pull requests.
          - config changes are checked like Settings in the app, and a running app
            reloads them.
          - Exit status: 0 success, 2 incomplete evidence, 64 usage error, 1 other error.
          - In activity --json, check evidence and problems before relying on totals.
            unknown_line_commits above 0 means line totals are a lower bound.
          - Reference: \(reference)

        """
    static let help: [String: String] = [
        "config": """
            Usage: bigboard config [get [SETTING [NAME]]] [--json]
                   bigboard config set|add|remove|unset SETTING [VALUE...]

            Show or change the settings the app uses. Changes are checked like Settings
            in the app: repositories need a local clone in a source folder, and the
            running app reloads them.

            Lists (set replaces, add and remove change entries, unset clears):
              sources          Folders that hold your clones. ~ and relative paths are
                               expanded.
              repositories     Collected repositories, as owner/name.
              exclude          Repository glob patterns to leave out.
              ai-identities    Extra AI author emails or @domains.
              bot-identities   Extra bot emails, @domains, or exact names.

            Named entries:
              group NAME REPOSITORY...              A repository group.
              work-area REPOSITORY NAME PATH...     Paths that make up a work area.
              identity EMAIL NAME [ALIAS...]        One person's name and other emails.
                                                    add and remove change aliases.
              unset removes the named entry. get takes the leading names to show one.

            Values (set, or unset for the default):
              since-days          Startup date range in days; 0 for all time (14).
              contributor-sort    recent, change, commits, name, added, removed, net,
                                  or ai (recent).
              refresh-interval    Automatic refresh minutes: 0, 5, 15, 30, or 60 (15).
              request-budget      Pull-request API requests per refresh (128).
              cached-only         true to read cached history without GitHub (false).
              all-files           true to count generated, vendored, fixture, and
                                  bulk-data lines (false).

            Options:
              --json    Print JSON.

            Examples:
              bigboard config
              bigboard config add sources ~/src
              bigboard config add repositories owner/name
              bigboard config set group Team owner/one owner/two
              bigboard config set since-days 30
            """,
        "status": """
            Usage: bigboard status [--json]

            Show the GitHub CLI account, the settings and cache locations, the source
            folders, and each collected repository with its local clone, if found.

            Options:
              --json    Print JSON.

            """,
        "activity": """
            Usage: bigboard activity [options]

            Summarize recorded change in your clones' default branches: totals, excluded
            lines, repositories, top paths, and people. Reads cached history and never
            fetches; run bigboard refresh first for current evidence.

            Options:
              --days N              Days to include (default: the app's range; 0 = all history).
              --repo OWNER/NAME     Limit to a collected repository (repeatable).
              --group NAME          Limit to a saved repository group.
              --top N               Rows per section (default 10).
              --path PATH           Narrow to a file or folder in one repository (needs exactly one
                                    --repo). Paths below it are grouped from there.
              --since TIME          Only changes after this time. A date (2026-10-01, local
                                    midnight) or ISO 8601 time. Use instead of --days.
              --until TIME          Only changes up to and including this time.
              --commits             List the supporting commits, newest first, limited by --top.
              --depth N             Group paths N folders deep (1–10, default 1), so a monorepo's
                                    packages/ splits into packages/<name>/ at depth 2.
              --compare             Also measure the previous window of the same length, with
                                    previous values and a change column (needs --days or --since).
              --people-sort ORDER   name (default), lines, or commits. lines and commits rank
                                    people with their counts, limited by --top.
              --person WHO          Narrow to one person, by email, alias, or exact name
                                    (repeatable). Their bot activity is included.
              --timeline            Add commits and lines per day (ranges up to 30 days), week
                                    (up to 180), or month, as the Activity board does.
              --include-bots        Include bot accounts.
              --json                Print JSON.

            JSON keys: days, scope, depth, totals, comparison, excluded, repositories,
            directories, people, commits, timeline, evidence, problems. scope echoes the
            repositories, path, interval, people, and bot policy the numbers cover. timeline
            has a unit (hour, day, week, or month) and points with each bucket's start. With --compare, comparison holds the previous
            window's start, end, and totals, and each repository and path row has previous. Each evidence entry gives a repository's scanned revision, scan and
            fetch times, and history, line-count, and path completeness.

            Exit status: 0, or 2 when evidence is incomplete (see problems).

            """,
        "prs": """
            Usage: bigboard prs [options]

            List open pull requests, including drafts, in your collected repositories, with
            review and check state and what needs your attention. Asks GitHub each time;
            pull requests are not cached.

            Options:
              --repo OWNER/NAME     Limit to a collected repository (repeatable).
              --group NAME          Limit to a saved repository group.
              --mine KIND           attention, authored, requested, or updated (repeatable).
              --author LOGIN        Limit to an author (repeatable).
              --review STATE        approved, changes_requested, review_required, or unknown
                                    (repeatable).
              --checks STATE        success, failure, error, pending, expected, or unknown
                                    (repeatable).
              --state STATE         open (ready for review) or draft (repeatable).
              --search TEXT         Match repository, title, author, or number.
              --sort ORDER          updated (default), attention, repository, author, or title.
              --top N               Pull requests to list (default 25).
              --files               Include changed file paths in JSON.
              --json                Print JSON.

            Filters of different kinds combine with AND; repeated values of one kind with OR.
            Exit status: 0, or 2 when pull-request evidence is incomplete (see problems).

            """,
        "refresh": """
            Usage: bigboard refresh [options]

            Fetch each collected clone's remote-tracking default branch with your own Git
            setup, then rescan its history. Never prompts and never changes your branches,
            index, working files, or Git config.

            Options:
              --repo OWNER/NAME     Limit to a repository (repeatable).
              --group NAME          Limit to a saved repository group.

            Exit status: 0, or 2 when any repository could not be refreshed.

            """,
        "open": """
            Usage: bigboard open

            Open the Bigboard app.

            """,
        "version": """
            Usage: bigboard version

            Show the version.

            """,
        "help": """
            Usage: bigboard help [command]

            Show general help, or help for one command.

            """,
    ]

    private func open() -> Int32 {
        do {
            try environment.openApplication()
            return 0
        } catch {
            environment.writeError("bigboard: \(error.localizedDescription)\n")
            return 1
        }
    }

    private struct Workspace {
        var account: GitHubIdentity
        var configuration: WorkspaceConfiguration
        var repositories: [Repository]
        var history: GitHistory
    }
    private func workspace(_ options: Options, requireSources: Bool = true) async throws -> Workspace {
        let state = await environment.auth.status()
        guard state.signedIn else {
            throw BigboardError.message(state.error.isEmpty ? GitHubCLIFailure.signedOut.localizedDescription : state.error)
        }
        let account = GitHubIdentity(login: state.login, accountID: state.accountID)
        let configuration = try environment.storage.loadConfiguration()
        let group = options.value("--group") ?? ""
        guard group.isEmpty || configuration.groups[group] != nil else {
            throw UsageError("Unknown group: \(group)")
        }
        guard !requireSources || !configuration.sourceRoots.isEmpty else {
            throw BigboardError.message(
                "No source folders are set. Run bigboard and choose the folders that hold your clones in Settings.")
        }
        let history = GitHistory(root: environment.storage.historyRoot)
        let clones = await CloneDiscovery.find(in: CloneDiscovery.roots(configuration.sourceRoots))
        let known = Dictionary(clones.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        var repositories = configuration.collected(
            configuration.repositories.map { known[$0] ?? Repository(id: $0, branch: "") }, group: group)
        let requested = options.values("--repo").map { $0.lowercased() }
        if !requested.isEmpty {
            let unknown = Set(requested).subtracting(repositories.map(\.id))
            guard unknown.isEmpty else {
                throw UsageError("Not a collected repository: \(unknown.sorted().joined(separator: ", "))")
            }
            repositories = repositories.filter { requested.contains($0.id) }
        }
        return Workspace(
            account: account, configuration: configuration, repositories: repositories, history: history)
    }

    private func status(_ options: Options) async throws -> Int32 {
        let workspace = try await workspace(options, requireSources: false)
        let report = StatusReport(
            login: workspace.account.login, accountID: workspace.account.accountID,
            settings: environment.storage.configurationFile.path, history: environment.storage.cacheRoot.path,
            sourceFolders: CloneDiscovery.roots(workspace.configuration.sourceRoots).map(\.path),
            repositories: workspace.repositories.map {
                StatusReport.Row(
                    id: $0.id, branch: $0.branch, clone: $0.path, shallow: $0.shallow, partial: $0.partial)
            })
        if options.flag("--json") {
            environment.write(try Self.json(report))
            return 0
        }
        var lines = [
            "GitHub CLI: \(report.login) (\(report.accountID))",
            "Settings:   \(report.settings)",
            "Caches:     \(report.history)",
            "",
            "Collected repositories: \(report.repositories.count)",
        ]
        let width = report.repositories.map(\.id.count).max() ?? 0
        let branchWidth = report.repositories.map(\.branch.count).max() ?? 0
        for row in report.repositories {
            let id: String = row.id.padding(toLength: width, withPad: " ", startingAt: 0)
            let branch: String = row.branch.padding(toLength: branchWidth, withPad: " ", startingAt: 0)
            var detail = row.clone.isEmpty ? "no local clone found in source folders" : row.displayPath
            if row.shallow { detail += " (shallow)" }
            if row.partial { detail += " (partial: line counts unknown)" }
            lines.append("  \(id)  \(branch)  " + detail)
        }
        if report.repositories.isEmpty { lines.append("  None. Choose repositories in Bigboard Settings.") }
        lines.insert(
            "Sources:    "
                + (report.sourceFolders.isEmpty ? "none. Choose folders in Bigboard Settings." : report.sourceFolders.joined(separator: ", ")), at: 3)
        environment.write(lines.joined(separator: "\n") + "\n")
        return 0
    }

    private func activity(_ options: Options) async throws -> Int32 {
        let explicitSince = try options.date("--since"), explicitUntil = try options.date("--until")
        let explicit = explicitSince != nil || explicitUntil != nil
        guard !explicit || options.value("--days") == nil else {
            throw UsageError("Use --days or --since/--until, not both.")
        }
        if let explicitSince, let explicitUntil, explicitSince >= explicitUntil {
            throw UsageError("--since must be before --until.")
        }
        let path = try options.value("--path").map(Self.repositoryPath)
        if path != nil, options.values("--repo").count != 1 {
            throw UsageError("--path needs exactly one --repo.")
        }
        let workspace = try await workspace(options)
        let days = explicit ? 0 : try options.integer("--days") ?? workspace.configuration.sinceDays
        let top = try options.integer("--top") ?? 10
        guard (0...3650).contains(days), top > 0 else { throw UsageError("--days must be 0–3650 and --top positive.") }
        guard let peopleSort = ActivityReport.PeopleSort(rawValue: options.value("--people-sort") ?? "name") else {
            throw UsageError("--people-sort must be name, lines, or commits.")
        }
        let depth = try options.integer("--depth") ?? 1
        guard (1...10).contains(depth) else { throw UsageError("--depth must be 1–10.") }
        let compare = options.flag("--compare")
        guard !compare || days > 0 || explicitSince != nil else {
            throw UsageError("--compare needs --days greater than 0 or --since.")
        }
        var commits: [Commit] = []
        var evidence: [ActivityReport.Evidence] = []
        for repo in workspace.repositories {
            var entry = ActivityReport.Evidence(repo, fetchedAt: await workspace.history.fetchedAt(repo.id))
            if repo.path.isEmpty {
                entry.unavailable("missing", kind: "no_clone", message: "No local clone found in source folders.")
            } else {
                do {
                    let scanned = try await workspace.history.scan(repo, configuration: workspace.configuration)
                    commits += scanned.1
                    entry.record(scanned.0)
                } catch {
                    entry.unavailable("failed", kind: "scan_failed", message: error.localizedDescription)
                }
            }
            evidence.append(entry)
        }
        let now = environment.now()
        let persons = try Self.resolvePeople(
            options.values("--person"), in: Analysis.select(commits, now: now),
            identities: workspace.configuration.identities)
        let people = Set(persons.map(\.id))
        let hideBots = !options.flag("--include-bots") && persons.isEmpty
        let since = explicit ? explicitSince : days > 0 ? now.addingTimeInterval(-Double(days) * 86_400) : nil
        let until = explicitUntil
        let pathScope = path.map { value in
            let repo = workspace.repositories.first?.id ?? ""
            let isFile = commits.contains { $0.repo == repo && $0.files.contains { $0.path == value } }
            return ActivityScope(repo: repo, path: value, isFile: isFile)
        }
        func window(_ start: Date?, _ end: Date?) -> [Commit] {
            let selected = Analysis.select(
                commits, since: start, until: end, hideBots: hideBots, people: people, now: now)
            return pathScope.map { Analysis.scoped(selected, scope: $0) } ?? selected
        }
        let selected = Analysis.select(
            commits, since: since, until: until, hideBots: hideBots, people: people, now: now)
        let excluded = pathScope.map { scope in
            var result: [String: Int] = [:]
            for commit in Analysis.unique(selected) where commit.repo == scope.repo && !commit.pathsUnknown {
                for file in commit.files where !file.excludedReason.isEmpty && scope.contains(file.path) {
                    result[file.excludedReason, default: 0] += file.added + file.removed
                }
            }
            return result
        }
        let previous: ActivityReport.Window? = since.flatMap { start in
            guard compare else { return nil }
            let length = (until ?? now).timeIntervalSince(start)
            let before = start.addingTimeInterval(-length)
            return ActivityReport.Window(start: before, end: start, commits: window(before, start))
        }
        let scope = ActivityReport.Scope(
            repositories: workspace.repositories.map(\.id), group: options.value("--group"), path: path,
            kind: pathScope.map { $0.isFile ? "file" : "directory" }, since: since, until: until,
            includeBots: !hideBots,
            people: persons.map(\.name))
        let current = window(since, until)
        let timeline = options.flag("--timeline")
            ? ActivityReport.Timeline(
                unit: Analysis.timelineUnit(since: since, now: until ?? now),
                points: Analysis.timeline(current, days: 0, since: since, now: until ?? now).map {
                    ActivityReport.Timeline.Point(start: $0.date, commits: $0.commits, added: $0.added, removed: $0.removed)
                })
            : nil
        let report = ActivityReport(
            account: workspace.account.login, generatedAt: now, days: days, commits: current,
            repositories: workspace.repositories, top: top, peopleSort: peopleSort, depth: depth,
            previous: previous, scope: scope, pathScope: pathScope, excluded: excluded,
            commitLimit: options.flag("--commits") ? top : nil, timeline: timeline, evidence: evidence)
        environment.write(options.flag("--json") ? try Self.json(report) : report.text())
        return report.problems.isEmpty ? 0 : 2
    }

    static func resolvePeople(
        _ wanted: [String], in commits: [Commit], identities: [String: IdentityRule] = [:]
    ) throws -> [PersonRow] {
        guard !wanted.isEmpty else { return [] }
        let everyone = Analysis.people(commits)
        var primary: [String: String] = [:]
        for (email, rule) in identities {
            for alias in [email] + rule.aliases {
                primary[alias.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()] =
                    email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            }
        }
        var chosen: [PersonRow] = []
        for raw in wanted {
            let typed = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            let value = primary[typed] ?? typed
            let byEmail = everyone.filter { person in
                person.id.lowercased() == value || person.email.lowercased() == value
                    || person.records.contains { $0.email.lowercased() == value }
            }
            let matches = byEmail.isEmpty ? everyone.filter { $0.name.lowercased() == value } : byEmail
            guard !matches.isEmpty else {
                throw BigboardError.message(
                    "No one in the collected history matches \(raw). Use an email or a name listed by bigboard activity --days 0.")
            }
            guard matches.count == 1 else {
                let names = matches.map { "\($0.name) <\($0.email)>" }.joined(separator: ", ")
                throw BigboardError.message("\(raw) matches several people: \(names). Use an email.")
            }
            if !chosen.contains(where: { $0.id == matches[0].id }) { chosen.append(matches[0]) }
        }
        return chosen
    }

    private func pullRequests(_ options: Options) async throws -> Int32 {
        let workspace = try await workspace(options, requireSources: false)
        let top = try options.integer("--top") ?? 25
        guard top > 0 else { throw UsageError("--top must be positive.") }
        func choices(_ name: String, _ allowed: [String], upper: Bool = false) throws -> Set<String> {
            Set(
                try options.values(name).map { value in
                    let normalized = value.lowercased()
                    guard allowed.contains(normalized) else {
                        throw UsageError("\(name) must be \(allowed.joined(separator: ", ")).")
                    }
                    return upper ? normalized.uppercased() : normalized
                })
        }
        var filters = PRFilters()
        filters.mine = try choices("--mine", ["attention", "authored", "requested", "updated"])
        filters.reviews = try choices(
            "--review", ["approved", "changes_requested", "review_required", "unknown"], upper: true)
        filters.checks = try choices(
            "--checks", ["success", "failure", "error", "pending", "expected", "unknown"], upper: true)
        filters.states = try choices("--state", ["open", "draft"])
        filters.authors = Set(options.values("--author"))
        filters.query = options.value("--search") ?? ""
        let sort = options.value("--sort") ?? "updated"
        guard ["updated", "attention", "repository", "author", "title"].contains(sort) else {
            throw UsageError("--sort must be updated, attention, repository, author, or title.")
        }
        filters.sort = sort
        let auth = environment.auth
        let github = GitHubService(token: { try await auth.token() }, transport: environment.transport)
        let result = await github.fetch(
            workspace.repositories, budget: workspace.configuration.requestBudget, previous: [])
        if result.authenticationRequired { throw GitHubCLIFailure.rejected }
        let viewer = workspace.account.login
        let report = PullRequestReport(
            viewer: viewer, generatedAt: environment.now(), group: options.value("--group"), filters: filters,
            result: result, top: top, files: options.flag("--files"))
        environment.write(options.flag("--json") ? try Self.json(report) : report.text())
        return report.problems.isEmpty ? 0 : 2
    }

    static func repositoryPath(_ value: String) throws -> String {
        var path = value
        while path.hasSuffix("/") { path.removeLast() }
        let parts = path.split(separator: "/", omittingEmptySubsequences: false)
        guard !path.isEmpty, !path.hasPrefix("/"), !path.contains("\0"),
            parts.allSatisfy({ !$0.isEmpty && $0 != "." && $0 != ".." })
        else { throw UsageError("--path must be a path inside the repository, such as src/ui.") }
        return path
    }

    private func refresh(_ options: Options) async throws -> Int32 {
        let workspace = try await workspace(options)
        guard !workspace.repositories.isEmpty else {
            environment.write("No collected repositories. Choose repositories in Bigboard Settings.\n")
            return 0
        }
        var failures = 0
        for repo in workspace.repositories {
            guard !repo.path.isEmpty else {
                failures += 1
                environment.writeError("\(repo.id): no local clone found in source folders\n")
                continue
            }
            do {
                let refreshed = try await environment.refreshHistory(workspace.history, repo)
                let scanned = try await workspace.history.scan(refreshed, configuration: workspace.configuration)
                environment.write("\(repo.id): \(scanned.1.count) commits at \(refreshed.revision.prefix(12))\n")
            } catch {
                failures += 1
                environment.writeError("\(repo.id): \(error.localizedDescription)\n")
            }
        }
        return failures == 0 ? 0 : 2
    }

    static func json<T: Encodable>(_ value: T) throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        encoder.keyEncodingStrategy = .convertToSnakeCase
        encoder.dateEncodingStrategy = .iso8601
        return String(decoding: try encoder.encode(value), as: UTF8.self) + "\n"
    }
}

struct UsageError: Error {
    var message: String
    init(_ message: String) { self.message = message }
}

struct Options {
    private var flags: Set<String> = []
    private var pairs: [(String, String)] = []
    init(_ arguments: [String], flags allowedFlags: Set<String> = [], values allowedValues: Set<String> = []) throws {
        var index = 0
        while index < arguments.count {
            let argument = arguments[index]
            var name = argument, inline: String?
            if let equals = argument.firstIndex(of: "="), argument.hasPrefix("--") {
                name = String(argument[..<equals])
                inline = String(argument[argument.index(after: equals)...])
            }
            if allowedFlags.contains(name), inline == nil {
                flags.insert(name)
            } else if allowedValues.contains(name) {
                if let inline {
                    pairs.append((name, inline))
                } else {
                    index += 1
                    guard index < arguments.count else { throw UsageError("\(name) needs a value.") }
                    pairs.append((name, arguments[index]))
                }
            } else {
                throw UsageError("Unknown option: \(argument)")
            }
            index += 1
        }
    }
    func flag(_ name: String) -> Bool { flags.contains(name) }
    func values(_ name: String) -> [String] { pairs.filter { $0.0 == name }.map(\.1) }
    func value(_ name: String) -> String? { values(name).last }
    func integer(_ name: String) throws -> Int? {
        guard let value = value(name) else { return nil }
        guard let number = Int(value) else { throw UsageError("\(name) must be a whole number.") }
        return number
    }
    func date(_ name: String) throws -> Date? {
        guard let value = value(name) else { return nil }
        let full = ISO8601DateFormatter()
        if let date = full.date(from: value) { return date }
        let day = DateFormatter()
        day.locale = Locale(identifier: "en_US_POSIX")
        day.calendar = Calendar(identifier: .gregorian)
        day.timeZone = .current
        day.dateFormat = "yyyy-MM-dd"
        if let date = day.date(from: value), day.string(from: date) == value { return date }
        throw UsageError("\(name) must be a date (2026-10-01) or ISO 8601 time (2026-10-01T12:00:00Z).")
    }
}

struct StatusReport: Encodable {
    struct Row: Encodable {
        var id: String
        var branch: String
        var clone: String
        var shallow: Bool
        var partial: Bool
        var displayPath: String { SettingsRepositories.displayPath(clone) }
    }
    var login: String
    var accountID: String
    var settings: String
    var history: String
    var sourceFolders: [String]
    var repositories: [Row]
}

struct ActivityReport: Encodable {
    enum PeopleSort: String, Encodable { case name, lines, commits }
    struct Totals: Encodable {
        var commits: Int
        var added: Int
        var removed: Int
        var people: Int
        var aiAssisted: Int
        var unknownLineCommits: Int
        init(_ value: ActivityTotals) {
            commits = value.commits
            added = value.added
            removed = value.removed
            people = value.people
            aiAssisted = value.ai
            unknownLineCommits = value.unknownLineCommits
        }
    }
    struct Problem: Encodable, Equatable {
        var kind: String
        var message: String
    }
    struct ProblemRow: Encodable {
        var repository: String
        var kind: String
        var message: String
    }
    struct Evidence: Encodable {
        var repository: String
        var branch: String?
        var remote: String?
        var revision: String?
        var scannedAt: Date?
        var fetchedAt: Date?
        var history = "complete"
        var lineCounts = "complete"
        var paths = "complete"
        var problems: [Problem] = []
        init(_ repo: Repository, fetchedAt: Date?) {
            repository = repo.id
            branch = repo.branch.isEmpty ? nil : repo.branch
            remote = repo.path.isEmpty ? nil : repo.remote
            self.fetchedAt = fetchedAt
        }
        mutating func unavailable(_ state: String, kind: String, message: String) {
            history = state
            lineCounts = "unknown"
            paths = "unknown"
            problems.append(Problem(kind: kind, message: message))
        }
        mutating func record(_ repo: Repository) {
            revision = repo.revision.isEmpty ? nil : repo.revision
            scannedAt = repo.scannedAt
            if repo.shallow {
                history = "shallow"
                problems.append(
                    Problem(kind: "shallow", message: "Shallow clone: earlier history is not available locally."))
            }
            if repo.partial {
                lineCounts = "unknown"
                paths = "unknown"
                problems.append(Problem(kind: "partial", message: "Partial clone: line counts and paths are unknown."))
            }
            if repo.skippedCommits > 0 {
                problems.append(
                    Problem(kind: "skipped_commits", message: GitHistory.skippedCommitsMessage(repo.skippedCommits)))
            }
        }
        enum CodingKeys: String, CodingKey {
            case repository, branch, remote, revision, scannedAt, fetchedAt, history, lineCounts, paths, problems
        }
        func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(repository, forKey: .repository)
            try container.encode(branch, forKey: .branch)
            try container.encode(remote, forKey: .remote)
            try container.encode(revision, forKey: .revision)
            try container.encode(scannedAt, forKey: .scannedAt)
            try container.encode(fetchedAt, forKey: .fetchedAt)
            try container.encode(history, forKey: .history)
            try container.encode(lineCounts, forKey: .lineCounts)
            try container.encode(paths, forKey: .paths)
            try container.encode(problems, forKey: .problems)
        }
    }
    struct RepositoryRow: Encodable {
        var id: String
        var totals: Totals
        var latest: Date?
        var previous: Totals?
    }
    struct DirectoryRow: Encodable {
        var repository: String
        var path: String
        var kind: String
        var totals: Totals
        var latest: Date?
        var previous: Totals?
    }
    struct Comparison: Encodable {
        var start: Date
        var end: Date
        var totals: Totals
    }
    struct Window {
        var start: Date
        var end: Date
        var commits: [Commit]
    }
    struct Scope: Encodable {
        var repositories: [String]
        var group: String?
        var path: String?
        var kind: String?
        var since: Date?
        var until: Date?
        var includeBots: Bool
        var people: [String] = []
        enum CodingKeys: String, CodingKey { case repositories, group, path, kind, since, until, includeBots, people }
        func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(repositories, forKey: .repositories)
            try container.encode(group, forKey: .group)
            try container.encode(path, forKey: .path)
            try container.encode(kind, forKey: .kind)
            try container.encode(since, forKey: .since)
            try container.encode(until, forKey: .until)
            try container.encode(includeBots, forKey: .includeBots)
            try container.encode(people, forKey: .people)
        }
    }
    struct CommitRow: Encodable {
        var repository: String
        var sha: String
        var url: String
        var date: Date
        var author: String
        var subject: String
        var added: Int
        var removed: Int
        var linesUnknown: Bool
    }
    struct Timeline: Encodable {
        struct Point: Encodable {
            var start: Date
            var commits: Int
            var added: Int
            var removed: Int
        }
        var unit: TimelineUnit
        var points: [Point]
    }
    struct PersonRowReport: Encodable {
        var name: String
        var bot: Bool
        var totals: Totals
        var repositories: [String]
        var latest: Date?
    }
    var account: String
    var generatedAt: Date
    var days: Int
    var scope: Scope?
    var depth: Int
    var totals: Totals
    var comparison: Comparison?
    var excluded: [String: Int]?
    var repositories: [RepositoryRow]
    var directories: [DirectoryRow]
    var peopleSort: PeopleSort
    var people: [PersonRowReport]
    var commits: [CommitRow]?
    var timeline: Timeline?
    var evidence: [Evidence]
    var problems: [ProblemRow]

    init(
        account: String, generatedAt: Date, days: Int, commits: [Commit], repositories: [Repository], top: Int,
        peopleSort: PeopleSort = .name, depth: Int = 1, previous: Window? = nil, scope: Scope? = nil,
        pathScope: ActivityScope? = nil, excluded: [String: Int]? = nil, commitLimit: Int? = nil,
        timeline: Timeline? = nil, evidence: [Evidence]
    ) {
        self.account = account
        self.timeline = timeline
        self.generatedAt = generatedAt
        self.days = days
        self.scope = scope
        self.depth = depth
        self.commits = commitLimit.map { limit in
            Array(
                Analysis.unique(commits)
                    .sorted { $0.date != $1.date ? $0.date > $1.date : Analysis.literalLess($0.id, $1.id) }
                    .prefix(limit)
                    .map {
                        CommitRow(
                            repository: $0.repo, sha: $0.id, url: "https://github.com/\($0.repo)/commit/\($0.id)",
                            date: $0.date, author: $0.author, subject: $0.subject, added: $0.added,
                            removed: $0.removed, linesUnknown: $0.linesUnknown)
                    })
        }
        comparison = previous.map {
            Comparison(start: $0.start, end: $0.end, totals: Totals(Analysis.totals($0.commits)))
        }
        self.evidence = evidence
        problems = evidence.flatMap { entry in
            entry.problems.map { ProblemRow(repository: entry.repository, kind: $0.kind, message: $0.message) }
        }
        self.peopleSort = peopleSort
        totals = Totals(Analysis.totals(commits))
        let counted = (excluded ?? Analysis.excludedLines(commits)).filter { $0.value > 0 }
        self.excluded = counted.isEmpty ? nil : counted
        let current = Dictionary(
            Analysis.activityRows(commits, repositories: repositories).map { ($0.id, $0) },
            uniquingKeysWith: { first, _ in first })
        let prior = previous.map { window in
            Dictionary(
                Analysis.activityRows(window.commits, repositories: repositories).map { ($0.id, $0) },
                uniquingKeysWith: { first, _ in first })
        }
        let none = Totals(ActivityTotals())
        self.repositories = repositories.compactMap { repo -> RepositoryRow? in
            let now = current[repo.id], before = prior?[repo.id]
            guard (now?.totals.commits ?? 0) > 0 || (before?.totals.commits ?? 0) > 0 else { return nil }
            return RepositoryRow(
                id: repo.id, totals: now.map { Totals($0.totals) } ?? none, latest: now?.latest,
                previous: prior == nil ? nil : before.map { Totals($0.totals) } ?? none)
        }
        .sorted {
            let left = $0.totals.added + $0.totals.removed, right = $1.totals.added + $1.totals.removed
            return left != right ? left > right : $0.id < $1.id
        }
        func key(_ repository: String, _ row: ActivityRow) -> String {
            repository + "\u{0}" + row.kind + "\u{0}" + row.scope.path
        }
        let priorPaths = previous.map { window in
            Dictionary(
                repositories.flatMap { repo in
                    Self.pathRows(window.commits, repositories, pathScope ?? ActivityScope(repo: repo.id), depth: depth)
                        .map { (key(repo.id, $0), Totals($0.totals)) }
                }, uniquingKeysWith: { first, _ in first })
        }
        directories = Array(
            repositories.flatMap { repo in
                Self.pathRows(commits, repositories, pathScope ?? ActivityScope(repo: repo.id), depth: depth)
                    .map { row in
                        DirectoryRow(
                            repository: repo.id, path: row.scope.path, kind: row.kind, totals: Totals(row.totals),
                            latest: row.latest, previous: priorPaths.map { $0[key(repo.id, row)] ?? none })
                    }
            }
            .sorted { (a: DirectoryRow, b: DirectoryRow) -> Bool in
                let left = a.totals.added + a.totals.removed, right = b.totals.added + b.totals.removed
                if left != right { return left > right }
                return (a.repository, a.path) < (b.repository, b.path)
            }
            .prefix(top))
        let ranked = Analysis.people(commits)
        let ordered: [PersonRow] =
            switch peopleSort {
            case .lines: Array(ranked.prefix(top))
            case .commits:
                Array(
                    ranked.sorted {
                        $0.totals.commits != $1.totals.commits
                            ? $0.totals.commits > $1.totals.commits : Analysis.literalLess($0.name, $1.name)
                    }
                    .prefix(top))
            case .name:
                ranked.sorted {
                    $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedSame
                        ? Analysis.literalLess($0.name, $1.name)
                        : $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
                }
            }
        people = ordered
                .map {
                    PersonRowReport(
                        name: $0.name, bot: $0.bot, totals: Totals($0.totals), repositories: $0.repos.sorted(),
                        latest: $0.latest)
                }
    }

    static func pathRows(
        _ commits: [Commit], _ repositories: [Repository], _ scope: ActivityScope, depth: Int
    ) -> [ActivityRow] {
        let rows = Analysis.activityRows(commits, repositories: repositories, scope: scope)
        guard depth > 1 else { return rows }
        return rows.flatMap { row -> [ActivityRow] in
            guard row.kind == "directory" else { return [row] }
            let children = pathRows(commits, repositories, row.scope, depth: depth - 1)
            return children.isEmpty ? [row] : children
        }
    }
    static func change(_ current: Int, _ previous: Int) -> String {
        if previous == 0 { return current == 0 ? "—" : "new" }
        if current == 0 { return "quiet" }
        let percent = Int(((Double(current - previous) / Double(previous)) * 100).rounded())
        return percent > 0 ? "+\(percent)%" : percent < 0 ? "−\(-percent)%" : "0%"
    }

    static func timestamp(_ date: Date) -> String {
        ISO8601DateFormatter().string(from: date)
    }

    func text() -> String {
        let explicit = days == 0 && (scope?.since != nil || scope?.until != nil)
        let range =
            explicit
            ? "after \(scope?.since.map(Self.timestamp) ?? "the start of history") through \(scope?.until.map(Self.timestamp) ?? "now")"
            : days == 0 ? "all history" : "last \(days) day" + (days == 1 ? "" : "s")
        var header = [account]
        if let chosen = scope?.people, !chosen.isEmpty { header.append(chosen.joined(separator: ", ")) }
        if let path = scope?.path, let repository = scope?.repositories.first {
            header.append(repository + "/" + path + (scope?.kind == "directory" ? "/" : ""))
        }
        header.append(range)
        header.append(counted(repositories.filter { $0.totals.commits > 0 }.count, "active repository", "active repositories"))
        var lines = [header.joined(separator: " · ")]
        let ai = totals.commits == 0 ? 0 : totals.aiAssisted * 100 / totals.commits
        lines.append(
            "\(counted(totals.commits, "commit", "commits")) · +\(number(totals.added)) −\(number(totals.removed)) lines · \(counted(totals.people, "person", "people")) · \(ai)% AI-assisted"
        )
        if let comparison {
            lines.append(
                (days > 0 ? "Previous \(days) day\(days == 1 ? "" : "s")" : "Previous period (after \(Self.timestamp(comparison.start)))") + ": \(counted(comparison.totals.commits, "commit", "commits")) · +\(number(comparison.totals.added)) −\(number(comparison.totals.removed)) lines · \(Self.change(totals.commits, comparison.totals.commits)) commits"
            )
        }
        if totals.unknownLineCommits > 0 {
            lines.append(
                "\(counted(totals.unknownLineCommits, "commit has", "commits have")) unknown line counts; line totals are a lower bound."
            )
        }
        if let excluded {
            let parts = excluded.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }
                .map { "\(number($0.value)) \($0.key)" }
            lines.append("Excluded: " + parts.joined(separator: " · ") + " lines (not counted above)")
        }
        func table(_ title: String, _ rows: [(String, Totals, Totals?)], people showPeople: Bool = true) {
            guard !rows.isEmpty else { return }
            let comparing = comparison != nil && showPeople
            lines.append("")
            lines.append(title)
            let width = max(rows.map(\.0.count).max() ?? 0, 4)
            let heading: String = "Name".padding(toLength: width, withPad: " ", startingAt: 0)
            lines.append(
                "  \(heading)  Commits      Added    Removed" + (showPeople ? "  People" : "")
                    + (comparing ? "  Previous  Change" : ""))
            for (label, value, previous) in rows {
                let name: String = label.padding(toLength: width, withPad: " ", startingAt: 0)
                let commits: String = number(value.commits).leftPad(7)
                let added: String = ("+" + number(value.added)).leftPad(10)
                let removed: String = ("−" + number(value.removed)).leftPad(10)
                let people: String = showPeople ? "  " + number(value.people).leftPad(6) : ""
                var row = "  \(name)  \(commits) \(added) \(removed)\(people)"
                if comparing {
                    let before = previous?.commits ?? 0
                    row += "  " + number(before).leftPad(8) + "  " + Self.change(value.commits, before).leftPad(6)
                }
                lines.append(row)
            }
        }
        if let timeline, !timeline.points.isEmpty {
            lines.append("")
            lines.append("Timeline (by \(timeline.unit.rawValue))")
            let formatter = DateFormatter()
            formatter.locale = Locale(identifier: "en_US_POSIX")
            formatter.dateFormat =
                timeline.unit == .month ? "yyyy-MM" : timeline.unit == .hour ? "yyyy-MM-dd HH:mm" : "yyyy-MM-dd"
            let peak = timeline.points.map { $0.added + $0.removed }.max() ?? 0
            for point in timeline.points {
                let change = point.added + point.removed
                let width = peak == 0 || change == 0 ? 0 : max(1, change * 24 / peak)
                lines.append(
                    "  \(formatter.string(from: point.start).padding(toLength: 10, withPad: " ", startingAt: 0))  "
                        + number(point.commits).leftPad(7) + ("+" + number(point.added)).leftPad(10)
                        + ("−" + number(point.removed)).leftPad(10) + "  " + String(repeating: "█", count: width))
            }
        }
        table("Repositories", repositories.map { ($0.id, $0.totals, $0.previous) })
        table(
            depth == 1 ? "Top paths by lines changed" : "Top paths by lines changed (\(depth) levels deep)",
            directories.map {
                ($0.repository + "/" + $0.path + ($0.kind == "directory" ? "/" : ""), $0.totals, $0.previous)
            })
        if let commits, !commits.isEmpty {
            lines.append("")
            lines.append("Commits (newest first)")
            for commit in commits {
                let change = "+\(number(commit.added)) −\(number(commit.removed))" + (commit.linesUnknown ? " ?" : "")
                let repository = scope?.repositories.count == 1 ? "" : "  [\(commit.repository)]"
                lines.append(
                    "  \(Self.timestamp(commit.date))  \(commit.sha.prefix(12))  \(change.leftPad(16))  \(commit.author): \(commit.subject)\(repository)")
            }
        }
        if peopleSort == .name {
            if !people.isEmpty {
                lines.append("")
                lines.append("People (\(number(people.count)))")
                let shown = people.prefix(25)
                let labels = shown.map { $0.name + ($0.bot ? " (bot)" : "") }
                let width = labels.map(\.count).max() ?? 0
                for (label, person) in zip(labels, shown) {
                    let name: String = label.padding(toLength: width, withPad: " ", startingAt: 0)
                    lines.append("  \(name)  \(person.repositories.joined(separator: ", "))")
                }
                if people.count > shown.count {
                    lines.append("  and \(number(people.count - shown.count)) more (see --json)")
                }
            }
        } else {
            table(
                "People by \(peopleSort.rawValue)", people.map { ($0.name + ($0.bot ? " (bot)" : ""), $0.totals, nil) },
                people: false)
        }
        if totals.commits == 0 { lines.append("\nNo recorded change in this range.") }
        if !problems.isEmpty {
            lines.append("")
            lines.append("Incomplete evidence:")
            lines += problems.map { "  \($0.repository): \($0.message)" }
        }
        lines.append("")
        lines.append("Activity describes recorded changes, not productivity.")
        return lines.joined(separator: "\n") + "\n"
    }
}

private func counted(_ value: Int, _ singular: String, _ plural: String) -> String {
    number(value) + " " + (value == 1 ? singular : plural)
}
private func number(_ value: Int) -> String {
    let formatter = NumberFormatter()
    formatter.numberStyle = .decimal
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.usesGroupingSeparator = true
    formatter.groupingSeparator = ","
    formatter.groupingSize = 3
    return formatter.string(from: NSNumber(value: value)) ?? String(value)
}
extension String {
    fileprivate func leftPad(_ width: Int) -> String {
        count >= width ? self : String(repeating: " ", count: width - count) + self
    }
}

struct PullRequestReport: Encodable {
    struct Filters: Encodable {
        var repositories: [String]
        var group: String?
        var mine: [String]
        var authors: [String]
        var reviews: [String]
        var checks: [String]
        var states: [String]
        var search: String?
        var sort: String
    }
    struct Counts: Encodable {
        var open: Int
        var matching: Int
        var needsAttention: Int
    }
    struct ReviewRow: Encodable {
        var author: String
        var state: String
        var submittedAt: Date?
    }
    struct Completeness: Encodable {
        var inventory: Bool
        var files: Bool
        var reviewers: Bool
        var reviews: Bool
        var failingChecks: Bool
    }
    struct Row: Encodable {
        var repository: String
        var number: Int
        var title: String
        var author: String
        var url: String
        var draft: Bool
        var createdAt: Date?
        var updatedAt: Date?
        var review: String
        var checks: String
        var failingChecks: [String]
        var mergeable: String
        var additions: Int?
        var deletions: Int?
        var changedFiles: Int
        var files: [String]?
        var labels: [String]
        var requestedReviewers: [String]
        var latestReviews: [ReviewRow]
        var needsAttention: Bool
        var attentionReason: String?
        var authoredByViewer: Bool
        var reviewRequested: Bool
        var updatedSinceReview: Bool
        var complete: Completeness
    }
    struct RepositoryEvidence: Encodable {
        var repository: String
        var open: Int
        var inventoryComplete: Bool
        var filesComplete: Bool
        var reviewersComplete: Bool
        var stale: Bool
    }
    struct Problem: Encodable {
        var repository: String?
        var message: String
    }
    var viewer: String
    var generatedAt: Date
    var filters: Filters
    var counts: Counts
    var pullRequests: [Row]
    var evidence: [RepositoryEvidence]
    var problems: [Problem]

    static func reason(_ pr: PullRequest, viewer: String) -> String? {
        guard Analysis.needsAttention(pr, viewer: viewer) else { return nil }
        if Analysis.requested(pr, viewer: viewer) { return "your review is requested" }
        if Analysis.updatedSinceReview(pr, viewer: viewer) { return "updated since your review" }
        if ["FAILURE", "ERROR"].contains(pr.checks) { return "checks are failing" }
        if pr.review == "CHANGES_REQUESTED" { return "changes were requested" }
        return "approved with passing checks"
    }

    init(
        viewer: String, generatedAt: Date, group: String?, filters: PRFilters, result: GitHubResult, top: Int,
        files: Bool
    ) {
        self.viewer = viewer
        self.generatedAt = generatedAt
        self.filters = Filters(
            repositories: result.repositories.map(\.id), group: group, mine: filters.mine.sorted(),
            authors: filters.authors.sorted(), reviews: filters.reviews.map { $0.lowercased() }.sorted(),
            checks: filters.checks.map { $0.lowercased() }.sorted(), states: filters.states.sorted(),
            search: filters.query.isEmpty ? nil : filters.query, sort: filters.sort)
        let matching = Analysis.filterPRs(result.pullRequests, viewer: viewer, filters: filters)
        counts = Counts(
            open: result.pullRequests.count, matching: matching.count,
            needsAttention: result.pullRequests.filter { Analysis.needsAttention($0, viewer: viewer) }.count)
        pullRequests = matching.prefix(top).map { pr in
            Row(
                repository: pr.repo, number: pr.number, title: pr.title, author: pr.author, url: pr.url,
                draft: pr.draft, createdAt: pr.createdAt, updatedAt: pr.updatedAt, review: pr.review.lowercased(),
                checks: pr.checks.lowercased(), failingChecks: pr.failingChecks, mergeable: pr.mergeable.lowercased(),
                additions: pr.additions, deletions: pr.deletions, changedFiles: pr.fileCount,
                files: files ? pr.files : nil, labels: pr.labels, requestedReviewers: pr.reviewers,
                latestReviews: pr.reviews.map {
                    ReviewRow(author: $0.author, state: $0.state.lowercased(), submittedAt: $0.submittedAt)
                },
                needsAttention: Analysis.needsAttention(pr, viewer: viewer),
                attentionReason: Self.reason(pr, viewer: viewer),
                authoredByViewer: !viewer.isEmpty && pr.author.lowercased() == viewer.lowercased(),
                reviewRequested: Analysis.requested(pr, viewer: viewer),
                updatedSinceReview: Analysis.updatedSinceReview(pr, viewer: viewer),
                complete: Completeness(
                    inventory: pr.inventoryComplete, files: pr.filesComplete, reviewers: pr.reviewersComplete,
                    reviews: pr.reviewsComplete, failingChecks: pr.failingChecksComplete))
        }
        let byRepository = Dictionary(grouping: result.pullRequests, by: \.repo)
        evidence = result.repositories.map {
            RepositoryEvidence(
                repository: $0.id, open: byRepository[$0.id]?.count ?? 0, inventoryComplete: $0.prInventoryComplete,
                filesComplete: $0.prFilesComplete, reviewersComplete: $0.prReviewersComplete, stale: $0.prStale)
        }
        let known = Set(result.repositories.map(\.id))
        problems = result.coverage.errors.map { message in
            let prefix = message.split(separator: ":", maxSplits: 1).first.map(String.init) ?? ""
            let repository = known.contains(prefix) ? prefix : nil
            let text =
                repository == nil
                ? message : String(message.dropFirst(prefix.count + 1)).trimmingCharacters(in: .whitespaces)
            return Problem(repository: repository, message: text)
        }
        for entry in evidence where !entry.inventoryComplete && !problems.contains(where: { $0.repository == entry.repository }) {
            problems.append(Problem(repository: entry.repository, message: "Pull-request inventory is incomplete."))
        }
    }

    func text() -> String {
        let attention = counts.needsAttention == 1 ? "1 needs your attention" : "\(counts.needsAttention) need your attention"
        var lines = [
            "\(viewer) · \(counts.open) open pull request\(counts.open == 1 ? "" : "s") in \(evidence.count) repositor\(evidence.count == 1 ? "y" : "ies") · \(attention)"
        ]
        if counts.matching != counts.open { lines.append("\(counts.matching) match the filters.") }
        if pullRequests.isEmpty {
            lines.append("")
            lines.append(counts.open == 0 ? "No open pull requests." : "No pull requests match the filters.")
        }
        let day = DateFormatter()
        day.locale = Locale(identifier: "en_US_POSIX")
        day.dateFormat = "yyyy-MM-dd"
        for pr in pullRequests {
            lines.append("")
            lines.append("\(pr.needsAttention ? "!" : " ") \(pr.repository)#\(pr.number)  \(pr.title)")
            var details = [pr.author, pr.draft ? "draft" : "ready for review"]
            details.append(
                [
                    "approved": "approved", "changes_requested": "changes requested",
                    "review_required": "needs review",
                ][pr.review] ?? "review unknown")
            details.append(
                [
                    "success": "checks passing", "failure": "checks failing", "error": "checks erroring",
                    "pending": "checks pending", "expected": "checks expected",
                ][pr.checks] ?? "checks unknown")
            if let additions = pr.additions, let deletions = pr.deletions {
                details.append("+\(additions) −\(deletions) in \(pr.changedFiles) file\(pr.changedFiles == 1 ? "" : "s")")
            }
            if let updated = pr.updatedAt { details.append("updated \(day.string(from: updated))") }
            lines.append("    " + details.joined(separator: " · "))
            if let reason = pr.attentionReason { lines.append("    Needs your attention: \(reason)") }
            if !pr.failingChecks.isEmpty { lines.append("    Failing: \(pr.failingChecks.joined(separator: ", "))") }
            lines.append("    \(pr.url)")
        }
        if counts.matching > pullRequests.count {
            lines.append("")
            lines.append("and \(counts.matching - pullRequests.count) more (use --top or --json)")
        }
        if !problems.isEmpty {
            lines.append("")
            lines.append("Incomplete evidence:")
            lines += problems.map { "  " + ($0.repository.map { "\($0): " } ?? "") + $0.message }
        }
        return lines.joined(separator: "\n") + "\n"
    }
}
