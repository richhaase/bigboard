import SwiftUI
import Observation

enum Board: String, CaseIterable, Identifiable {
    case heat = "Heat Maps"
    case activity = "Activity"
    case contributors = "Contributors"
    case pullRequests = "Pull requests"
    var id: String { rawValue }
    var symbol: String {
        switch self {
        case .heat: "square.grid.3x3.fill"
        case .activity: "chart.bar.xaxis"
        case .contributors: "person.2"
        case .pullRequests: "arrow.triangle.pull"
        }
    }
}
enum ActivityLens: String, CaseIterable, Identifiable {
    case directories = "Directories"
    case commits = "Commits"
    case people = "People"
    case related = "Related"
    var id: String { rawValue }
}
enum Panel: String, Identifiable {
    case settings, groups, dateRange, status, help, identities
    var id: String { rawValue }
    var isSettings: Bool { self == .settings || self == .groups }
}
struct RelatedEvidence: Equatable {
    var mode: String
    var values: Set<String>
}
struct BoardFilters: Equatable {
    var query = ""
    var sort = "change"
    var reversed = false
    var hideBots = false
    var people: Set<String> = []
    var authors: Set<String> = []
    var reviews: Set<String> = []
    var checks: Set<String> = []
    var states: Set<String> = []
    var mine: Set<String> = []
}
struct RepositoryChecklist {
    var selection: Set<String>
    var available: Set<String>
    var checked: Set<String> { selection.isEmpty ? available : selection.intersection(available) }
    func setting(_ id: String, checked isChecked: Bool) -> Set<String>? {
        guard available.contains(id) else { return nil }
        var updated = checked
        if isChecked { updated.insert(id) } else { updated.remove(id) }
        guard !updated.isEmpty else { return nil }
        return updated == available ? [] : updated
    }
}
enum DateRange: Hashable {
    case hours(Int)
    case days(Int)

    static let presets: [DateRange] = [
        .hours(1), .hours(4), .hours(12), .days(1),
        .days(7), .days(14), .days(30), .days(90), .days(365), .days(0),
    ]

