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
    static let commands: Set<String> = ["open", "status", "activity", "refresh", "help", "--help", "-h", "version", "--version"]
    var environment: CommandLineEnvironment

    func run(_ arguments: [String]) async -> Int32 {
        guard let command = arguments.first else { return open() }
        let options = Array(arguments.dropFirst())
        do {
            switch command {
            case "open": return open()
            case "status": return try await status(try Options(options, flags: ["--json"]))
            case "activity":
                return try await activity(
                    try Options(options, flags: ["--json", "--include-bots"], values: ["--days", "--repo", "--group", "--top", "--people-sort"]))
            case "refresh": return try await refresh(try Options(options, values: ["--repo", "--group"]))
            case "help", "--help", "-h":
                environment.write(Self.usage)
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
            environment.writeError("bigboard: \(error.message)\n\n" + Self.usage)
            return 64
        } catch {
            environment.writeError("bigboard: \(error.localizedDescription)\n")
            return 1
        }
    }

    static let usage = """
        Usage: bigboard [command] [options]

        With no command, opens the Bigboard app.

        Commands:
          open                     Open the Bigboard app.
          status [--json]          Show the GitHub CLI account and collected repositories.
          activity [options]       Summarize recorded change from cached history.
              --days N             Days to include (default: the app's range; 0 = all history).
              --repo OWNER/NAME    Limit to a repository (repeatable).
              --group NAME         Limit to a saved repository group.
              --top N              Rows per section (default 10).
              --people-sort ORDER  name (default), lines, or commits. lines and commits rank
                                   people with their counts, limited by --top.
              --include-bots       Include bot accounts.
              --json               Print JSON.
          refresh [options]        Fetch the default branch of collected clones with your Git setup.
              --repo OWNER/NAME    Limit to a repository (repeatable).
              --group NAME         Limit to a saved repository group.
          help                     Show this help.
          version                  Show the version.

        """

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
        let workspace = try await workspace(options)
        let days = try options.integer("--days") ?? workspace.configuration.sinceDays
        let top = try options.integer("--top") ?? 10
        guard (0...3650).contains(days), top > 0 else { throw UsageError("--days must be 0–3650 and --top positive.") }
        guard let peopleSort = ActivityReport.PeopleSort(rawValue: options.value("--people-sort") ?? "name") else {
            throw UsageError("--people-sort must be name, lines, or commits.")
        }
        var commits: [Commit] = []
        var problems: [String] = []
        for repo in workspace.repositories {
            do {
                let scanned = try await workspace.history.scan(repo, configuration: workspace.configuration)
                commits += scanned.1
                if !scanned.0.error.isEmpty { problems.append("\(repo.id): \(scanned.0.error)") }
            } catch {
                problems.append("\(repo.id): \(error.localizedDescription)")
            }
        }
        let now = environment.now()
        let selected = Analysis.select(
            commits, days: days, hideBots: !options.flag("--include-bots"), now: now)
        let report = ActivityReport(
            account: workspace.account.login, generatedAt: now, days: days, commits: selected,
            repositories: workspace.repositories, top: top, peopleSort: peopleSort, problems: problems)
        environment.write(options.flag("--json") ? try Self.json(report) : report.text())
        return problems.isEmpty ? 0 : 2
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
        init(_ value: ActivityTotals) {
            commits = value.commits
            added = value.added
            removed = value.removed
            people = value.people
            aiAssisted = value.ai
        }
    }
    struct RepositoryRow: Encodable {
        var id: String
        var totals: Totals
        var latest: Date?
    }
    struct DirectoryRow: Encodable {
        var repository: String
        var path: String
        var kind: String
        var totals: Totals
        var latest: Date?
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
    var totals: Totals
    var excluded: [String: Int]?
    var repositories: [RepositoryRow]
    var directories: [DirectoryRow]
    var peopleSort: PeopleSort
    var people: [PersonRowReport]
    var problems: [String]

    init(
        account: String, generatedAt: Date, days: Int, commits: [Commit], repositories: [Repository], top: Int,
        peopleSort: PeopleSort = .name, problems: [String]
    ) {
        self.account = account
        self.generatedAt = generatedAt
        self.days = days
        self.problems = problems
        self.peopleSort = peopleSort
        totals = Totals(Analysis.totals(commits))
        let excluded = Analysis.excludedLines(commits).filter { $0.value > 0 }
        self.excluded = excluded.isEmpty ? nil : excluded
        self.repositories = Analysis.activityRows(commits, repositories: repositories)
            .filter { $0.totals.commits > 0 }
            .sorted { $0.totals.change != $1.totals.change ? $0.totals.change > $1.totals.change : $0.id < $1.id }
            .map { RepositoryRow(id: $0.id, totals: Totals($0.totals), latest: $0.latest) }
        directories = Array(
            repositories.flatMap { repo in
                Analysis.activityRows(commits, repositories: repositories, scope: ActivityScope(repo: repo.id))
                    .map {
                        DirectoryRow(
                            repository: repo.id, path: $0.scope.path, kind: $0.kind, totals: Totals($0.totals),
                            latest: $0.latest)
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

    func text() -> String {
        let range = days == 0 ? "all history" : "last \(days) day" + (days == 1 ? "" : "s")
        var lines = [
            "\(account) · \(range) · \(counted(repositories.count, "active repository", "active repositories"))"
        ]
        let ai = totals.commits == 0 ? 0 : totals.aiAssisted * 100 / totals.commits
        lines.append(
            "\(counted(totals.commits, "commit", "commits")) · +\(number(totals.added)) −\(number(totals.removed)) lines · \(counted(totals.people, "person", "people")) · \(ai)% AI-assisted"
        )
        if let excluded {
            let parts = excluded.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }
                .map { "\(number($0.value)) \($0.key)" }
            lines.append("Excluded: " + parts.joined(separator: " · ") + " lines (not counted above)")
        }
        func table(_ title: String, _ rows: [(String, Totals)], people showPeople: Bool = true) {
            guard !rows.isEmpty else { return }
            lines.append("")
            lines.append(title)
            let width = max(rows.map(\.0.count).max() ?? 0, 4)
            let heading: String = "Name".padding(toLength: width, withPad: " ", startingAt: 0)
            lines.append("  \(heading)  Commits      Added    Removed" + (showPeople ? "  People" : ""))
            for (label, value) in rows {
                let name: String = label.padding(toLength: width, withPad: " ", startingAt: 0)
                let commits: String = number(value.commits).leftPad(7)
                let added: String = ("+" + number(value.added)).leftPad(10)
                let removed: String = ("−" + number(value.removed)).leftPad(10)
                let people: String = showPeople ? "  " + number(value.people).leftPad(6) : ""
                lines.append("  \(name)  \(commits) \(added) \(removed)\(people)")
            }
        }
        table("Repositories", repositories.map { ($0.id, $0.totals) })
        table(
            "Top paths by lines changed",
            directories.map { ($0.repository + "/" + $0.path + ($0.kind == "directory" ? "/" : ""), $0.totals) })
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
                "People by \(peopleSort.rawValue)", people.map { ($0.name + ($0.bot ? " (bot)" : ""), $0.totals) },
                people: false)
        }
        if totals.commits == 0 { lines.append("\nNo recorded change in this range.") }
        if !problems.isEmpty {
            lines.append("")
            lines.append("Incomplete evidence:")
            lines += problems.map { "  " + $0 }
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
