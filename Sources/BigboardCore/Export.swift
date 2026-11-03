import Foundation

struct ExportContribution: Encodable, Equatable {
    var commits = 0
    var added = 0
    var removed = 0
    var net = 0
    var totalChange = 0
    var aiCommits = 0
    var unknownLineCommits: Int? = nil
    enum CodingKeys: String, CodingKey {
        case commits, added, removed, net
        case totalChange = "total_change", aiCommits = "ai_commits", unknownLineCommits =
            "unknown_line_commits"
    }
    mutating func add(_ commit: Commit) {
        commits += 1
        if commit.linesUnknown {
            unknownLineCommits = (unknownLineCommits ?? 0) + 1
        } else {
            added += commit.added
            removed += commit.removed
            net += commit.added - commit.removed
            totalChange += commit.added + commit.removed
        }
        if commit.ai { aiCommits += 1 }
    }
}
struct ExportAuthor: Encodable {
    var identity: String
    var name: String
    var commits: Int
    var added: Int
    var removed: Int
    var net: Int
    var totalChange: Int
    var aiCommits: Int
    var bot: Bool
    var firstCommit: String
    var lastCommit: String
    var activeDays: Int
    var perRepo: [String: ExportContribution]
    var unknownLineCommits: Int?
    enum CodingKeys: String, CodingKey {
        case name, commits, added, removed, net, bot
        case totalChange = "total_change", aiCommits = "ai_commits", firstCommit = "first_commit",
            lastCommit = "last_commit", activeDays = "active_days", perRepo = "per_repo", unknownLineCommits =
            "unknown_line_commits"
    }
}
enum ActivityExport {
    static func authors(
        _ commits: [Commit], configuration: WorkspaceConfiguration = WorkspaceConfiguration(),
        calendar: Calendar = .current
    ) -> [ExportAuthor] {
        var groups: [[Commit]] = [], indices: [String: Int] = [:]
        for commit in commits {
            if !commit.id.isEmpty, let index = indices[commit.id] {
                groups[index].append(commit)
            } else {
                if !commit.id.isEmpty { indices[commit.id] = groups.count }
                groups.append([commit])
            }
        }
        struct Accumulation {
            var names: [String: Int] = [:]
            var totals = ExportContribution()
            var perRepo: [String: ExportContribution] = [:]
            var dates: [Date] = []
            var bot = false
        }
        var people: [String: Accumulation] = [:]
        for copies in groups {
            guard let preferred = Analysis.unique(copies).first else { continue }
            var value = people[preferred.identity] ?? Accumulation()
            value.names[preferred.author, default: 0] += 1
            value.totals.add(preferred)
            value.dates.append(preferred.date)
            value.bot =
                value.bot
                || HistoryRules.isBot(
                    name: preferred.author, email: preferred.email, extra: configuration.botIdentities)
            for repo in Set(copies.map(\.repo)) {
                value.perRepo[repo, default: ExportContribution()].add(preferred)
            }
            people[preferred.identity] = value
        }
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = calendar.timeZone
        let rows = people.map { identity, value in
            let name =
                value.names.keys
                .sorted { a, b in
                    if value.names[a] != value.names[b] {
                        return value.names[a, default: 0] > value.names[b, default: 0]
                    }
                    if a.unicodeScalars.count != b.unicodeScalars.count {
                        return a.unicodeScalars.count > b.unicodeScalars.count
                    }
                    return Analysis.literalLess(a, b)
                }
                .first ?? ""
            return ExportAuthor(
                identity: identity, name: name, commits: value.totals.commits, added: value.totals.added,
                removed: value.totals.removed, net: value.totals.net, totalChange: value.totals.totalChange,
                aiCommits: value.totals.aiCommits, bot: value.bot,
                firstCommit: formatter.string(from: value.dates.min()!),
                lastCommit: formatter.string(from: value.dates.max()!),
                activeDays: Set(value.dates.map { calendar.startOfDay(for: $0) }).count,
                perRepo: value.perRepo, unknownLineCommits: value.totals.unknownLineCommits)
        }
        return rows.sorted { a, b in
            func metric(_ row: ExportAuthor) -> Int {
                switch configuration.contributorSort {
                case "commits": row.commits
                case "added": row.added
                case "removed": row.removed
                case "net": row.net
                default: row.totalChange
                }
            }
            if configuration.contributorSort == "ai" {
                let lhs = UInt64(max(0, a.aiCommits)).multipliedFullWidth(by: UInt64(max(1, b.commits)))
                let rhs = UInt64(max(0, b.aiCommits)).multipliedFullWidth(by: UInt64(max(1, a.commits)))
                if lhs.high != rhs.high { return lhs.high > rhs.high }
                if lhs.low != rhs.low { return lhs.low > rhs.low }
            } else if metric(a) != metric(b) {
                return metric(a) > metric(b)
            }
            if a.totalChange != b.totalChange { return a.totalChange > b.totalChange }
            if a.commits != b.commits { return a.commits > b.commits }
            if a.name != b.name { return Analysis.literalLess(a.name, b.name) }
            return Analysis.literalLess(a.identity, b.identity)
        }
    }
    static func json(
        _ commits: [Commit], configuration: WorkspaceConfiguration = WorkspaceConfiguration(),
        calendar: Calendar = .current
    ) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        var data = try encoder.encode(authors(commits, configuration: configuration, calendar: calendar))
        data.append(10)
        return data
    }
}