    var label: String {
        switch self {
        case .hours(1): "Last hour"
        case .hours(let count): "Last \(count) hours"
        case .days(0): "All time"
        case .days(1): "Last 24 hours"
        case .days(let count): "Last \(count) days"
        }
    }
    func start(relativeTo date: Date) -> Date? {
        switch self {
        case .hours(let count): date.addingTimeInterval(-Double(count) * 3600)
        case .days(0): nil
        case .days(let count): date.addingTimeInterval(-Double(count) * 86400)
        }
    }
}
@MainActor @Observable final class BoardState {
    var board: Board = .heat
    var dateRange = DateRange.days(14) { didSet { if dateRange != oldValue { activityInterval = nil } } }
    var repositories: Set<String> = []
    var heat = BoardFilters()
    var heatScope = ActivityScope()
    var activityInterval: DateInterval?
    var activityIncludesStart = false
    var activity = BoardFilters()
    var activityPeopleQuery = ""
    var contributors = BoardFilters(sort: "recent")
    var prs = BoardFilters(sort: "updated")
    var scope = ActivityScope()
    var lens: ActivityLens = .directories
    var heatMetric = "lines"
    var heatExpanded = false
    var relatedMode = "commits"
    var relatedEvidence: RelatedEvidence?
    var activitySelection: String?
    var contributorSelection: String?
    var prSelection: String?
    var scopedPerson: String?
    var contextualRepository: String?
    var savedPRFilters: BoardFilters?
    var repositoryFilterPresented = false {
        didSet { if repositoryFilterPresented { panel = nil } }
    }
    var panel: Panel? { didSet { if panel != nil { repositoryFilterPresented = false } } }
    var preferencesApplied = false
    var rangeLabel: String {
        dateRange.label
    }
    var searchPrompt: String? {
        switch board {
        case .heat: nil
        case .activity:
            switch lens {
            case .commits: "Search commits"
            case .people: scopedPerson == nil ? "Find a contributor" : "Search commits"
            case .directories, .related: nil
            }
        case .contributors: "Find a contributor"
        case .pullRequests: "Search pull requests"
        }
    }
    var searchQuery: String {
        get {
            board == .activity && lens == .people && scopedPerson == nil
                ? activityPeopleQuery : currentFilters.query
        }
        set {
            if board == .activity && lens == .people && scopedPerson == nil {
                activityPeopleQuery = newValue
            } else {
                currentFilters.query = newValue
            }
        }
    }
    var currentFilters: BoardFilters {
        get {
            switch board {
            case .heat: heat
            case .activity: activity
            case .contributors: contributors
            case .pullRequests: prs
            }
        }
        set {
            switch board {
            case .heat: heat = newValue
            case .activity: activity = newValue
            case .contributors: contributors = newValue
            case .pullRequests: prs = newValue
            }
        }
    }
    var repositoryFilterLabel: String {
        repositories.isEmpty ? "All" : "\(repositories.count) selected"
    }
    @discardableResult func setRepositoryFilter(_ ids: Set<String>, available: [Repository]) -> Bool {
        let selected = ids.intersection(available.map(\.id))
        guard ids.isEmpty || !selected.isEmpty else { return false }
        repositories = selected
        reconcile()
        return true
    }
    func visibleRepositories(_ snapshot: Snapshot) -> [Repository] {
        snapshot.repositories.filter { repositories.isEmpty || repositories.contains($0.id) }
    }
    var activityRangeLabel: String {
        activityInterval.map { HeatRange.label($0) } ?? rangeLabel
    }
    func heatRange(_ snapshot: Snapshot, calendar: Calendar = .current) -> HeatRange {
        HeatRange(
            days: 0, since: dateRange.start(relativeTo: snapshot.generatedAt),
            now: snapshot.generatedAt,
            records: snapshot.commits.filter {
                repositories.isEmpty || repositories.contains($0.repo)
            }, calendar: calendar)
    }
    func heatRecords(_ snapshot: Snapshot) -> [Commit] {
        Analysis.select(
            snapshot.commits, repos: repositories, hideBots: heat.hideBots,
            people: heat.people, now: snapshot.generatedAt)
    }
    func filteredCommits(_ snapshot: Snapshot, for board: Board? = nil) -> [Commit] {
        let selectedBoard = board ?? self.board
        let filters = selectedBoard == .heat ? heat : selectedBoard == .contributors ? contributors : activity
        let interval = selectedBoard == .activity ? activityInterval : nil
        return Analysis.select(
            snapshot.commits,
            since: interval == nil ? dateRange.start(relativeTo: snapshot.generatedAt) : nil,
            repos: repositories, hideBots: filters.hideBots, people: filters.people,
            now: interval?.end ?? snapshot.generatedAt
        ).filter { commit in
            guard let interval else { return true }
            return
                (commit.date > interval.start || (activityIncludesStart && commit.date == interval.start))
                && commit.date <= interval.end
        }
    }
    func timelinePoints(_ records: [Commit], snapshot: Snapshot) -> [TimelinePoint] {
        let interval = board == .activity ? activityInterval : nil
        return Analysis.timeline(
            records, days: 0, since: interval?.start ?? dateRange.start(relativeTo: snapshot.generatedAt),
            now: interval?.end ?? snapshot.generatedAt,
            includesStart: interval != nil && activityIncludesStart)
    }
    func exploreHeat(_ scope: ActivityScope) {
        heatScope = scope
        heatExpanded = false
    }
    func backInHeat() {
        if !heatScope.path.isEmpty {
            heatScope.path = heatScope.path.split(separator: "/").dropLast().joined(separator: "/")
        } else if !heatScope.areaName.isEmpty {
            heatScope.areaName = ""
        } else {
            heatScope = ActivityScope()
        }
        heatExpanded = false
    }
    func showHeatActivity(
        _ scope: ActivityScope, interval: DateInterval?,
        includesStart: Bool = false
    ) {
        explore(scope)
        lens = scope.isFile ? .commits : .directories
        activity.query = ""
        activityPeopleQuery = ""
        activity.hideBots = heat.hideBots
        activity.people = heat.people
        activityInterval = interval
        activityIncludesStart = includesStart
        switchBoard(.activity)
    }
    func searchCommits(_ records: [Commit]) -> [Commit] {
        records.filter {
            activity.query.isEmpty
                || "\($0.subject) \($0.author) \($0.email)".localizedCaseInsensitiveContains(activity.query)
        }
    }
    func activityRows(_ snapshot: Snapshot, configuration: WorkspaceConfiguration) -> [ActivityRow] {
        let rows =
            Analysis.activityRows(
                filteredCommits(snapshot, for: .activity), repositories: visibleRepositories(snapshot), scope: scope,
                workAreas: configuration.workAreas
            )
        func compare(_ a: ActivityRow, _ b: ActivityRow) -> Bool {
            switch activity.sort {
            case "commits":
                if a.totals.commits != b.totals.commits { return a.totals.commits > b.totals.commits }
            case "recent":
                if a.latest != b.latest { return (a.latest ?? .distantPast) > (b.latest ?? .distantPast) }
            case "name": break
            default: if a.totals.change != b.totals.change { return a.totals.change > b.totals.change }
            }
            return a.label != b.label ? a.label < b.label : a.id < b.id
        }
        return rows.sorted { activity.reversed ? compare($1, $0) : compare($0, $1) }
    }
    func peopleRows(_ snapshot: Snapshot) -> [PersonRow] {
        let rows = Analysis.people(filteredCommits(snapshot, for: .contributors))
            .filter {
                contributors.query.isEmpty
                    || "\($0.name) \($0.email)".localizedCaseInsensitiveContains(contributors.query)
            }
        func compare(_ a: PersonRow, _ b: PersonRow) -> Bool {
            switch contributors.sort {
            case "name": break
            case "commits":
                if a.totals.commits != b.totals.commits { return a.totals.commits > b.totals.commits }
            case "added": if a.totals.added != b.totals.added { return a.totals.added > b.totals.added }
            case "removed":
                if a.totals.removed != b.totals.removed { return a.totals.removed > b.totals.removed }
            case "net": if a.totals.net != b.totals.net { return a.totals.net > b.totals.net }
            case "ai":
                if a.totals.aiPercent != b.totals.aiPercent { return a.totals.aiPercent > b.totals.aiPercent }
            case "recent":
                if a.latest != b.latest { return (a.latest ?? .distantPast) > (b.latest ?? .distantPast) }
            default: if a.totals.change != b.totals.change { return a.totals.change > b.totals.change }
            }
            return a.name != b.name ? a.name < b.name : a.id < b.id
        }
        return rows.sorted { contributors.reversed ? compare($1, $0) : compare($0, $1) }
    }

