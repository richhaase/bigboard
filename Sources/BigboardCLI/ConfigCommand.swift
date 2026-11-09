import Foundation

extension CommandLineTool {
    static let configKeys = [
        "sources", "repositories", "group", "work-area", "identity", "exclude", "ai-identities",
        "bot-identities", "all-files", "since-days", "contributor-sort", "refresh-interval", "request-budget",
        "cached-only",
    ]
    private static let listKeys: Set<String> = ["sources", "repositories", "exclude", "ai-identities", "bot-identities"]
    private static let scalarKeys: Set<String> = [
        "all-files", "since-days", "contributor-sort", "refresh-interval", "request-budget", "cached-only",
    ]

    func config(_ arguments: [String]) async throws -> Int32 {
        var words: [String] = []
        var json = false
        for argument in arguments {
            if argument == "--json" {
                json = true
            } else if argument.hasPrefix("--") {
                throw UsageError("Unknown option: \(argument)")
            } else {
                words.append(argument)
            }
        }
        let action = words.first ?? "get"
        let rest = Array(words.dropFirst())
        let storage = environment.storage
        let current = try storage.loadConfiguration()
        switch action {
        case "get":
            guard let key = rest.first else {
                environment.write(json ? try Self.json(report(current)) : Self.describe(current, settings: storage.configurationFile.path))
                return 0
            }
            try Self.checkKey(key)
            environment.write(try render(key, Array(rest.dropFirst()), current, json: json))
            return 0
        case "set", "add", "remove", "unset":
            guard let key = rest.first else { throw UsageError("config \(action) needs a setting name.") }
            try Self.checkKey(key)
            let values = Array(rest.dropFirst())
            var next = current
            var selector = try change(action, key, values, &next)
            if next != current {
                if next.repositories != current.repositories || next.sourceRoots != current.sourceRoots {
                    try await checkClones(next)
                }
                try storage.saveConfiguration(next)
            }
            if action == "unset" || (key == "group" && next.groups[selector.first ?? ""] == nil)
                || (key == "work-area" && !(next.workAreas[selector.first ?? ""] ?? []).contains { $0.name == selector.last })
            {
                selector = []
            }
            environment.write(try render(key, selector, next, json: json))
            return 0
        default:
            throw UsageError("Unknown config action: \(action). Use get, set, add, remove, or unset.")
        }
    }

    private static func checkKey(_ key: String) throws {
        guard configKeys.contains(key) else {
            throw UsageError("Unknown setting: \(key). Settings: \(configKeys.joined(separator: ", ")).")
        }
    }

    private func change(_ action: String, _ key: String, _ values: [String], _ config: inout WorkspaceConfiguration)
        throws -> [String]
    {
        if Self.scalarKeys.contains(key) {
            switch action {
            case "set":
                guard values.count == 1 else { throw UsageError("config set \(key) takes one value.") }
                try Self.setScalar(key, values[0], &config)
            case "unset":
                guard values.isEmpty else { throw UsageError("config unset \(key) takes no values.") }
                try Self.setScalar(key, nil, &config)
            default:
                throw UsageError("\(key) has one value. Use config set or config unset.")
            }
            return []
        }
        if Self.listKeys.contains(key) {
            if action == "unset" {
                guard values.isEmpty else { throw UsageError("config unset \(key) takes no values.") }
                Self.setList(key, WorkspaceConfiguration(), &config)
                return []
            }
            guard !values.isEmpty else {
                throw UsageError("config \(action) \(key) needs at least one value. Use config unset \(key) to clear it.")
            }
            let entries = try normalized(key, values, existing: action != "remove")
            var list = Self.list(key, config)
            switch action {
            case "set": list = entries
            case "add": list += entries.filter { !list.contains($0) }
            default:
                let missing = entries.filter { !list.contains($0) }
                guard missing.isEmpty else {
                    throw BigboardError.message("Not in \(key): \(missing.joined(separator: ", "))")
                }
                list.removeAll { entries.contains($0) }
            }
            if key != "sources" { list.sort() }
            var updated = config
            switch key {
            case "sources": updated.sourceRoots = list
            case "repositories": updated.repositories = list
            case "exclude": updated.exclude = list
            case "ai-identities": updated.aiIdentities = list
            default: updated.botIdentities = list
            }
            config = updated
            return []
        }
        switch key {
        case "group": return try changeGroup(action, values, &config)
        case "work-area": return try changeWorkArea(action, values, &config)
        default: return try changeIdentity(action, values, &config)
        }
    }

