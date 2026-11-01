import Foundation

struct HistoryPeriod: Equatable, Sendable {
    var start: Date
    var end: Date
    func contains(_ date: Date, snapshot: Date) -> Bool { date >= start && date < end && date <= snapshot }
    func duration(at snapshot: Date) -> Double { max(0, min(end, snapshot).timeIntervalSince(start)) }
    func partial(at snapshot: Date) -> Bool { end > snapshot }
    func label(timeZone: TimeZone) -> String {
        var style = Date.FormatStyle(date: .abbreviated, time: .omitted)
        style.timeZone = timeZone
        return "\(start.formatted(style)) – \(end.addingTimeInterval(-1).formatted(style))"
    }
}
struct HistoryComparison: Equatable, Sendable {
    var a: HistoryPeriod
    var b: HistoryPeriod
    var timeZoneID: String
    var timeZone: TimeZone { TimeZone(identifier: timeZoneID) ?? .gmt }
    var overlaps: Bool { max(a.start, b.start) < min(a.end, b.end) }
    static func calendarPeriod(start: Date, through end: Date, calendar: Calendar) -> HistoryPeriod {
        HistoryPeriod(start: calendar.startOfDay(for: start), end: calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: end))!)
    }
    static func previous(_ period: HistoryPeriod, calendar: Calendar) -> HistoryPeriod {
        let count = calendar.dateComponents([.day], from: period.start, to: period.end).day ?? 1
        return HistoryPeriod(start: calendar.date(byAdding: .day, value: -count, to: period.start)!, end: period.start)
    }
    static func preset(days: Int, now: Date, calendar: Calendar = .current) -> HistoryComparison {
        let end = calendar.startOfDay(for: now)
        let a = HistoryPeriod(start: calendar.date(byAdding: .day, value: -max(1, days), to: end)!, end: end)
        return HistoryComparison(a: a, b: previous(a, calendar: calendar), timeZoneID: calendar.timeZone.identifier)
    }
    func validation(at snapshot: Date) -> String? {
        guard a.start < a.end, b.start < b.end else { return "Each start date must be on or before its end date." }
        guard a.start < snapshot, b.start < snapshot else { return "Both periods must begin before the snapshot time." }
        var calendar = Calendar(identifier: .gregorian); calendar.timeZone = timeZone
        let tomorrow = calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: snapshot))!
        guard a.end <= tomorrow, b.end <= tomorrow else { return "End dates cannot be after the snapshot's calendar day." }
        return nil
    }
}
enum ComparisonMeasure: String, CaseIterable, Identifiable, Sendable {
    case lines = "Lines changed", commits = "Commits", added = "Additions", removed = "Removals", people = "Contributors"
    var id: Self { self }
    func value(_ totals: ActivityTotals) -> Double {
        let value = switch self { case .lines: totals.change; case .commits: totals.commits; case .added: totals.added; case .removed: totals.removed; case .people: totals.people }
        return Double(value)
    }
    var permitsRate: Bool { self != .people }
}
enum ComparisonOrder: String, CaseIterable, Identifiable, Sendable {
    case difference = "Largest difference", commits = "Commit difference", a = "A volume", b = "B volume", name = "Name"
    var id: Self { self }
}
struct ComparisonValues: Sendable {
    var a: ActivityTotals
    var b: ActivityTotals
    var reliable = true
    func values(_ measure: ComparisonMeasure, comparison: HistoryComparison, snapshot: Date, rates: Bool) -> (Double, Double) {
        let rate = rates && measure.permitsRate
        return (measure.value(a) / (rate ? max(comparison.a.duration(at: snapshot) / 86400, .leastNonzeroMagnitude) : 1), measure.value(b) / (rate ? max(comparison.b.duration(at: snapshot) / 86400, .leastNonzeroMagnitude) : 1))
    }
    func relative(_ measure: ComparisonMeasure, a: Double, b: Double) -> String {
        guard reliable && (measure == .people || measure == .commits || (!self.a.unknown && !self.b.unknown)) else { return "Incomplete evidence" }
        if b == 0 { return a == 0 ? "No activity in either" : "Activity only in A" }
        if a == 0 { return "Activity only in B" }
        return String(format: "%+.1f%%", (a - b) / b * 100)
    }
}
struct ComparisonArea: Identifiable, Sendable {
    var scope: ActivityScope
    var label: String
    var kind: String
    var values: ComparisonValues
    var id: String { "\(scope.repo)\u{0}\(scope.path)\u{0}\(scope.isFile)\u{0}\(scope.areaName)" }
}
struct ComparisonPerson: Identifiable, Sendable {
    var id: String
    var name: String
    var email: String
    var values: ComparisonValues
}
struct ComparisonEvidence: Sendable {
    var a: [Commit]
    var b: [Commit]
    var people: [ComparisonPerson]
    var values: ComparisonValues
    var warnings: [String]
}
struct ComparisonProjection: Sendable {
    var recordsA: [Commit]
    var recordsB: [Commit]
    var canonical: [Commit]
    var evidence: ComparisonEvidence
    var rows: [ComparisonArea]
    func sortedRows(order: ComparisonOrder, measure: ComparisonMeasure, comparison: HistoryComparison, snapshot: Date, rates: Bool, query: String) -> [ComparisonArea] {
        func weight(_ row: ComparisonArea) -> Double {
            let pair = row.values.values(order == .commits ? .commits : measure, comparison: comparison, snapshot: snapshot, rates: rates)
            switch order { case .difference, .commits: return abs(pair.0 - pair.1); case .a: return pair.0; case .b: return pair.1; case .name: return 0 }
        }
        return rows.filter { query.isEmpty || $0.label.localizedCaseInsensitiveContains(query) }.sorted {
            let a = weight($0), b = weight($1)
            return a == b ? $0.id < $1.id : a > b
        }
    }
    static func build(snapshot: Snapshot, configuration: WorkspaceConfiguration, comparison: HistoryComparison, repositories: Set<String>, scope: ActivityScope, hideBots: Bool, people: Set<String>) throws -> ComparisonProjection {
        try Task.checkCancellation()
        let repos = snapshot.repositories.filter { repositories.isEmpty || repositories.contains($0.id) }
        let ids = Set(repos.map(\.id))
        let union = snapshot.commits.filter { ids.contains($0.repo) && (comparison.a.contains($0.date, snapshot: snapshot.generatedAt) || comparison.b.contains($0.date, snapshot: snapshot.generatedAt)) }
        let preferred = Analysis.unique(union)
        let canonical = Dictionary(preferred.filter { !$0.id.isEmpty }.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let bots = Set(preferred.filter(\.bot).map(\.identity))
        var a: [Commit] = [], b: [Commit] = []
        for (index, original) in union.enumerated() {
            if index % 256 == 0 { try Task.checkCancellation() }
            var c = original
            if let p = canonical[c.id] { c.author = p.author; c.email = p.email; c.bot = p.bot; c.ai = p.ai }
            let identity = canonical[c.id]?.identity ?? c.identity
            guard (!hideBots || !bots.contains(identity)) && (people.isEmpty || people.contains(identity)) else { continue }
            if comparison.a.contains(c.date, snapshot: snapshot.generatedAt) { a.append(c) }
            if comparison.b.contains(c.date, snapshot: snapshot.generatedAt) { b.append(c) }
        }
        let locations = Analysis.activityRows(a + b, repositories: repos, scope: scope, workAreas: configuration.workAreas)
        var rows: [ComparisonArea] = []
        for row in locations {
            try Task.checkCancellation()
            let sa = Analysis.scoped(a, scope: row.scope, workAreas: configuration.workAreas)
            let sb = Analysis.scoped(b, scope: row.scope, workAreas: configuration.workAreas)
            rows.append(ComparisonArea(scope: row.scope, label: row.label, kind: row.kind, values: values(sa, sb, repos: repos, scope: row.scope, all: a + b)))
        }
        try Task.checkCancellation()
        return ComparisonProjection(recordsA: a, recordsB: b, canonical: preferred, evidence: evidence(a, b, repos: repos, scope: scope, areas: configuration.workAreas, canonical: preferred), rows: rows)
    }
    static func values(_ a: [Commit], _ b: [Commit], repos: [Repository], scope: ActivityScope, all: [Commit]) -> ComparisonValues {
        let relevant = repos.filter { scope.repo.isEmpty || $0.id == scope.repo }
        let available = !relevant.isEmpty && relevant.allSatisfy { !$0.stale && $0.error.isEmpty && $0.scannedAt != nil }
        let paths = scope.path.isEmpty && scope.areaName.isEmpty || !all.contains { $0.repo == scope.repo && $0.pathsUnknown }
        return ComparisonValues(a: Analysis.totals(a), b: Analysis.totals(b), reliable: available && paths)
    }
    static func evidence(_ a: [Commit], _ b: [Commit], repos: [Repository], scope: ActivityScope, areas: [String: [WorkArea]], canonical: [Commit]) -> ComparisonEvidence {
        let sa = Analysis.scoped(a, scope: scope, workAreas: areas), sb = Analysis.scoped(b, scope: scope, workAreas: areas)
        let pa = Analysis.people(sa, canonicalRecords: canonical), pb = Analysis.people(sb, canonicalRecords: canonical)
        let da = Dictionary(uniqueKeysWithValues: pa.map { ($0.id, $0) }), db = Dictionary(uniqueKeysWithValues: pb.map { ($0.id, $0) })
        let totals = values(sa, sb, repos: repos, scope: scope, all: a + b)
        var people: [ComparisonPerson] = []
        for id in Set(da.keys).union(db.keys) {
            let first = da[id], second = db[id]
            let name = first?.name ?? second?.name ?? "Unknown contributor"
            let email = first?.email ?? second?.email ?? ""
            let values = ComparisonValues(a: first?.totals ?? ActivityTotals(), b: second?.totals ?? ActivityTotals(), reliable: totals.reliable)
            people.append(ComparisonPerson(id: id, name: name, email: email, values: values))
        }
        people.sort { $0.name == $1.name ? $0.id < $1.id : $0.name < $1.name }
        var warnings: [String] = []
        if !totals.reliable { warnings.append("History is stale, unavailable, or has unknown paths. Totals may omit evidence; an empty result is not a verified zero.") }
        if totals.a.unknown || totals.b.unknown { warnings.append("Some line counts are unknown. Displayed lines are measured subtotals; relative line changes are unavailable.") }
        let relevant = (a + b).filter { scope.repo.isEmpty || $0.repo == scope.repo }
        if relevant.contains(where: \.pathsUnknown) { warnings.append("Some commits have unknown paths and cannot be assigned to directories or files.") }
        let excluded = Analysis.excludedLines(relevant).values.reduce(0, +)
        if excluded > 0 { warnings.append("\(excluded) generated, vendored, or bulk-data lines excluded across the selected evidence.") }
        return ComparisonEvidence(a: sa, b: sb, people: people, values: totals, warnings: warnings)
    }
}

enum ComparisonTimeline {
    static func points(_ records: [Commit], period: HistoryPeriod, snapshot: Date, calendar: Calendar) throws -> [TimelinePoint] {
        let end = min(period.end, snapshot)
        guard period.start < end else { return [] }
        let span = end.timeIntervalSince(period.start) / 86400
        let component: Calendar.Component = span <= 30 ? .day : span <= 180 ? .weekOfYear : .month
        func bucket(_ date: Date) -> Date { calendar.dateInterval(of: component, for: date)!.start }
        var rows: [Date: TimelinePoint] = [:]
        var date = bucket(period.start)
        while date < end {
            try Task.checkCancellation()
            rows[date] = TimelinePoint(date: date)
            guard let next = calendar.date(byAdding: component, value: 1, to: date), next > date else { break }
            date = next
        }
        for c in Analysis.unique(records) where period.contains(c.date, snapshot: snapshot) {
            let key = bucket(c.date)
            var row = rows[key] ?? TimelinePoint(date: key)
            row.commits += 1; row.added += c.linesUnknown ? 0 : c.added; row.removed += c.linesUnknown ? 0 : c.removed; rows[key] = row
        }
        return rows.values.sorted { $0.date < $1.date }
    }
}
