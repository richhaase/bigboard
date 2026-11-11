import Foundation
import AppKit

@MainActor enum BoardMarkdown {
    static func copy(
        snapshot: Snapshot, configuration: WorkspaceConfiguration, navigation: BoardState
    ) {
        let text = render(snapshot: snapshot, configuration: configuration, navigation: navigation)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
    static func render(
        snapshot: Snapshot, configuration: WorkspaceConfiguration, navigation: BoardState
    )
        -> String
    {
        var lines = [
            "# Bigboard · \(navigation.board.rawValue)", "",
            "Snapshot: \(snapshot.generatedAt.formatted(.iso8601))",
            "Range: \(navigation.board == .pullRequests ? "Current open work" : navigation.rangeLabel)",
            "Repositories: \(navigation.repositories.isEmpty ? "All tracked" : navigation.repositories.sorted().joined(separator: ", "))",
        ]
        if navigation.searchPrompt != nil && !navigation.searchQuery.isEmpty {
            lines.append("Search: \(navigation.searchQuery)")
        }
        let viewingPRTrends = navigation.board == .activity && navigation.lens == .prTrends
        if navigation.currentFilters.hideBots && navigation.board != .pullRequests && !viewingPRTrends {
            lines.append("Bots hidden")
        }
        if snapshot.cachedOnly { lines.append("Cached history; PR context unavailable") }
        if !snapshot.coverage.errors.isEmpty {
            lines.append("Evidence may be incomplete: \(snapshot.coverage.errors.count) data issues")
        }
        lines.append("")
        switch navigation.board {
        case .heat:
            let range = navigation.heatRange(snapshot)
            lines += [
                "## Activity over time", "Measure: \(navigation.heatMetric)",
                "Scope: \(scopeName(navigation.heatScope))",
                "Selected: \(HeatRange.label(range.current))",
                "\(range.unitLabel) activity. Totals cover the selected range.",
            ]
            let rows = Analysis.heatRows(
                navigation.heatRecords(snapshot), repositories: navigation.visibleRepositories(snapshot),
                scope: navigation.heatScope, workAreas: configuration.workAreas,
                metric: navigation.heatMetric, range: range
            )
            lines += table(
                ["Area", "Total", "Coverage"],
                rows.map {
                    [
                        escaped($0.label), $0.heat.total.formatted(),
                        $0.heat.incomplete ? "Incomplete or stale; known activity only" : "Loaded history",
                    ]
                })
        case .activity:
            let records = navigation.filteredCommits(snapshot, for: .activity)
            let scoped = navigation.evidenceRecords(
                Analysis.scoped(records, scope: navigation.scope, workAreas: configuration.workAreas),
                canonical: records)
            let mergedPRs = navigation.activityMergedPRs(snapshot)
            let coverage = mergedPRs.coverage(in: navigation.scope)
            let mergedPRCount = mergedPRs.count(in: navigation.scope, workAreas: configuration.workAreas)
            lines += ["## \(navigation.lens.rawValue)", "Scope: \(scopeName(navigation.scope))", ""]
            lines.append("Merged PRs: \(coverage.label(mergedPRCount))")
            if let note = coverage.note { lines.append(note) }
            if !viewingPRTrends, let note = navigation.mergedPRScopeNote { lines.append(note) }
            if !viewingPRTrends, let evidence = navigation.relatedEvidence {
                lines.append(
                    "Restricted to \(evidence.mode == "people" ? "contributors" : "commits") shared with the previous area"
                )
            }
            switch navigation.lens {
            case .directories:
                if navigation.scope.isFile {
                    lines += commits(navigation.searchCommits(scoped))
                    break
                }
                lines += table(
                    ["Path", "Commits", "People", "Added", "Removed", "Merged PRs"],
                    navigation.activityRows(snapshot, configuration: configuration)
                        .map {
                            [
                                escaped($0.label + ($0.kind == "directory" ? "/" : "")),
                                "\($0.totals.commits)", "\($0.totals.people)", "\($0.totals.added)",
                                "\($0.totals.removed)\($0.totals.unknown ? " + ?" : "")",
                                mergedPRs.coverage(in: $0.scope).label($0.mergedPRCount),
                            ]
                        })
            case .commits:
                lines += commits(navigation.searchCommits(scoped))
            case .people:
                if let selected = navigation.scopedPerson {
                    lines += commits(
                        navigation.searchCommits(
                            Analysis.people(scoped, canonicalRecords: records).first { $0.id == selected }?
                                .records ?? []))
                } else {
                    lines += people(
                        Analysis.people(scoped, canonicalRecords: records)
                            .filter { matches("\($0.name) \($0.email)", navigation.activityPeopleQuery) })
                }
            case .related:
                lines += [
                    "Evidence: \(navigation.relatedMode == "people" ? "Contributor overlap" : "Shared commits")",
                    "Historical overlap does not establish ownership or current presence.", "",
                ]
                lines += table(
                    ["Area", "Evidence count"],
                    Analysis.related(
                        records, scope: navigation.scope, mode: navigation.relatedMode,
                        workAreas: configuration.workAreas
                    )
                    .map { [escaped(scopeName($0.scope)), "\($0.count)"] })
            case .prTrends:
                lines += [
                    "PR size is additions plus deletions. Net growth is additions minus deletions.",
                    "Size statistics and line totals use PRs with available line counts across the whole PR.",
                ]
                let trend = navigation.activityPRTrends(snapshot, configuration: configuration)
                lines += table(
                    ["Week of", "Merged PRs", "PRs with line counts", "Median size", "P90 size", "Additions", "Deletions", "Net growth"],
                    trend.weeks.map { week in
                        let size = week.size
                        let date = size.weekStart.formatted(date: .abbreviated, time: .omitted)
                        let net = size.net.formatted(.number.sign(strategy: .always(includingZero: false)))
                        return [
                            date + (size.isPartial ? " (partial)" : ""),
                            week.hasMergeCount ? week.coverage.label(size.mergedCount) : "—",
                            week.hasMergeCount ? size.sizedCount.formatted() : "—",
                            size.medianChangedLines?.formatted(.number.precision(.fractionLength(0...1))) ?? "—",
                            size.p90ChangedLines?.formatted() ?? "—",
                            week.hasLineTotals ? size.additions.formatted() : "—",
                            week.hasLineTotals ? size.deletions.formatted() : "—",
                            week.hasLineTotals ? net : "—",
                        ]
                    })
            }
        case .contributors:
            lines += people(navigation.peopleRows(snapshot))
        case .pullRequests:
            if let repo = navigation.contextualRepository { lines.append("Context: \(repo)") }
            let filters = navigation.prs
            for (name, values) in [
                ("Authors", filters.authors), ("Review", filters.reviews), ("Checks", filters.checks),
                ("State", filters.states), ("Mine", filters.mine),
            ] where !values.isEmpty { lines.append("\(name): \(values.sorted().joined(separator: ", "))") }
            lines += table(
                ["Repository", "PR", "Title", "Author", "Review", "Checks", "Updated", "Coverage"],
                navigation.pullRequests(snapshot)
                    .map {
                        [
                            escaped($0.repo), "[#\($0.number)](\($0.url))", escaped($0.title),
                            escaped($0.author), $0.review, $0.checks,
                            $0.updatedAt?.formatted(.iso8601) ?? "Unknown",
                            $0.inventoryComplete && $0.filesComplete && $0.reviewersComplete
                                && $0.reviewsComplete ? "Complete" : "Partial",
                        ]
                    })
        }
        lines += ["", "Activity describes change, not productivity."]
        return lines.joined(separator: "\n")
    }
    private static func table(_ headings: [String], _ rows: [[String]]) -> [String] {
        [
            "", "| " + headings.joined(separator: " | ") + " |",
            "| " + headings.map { _ in "---" }.joined(separator: " | ") + " |",
        ] + rows.map { "| " + $0.joined(separator: " | ") + " |" }
    }
    private static func escaped(_ value: String) -> String {
        value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "|", with: "\\|")
            .replacingOccurrences(of: "\n", with: " ").replacingOccurrences(of: "\r", with: " ")
    }
    private static func matches(_ value: String, _ query: String) -> Bool {
        query.isEmpty || value.localizedCaseInsensitiveContains(query)
    }
    private static func scopeName(_ scope: ActivityScope) -> String {
        scope.repo.isEmpty
            ? "All repositories"
            : scope.repo
                + (scope.path.isEmpty && scope.areaName.isEmpty
                    ? ""
                    : " / "
                        + (scope.areaName.isEmpty ? scope.path + (scope.isFile ? "" : "/") : scope.areaName))
    }
    private static func people(_ rows: [PersonRow]) -> [String] {
        table(
            ["Contributor", "Commits", "Added", "Removed", "AI share"],
            rows.map {
                [
                    escaped($0.name), "\($0.totals.commits)", "\($0.totals.added)",
                    "\($0.totals.removed)\($0.totals.unknown ? " + ?" : "")", "\(Int($0.totals.aiPercent))%",
                ]
            })
    }
    private static func commits(_ records: [Commit]) -> [String] {
        table(
            ["Date", "Commit", "Subject", "Author", "Added", "Removed"],
            Analysis.unique(records).sorted { $0.date > $1.date }
                .map {
                    [
                        $0.date.formatted(.iso8601),
                        "[\($0.id.prefix(7))](https://github.com/\($0.repo)/commit/\($0.id))",
                        escaped($0.subject), escaped($0.author), "\($0.added)",
                        "\($0.removed)\($0.linesUnknown ? " + ?" : "")",
                    ]
                })
    }
}