    private func changeGroup(_ action: String, _ values: [String], _ config: inout WorkspaceConfiguration) throws
        -> [String]
    {
        guard let name = values.first?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty else {
            throw UsageError("config \(action) group needs a group name.")
        }
        let repositories = try normalized("repositories", Array(values.dropFirst()))
        let existing = config.groups[name]
        switch action {
        case "set":
            guard !repositories.isEmpty else { throw UsageError("config set group needs at least one repository.") }
            config.groups[name] = repositories
        case "add":
            guard !repositories.isEmpty else { throw UsageError("config add group needs at least one repository.") }
            config.groups[name] = Array(Set(existing ?? []).union(repositories)).sorted()
        case "remove":
            guard let existing else { throw BigboardError.message("No group named \(name).") }
            guard !repositories.isEmpty else { throw UsageError("config remove group needs at least one repository.") }
            let missing = repositories.filter { !existing.contains($0) }
            guard missing.isEmpty else {
                throw BigboardError.message("Not in group \(name): \(missing.joined(separator: ", "))")
            }
            let remaining = existing.filter { !repositories.contains($0) }
            config.groups[name] = remaining.isEmpty ? nil : remaining
        default:
            guard values.count == 1 else { throw UsageError("config unset group takes only a group name.") }
            guard existing != nil else { throw BigboardError.message("No group named \(name).") }
            config.groups[name] = nil
        }
        return [name]
    }

    private func changeWorkArea(_ action: String, _ values: [String], _ config: inout WorkspaceConfiguration) throws
        -> [String]
    {
        guard values.count >= 2 else { throw UsageError("config \(action) work-area needs a repository and an area name.") }
        let repository = try normalized("repositories", [values[0]])[0]
        let name = values[1].trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { throw UsageError("Work-area names cannot be empty.") }
        let paths = parseListEntries(values.dropFirst(2).joined(separator: "\n"))
        var areas = config.workAreas[repository] ?? []
        let index = areas.firstIndex { $0.name == name }
        switch action {
        case "set", "add":
            guard !paths.isEmpty else { throw UsageError("config \(action) work-area needs at least one path.") }
            if let index {
                let merged = action == "set" ? paths : areas[index].paths + paths.filter { !areas[index].paths.contains($0) }
                areas[index] = WorkArea(name: name, paths: merged)
            } else {
                areas.append(WorkArea(name: name, paths: paths))
            }
        case "remove":
            guard let index else { throw BigboardError.message("No work area named \(name) in \(repository).") }
            guard !paths.isEmpty else { throw UsageError("config remove work-area needs at least one path.") }
            let missing = paths.filter { !areas[index].paths.contains($0) }
            guard missing.isEmpty else {
                throw BigboardError.message("Not in work area \(name): \(missing.joined(separator: ", "))")
            }
            areas[index].paths.removeAll { paths.contains($0) }
            if areas[index].paths.isEmpty { areas.remove(at: index) }
        default:
            guard values.count == 2 else { throw UsageError("config unset work-area takes a repository and an area name.") }
            guard let index else { throw BigboardError.message("No work area named \(name) in \(repository).") }
            areas.remove(at: index)
        }
        config.workAreas[repository] = areas.isEmpty ? nil : areas
        return [repository, name]
    }

