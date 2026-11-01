import SwiftUI
import Observation

enum Board: String, CaseIterable, Identifiable {
    case activity = "Activity", contributors = "Contributors", pullRequests = "Pull requests"
    var id: String { rawValue }
    var symbol: String { switch self { case .activity: "chart.bar.xaxis"; case .contributors: "person.2"; case .pullRequests: "arrow.triangle.pull" } }
}
enum ActivityLens: String, CaseIterable, Identifiable {
    case heat = "Heat", directories = "Directories", commits = "Commits", people = "People", related = "Related"
    var id: String { rawValue }
}
enum ToolbarPopover: Equatable { case date, repositories, groups }
enum Panel: String, Identifiable { case settings, filters, status, help, identities, repositories; var id: String { rawValue } }
struct RelatedEvidence: Equatable { var mode: String; var values: Set<String> }
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
        if isChecked { updated.insert(id) }
        else { updated.remove(id) }
        guard !updated.isEmpty else { return nil }
        return updated == available ? [] : updated
    }
}
@MainActor @Observable final class BoardState {
    var board: Board = .activity
    var days = 14
    var sinceLastLook = false
    var repositories: Set<String> = []
    var activity = BoardFilters()
    var contributors = BoardFilters()
    var prs = BoardFilters(sort: "updated")
    var scope = ActivityScope()
    var lens: ActivityLens = .heat
    var heatMetric = "lines"
    var heatExpanded = false
    var relatedMode = "commits"
    var relatedEvidence: RelatedEvidence?
    var activitySelection: String?
    var contributorSelection: String?
    var prSelection: String?
    var scopedPerson: String?
    var commitLimit = 100
    var contextualRepository: String?
    var savedPRFilters: BoardFilters?
    var toolbarPopover: ToolbarPopover?
    var repositoryPopoverPresented: Bool {
        get { toolbarPopover == .repositories }
        set { setToolbarPopover(.repositories, presented: newValue) }
    }
    var panel: Panel? { didSet { if panel != nil { toolbarPopover = nil } } }
    var preferencesApplied = false
    var rangeLabel: String { sinceLastLook ? "Since last look" : days == 0 ? "All time" : "Last \(days) days" }
    var currentFilters: BoardFilters {
        get { switch board { case .activity: activity; case .contributors: contributors; case .pullRequests: prs } }
        set { switch board { case .activity: activity = newValue; case .contributors: contributors = newValue; case .pullRequests: prs = newValue } }
    }
    func dateMatches(_ date: Date?, snapshot: Snapshot) -> Bool {
        if sinceLastLook, let bookmark = snapshot.bookmark { return date.map { $0 > bookmark } ?? false }
        guard days > 0 else { return true }
        return date.map { $0 > snapshot.generatedAt.addingTimeInterval(-Double(days) * 86400) } ?? false
    }
    func toggleToolbarPopover(_ value: ToolbarPopover) { panel = nil; toolbarPopover = toolbarPopover == value ? nil : value }
    func setToolbarPopover(_ value: ToolbarPopover, presented: Bool) {
        if presented { panel = nil; toolbarPopover = value }
        else if toolbarPopover == value { toolbarPopover = nil }
    }
    func dismissToolbarPopover() { toolbarPopover = nil }
    func toggleRepositoryPopover() { toggleToolbarPopover(.repositories) }
    func dismissRepositoryPopover() { setToolbarPopover(.repositories, presented: false) }
    func handlePopoverShortcut(_ key: String) -> Bool {
        guard toolbarPopover != nil else { return false }
        if key == "\u{1b}" { dismissToolbarPopover() }
        return true
    }
    var repositoryFilterLabel: String { repositories.isEmpty ? "All" : "\(repositories.count) selected" }
    @discardableResult func setRepositoryFilter(_ ids: Set<String>, available: [Repository]) -> Bool {
        let selected = ids.intersection(available.map(\.id))
        guard ids.isEmpty || !selected.isEmpty else { return false }
        repositories = selected
        reconcile()
        return true
    }
    func visibleRepositories(_ snapshot: Snapshot) -> [Repository] { snapshot.repositories.filter { repositories.isEmpty || repositories.contains($0.id) } }
    func filteredCommits(_ snapshot: Snapshot, heat: Bool = false, for board: Board? = nil) -> [Commit] {
        let filters = (board ?? self.board) == .contributors ? contributors : activity
        return Analysis.select(snapshot.commits, days: heat ? 0 : days, since: !heat && sinceLastLook ? snapshot.bookmark : nil, repos: repositories, hideBots: filters.hideBots, people: filters.people, now: snapshot.generatedAt)
    }
    func timelinePoints(_ records: [Commit], snapshot: Snapshot, heat: Bool = false) -> [TimelinePoint] { Analysis.timeline(records, days: heat ? 0 : days, since: !heat && sinceLastLook ? snapshot.bookmark : nil, now: snapshot.generatedAt) }
    func searchCommits(_ records: [Commit]) -> [Commit] { records.filter { activity.query.isEmpty || "\($0.subject) \($0.author) \($0.email)".localizedCaseInsensitiveContains(activity.query) } }
    func activityRows(_ snapshot: Snapshot, configuration: WorkspaceConfiguration) -> [ActivityRow] {
        let rows = Analysis.activityRows(filteredCommits(snapshot), repositories: visibleRepositories(snapshot), scope: scope, workAreas: configuration.workAreas).filter { activity.query.isEmpty || $0.label.localizedCaseInsensitiveContains(activity.query) }
        func compare(_ a: ActivityRow, _ b: ActivityRow) -> Bool {
            switch activity.sort {
            case "commits": if a.totals.commits != b.totals.commits { return a.totals.commits > b.totals.commits }
            case "recent": if a.latest != b.latest { return (a.latest ?? .distantPast) > (b.latest ?? .distantPast) }
            case "name": break
            default: if a.totals.change != b.totals.change { return a.totals.change > b.totals.change }
            }
            return a.label != b.label ? a.label < b.label : a.id < b.id
        }
        return rows.sorted { activity.reversed ? compare($1, $0) : compare($0, $1) }
    }
    func peopleRows(_ snapshot: Snapshot) -> [PersonRow] {
        let rows = Analysis.people(filteredCommits(snapshot, for: .contributors)).filter { contributors.query.isEmpty || "\($0.name) \($0.email)".localizedCaseInsensitiveContains(contributors.query) }
        func compare(_ a: PersonRow, _ b: PersonRow) -> Bool {
            switch contributors.sort {
            case "name": break
            case "commits": if a.totals.commits != b.totals.commits { return a.totals.commits > b.totals.commits }
            case "added": if a.totals.added != b.totals.added { return a.totals.added > b.totals.added }
            case "removed": if a.totals.removed != b.totals.removed { return a.totals.removed > b.totals.removed }
            case "net": if a.totals.net != b.totals.net { return a.totals.net > b.totals.net }
            case "ai": if a.totals.aiPercent != b.totals.aiPercent { return a.totals.aiPercent > b.totals.aiPercent }
            case "recent": if a.latest != b.latest { return (a.latest ?? .distantPast) > (b.latest ?? .distantPast) }
            default: if a.totals.change != b.totals.change { return a.totals.change > b.totals.change }
            }
            return a.name != b.name ? a.name < b.name : a.id < b.id
        }
        return rows.sorted { contributors.reversed ? compare($1, $0) : compare($0, $1) }
    }

