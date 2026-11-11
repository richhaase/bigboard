import Foundation

struct ActivityTotals: Sendable, Equatable {
    var commits = 0
    var added = 0
    var removed = 0
    var people = 0
    var ai = 0
    var unknownLineCommits = 0
    var unknown: Bool { unknownLineCommits > 0 }
    var change: Int { added + removed }
    var net: Int { added - removed }
    var aiPercent: Double { commits == 0 ? 0 : Double(ai) * 100 / Double(commits) }
}
struct PersonRow: Identifiable, Sendable {
    var id: String
    var name: String
    var email: String
    var bot: Bool
    var records: [Commit]
    var totals: ActivityTotals
    var latest: Date?
    var repos: [String]
}
struct ActivityRow: Identifiable, Sendable {
    var id: String
    var label: String
    var kind: String
    var scope: ActivityScope
    var records: [Commit]
    var totals: ActivityTotals
    var latest: Date?
    var mergedPRCount = 0
}
enum TimelineUnit: String, Sendable, Encodable {
    case hour, day, week, month
}
struct TimelinePoint: Identifiable, Sendable {
    var date: Date
    var commits = 0
    var added = 0
    var removed = 0
    var id: Date { date }
}
struct RelatedRow: Identifiable, Sendable {
    var scope: ActivityScope
    var evidence: [String]
    var records: [Commit]
    var id: String { "\(scope.repo)\u{0}\(scope.path)\u{0}\(scope.areaName)" }
    var count: Int { evidence.count }
}
struct PRFilters: Sendable {
    var query = ""
    var repos: Set<String> = []
    var authors: Set<String> = []
    var reviews: Set<String> = []
    var checks: Set<String> = []
    var states: Set<String> = []
    var mine: Set<String> = []
    var sort = "updated"
    var reverse = false
}