    private func changeIdentity(_ action: String, _ values: [String], _ config: inout WorkspaceConfiguration) throws
        -> [String]
    {
        guard let first = values.first else { throw UsageError("config \(action) identity needs an email address.") }
        let email = first.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard email.contains("@") else { throw UsageError("Identity must start with an email address: \(first)") }
        let existing = config.identities[email]
        switch action {
        case "set":
            guard values.count >= 2 else { throw UsageError("config set identity needs an email address and a name.") }
            let aliases = Self.emails(values.dropFirst(2))
            IdentityStaging.stage(
                Set([email] + aliases), primary: email, name: values[1],
                replacing: existing == nil ? nil : email, into: &config.identities)
        case "add":
            let aliases = Self.emails(values.dropFirst())
            guard !aliases.isEmpty else { throw UsageError("config add identity needs at least one alias.") }
            IdentityStaging.stage(
                Set([email] + (existing?.aliases ?? []) + aliases), primary: email, name: existing?.name ?? "",
                replacing: existing == nil ? nil : email, into: &config.identities)
        case "remove":
            guard let existing else { throw BigboardError.message("No identity for \(email).") }
            let aliases = Self.emails(values.dropFirst())
            guard !aliases.isEmpty else { throw UsageError("config remove identity needs at least one alias.") }
            let missing = aliases.filter { !existing.aliases.contains($0) }
            guard missing.isEmpty else {
                throw BigboardError.message("Not an alias of \(email): \(missing.joined(separator: ", "))")
            }
            config.identities[email]?.aliases.removeAll { aliases.contains($0) }
        default:
            guard values.count == 1 else { throw UsageError("config unset identity takes only an email address.") }
            guard existing != nil else { throw BigboardError.message("No identity for \(email).") }
            config.identities[email] = nil
        }
        return [email]
    }

    private static func emails<S: Sequence<String>>(_ values: S) -> [String] {
        parseListEntries(values.joined(separator: "\n")).map { $0.lowercased() }
    }

    private func normalized(_ key: String, _ values: [String], existing: Bool = true) throws -> [String] {
        let entries = parseListEntries(values.joined(separator: "\n"))
        switch key {
        case "repositories":
            return try entries.map { value in
                guard let id = canonicalGitHubRepository(value.lowercased()) else {
                    throw BigboardError.message("Repository must be owner/name: \(value)")
                }
                return id
            }
        case "sources":
            var seen = Set<String>(), paths: [String] = []
            for value in values.flatMap({ $0.components(separatedBy: "\n") }) {
                let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !trimmed.isEmpty else { continue }
                let expanded = (trimmed as NSString).expandingTildeInPath
                let url =
                    expanded.hasPrefix("/")
                    ? URL(fileURLWithPath: expanded)
                    : URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent(expanded)
                let path = url.standardizedFileURL.path
                var directory: ObjCBool = false
                guard !existing || (FileManager.default.fileExists(atPath: path, isDirectory: &directory) && directory.boolValue)
                else {
                    throw BigboardError.message("Not a folder: \(path)")
                }
                if seen.insert(path).inserted { paths.append(path) }
            }
            return paths
        default:
            return entries
        }
    }

    private func checkClones(_ config: WorkspaceConfiguration) async throws {
        guard !config.repositories.isEmpty else {
            throw BigboardError.message("Select at least one repository to track.")
        }
        let clones = await CloneDiscovery.find(in: CloneDiscovery.roots(config.sourceRoots))
        let unavailable = Set(config.repositories).subtracting(clones.map(\.id)).sorted()
        guard unavailable.isEmpty else {
            throw BigboardError.message(
                "No local clone was found for: \(unavailable.joined(separator: ", ")). Add their folder with bigboard config add sources, or leave them out."
            )
        }
    }