    func evidenceRecords(_ records: [Commit], canonical: [Commit]) -> [Commit] {
        guard let evidence = relatedEvidence else { return records }
        if evidence.mode == "people" { return Analysis.people(records, canonicalRecords: canonical).filter { evidence.values.contains($0.id) }.flatMap(\.records) }
        return records.filter { evidence.values.contains($0.id) }
    }
    func setLens(_ value: ActivityLens) { lens = value; relatedEvidence = nil; scopedPerson = nil }
    func exploreRelated(_ row: RelatedRow) {
        let mode = relatedMode
        explore(row.scope)
        lens = mode == "people" ? .people : .commits
        relatedEvidence = RelatedEvidence(mode: mode, values: Set(row.evidence))
    }
    func explore(_ scope: ActivityScope) { self.scope = scope; relatedEvidence = nil; activitySelection = nil; scopedPerson = nil; commitLimit = 100; heatExpanded = false; if scope.isFile { lens = .commits } }
    func back() {
        if contextualRepository != nil { returnToActivity(); return }
        if scopedPerson != nil { scopedPerson = nil; return }
        relatedEvidence = nil
        if scope.isFile {
            scope.path = scope.path.split(separator: "/").dropLast().joined(separator: "/"); scope.isFile = false; lens = .directories
        } else if lens != .heat && lens != .directories { lens = .directories; return }
        else if !scope.path.isEmpty { scope.path = scope.path.split(separator: "/").dropLast().joined(separator: "/"); scope.isFile = false }
        else if !scope.areaName.isEmpty { scope.areaName = "" }
        else { scope = ActivityScope() }
        activitySelection = nil; heatExpanded = false
    }
    func showPRs(repo: String, selected: String? = nil) {
        if contextualRepository == nil { savedPRFilters = prs }
        prs = BoardFilters(sort: "updated"); contextualRepository = repo; prSelection = selected; board = .pullRequests
    }
    func returnToActivity() { restorePRFilters(); board = .activity }
    func restorePRFilters() { if let savedPRFilters { prs = savedPRFilters }; savedPRFilters = nil; contextualRepository = nil }
    func switchBoard(_ value: Board) { dismissToolbarPopover(); restorePRFilters(); board = value }
    func togglePRMine(_ value: String) {
        guard board == .pullRequests else { return }
        prs.mine = prs.mine == [value] ? [] : [value]
    }
    func resetBoard() { let old = currentFilters; currentFilters = BoardFilters(sort: old.sort, reversed: old.reversed) }
    func resetForAccount(_ configuration: WorkspaceConfiguration) {
        dismissToolbarPopover()
        days = configuration.sinceDays; sinceLastLook = false; repositories = []
        activity = BoardFilters(); contributors = BoardFilters(sort: configuration.contributorSort); prs = BoardFilters(sort: "updated")
        scope = ActivityScope(); lens = .heat; activitySelection = nil; contributorSelection = nil; prSelection = nil
        scopedPerson = nil; relatedEvidence = nil; contextualRepository = nil; savedPRFilters = nil; preferencesApplied = true
    }
    func reconcile(_ snapshot: Snapshot) {
        let available = Set(snapshot.repositories.map(\.id))
        repositories.formIntersection(available)
        if !scope.repo.isEmpty && !available.contains(scope.repo) { scope = ActivityScope(); activitySelection = nil; scopedPerson = nil; relatedEvidence = nil; lens = .heat }
        if let contextualRepository, !available.contains(contextualRepository) { restorePRFilters() }
        reconcile()
    }
    func reconcile() {
        if !scope.repo.isEmpty && !repositories.isEmpty && !repositories.contains(scope.repo) { scope = ActivityScope(); activitySelection = nil; scopedPerson = nil; relatedEvidence = nil; lens = .heat }
        if let contextualRepository, !repositories.isEmpty && !repositories.contains(contextualRepository) { restorePRFilters() }
    }
}
