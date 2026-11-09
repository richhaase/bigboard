import SwiftUI
import AppKit

struct MainView: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @State private var appVisible = false
    @Environment(\.openWindow) private var openWindow
    @Environment(\.openSettings) private var openSettings
    private var automaticRefreshDate: Date? {
        workspace.nextAutomaticRefresh(visible: appVisible, modalOpen: navigation.panel != nil)
    }
    var body: some View {
        NavigationSplitView {
            List(selection: Binding(get: { navigation.board }, set: { navigation.switchBoard($0) })) {
                ForEach(Board.allCases) { board in
                    Label(board.rawValue, systemImage: board.symbol).tag(board)
                }
            }
            .navigationTitle("Bigboard").navigationSplitViewColumnWidth(min: 190, ideal: 210, max: 280)
        } detail: {
            VStack(spacing: 0) {
                if workspace.authStatus.signedIn, let message = bannerMessage {
                    HStack {
                        Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                        Text(message).textSelection(.enabled).lineLimit(2)
                        Spacer()
                        Button("Details") { openWindow(id: WindowID.dataStatus) }
                        Button("Retry") { Task { await workspace.refresh() } }
                            .disabled(workspace.busy)
                        if !workspace.error.isEmpty {
                            Button("Dismiss") { workspace.error = "" }
                        }
                    }
                    .padding(.horizontal, 12).padding(.vertical, 8).background(.orange.opacity(0.12))
                }
                if !workspace.authStatus.signedIn {
                    signedOut
                } else if !workspace.checkingSignIn, workspace.configuration.sourceRoots.isEmpty {
                    chooseSources
                } else if workspace.snapshot.repositories.isEmpty {
                    emptyWorkspace
                } else {
                    Group {
                        switch navigation.board {
                        case .heat: HeatBoard(workspace: workspace, navigation: navigation)
                        case .activity: ActivityBoard(workspace: workspace, navigation: navigation)
                        case .contributors: ContributorsBoard(workspace: workspace, navigation: navigation)
                        case .pullRequests: PullRequestsBoard(workspace: workspace, navigation: navigation)
                        }
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                Divider()
                HStack {
                    Text("Activity describes change, not productivity.")
                    Spacer()
                    footerDataStatus
                }
                .font(.caption).foregroundStyle(.secondary).padding(8)
            }
            .navigationTitle(navigation.board.rawValue)
        }
        .toolbar {
            ToolbarItem(id: "history", placement: .navigation) {
                ControlGroup {
                    Button("Back", systemImage: "chevron.left") { navigation.goBack() }
                        .disabled(!navigation.canGoBack || navigation.panel != nil)
                        .help("Back")
                    Button("Forward", systemImage: "chevron.right") { navigation.goForward() }
                        .disabled(!navigation.canGoForward || navigation.panel != nil)
                        .help("Forward")
                }
                .controlGroupStyle(.navigation)
            }
            ToolbarItem(id: "refresh", placement: .primaryAction) {
                if workspace.canStopRefresh {
                    Button("Stop", systemImage: "xmark") { Task { await workspace.stop() } }
                        .help("Stop refreshing and keep the previous data (⌘.)")
                } else {
                    Button("Refresh", systemImage: "arrow.clockwise") { Task { await workspace.refresh() } }
                        .disabled(workspace.busy || !workspace.authStatus.signedIn || navigation.panel != nil)
                        .help("Refresh history and pull requests (⌘R)")
                }
            }
            ToolbarSpacer(.fixed, placement: .primaryAction)
            if navigation.board != .pullRequests {
                ToolbarItem(id: "date-filter", placement: .primaryAction) {
                    dateFilter
                }
            }
            ToolbarItem(id: "repository-filter", placement: .primaryAction) {
                repositoryFilter
            }
        }
        .onAppear { appVisible = NSApp.occlusionState.contains(.visible) }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didChangeOcclusionStateNotification)) { _ in
            appVisible = NSApp.occlusionState.contains(.visible)
        }
        .task(id: automaticRefreshDate) {
            guard let deadline = automaticRefreshDate else { return }
            do {
                try await Task.sleep(for: .seconds(max(0, deadline.timeIntervalSinceNow)), tolerance: .seconds(1))
                try Task.checkCancellation()
                await workspace.refreshAutomatically(visible: appVisible, modalOpen: navigation.panel != nil)
            } catch {}
        }
        .sheet(item: $navigation.panel) { panel in
            WorkspacePanel(panel: panel, workspace: workspace, navigation: navigation)
        }
        .onChange(of: workspace.snapshot.accountID) { old, new in
            if old != new { navigation.resetForAccount(workspace.configuration) }
        }
        .onChange(of: workspace.snapshot.repositories.map(\.id)) { _, _ in
            navigation.reconcile(workspace.snapshot)
        }
        .onChange(of: workspace.configuration.sinceDays) { _, value in
            if !navigation.preferencesApplied {
                navigation.dateRange = .days(value)
                navigation.preferencesApplied = true
            }
        }
        .onChange(of: navigation.repositories) { navigation.reconcile() }
    }
    private var dateFilter: some View {
        Menu {
            ForEach(DateRange.presets, id: \.self) { range in
                Toggle(range.label, isOn: Binding(
                    get: { navigation.dateRange == range },
                    set: { selected in
                        if selected {
                            navigation.dateRange = range
                        }
                    })
                )
            }
            Divider()
            if navigation.dateRange.isCustomInterval {
                Toggle(navigation.rangeLabel, isOn: .constant(true))
            }
            Button("Custom range…") { navigation.panel = .dateRange }
        } label: {
            Label(navigation.rangeLabel, systemImage: "calendar")
        }
        .labelStyle(.titleAndIcon)
        .accessibilityLabel("Date filter")
        .accessibilityValue(navigation.rangeLabel)
        .help("Date range for Heat Maps, Activity, and Contributors")
    }
    private var repositoryFilter: some View {
        Button {
            navigation.repositoryFilterPresented.toggle()
        } label: {
            Label("Repositories: \(repositoryScopeLabel)", systemImage: "line.3.horizontal.decrease.circle")
        }
        .labelStyle(.titleAndIcon)
        .accessibilityLabel("Repository filter")
        .accessibilityValue(repositoryScopeLabel)
        .help("Choose a group or filter repositories across all boards")
        .popover(
            isPresented: $navigation.repositoryFilterPresented, attachmentAnchor: .rect(.bounds), arrowEdge: .bottom
        ) {
            VStack(spacing: 0) {
                HStack {
                    Text("Repositories").font(.headline)
                    Spacer()
                    Button("Manage Groups…") {
                        navigation.repositoryFilterPresented = false
                        navigation.settingsTab = .organization
                        openSettings()
                    }
                }
                .padding(16)
                Picker("Show", selection: Binding(
                    get: {
                        activeGroup ?? (navigation.repositories.isEmpty ? "" : Self.customSelection)
                    },
                    set: { name in
                        guard name != Self.customSelection else { return }
                        navigation.showGroup(
                            name.isEmpty ? nil : workspace.configuration.groups[name],
                            available: workspace.snapshot.repositories)
                    })
                ) {
                    Text("All repositories").tag("")
                    ForEach(workspace.configuration.groups.keys.sorted(), id: \.self) { name in
                        Text(name).tag(name)
                            .disabled(
                                !navigation.groupIsAvailable(
                                    workspace.configuration.groups[name] ?? [],
                                    available: workspace.snapshot.repositories))
                    }
                    if activeGroup == nil && !navigation.repositories.isEmpty {
                        Text("Custom selection").tag(Self.customSelection)
                    }
                }
                .padding(.horizontal, 16).padding(.bottom, 12)
                Divider()
                RepositoryFilterPanel(
                    repositories: workspace.snapshot.repositories, navigation: navigation,
                    repositoryListHeightLimit: 240)
            }
            .frame(width: 420)
            .fixedSize(horizontal: false, vertical: true)
            .buttonStyle(.automatic)
            .onExitCommand { navigation.repositoryFilterPresented = false }
        }
    }
    private static let customSelection = "\u{0}custom"
    private var activeGroup: String? {
        navigation.activeGroup(workspace.configuration.groups, available: workspace.snapshot.repositories)
    }
    private var repositoryScopeLabel: String {
        activeGroup ?? navigation.repositoryFilterLabel
    }
    private var bannerMessage: String? {
        if !workspace.error.isEmpty { return workspace.error }
        let issues = workspace.snapshot.coverage.errors.count
        guard issues > 0, !workspace.busy else { return nil }
        if workspace.snapshot.coverage.phase == "partial",
            workspace.snapshot.repositories.allSatisfy(\.stale)
        {
            return "Refresh could not finish. Showing the last loaded data, which may be stale."
        }
        return issues == 1
            ? "1 data issue. Some results may be incomplete."
            : "\(issues) data issues. Some results may be incomplete."
    }
    private var footerDataStatus: some View {
        let status = DataStatusPresentation(
            snapshot: workspace.snapshot, busy: workspace.busy, checkingSignIn: workspace.checkingSignIn,
            signedIn: workspace.authStatus.signedIn, error: workspace.error, progress: workspace.progress)
        return Button {
            openWindow(id: WindowID.dataStatus)
        } label: {
            HStack(spacing: 6) {
                TimelineView(.periodic(from: .now, by: 30)) { context in
                    Text(
                        status.loading
                            ? status.label
                            : DataFreshness.label(
                                snapshot: workspace.snapshot, lastUpdated: workspace.lastUpdated, now: context.date)
                    )
                    .lineLimit(1)
                }
                Group {
                    if let progress = workspace.progress, progress.total > 0 {
                        ProgressView(value: Double(progress.completed), total: Double(progress.total))
                            .progressViewStyle(.circular).controlSize(.mini)
                    } else if status.loading {
                        ProgressView().controlSize(.mini)
                    } else {
                        Image(systemName: status.symbol)
                            .foregroundStyle(status.warning ? Color.orange : Color.secondary)
                    }
                }
                .frame(width: 14, height: 14)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Data status")
        .accessibilityValue(
            status.loading
                ? status.label
                : "\(DataFreshness.label(snapshot: workspace.snapshot, lastUpdated: workspace.lastUpdated, now: .now)). \(status.label)"
        )
        .help("\(status.label). Click for collection and coverage details.")
    }
    private var signedOut: some View {
        VStack(spacing: 18) {
            Image(systemName: "square.grid.3x3.square").font(.system(size: 54)).foregroundStyle(.tint)
            Text("Your work, in view.").font(.largeTitle.bold())
            Text(
                "Bigboard uses your GitHub CLI sign-in to explore repository history, contributors, and pull requests."
            )
            .foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: 460)
            if workspace.checkingSignIn {
                ProgressView("Checking GitHub CLI…")
            } else {
                Button("Check again") { Task { await workspace.checkConnection() } }
                    .buttonStyle(.borderedProminent)
            }
            if !workspace.authStatus.error.isEmpty {
                Text(workspace.authStatus.error).foregroundStyle(.orange).textSelection(.enabled)
            }
        }
        .padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private var chooseSources: some View {
        VStack(spacing: 18) {
            Image(systemName: "folder.badge.gearshape").font(.system(size: 54)).foregroundStyle(.tint)
            Text("Where are your clones?").font(.largeTitle.bold())
            Text(
                "Bigboard reads history from Git clones you already have. Choose the folders that hold them. You can change these at any time in Settings."
            )
            .foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: 460)
            Button("Choose Folders…") {
                let folders = SettingsRepositories.chooseFolders()
                guard !folders.isEmpty else { return }
                Task {
                    if await workspace.addSourceFolders(folders) {
                        navigation.settingsTab = .repositories
                        openSettings()
                    }
                }
            }
            .buttonStyle(.borderedProminent).disabled(workspace.busy)
        }
        .padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private var emptyWorkspace: some View {
        VStack {
            EmptyBoard(
                title: workspace.busy ? "Loading repositories" : "Choose your repositories",
                message: workspace.snapshot.coverage.message.isEmpty
                    ? "Choose the repositories to track in Settings."
                    : workspace.snapshot.coverage.message, symbol: "shippingbox")
            Button("Open Settings") {
                navigation.settingsTab = .repositories
                openSettings()
            }
            .buttonStyle(.borderedProminent)
                .padding(.bottom, 50)
        }
    }
}
struct DataStatusPresentation: Equatable {
    var label: String
    var symbol: String
    var loading = false
    var warning = false
    init(
        snapshot: Snapshot, busy: Bool, checkingSignIn: Bool, signedIn: Bool, error: String,
        progress: WorkspaceModel.RefreshProgress? = nil
    ) {
        if checkingSignIn {
            label = "Checking GitHub CLI"
            symbol = "arrow.triangle.2.circlepath"
            loading = true
        } else if busy, let progress, progress.total > 0 {
            label = "Updating history · \(progress.completed) of \(progress.total) repositories"
            symbol = "arrow.triangle.2.circlepath"
            loading = true
        } else if busy {
            label = snapshot.coverage.message.isEmpty ? "Refreshing repository evidence" : snapshot.coverage.message
            symbol = "arrow.triangle.2.circlepath"
            loading = true
        } else if snapshot.coverage.phase == "stopped" {
            label = "Refresh stopped; showing earlier data"
            symbol = "stop.circle"
            warning = true
        } else if !error.isEmpty || !snapshot.coverage.errors.isEmpty || snapshot.coverage.phase == "partial"
            || snapshot.repositories.contains(where: {
                $0.stale || $0.prStale
                    || (!snapshot.cachedOnly
                        && (!$0.prInventoryComplete || !$0.prFilesComplete || !$0.prReviewersComplete))
            })
        {
            label = "Some evidence is incomplete or stale"
            symbol = "exclamationmark.circle"
            warning = true
        } else if snapshot.cachedOnly {
            label = "Cached history"
            symbol = "externaldrive"
        } else if !signedIn {
            label = "Connect GitHub CLI to load repository evidence"
            symbol = "info.circle"
        } else if snapshot.repositories.isEmpty {
            label = "No repository evidence loaded"
            symbol = "info.circle"
        } else {
            label = "Repository evidence loaded"
            symbol = "checkmark.circle"
        }
    }
}
enum DataFreshness {
    static func label(snapshot: Snapshot, lastUpdated: Date?, now: Date) -> String {
        if snapshot.cachedOnly { return "Cached history" + age(oldest(snapshot), now: now, prefix: " from ") }
        if let lastUpdated { return "Updated" + age(lastUpdated, now: now, prefix: " ") }
        if let oldest = oldest(snapshot) { return "History from" + age(oldest, now: now, prefix: " ") }
        return "Not updated yet"
    }
    private static func oldest(_ snapshot: Snapshot) -> Date? {
        snapshot.repositories.compactMap(\.scannedAt).min()
    }
    private static func age(_ date: Date?, now: Date, prefix: String) -> String {
        guard let date else { return "" }
        if now.timeIntervalSince(date) < 60 { return prefix + "just now" }
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .full
        return prefix + formatter.localizedString(for: date, relativeTo: now)
    }
}