enum Analysis {
    static func literalLess(_ a: String, _ b: String) -> Bool { a.utf8.lexicographicallyPrecedes(b.utf8) }
    static func unique(_ commits: [Commit]) -> [Commit] {
        var result: [Commit] = []
        var indices: [String: Int] = [:]
        for commit in commits {
            if !commit.id.isEmpty, let index = indices[commit.id] {
                let old = result[index]
                let oldKey = old.identity + "\u{0}" + old.author
                let newKey = commit.identity + "\u{0}" + commit.author
                if (old.linesUnknown && !commit.linesUnknown)
                    || (old.linesUnknown == commit.linesUnknown
                        && (literalLess(commit.repo, old.repo)
                            || (commit.repo == old.repo && literalLess(newKey, oldKey))))
                {
                    result[index] = commit
                }
            } else {
                if !commit.id.isEmpty { indices[commit.id] = result.count }
                result.append(commit)
            }
        }
        return result
    }
    static func inRange(_ date: Date?, days: Int = 0, since: Date? = nil, now: Date) -> Bool {
        if let since { return date.map { $0 > since } ?? false }
        guard days > 0 else { return true }
        return date.map { $0 > now.addingTimeInterval(-Double(days) * 86_400) } ?? false
    }
    static func select(
        _ commits: [Commit], days: Int = 0, since: Date? = nil, until: Date? = nil, repos: Set<String> = [],
        hideBots: Bool = false, people: Set<String> = [], now: Date = Date()
    ) -> [Commit] {
        let selected = commits.filter { commit in
            (repos.isEmpty || repos.contains(commit.repo)) && inRange(commit.date, days: days, since: since, now: now)
                && until.map { commit.date <= $0 } != false
        }
        let preferred = unique(selected)
        let canonical = Dictionary(
            preferred.filter { !$0.id.isEmpty }.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let bots = Set(preferred.filter(\.bot).map(\.identity))
        return selected.filter {
            let c = canonical[$0.id] ?? $0
            return (!hideBots || !bots.contains(c.identity))
                && (people.isEmpty || people.contains(c.identity))
        }
    }
    static func totals(_ commits: [Commit]) -> ActivityTotals {
        let selected = unique(commits)
        return ActivityTotals(
            commits: selected.count, added: selected.reduce(0) { $0 + ($1.linesUnknown ? 0 : $1.added) },
            removed: selected.reduce(0) { $0 + ($1.linesUnknown ? 0 : $1.removed) },
            people: Set(selected.map(\.identity)).count, ai: selected.filter(\.ai).count,
            unknownLineCommits: selected.filter(\.linesUnknown).count)
    }
    static func excludedLines(_ commits: [Commit]) -> [String: Int] {
        var result: [String: Int] = [:]
        for commit in unique(commits) {
            for file in commit.files where !file.excludedReason.isEmpty {
                result[file.excludedReason, default: 0] += file.added + file.removed
            }
        }
        return result
    }
    static func people(_ commits: [Commit], canonicalRecords: [Commit]? = nil) -> [PersonRow] {
        let canonical = Dictionary(
            unique(canonicalRecords ?? commits).filter { !$0.id.isEmpty }.map { ($0.id, $0) },
            uniquingKeysWith: { first, _ in first })
        var grouped: [String: [Commit]] = [:]
        for var commit in commits {
            let id = canonical[commit.id]?.identity ?? commit.identity
            if let preferred = canonical[commit.id] {
                commit.author = preferred.author
                commit.email = preferred.email
                commit.ai = preferred.ai
                commit.bot = preferred.bot
            }
            grouped[id, default: []].append(commit)
        }
        return
            grouped.map { id, records in
                let selected = unique(records)
                let names = Dictionary(grouping: selected, by: \.author).mapValues(\.count)
                let name =
                    names.keys
                    .sorted { a, b in
                        if names[a] != names[b] { return (names[a] ?? 0) > (names[b] ?? 0) }
                        if a.unicodeScalars.count != b.unicodeScalars.count {
                            return a.unicodeScalars.count > b.unicodeScalars.count
                        }
                        return literalLess(a, b)
                    }
                    .first ?? "Unknown contributor"
                return PersonRow(
                    id: id, name: name.isEmpty ? (records.first?.email ?? "Unknown contributor") : name,
                    email: records.first?.email ?? "", bot: records.contains(where: \.bot), records: records,
                    totals: totals(records), latest: records.map(\.date).max(),
                    repos: Array(Set(records.map(\.repo))).sorted(by: literalLess))
            }
            .sorted { a, b in
                a.totals.change != b.totals.change
                    ? a.totals.change > b.totals.change
                    : a.totals.commits != b.totals.commits
                        ? a.totals.commits > b.totals.commits : literalLess(a.name, b.name)
            }
    }
    static func classifyWorkArea(_ path: String, rules: [WorkArea]) -> String {
        var selected = "", length = -1
        for rule in rules {
            for raw in rule.paths {
                let prefix = WorkArea.canonicalPrefix(raw)
                if (path == prefix || path.hasPrefix(prefix + "/")) && prefix.utf8.count > length {
                    selected = rule.name
                    length = prefix.utf8.count
                }
            }
        }
        return selected
    }
    static func scoped(_ commits: [Commit], scope: ActivityScope, workAreas: [String: [WorkArea]] = [:])
        -> [Commit]
    {
        guard !scope.repo.isEmpty else { return commits }
        let selected = commits.filter { $0.repo == scope.repo }
        guard !scope.path.isEmpty || !scope.areaName.isEmpty else { return selected }
        return selected.compactMap { original in
            guard !original.pathsUnknown else { return nil }
            let files = original.files.filter { file in
                guard file.excludedReason.isEmpty else { return false }
                if !scope.areaName.isEmpty
                    && classifyWorkArea(file.path, rules: workAreas[scope.repo] ?? []) != scope.areaName
                {
                    return false
                }
                return scope.contains(file.path)
            }
            guard !files.isEmpty else { return nil }
            var commit = original
            commit.files = files
            commit.added = files.reduce(0) { $0 + $1.added }
            commit.removed = files.reduce(0) { $0 + $1.removed }
            return commit
        }
    }
    static func relatedPRs(
        _ pullRequests: [PullRequest], scope: ActivityScope, workAreas: [String: [WorkArea]] = [:],
        openedBy: Date? = nil
    ) -> [PullRequest] {
        pullRequests.filter { pr in
            guard pr.repo == scope.repo else { return false }
            if let openedBy, let created = pr.createdAt, created > openedBy { return false }
            if scope.path.isEmpty && scope.areaName.isEmpty { return true }
            return pr.files.contains {
                scope.contains($0)
                    && (scope.areaName.isEmpty
                        || classifyWorkArea($0, rules: workAreas[scope.repo] ?? []) == scope.areaName)
            }
        }
    }
    static func mergedPRs(
        _ pullRequests: [PullRequest], scope: ActivityScope, workAreas: [String: [WorkArea]] = [:]
    ) -> [PullRequest] {
        let scoped = scope.repo.isEmpty ? pullRequests : relatedPRs(pullRequests, scope: scope, workAreas: workAreas)
        var seen: Set<String> = []
        return scoped.filter { seen.insert($0.id).inserted }
    }
    static func activityRows(
        _ commits: [Commit], repositories: [Repository], scope: ActivityScope = ActivityScope(),
        workAreas: [String: [WorkArea]] = [:], mergedPullRequests: [PullRequest] = []
    ) -> [ActivityRow] {
        if scope.repo.isEmpty {
            let grouped = Dictionary(grouping: commits, by: \.repo)
            let pullRequestsByRepo = Dictionary(grouping: mergedPullRequests, by: \.repo)
            return repositories.map { repo in
                let rows = grouped[repo.id] ?? []
                return ActivityRow(
                    id: repo.id, label: repo.id, kind: "repository", scope: ActivityScope(repo: repo.id),
                    records: rows, totals: totals(rows), latest: rows.map(\.date).max(),
                    mergedPRCount: Set((pullRequestsByRepo[repo.id] ?? []).map(\.id)).count)
            }
        }
        guard !scope.isFile else { return [] }
        let prefix = scope.path.isEmpty ? "" : scope.path + "/"
        let rules = workAreas[scope.repo] ?? []
        func childScope(for path: String) -> ActivityScope? {
            guard path.hasPrefix(prefix),
                scope.areaName.isEmpty || classifyWorkArea(path, rules: rules) == scope.areaName
            else { return nil }
            let remainder = path.dropFirst(prefix.count)
            guard let first = remainder.split(separator: "/", omittingEmptySubsequences: false).first,
                !first.isEmpty
            else { return nil }
            return ActivityScope(
                repo: scope.repo, path: prefix + first, isFile: !remainder.contains("/"), areaName: scope.areaName)
        }
        var grouped: [ActivityScope: [Commit]] = [:]
        for commit in commits where commit.repo == scope.repo && !commit.pathsUnknown {
            var changes: [ActivityScope: [FileChange]] = [:]
            for file in commit.files where file.excludedReason.isEmpty {
                guard let location = childScope(for: file.path) else { continue }
                changes[location, default: []].append(file)
            }
            for (location, files) in changes {
                var record = commit
                record.files = files
                record.added = files.reduce(0) { $0 + $1.added }
                record.removed = files.reduce(0) { $0 + $1.removed }
                grouped[location, default: []].append(record)
            }
        }
        var mergedPRIDs: [ActivityScope: Set<String>] = [:]
        for pullRequest in mergedPullRequests where pullRequest.repo == scope.repo {
            let id = pullRequest.id
            for path in pullRequest.files {
                guard let location = childScope(for: path) else { continue }
                mergedPRIDs[location, default: []].insert(id)
            }
        }
        return
            Set(grouped.keys).union(mergedPRIDs.keys).map { location in
                let records = grouped[location] ?? []
                let kind = location.isFile ? "file" : "directory"
                return ActivityRow(
                    id: kind + ":" + location.path,
                    label: String(location.path.split(separator: "/").last ?? ""), kind: kind,
                    scope: location, records: records, totals: totals(records),
                    latest: records.map(\.date).max(), mergedPRCount: mergedPRIDs[location]?.count ?? 0)
            }
            .sorted {
                $0.totals.change != $1.totals.change
                    ? $0.totals.change > $1.totals.change : literalLess($0.id, $1.id)
            }
    }
    static func related(
        _ commits: [Commit], scope: ActivityScope, mode: String = "commits",
        workAreas: [String: [WorkArea]] = [:]
    ) -> [RelatedRow] {
        let canonical = Dictionary(
            unique(commits).filter { !$0.id.isEmpty }.map { ($0.id, $0) },
            uniquingKeysWith: { first, _ in first })
        func evidence(_ c: Commit) -> String { mode == "people" ? (canonical[c.id] ?? c).identity : c.id }
        let wanted = Set(
            scoped(commits, scope: scope, workAreas: workAreas).map(evidence).filter { !$0.isEmpty })
        var grouped: [ActivityScope: [Commit]] = [:]
        for commit in commits where wanted.contains(evidence(commit)) {
            let files = commit.pathsUnknown ? [] : commit.files.filter { $0.excludedReason.isEmpty }
            var targets: Set<ActivityScope> =
                scope.path.isEmpty
                ? [ActivityScope(repo: commit.repo)]
                : Set(
                    files.map { file in
                        ActivityScope(
                            repo: commit.repo,
                            path: file.path.split(separator: "/").dropLast().joined(separator: "/"))
                    })
            for file in files {
                let name = classifyWorkArea(file.path, rules: workAreas[commit.repo] ?? [])
                if !name.isEmpty { targets.insert(ActivityScope(repo: commit.repo, areaName: name)) }
            }
            for target in targets
            where target != scope
                && !(target.areaName.isEmpty && target.repo == scope.repo && scope.path.isEmpty
                    && scope.areaName.isEmpty)
            { grouped[target, default: []].append(commit) }
        }
        return
            grouped.map {
                RelatedRow(
                    scope: $0.key, evidence: Array(Set($0.value.map(evidence))).sorted(by: literalLess),
                    records: $0.value)
            }
            .sorted { a, b in a.count != b.count ? a.count > b.count : literalLess(a.id, b.id) }
    }
    static func daily(
        _ commits: [Commit], days: Int = 30, since: Date? = nil, now: Date = Date(),
        calendar: Calendar = .current
    ) -> [TimelinePoint] {
        let cutoff = since ?? (days > 0 ? now.addingTimeInterval(-Double(days) * 86_400) : nil)
        let records = unique(commits)
            .filter { commit in
                commit.date <= now && cutoff.map { commit.date > $0 } != false
            }
        let start = cutoff ?? records.map(\.date).min() ?? now
        var date = calendar.startOfDay(for: start)
        var rows: [Date: TimelinePoint] = [:]
        while date <= now {
            rows[date] = TimelinePoint(date: date)
            date = calendar.date(byAdding: .day, value: 1, to: date)!
        }
        for c in records {
            let key = calendar.startOfDay(for: c.date)
            var row = rows[key] ?? TimelinePoint(date: key)
            row.commits += 1
            row.added += c.linesUnknown ? 0 : c.added
            row.removed += c.linesUnknown ? 0 : c.removed
            rows[key] = row
        }
        return rows.values.sorted { $0.date < $1.date }
    }
    static func timeline(
        _ commits: [Commit], days: Int = 30, since: Date? = nil, now: Date = Date(),
        calendar: Calendar = .current, includesStart: Bool = false
    ) -> [TimelinePoint] {
        let records = unique(commits)
        let cutoff = since ?? (days > 0 ? now.addingTimeInterval(-Double(days) * 86_400) : nil)
        let unit = timelineUnit(since: cutoff, now: now)
        let hourly = unit == .hour, daily = unit == .day, weekly = unit == .week
        func bucket(_ date: Date) -> Date {
            if hourly { return calendar.dateInterval(of: .hour, for: date)?.start ?? date }
            if daily { return calendar.startOfDay(for: date) }
            if weekly { return weekStart(date, calendar: calendar) }
            return calendar.date(from: calendar.dateComponents([.year, .month], from: date))!
        }
        let start = cutoff.map(bucket) ?? bucket(records.reduce(now) { min($0, $1.date) })
        let component: Calendar.Component = hourly ? .hour : daily ? .day : weekly ? .weekOfYear : .month
        var rows: [Date: TimelinePoint] = [:]
        var date = start
        while date < now || date == start {
            rows[date] = TimelinePoint(date: date)
            date = calendar.date(byAdding: component, value: 1, to: date)!
        }
        for c in records {
            if let cutoff, c.date < cutoff || (!includesStart && c.date == cutoff) { continue }
            var key = bucket(c.date)
            if rows[key] == nil, c.date == now, let last = rows.keys.max() { key = last }
            guard rows[key] != nil || c.date > now else { continue }
            var row = rows[key] ?? TimelinePoint(date: key)
            row.commits += 1
            row.added += c.linesUnknown ? 0 : c.added
            row.removed += c.linesUnknown ? 0 : c.removed
            rows[key] = row
        }
        return rows.values.sorted { $0.date < $1.date }
    }
    static func timelineUnit(since: Date?, now: Date) -> TimelineUnit {
        guard let since else { return .month }
        let span = max(0, now.timeIntervalSince(since) / 86_400)
        return span <= 2 ? .hour : span <= 30 ? .day : span <= 180 ? .week : .month
    }
    private static func weekStart(_ date: Date, calendar: Calendar) -> Date {
        calendar.dateInterval(of: .weekOfYear, for: date)?.start ?? calendar.startOfDay(for: date)
    }
    static func updatedSinceReview(_ pr: PullRequest, viewer: String) -> Bool {
        guard !viewer.isEmpty, let updated = pr.updatedAt,
            let reviewed = pr.reviews.filter({ $0.author.lowercased() == viewer.lowercased() })
                .compactMap(\.submittedAt).max()
        else { return false }
        return updated > reviewed
    }
    static func requested(_ pr: PullRequest, viewer: String) -> Bool {
        !viewer.isEmpty
            && pr.reviewers.contains { !$0.contains("/") && $0.lowercased() == viewer.lowercased() }
    }
    static func needsAttention(_ pr: PullRequest, viewer: String) -> Bool {
        guard !viewer.isEmpty else { return false }
        let own = pr.author.lowercased() == viewer.lowercased()
        return (!own && (requested(pr, viewer: viewer) || updatedSinceReview(pr, viewer: viewer)))
            || (own
                && (["FAILURE", "ERROR"].contains(pr.checks) || pr.review == "CHANGES_REQUESTED"
                    || (!pr.draft && pr.review == "APPROVED" && pr.checks == "SUCCESS")))
    }
    static func reviewRank(_ pr: PullRequest) -> Int {
        ["CHANGES_REQUESTED": 0, "REVIEW_REQUIRED": 1, "APPROVED": 2][pr.review] ?? 3
    }
    static func checksRank(_ pr: PullRequest) -> Int {
        ["FAILURE": 0, "ERROR": 0, "PENDING": 1, "EXPECTED": 1, "SUCCESS": 2][pr.checks] ?? 3
    }
    static func prSize(_ pr: PullRequest) -> Int {
        guard let additions = pr.additions, let deletions = pr.deletions else { return -1 }
        return additions + deletions
    }
    static func filterPRs(
        _ prs: [PullRequest], viewer: String, filters: PRFilters = PRFilters(), now: Date = Date()
    ) -> [PullRequest] {
        func matches(_ values: Set<String>, _ value: String) -> Bool {
            values.isEmpty || values.contains(value)
        }
        func mine(_ pr: PullRequest) -> Bool {
            filters.mine.isEmpty
                || (!viewer.isEmpty
                    && filters.mine.contains { value in
                        switch value {
                        case "attention": needsAttention(pr, viewer: viewer)
                        case "authored": pr.author.lowercased() == viewer.lowercased()
                        case "requested": requested(pr, viewer: viewer)
                        case "updated": updatedSinceReview(pr, viewer: viewer)
                        default: false
                        }
                    })
        }
        func priority(_ pr: PullRequest) -> Int {
            requested(pr, viewer: viewer)
                ? 0
                : ["FAILURE", "ERROR"].contains(pr.checks)
                    ? 1 : pr.review == "CHANGES_REQUESTED" ? 2 : pr.review == "APPROVED" ? 3 : 4
        }
        let query = filters.query.lowercased()
        let selected = prs.filter { p in
            matches(filters.repos, p.repo)
                && (query.isEmpty
                    || "\(p.repo) \(p.title) \(p.author) \(p.number)".lowercased().contains(query))
                && matches(Set(filters.authors.map { $0.lowercased() }), p.author.lowercased())
                && matches(filters.reviews, p.review) && matches(filters.checks, p.checks)
                && matches(filters.states, p.draft ? "draft" : "open") && mine(p)
        }
        func compare(_ a: PullRequest, _ b: PullRequest) -> Bool {
            if filters.sort == "attention" {
                if priority(a) != priority(b) { return priority(a) < priority(b) }
                if a.updatedAt != b.updatedAt {
                    return (a.updatedAt ?? .distantFuture) < (b.updatedAt ?? .distantFuture)
                }
            } else if filters.sort == "review", reviewRank(a) != reviewRank(b) {
                return reviewRank(a) < reviewRank(b)
            } else if filters.sort == "checks", checksRank(a) != checksRank(b) {
                return checksRank(a) < checksRank(b)
            } else if filters.sort == "size", prSize(a) != prSize(b) {
                return prSize(a) > prSize(b)
            } else if filters.sort == "created", a.createdAt != b.createdAt {
                return (a.createdAt ?? .distantPast) > (b.createdAt ?? .distantPast)
            } else if ["review", "checks", "size", "created"].contains(filters.sort) {
                if a.updatedAt != b.updatedAt {
                    return (a.updatedAt ?? .distantPast) > (b.updatedAt ?? .distantPast)
                }
            } else {
                let lhs =
                    filters.sort == "repository"
                    ? a.repo
                    : filters.sort == "author"
                        ? a.author.lowercased() : filters.sort == "title" ? a.title : ""
                let rhs =
                    filters.sort == "repository"
                    ? b.repo
                    : filters.sort == "author"
                        ? b.author.lowercased() : filters.sort == "title" ? b.title : ""
                if lhs != rhs { return literalLess(lhs, rhs) }
                if a.updatedAt != b.updatedAt {
                    return (a.updatedAt ?? .distantPast) > (b.updatedAt ?? .distantPast)
                }
            }
            return a.repo != b.repo ? literalLess(a.repo, b.repo) : a.number > b.number
        }
        return selected.sorted { filters.reverse ? compare($1, $0) : compare($0, $1) }
    }
}