    func evidenceRecords(_ records: [Commit], canonical: [Commit]) -> [Commit] {
        guard let evidence = relatedEvidence else { return records }
        if evidence.mode == "people" {
            return Analysis.people(records, canonicalRecords: canonical)
                .filter { evidence.values.contains($0.id) }.flatMap(\.records)
        }
        return records.filter { evidence.values.contains($0.id) }
    }
    func setLens(_ value: ActivityLens) {
        lens = value
        relatedEvidence = nil
        scopedPerson = nil
    }
    func exploreRelated(_ row: RelatedRow) {
        let mode = relatedMode
        explore(row.scope)
        lens = mode == "people" ? .people : .commits
        relatedEvidence = RelatedEvidence(mode: mode, values: Set(row.evidence))
    }
    func explore(_ scope: ActivityScope) {
        self.scope = scope
        relatedEvidence = nil
        activitySelection = nil
        scopedPerson = nil
        if scope.isFile { lens = .commits }
    }
    var canGoBackInActivity: Bool { !scope.repo.isEmpty || lens != .directories }
    func back() {
        if contextualRepository != nil {
            returnToActivity()
            return
        }
        if scopedPerson != nil {
            scopedPerson = nil
            return
        }
        relatedEvidence = nil
        if scope.isFile {
            scope.path = scope.path.split(separator: "/").dropLast().joined(separator: "/")
            scope.isFile = false
            lens = .directories
        } else if lens != .directories {
            lens = .directories
            return
        } else if !scope.path.isEmpty {
            scope.path = scope.path.split(separator: "/").dropLast().joined(separator: "/")
            scope.isFile = false
        } else if !scope.areaName.isEmpty {
            scope.areaName = ""
        } else {
            scope = ActivityScope()
        }
        activitySelection = nil
    }
    func showPRs(repo: String, selected: String? = nil) {
        if contextualRepository == nil { savedPRFilters = prs }
        prs = BoardFilters(sort: "updated")
        contextualRepository = repo
        prSelection = selected
        board = .pullRequests
    }
    func returnToActivity() {
        restorePRFilters()
        board = .activity
    }
    func restorePRFilters() {
        if let savedPRFilters { prs = savedPRFilters }
        savedPRFilters = nil
        contextualRepository = nil
    }
    func switchBoard(_ value: Board) {
        repositoryFilterPresented = false
        restorePRFilters()
        board = value
    }
    func togglePRMine(_ value: String) {
        guard board == .pullRequests else { return }
        prs.mine = prs.mine == [value] ? [] : [value]
    }
    func resetBoard() {
        let old = currentFilters
        currentFilters = BoardFilters(sort: old.sort, reversed: old.reversed)
    }
    func resetForAccount(_ configuration: WorkspaceConfiguration) {
        repositoryFilterPresented = false
        dateRange = .days(configuration.sinceDays)
        repositories = []
        heat = BoardFilters()
        heatScope = ActivityScope()
        heatMetric = "lines"
        heatExpanded = false
        activityInterval = nil
        activityIncludesStart = false
        activity = BoardFilters()
        activityPeopleQuery = ""
        contributors = BoardFilters(sort: configuration.contributorSort)
        prs = BoardFilters(sort: "updated")
        scope = ActivityScope()
        lens = .directories
        activitySelection = nil
        contributorSelection = nil
        prSelection = nil
        scopedPerson = nil
        relatedEvidence = nil
        contextualRepository = nil
        savedPRFilters = nil
        preferencesApplied = true
    }
    func reconcile(_ snapshot: Snapshot) {
        let available = Set(snapshot.repositories.map(\.id))
        repositories.formIntersection(available)
        if !heatScope.repo.isEmpty && !available.contains(heatScope.repo) {
            exploreHeat(ActivityScope())
        }
        if !scope.repo.isEmpty && !available.contains(scope.repo) {
            scope = ActivityScope()
            activityInterval = nil
            activitySelection = nil
            scopedPerson = nil
            relatedEvidence = nil
            lens = .directories
        }
        if let contextualRepository, !available.contains(contextualRepository) { restorePRFilters() }
        reconcile()
    }
    func reconcile() {
        if !heatScope.repo.isEmpty && !repositories.isEmpty && !repositories.contains(heatScope.repo) {
            exploreHeat(ActivityScope())
        }
        if !scope.repo.isEmpty && !repositories.isEmpty && !repositories.contains(scope.repo) {
            scope = ActivityScope()
            activityInterval = nil
            activitySelection = nil
            scopedPerson = nil
            relatedEvidence = nil
            lens = .directories
        }
        if let contextualRepository,
            !repositories.isEmpty && !repositories.contains(contextualRepository)
        {
            restorePRFilters()
        }
    }

    func pullRequests(_ snapshot: Snapshot) -> [PullRequest] {
        let filters = PRFilters(
            query: prs.query, repos: repositories,
            authors: prs.authors, reviews: prs.reviews, checks: prs.checks, states: prs.states,
            mine: prs.mine, sort: prs.sort, reverse: prs.reversed)
        return
            Analysis.filterPRs(
                snapshot.pullRequests, viewer: snapshot.viewer, filters: filters, now: snapshot.generatedAt
            )
            .filter { contextualRepository == nil || $0.repo == contextualRepository }
    }
}
