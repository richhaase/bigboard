import SwiftUI

struct MainView: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @FocusState private var searchFocused: Bool
    var body: some View {
        NavigationSplitView {
            List(selection: Binding(get: { navigation.board }, set: { navigation.switchBoard($0) })) {
                ForEach(Board.allCases) { board in Label(board.rawValue, systemImage: board.symbol).tag(board)
                }
            }
            .navigationTitle("Bigboard").navigationSplitViewColumnWidth(min: 190, ideal: 210, max: 280)
        } detail: {
            VStack(spacing: 0) {
                if !workspace.error.isEmpty {
                    HStack {
                        Image(systemName: "exclamationmark.triangle")
                        Text(workspace.error).textSelection(.enabled)
                        Spacer()
                        Button("Dismiss") { workspace.error = "" }
                    }
                    .padding(10).background(.orange.opacity(0.12))
                }
                if workspace.busy {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text(
                            workspace.snapshot.coverage.message.isEmpty
                                ? "Loading repository evidence…" : workspace.snapshot.coverage.message
                        )
                        .font(.callout)
                        Spacer()
                    }
                    .padding(10)
                }
                if !workspace.snapshot.coverage.errors.isEmpty {
                    Button {
                        navigation.panel = .status
                    } label: {
                        HStack {
                            Image(systemName: "exclamationmark.circle")
                            Text(
                                "\(workspace.snapshot.coverage.errors.count) data issues · Some results may be incomplete"
                            )
                            Spacer()
                            Image(systemName: "chevron.right")
                        }
                        .padding(10)
                    }
                    .buttonStyle(.plain).foregroundStyle(.orange)
                }
                if !workspace.authStatus.signedIn {
                    signedOut
                } else if workspace.snapshot.repositories.isEmpty {
                    emptyWorkspace
                } else {
                    HStack {
                        TextField(
                            "Search \(navigation.board.rawValue.lowercased())",
                            text: Binding(
                                get: { navigation.currentFilters.query },
                                set: { navigation.currentFilters.query = $0 })
                        )
                        .textFieldStyle(.roundedBorder).focused($searchFocused)
                        .onSubmit { searchFocused = false }.onExitCommand { searchFocused = false }
                        .focusedSceneValue(\.boardSearch, $searchFocused)
                        Button("Filters", systemImage: "line.3.horizontal.decrease") {
                            navigation.panel = .filters
                        }
                        if navigation.board != .pullRequests {
                            Toggle(
                                "Hide bots",
                                isOn: Binding(
                                    get: { navigation.currentFilters.hideBots },
                                    set: { navigation.currentFilters.hideBots = $0 })
                            )
                            .toggleStyle(.checkbox)
                        }
                        Button("Copy view", systemImage: "doc.on.doc") {
                            BoardMarkdown.copy(
                                snapshot: workspace.snapshot, configuration: workspace.configuration,
                                navigation: navigation)
                        }
                        .labelStyle(.iconOnly).help("Copy this view as Markdown")
                    }
                    .padding(12)
                    Group {
                        switch navigation.board {
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
            ToolbarItem(id: "date-filter", placement: .primaryAction) {
                dateFilter
            }
            ToolbarItem(id: "repository-filter", placement: .primaryAction) {
                repositoryFilter
            }
            if !workspace.configuration.groups.isEmpty {
                ToolbarItem(id: "workspace-group", placement: .primaryAction) {
                    groupFilter
                }
            }
            ToolbarSpacer(.fixed, placement: .primaryAction)
            ToolbarItem(id: "refresh", placement: .primaryAction) {
                Button("Refresh", systemImage: "arrow.clockwise") {
                    Task { await workspace.refresh() }
                }
                .disabled(workspace.busy || !workspace.authStatus.signedIn)
                .help("Refresh repository evidence")
            }
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
                navigation.days = value
                navigation.preferencesApplied = true
            }
        }
        .onChange(of: navigation.repositories) { navigation.reconcile() }
    }
    private var dateFilter: some View {
        Menu {
            ForEach([1, 7, 14, 30, 90, 365, 0], id: \.self) { days in
                Toggle(days == 0 ? "All time" : "Last \(days) days", isOn: Binding(
                    get: { !navigation.sinceLastLook && navigation.days == days },
                    set: { selected in
                        if selected {
                            navigation.days = days
                            navigation.sinceLastLook = false
                        }
                    })
                )
            }
            Divider()
            Toggle("Since last look", isOn: $navigation.sinceLastLook)
                .disabled(workspace.snapshot.bookmark == nil)
            Divider()
            Button("Custom range…") { navigation.panel = .filters }
        } label: {
            Label(navigation.rangeLabel, systemImage: "calendar")
        }
        .labelStyle(.titleAndIcon)
        .accessibilityLabel("Date filter")
        .accessibilityValue(navigation.rangeLabel)
        .help("Date filter for all boards")
    }
    private var repositoryFilter: some View {
        Button {
            navigation.repositoryFilterPresented.toggle()
        } label: {
            Label("Repositories: \(navigation.repositoryFilterLabel)", systemImage: "line.3.horizontal.decrease.circle")
        }
        .labelStyle(.titleAndIcon)
        .accessibilityLabel("Repository filter")
        .accessibilityValue(navigation.repositoryFilterLabel)
        .help("Filter loaded repositories across all boards")
        .popover(
            isPresented: $navigation.repositoryFilterPresented, attachmentAnchor: .rect(.bounds), arrowEdge: .bottom
        ) {
            VStack(spacing: 0) {
                HStack {
                    Text("Repository filter").font(.headline)
                    Spacer()
                }
                .padding(16)
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
    private var groupFilter: some View {
        Menu {
            Picker("Workspace group", selection: Binding(
                get: { workspace.activeGroup },
                set: { name in Task { await workspace.selectGroup(name) } })
            ) {
                Text("All groups").tag("")
                ForEach(workspace.configuration.groups.keys.sorted(), id: \.self) { name in
                    Text(name).tag(name)
                }
            }
            .pickerStyle(.inline)
        } label: {
            Label(workspace.activeGroup.isEmpty ? "All groups" : workspace.activeGroup, systemImage: "square.stack")
        }
        .labelStyle(.titleAndIcon)
        .accessibilityLabel("Workspace group")
        .accessibilityValue(workspace.activeGroup.isEmpty ? "All groups" : workspace.activeGroup)
        .disabled(workspace.busy)
        .help("Choose workspace group")
    }
    private var footerDataStatus: some View {
        let status = DataStatusPresentation(
            snapshot: workspace.snapshot, busy: workspace.busy, checkingSignIn: workspace.checkingSignIn,
            signedIn: workspace.authStatus.signedIn, error: workspace.error)
        let updated =
            workspace.snapshot.cachedOnly
            ? "Cached history"
            : "Updated \(workspace.snapshot.generatedAt.formatted(date: .abbreviated, time: .shortened))"
        return Button {
            navigation.panel = .status
        } label: {
            HStack(spacing: 6) {
                Text(updated)
                Group {
                    if status.loading {
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
        .accessibilityValue("\(updated). \(status.label)")
        .help("\(status.label). Click for collection and coverage details.")
    }
    private var signedOut: some View {
        VStack(spacing: 18) {
            Image(systemName: "square.grid.3x3.square").font(.system(size: 54)).foregroundStyle(.tint)
            Text("Your work, in view.").font(.largeTitle.bold())
            Text("Connect GitHub to explore repository history, contributors, and pull requests.")
                .foregroundStyle(.secondary).multilineTextAlignment(.center)
            if workspace.checkingSignIn {
                ProgressView("Checking saved sign-in…")
            } else if workspace.authStatus.configured {
                Button(workspace.authStatus.pending ? "Waiting for GitHub…" : "Sign in with GitHub") {
                    Task { await workspace.signIn() }
                }
                .buttonStyle(.borderedProminent).disabled(workspace.authStatus.pending)
                if workspace.authStatus.pending {
                    Button("Cancel sign-in") { Task { await workspace.cancelSignIn() } }
                }
            } else {
                Text(
                    "GitHub sign-in is not configured in this build. Build with the publisher’s GitHub App settings to connect."
                )
                .font(.callout).foregroundStyle(.secondary).frame(maxWidth: 460)
                .multilineTextAlignment(.center)
            }
            if !workspace.authStatus.error.isEmpty {
                Text(workspace.authStatus.error).foregroundStyle(.orange).textSelection(.enabled)
            }
        }
        .padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private var emptyWorkspace: some View {
        VStack {
            EmptyBoard(
                title: workspace.busy ? "Loading repositories" : "Choose your repositories",
                message: workspace.snapshot.coverage.message.isEmpty
                    ? "Discover repositories in Settings, choose what to track, and save your workspace."
                    : workspace.snapshot.coverage.message, symbol: "shippingbox")
            Button("Open Settings") { navigation.panel = .settings }.buttonStyle(.borderedProminent)
                .padding(.bottom, 50)
        }
    }
}
struct DataStatusPresentation: Equatable {
    var label: String
    var symbol: String
    var loading = false
    var warning = false
    init(snapshot: Snapshot, busy: Bool, checkingSignIn: Bool, signedIn: Bool, error: String) {
        if checkingSignIn {
            label = "Checking saved sign-in"
            symbol = "arrow.triangle.2.circlepath"
            loading = true
        } else if busy {
            label = "Refreshing repository evidence"
            symbol = "arrow.triangle.2.circlepath"
            loading = true
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
            label = "Sign in to load repository evidence"
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
