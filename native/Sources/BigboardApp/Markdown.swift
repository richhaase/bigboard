import Foundation

@MainActor enum BoardMarkdown {
    static func render(snapshot: Snapshot, configuration: WorkspaceConfiguration, navigation: BoardState) -> String {
        if navigation.comparisonActive, let comparison = navigation.comparison { return comparisonMarkdown(snapshot: snapshot, configuration: configuration, navigation: navigation, comparison: comparison) }
        var lines = ["# Bigboard · \(navigation.board.rawValue)", "", "Snapshot: \(snapshot.generatedAt.formatted(.iso8601))", "Range: \(navigation.lens == .heat && navigation.board == .activity ? "Heat historical window" : navigation.rangeLabel)", "Repositories: \(navigation.repositories.isEmpty ? "All tracked" : navigation.repositories.sorted().joined(separator: ", "))"]
        if !navigation.currentFilters.query.isEmpty { lines.append("Search: \(navigation.currentFilters.query)") }
        if navigation.currentFilters.hideBots && navigation.board != .pullRequests { lines.append("Bots hidden") }
        if snapshot.cachedOnly { lines.append("Cached history; PR context unavailable") }
        if !snapshot.coverage.errors.isEmpty { lines.append("Evidence may be incomplete: \(snapshot.coverage.errors.count) data issues") }
        lines.append("")
        switch navigation.board {
        case .activity:
            let records = navigation.filteredCommits(snapshot, heat: navigation.lens == .heat)
            let scoped = navigation.evidenceRecords(Analysis.scoped(records, scope: navigation.scope, workAreas: configuration.workAreas), canonical: records)
            lines += ["## \(navigation.lens.rawValue)", "Scope: \(scopeName(navigation.scope))", ""]
            if let evidence = navigation.relatedEvidence { lines.append("Restricted to \(evidence.mode == "people" ? "contributors" : "commits") shared with the previous area") }
            switch navigation.lens {
            case .heat:
                lines.append("Measure: \(navigation.heatMetric). Weekly activity is relative to the area's preceding 12 weeks; change compares recent 4 weeks with the preceding 12.")
                let rows = Analysis.heatRows(records, repositories: navigation.visibleRepositories(snapshot), scope: navigation.scope, workAreas: configuration.workAreas, metric: navigation.heatMetric, now: snapshot.generatedAt).filter { matches($0.label, navigation.activity.query) }
                lines += table(["Area", "Recent / week", "Prior / week", "Change", "Total"], rows.map { [escaped($0.label), number($0.heat.recent), number($0.heat.prior), $0.heat.status == "new" ? "New" : $0.heat.ratio.map { String(format: "%.2f×", $0) } ?? "—", number($0.heat.total)] })
            case .directories:
                if navigation.scope.isFile { lines += commits(navigation.searchCommits(scoped)); break }
                lines += table(["Path", "Commits", "People", "Added", "Removed"], navigation.activityRows(snapshot, configuration: configuration).map { [escaped($0.label + ($0.kind == "directory" ? "/" : "")), "\($0.totals.commits)", "\($0.totals.people)", "\($0.totals.added)", "\($0.totals.removed)\($0.totals.unknown ? " + ?" : "")"] })
            case .commits:
                lines += commits(navigation.searchCommits(scoped))
            case .people:
                if let selected = navigation.scopedPerson { lines += commits(navigation.searchCommits(Analysis.people(scoped, canonicalRecords: records).first { $0.id == selected }?.records ?? [])) }
                else { lines += people(Analysis.people(scoped, canonicalRecords: records).filter { matches("\($0.name) \($0.email)", navigation.activity.query) }) }
            case .related:
                lines += ["Evidence: \(navigation.relatedMode == "people" ? "Contributor overlap" : "Shared commits")", "Historical overlap does not establish ownership or current presence.", ""]
                lines += table(["Area", "Evidence count"], Analysis.relatedScopes(records, scope: navigation.scope, mode: navigation.relatedMode, workAreas: configuration.workAreas).filter { matches(scopeName($0.scope), navigation.activity.query) }.map { [escaped(scopeName($0.scope)), "\($0.count)"] })
            }
        case .contributors:
            lines += people(navigation.peopleRows(snapshot))
        case .pullRequests:
            if let repo = navigation.contextualRepository { lines.append("Context: \(repo)") }
            let filters = navigation.prs
            for (name, values) in [("Authors", filters.authors), ("Review", filters.reviews), ("Checks", filters.checks), ("State", filters.states), ("Mine", filters.mine)] where !values.isEmpty { lines.append("\(name): \(values.sorted().joined(separator: ", "))") }
            lines += table(["Repository", "PR", "Title", "Author", "Review", "Checks", "Updated", "Coverage"], navigation.pullRequests(snapshot).map { [escaped($0.repo), "[#\($0.number)](\($0.url))", escaped($0.title), escaped($0.author), $0.review, $0.checks, $0.updatedAt?.formatted(.iso8601) ?? "Unknown", $0.inventoryComplete && $0.filesComplete && $0.reviewersComplete && $0.reviewsComplete ? "Complete" : "Partial"] })
        }
        lines += ["", "Activity describes change, not productivity."]
        return lines.joined(separator: "\n")
    }
    private static func comparisonMarkdown(snapshot: Snapshot, configuration: WorkspaceConfiguration, navigation: BoardState, comparison: HistoryComparison) -> String {
        var lines = ["# Bigboard · Activity comparison", "", "Snapshot: \(snapshot.generatedAt.formatted(.iso8601))", "A: \(comparison.a.label(timeZone: comparison.timeZone))", "B: \(comparison.b.label(timeZone: comparison.timeZone))", "Timezone: \(comparison.timeZoneID)", "Scope: \(scopeName(navigation.scope))", "Repositories: \(navigation.repositories.isEmpty ? "All tracked" : navigation.repositories.sorted().joined(separator: ", "))", "Differences: A − B", "Units: \(navigation.comparisonRates ? "Per elapsed 24 hours for commits and lines; contributors are distinct counts" : "Raw totals")"]
        if navigation.activity.hideBots { lines.append("Bots hidden") }
        if !navigation.activity.people.isEmpty { lines.append("Contributor filter: \(navigation.activity.people.sorted().joined(separator: ", "))") }
        if !navigation.activity.query.isEmpty { lines.append("Search: \(navigation.activity.query)") }
        lines.append("Elapsed 24-hour days: A \(number(comparison.a.duration(at: snapshot.generatedAt) / 86400)), B \(number(comparison.b.duration(at: snapshot.generatedAt) / 86400))")
        if comparison.a.partial(at: snapshot.generatedAt) || comparison.b.partial(at: snapshot.generatedAt) { lines.append("Partial period: evidence ends at snapshot time") }
        if comparison.overlaps { lines.append("Overlapping periods; each is counted independently") }
        if snapshot.cachedOnly { lines.append("Cached history; PR context unavailable") }
        do {
            let projection = try ComparisonProjection.build(snapshot: snapshot, configuration: configuration, comparison: comparison, repositories: navigation.repositories, scope: navigation.scope, hideBots: navigation.activity.hideBots, people: navigation.activity.people)
            var evidence = projection.evidence
            if let restriction = navigation.relatedEvidence {
                func restricted(_ records: [Commit]) -> [Commit] {
                    if restriction.mode == "people" { return Analysis.people(records, canonicalRecords: projection.canonical).filter { restriction.values.contains($0.id) }.flatMap(\.records) }
                    return records.filter { restriction.values.contains($0.id) }
                }
                evidence = ComparisonProjection.evidence(restricted(projection.recordsA), restricted(projection.recordsB), repos: snapshot.repositories.filter { navigation.repositories.isEmpty || navigation.repositories.contains($0.id) }, scope: navigation.scope, areas: configuration.workAreas, canonical: projection.canonical)
                lines.append("Restricted to \(restriction.mode) shared with the previous area")
            }
            lines += evidence.warnings
            func cells(_ values: ComparisonValues, measure: ComparisonMeasure) -> [String] {
                let pair = values.values(measure, comparison: comparison, snapshot: snapshot.generatedAt, rates: navigation.comparisonRates)
                func display(_ value: Double) -> String { navigation.comparisonRates && measure.permitsRate ? value.formatted(.number.precision(.significantDigits(1...4))) : value.formatted(.number.precision(.fractionLength(0))) }
                return [display(pair.0), display(pair.1), display(pair.0 - pair.1), values.relative(measure, a: pair.0, b: pair.1)]
            }
            lines.append("Selected measure: \(navigation.comparisonMeasure.rawValue)")
            lines += table(["Measure", "A", "B", "A − B", "Relative / coverage"], ComparisonMeasure.allCases.map { [$0.rawValue] + cells(evidence.values, measure: $0) })
            let records = navigation.comparisonPeriodA ? evidence.a : evidence.b
            lines += ["", "## \(navigation.lens.rawValue)", "Evidence period: \(navigation.comparisonPeriodA ? "A" : "B")"]
            switch navigation.lens {
            case .directories:
                if navigation.scope.isFile { lines += commits(navigation.searchCommits(records)); break }
                lines += table(["Area", "A", "B", "A − B", "Relative / coverage"], projection.sortedRows(order: navigation.comparisonOrder, measure: navigation.comparisonMeasure, comparison: comparison, snapshot: snapshot.generatedAt, rates: navigation.comparisonRates, query: navigation.activity.query).map { [escaped($0.label)] + cells($0.values, measure: navigation.comparisonMeasure) })
            case .commits: lines += commits(navigation.searchCommits(records))
            case .people:
                if let selected = navigation.scopedPerson { lines += commits(navigation.searchCommits(Analysis.people(records, canonicalRecords: projection.canonical).first { $0.id == selected }?.records ?? [])) }
                else { lines += table(["Contributor", "A", "B", "A − B", "Relative / coverage"], evidence.people.filter { matches("\($0.name) \($0.email)", navigation.activity.query) }.map { [escaped($0.name)] + cells($0.values, measure: navigation.comparisonMeasure) }) }
            case .related:
                let all = navigation.comparisonPeriodA ? projection.recordsA : projection.recordsB
                lines += ["Related by \(navigation.relatedMode); overlap does not establish ownership."]
                lines += table(["Area", "Evidence count"], Analysis.relatedScopes(all, scope: navigation.scope, mode: navigation.relatedMode, workAreas: configuration.workAreas).filter { matches(scopeName($0.scope), navigation.activity.query) }.map { [escaped(scopeName($0.scope)), "\($0.count)"] })
            case .heat: break
            }
        } catch { lines.append("Comparison unavailable: \(error.localizedDescription)") }
        lines += ["", "Activity describes change, not productivity. Historical PR statistics are not included."]
        return lines.joined(separator: "\n")
    }
    private static func table(_ headings: [String], _ rows: [[String]]) -> [String] { ["", "| " + headings.joined(separator: " | ") + " |", "| " + headings.map { _ in "---" }.joined(separator: " | ") + " |"] + rows.map { "| " + $0.joined(separator: " | ") + " |" } }
    private static func escaped(_ value: String) -> String { value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "|", with: "\\|").replacingOccurrences(of: "\n", with: " ").replacingOccurrences(of: "\r", with: " ") }
    private static func matches(_ value: String, _ query: String) -> Bool { query.isEmpty || value.localizedCaseInsensitiveContains(query) }
    private static func number(_ value: Double) -> String { value.formatted(.number.precision(.fractionLength(0...1))) }
    private static func scopeName(_ scope: ActivityScope) -> String { scope.repo.isEmpty ? "All repositories" : scope.repo + (scope.path.isEmpty && scope.areaName.isEmpty ? "" : " / " + (scope.areaName.isEmpty ? scope.path + (scope.isFile ? "" : "/") : scope.areaName)) }
    private static func people(_ rows: [PersonRow]) -> [String] { table(["Contributor", "Commits", "Added", "Removed", "AI share"], rows.map { [escaped($0.name), "\($0.totals.commits)", "\($0.totals.added)", "\($0.totals.removed)\($0.totals.unknown ? " + ?" : "")", "\(Int($0.totals.aiPercent))%"] }) }
    private static func commits(_ records: [Commit]) -> [String] { table(["Date", "Commit", "Subject", "Author", "Added", "Removed"], Analysis.unique(records).sorted { $0.date > $1.date }.map { [$0.date.formatted(.iso8601), "[\($0.id.prefix(7))](https://github.com/\($0.repo)/commit/\($0.id))", escaped($0.subject), escaped($0.author), "\($0.added)", "\($0.removed)\($0.linesUnknown ? " + ?" : "")"] }) }
}