    private static func setScalar(_ key: String, _ value: String?, _ config: inout WorkspaceConfiguration) throws {
        let defaults = WorkspaceConfiguration()
        func integer() throws -> Int? {
            guard let value else { return nil }
            guard let number = Int(value) else { throw UsageError("\(key) must be a whole number.") }
            return number
        }
        func boolean() throws -> Bool? {
            guard let value else { return nil }
            switch value.lowercased() {
            case "true", "yes", "on", "1": return true
            case "false", "no", "off", "0": return false
            default: throw UsageError("\(key) must be true or false.")
            }
        }
        switch key {
        case "all-files": config.allFiles = try boolean() ?? defaults.allFiles
        case "cached-only": config.cachedOnly = try boolean() ?? defaults.cachedOnly
        case "since-days": config.sinceDays = try integer() ?? defaults.sinceDays
        case "refresh-interval": config.refreshIntervalMinutes = try integer() ?? defaults.refreshIntervalMinutes
        case "request-budget": config.requestBudget = try integer() ?? defaults.requestBudget
        default:
            if let value, !WorkspaceStorage.contributorSorts.contains(value) {
                throw UsageError(
                    "contributor-sort must be one of: \(WorkspaceStorage.contributorSorts.joined(separator: ", ")).")
            }
            config.contributorSort = value ?? defaults.contributorSort
        }
    }

    private static func list(_ key: String, _ config: WorkspaceConfiguration) -> [String] {
        switch key {
        case "sources": config.sourceRoots
        case "repositories": config.repositories
        case "exclude": config.exclude
        case "ai-identities": config.aiIdentities
        default: config.botIdentities
        }
    }
    private static func setList(_ key: String, _ source: WorkspaceConfiguration, _ config: inout WorkspaceConfiguration) {
        switch key {
        case "sources": config.sourceRoots = source.sourceRoots
        case "repositories": config.repositories = source.repositories
        case "exclude": config.exclude = source.exclude
        case "ai-identities": config.aiIdentities = source.aiIdentities
        default: config.botIdentities = source.botIdentities
        }
    }

    private func report(_ config: WorkspaceConfiguration) -> ConfigReport {
        ConfigReport(config, settings: environment.storage.configurationFile.path)
    }

    private func render(_ key: String, _ selector: [String], _ config: WorkspaceConfiguration, json: Bool) throws
        -> String
    {
        let report = report(config)
        let maxSelector = ["group": 1, "work-area": 2, "identity": 1][key] ?? 0
        guard selector.count <= maxSelector else { throw UsageError("Too many values for config get \(key).") }
        func pick<T>(_ rows: [T], _ matches: (T) -> Bool, _ missing: String) throws -> [T] {
            let picked = rows.filter(matches)
            guard selector.isEmpty || !picked.isEmpty else { throw BigboardError.message(missing) }
            return picked
        }
        switch key {
        case "group":
            let rows = try pick(report.groups, { selector.isEmpty || $0.name == selector[0] }, "No group named \(selector.first ?? "").")
            if json { return try Self.json(selector.isEmpty ? rows : [rows[0]]) }
            if selector.count == 1 { return rows[0].repositories.map { $0 + "\n" }.joined() }
            return rows.map { "\($0.name): \($0.repositories.joined(separator: ", "))\n" }.joined()
        case "work-area":
            let wanted = selector.first.map { $0.lowercased() }
            let rows = try pick(
                report.workAreas,
                { (wanted == nil || $0.repository == wanted) && (selector.count < 2 || $0.name == selector[1]) },
                "No matching work area.")
            if json { return try Self.json(rows) }
            if selector.count == 2 { return rows[0].paths.map { $0 + "\n" }.joined() }
            return rows.map { "\($0.repository) \($0.name): \($0.paths.joined(separator: ", "))\n" }.joined()
        case "identity":
            let wanted = selector.first?.lowercased()
            let rows = try pick(report.identities, { wanted == nil || $0.email == wanted }, "No identity for \(wanted ?? "").")
            if json { return try Self.json(rows) }
            return rows.map(Self.identityLine).joined()
        case "all-files": return json ? try Self.json(report.allFiles) : "\(report.allFiles)\n"
        case "cached-only": return json ? try Self.json(report.cachedOnly) : "\(report.cachedOnly)\n"
        case "since-days": return json ? try Self.json(report.sinceDays) : "\(report.sinceDays)\n"
        case "refresh-interval": return json ? try Self.json(report.refreshInterval) : "\(report.refreshInterval)\n"
        case "request-budget": return json ? try Self.json(report.requestBudget) : "\(report.requestBudget)\n"
        case "contributor-sort": return json ? try Self.json(report.contributorSort) : "\(report.contributorSort)\n"
        default:
            let values = Self.list(key, config)
            return json ? try Self.json(values) : values.map { $0 + "\n" }.joined()
        }
    }

    private static func identityLine(_ row: ConfigReport.Identity) -> String {
        var line = row.email
        if !row.name.isEmpty { line += " \(row.name)" }
        if !row.aliases.isEmpty { line += " (also \(row.aliases.joined(separator: ", ")))" }
        return line + "\n"
    }

    static func describe(_ config: WorkspaceConfiguration, settings: String) -> String {
        let report = ConfigReport(config, settings: settings)
        var lines = ["Settings: \(settings)", ""]
        func list(_ key: String, _ values: [String]) {
            lines.append(values.isEmpty ? "\(key): none" : "\(key):")
            lines += values.map { "  \($0)" }
        }
        list("sources", report.sources)
        list("repositories", report.repositories)
        list("group", report.groups.map { "\($0.name): \($0.repositories.joined(separator: ", "))" })
        list("work-area", report.workAreas.map { "\($0.repository) \($0.name): \($0.paths.joined(separator: ", "))" })
        list("identity", report.identities.map { String(identityLine($0).dropLast()) })
        list("exclude", report.exclude)
        list("ai-identities", report.aiIdentities)
        list("bot-identities", report.botIdentities)
        lines += [
            "all-files: \(report.allFiles)",
            "since-days: \(report.sinceDays)",
            "contributor-sort: \(report.contributorSort)",
            "refresh-interval: \(report.refreshInterval)",
            "request-budget: \(report.requestBudget)",
            "cached-only: \(report.cachedOnly)",
        ]
        return lines.joined(separator: "\n") + "\n"
    }
}

struct ConfigReport: Encodable {
    struct Group: Encodable {
        var name: String
        var repositories: [String]
    }
    struct Area: Encodable {
        var repository: String
        var name: String
        var paths: [String]
    }
    struct Identity: Encodable {
        var email: String
        var name: String
        var aliases: [String]
    }
    var settings: String
    var sources: [String]
    var repositories: [String]
    var groups: [Group]
    var workAreas: [Area]
    var identities: [Identity]
    var exclude: [String]
    var aiIdentities: [String]
    var botIdentities: [String]
    var allFiles: Bool
    var sinceDays: Int
    var contributorSort: String
    var refreshInterval: Int
    var requestBudget: Int
    var cachedOnly: Bool

    init(_ config: WorkspaceConfiguration, settings: String) {
        self.settings = settings
        sources = config.sourceRoots
        repositories = config.repositories
        groups = config.groups.keys.sorted().map { Group(name: $0, repositories: config.groups[$0] ?? []) }
        workAreas = config.workAreas.keys.sorted().flatMap { repository in
            (config.workAreas[repository] ?? []).map { Area(repository: repository, name: $0.name, paths: $0.paths) }
        }
        identities = config.identities.keys.sorted().map {
            Identity(email: $0, name: config.identities[$0]?.name ?? "", aliases: config.identities[$0]?.aliases ?? [])
        }
        exclude = config.exclude
        aiIdentities = config.aiIdentities
        botIdentities = config.botIdentities
        allFiles = config.allFiles
        sinceDays = config.sinceDays
        contributorSort = config.contributorSort
        refreshInterval = config.refreshIntervalMinutes
        requestBudget = config.requestBudget
        cachedOnly = config.cachedOnly
    }
}
