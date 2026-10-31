import SwiftUI
import AppKit

struct MainView: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @FocusState private var searchFocused: Bool
    var body: some View {
        NavigationSplitView {
            List(selection: Binding(get: { navigation.board }, set: { navigation.switchBoard($0) })) {
                ForEach(Board.allCases) { board in Label(board.rawValue, systemImage: board.symbol).tag(board) }
            }.navigationTitle("Bigboard").navigationSplitViewColumnWidth(min: 190, ideal: 210, max: 280)
        } detail: {
            VStack(spacing: 0) {
                if workspace.isPreview { Text("Interface preview · Synthetic data · No account or repository access").font(.caption).frame(maxWidth: .infinity).padding(6).background(.yellow.opacity(0.15)) }
                if !workspace.error.isEmpty { HStack { Image(systemName: "exclamationmark.triangle"); Text(workspace.error).textSelection(.enabled); Spacer(); Button("Dismiss") { workspace.error = "" } }.padding(10).background(.orange.opacity(0.12)) }
                if workspace.busy { HStack { ProgressView().controlSize(.small); Text(workspace.snapshot.coverage.message.isEmpty ? "Loading repository evidence…" : workspace.snapshot.coverage.message).font(.callout); Spacer() }.padding(10) }
                if !workspace.snapshot.coverage.errors.isEmpty { Button { navigation.panel = .status } label: { HStack { Image(systemName: "exclamationmark.circle"); Text("\(workspace.snapshot.coverage.errors.count) data issues · Some results may be incomplete"); Spacer(); Image(systemName: "chevron.right") }.padding(10) }.buttonStyle(.plain).foregroundStyle(.orange) }
                if !workspace.authStatus.signedIn { signedOut }
                else if workspace.snapshot.repositories.isEmpty { emptyWorkspace }
                else {
                    HStack {
                        TextField("Search \(navigation.board.rawValue.lowercased())", text: Binding(get: { navigation.currentFilters.query }, set: { navigation.currentFilters.query = $0 })).textFieldStyle(.roundedBorder).focused($searchFocused).onSubmit { searchFocused = false }.onExitCommand { searchFocused = false }
                        Button("Filters", systemImage: "line.3.horizontal.decrease") { navigation.panel = .filters }
                        if navigation.board != .pullRequests { Toggle("Hide bots", isOn: Binding(get: { navigation.currentFilters.hideBots }, set: { navigation.currentFilters.hideBots = $0 })).toggleStyle(.checkbox) }
                        Button("Copy view", systemImage: "doc.on.doc") { copyView() }.labelStyle(.iconOnly).help("Copy this view as Markdown")
                    }.padding(12)
                    Group {
                        switch navigation.board {
                        case .activity: ActivityBoard(workspace: workspace, navigation: navigation)
                        case .contributors: ContributorsBoard(workspace: workspace, navigation: navigation)
                        case .pullRequests: PullRequestsBoard(workspace: workspace, navigation: navigation)
                        }
                    }.frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                Divider()
                HStack { Text("Activity describes change, not productivity."); Spacer(); footerDataStatus }.font(.caption).foregroundStyle(.secondary).padding(8)
            }.navigationTitle(navigation.board.rawValue)
        }
        .toolbar {
            ToolbarItem(id: "workspace-controls", placement: .primaryAction) {
                HStack(spacing: 8) {
                    dateFilter
                    repositoryFilter
                    if !workspace.configuration.groups.isEmpty { groupFilter }
                    Button { Task { await workspace.refresh() } } label: { ToolbarControlLabel(symbol: "arrow.clockwise") }
                        .accessibilityLabel("Refresh")
                        .disabled(workspace.busy || !workspace.authStatus.signedIn)
                        .help("Refresh repository evidence")
                }
                .buttonStyle(ToolbarControlStyle())
                .padding(.trailing, 12)
                .fixedSize(horizontal: true, vertical: false)
            }.sharedBackgroundVisibility(.hidden)
        }
        .sheet(item: $navigation.panel) { panel in WorkspacePanel(panel: panel, workspace: workspace, navigation: navigation) }
        .onChange(of: workspace.snapshot.accountID) { old, new in
            if old != new { navigation.resetForAccount(workspace.configuration) }
        }
        .onChange(of: workspace.snapshot.repositories.map(\.id)) { _, _ in navigation.reconcile(workspace.snapshot) }
        .onChange(of: workspace.configuration.sinceDays) { _, value in if !navigation.preferencesApplied { navigation.days = value; navigation.preferencesApplied = true } }
        .onChange(of: navigation.repositories) { navigation.reconcile() }
        .background(KeyCommandMonitor { key in handleKey(key) })
    }
    private func toolbarPopover(_ value: ToolbarPopover) -> Binding<Bool> {
        Binding(get: { navigation.toolbarPopover == value }, set: { navigation.setToolbarPopover(value, presented: $0) })
    }
    private var dateFilter: some View {
        Button { navigation.toggleToolbarPopover(.date) } label: {
            ToolbarControlLabel(title: navigation.rangeLabel, symbol: "calendar", disclosure: true)
        }
        .accessibilityLabel("Date filter")
        .accessibilityValue(navigation.rangeLabel)
        .help("Date filter for all boards")
        .popover(isPresented: toolbarPopover(.date), attachmentAnchor: .rect(.bounds), arrowEdge: .bottom) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Date filter").font(.headline).padding(8)
                ForEach([1, 7, 14, 30, 90, 365, 0], id: \.self) { days in
                    ToolbarChoiceRow(title: days == 0 ? "All time" : "Last \(days) days", selected: !navigation.sinceLastLook && navigation.days == days) {
                        navigation.days = days
                        navigation.sinceLastLook = false
                        navigation.dismissToolbarPopover()
                    }
                }
                Divider().padding(.vertical, 4)
                ToolbarChoiceRow(title: "Since last look", selected: navigation.sinceLastLook) {
                    navigation.sinceLastLook = true
                    navigation.dismissToolbarPopover()
                }.disabled(workspace.snapshot.bookmark == nil)
                ToolbarChoiceRow(title: "Custom range…") { navigation.panel = .filters }
            }.padding(8).frame(width: 220).buttonStyle(.automatic)
        }
    }
    private var repositoryFilter: some View {
        Button { navigation.toggleRepositoryPopover() } label: {
            ToolbarControlLabel(title: "Repository filter: \(navigation.repositoryFilterLabel)", symbol: "line.3.horizontal.decrease.circle", disclosure: true)
        }
        .accessibilityLabel("Repository filter")
        .accessibilityValue(navigation.repositoryFilterLabel)
        .help("Filter loaded repositories across all boards")
        .popover(isPresented: toolbarPopover(.repositories), attachmentAnchor: .rect(.bounds), arrowEdge: .bottom) {
            VStack(spacing: 0) {
                HStack { Text("Repository filter").font(.headline); Spacer() }.padding(16)
                Divider()
                RepositoryFilterPanel(repositories: workspace.snapshot.repositories, navigation: navigation, repositoryListHeightLimit: 240)
            }.frame(width: 420).fixedSize(horizontal: false, vertical: true).buttonStyle(.automatic)
        }
    }
    private var groupFilter: some View {
        Button { navigation.toggleToolbarPopover(.groups) } label: {
            ToolbarControlLabel(title: workspace.activeGroup.isEmpty ? "All groups" : workspace.activeGroup, symbol: "square.stack", disclosure: true)
        }
        .accessibilityLabel("Workspace group")
        .accessibilityValue(workspace.activeGroup.isEmpty ? "All groups" : workspace.activeGroup)
        .disabled(workspace.busy)
        .help("Choose workspace group")
        .popover(isPresented: toolbarPopover(.groups), attachmentAnchor: .rect(.bounds), arrowEdge: .bottom) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Workspace group").font(.headline).padding(8)
                ScrollView {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach([""] + workspace.configuration.groups.keys.sorted(), id: \.self) { name in
                            ToolbarChoiceRow(title: name.isEmpty ? "All groups" : name, selected: workspace.activeGroup == name) {
                                navigation.dismissToolbarPopover()
                                Task { await workspace.selectGroup(name) }
                            }.disabled(workspace.busy)
                        }
                    }
                }
                .frame(height: min(CGFloat(workspace.configuration.groups.count + 1) * 32 - 2, 320))
            }.padding(8).frame(minWidth: 220).buttonStyle(.automatic)
        }
    }
    private var footerDataStatus: some View {
        let status = DataStatusPresentation(snapshot: workspace.snapshot, busy: workspace.busy, checkingSignIn: workspace.checkingSignIn, signedIn: workspace.authStatus.signedIn, error: workspace.error)
        let updated = workspace.snapshot.cachedOnly ? "Cached history" : "Updated \(workspace.snapshot.generatedAt.formatted(date: .abbreviated, time: .shortened))"
        return Button { navigation.panel = .status } label: {
            HStack(spacing: 6) {
                Text(updated)
                Group {
                    if status.loading { ProgressView().controlSize(.mini) }
                    else { Image(systemName: status.symbol).foregroundStyle(status.warning ? Color.orange : Color.secondary) }
                }.frame(width: 14, height: 14)
            }.contentShape(Rectangle())
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
            Text("Connect GitHub to explore repository history, contributors, and pull requests.").foregroundStyle(.secondary).multilineTextAlignment(.center)
            if workspace.checkingSignIn {
                ProgressView("Checking saved sign-in…")
            } else if workspace.authStatus.configured {
                Button(workspace.authStatus.pending ? "Waiting for GitHub…" : "Sign in with GitHub") { Task { await workspace.signIn() } }.buttonStyle(.borderedProminent).disabled(workspace.authStatus.pending)
                if workspace.authStatus.pending { Button("Cancel sign-in") { Task { await workspace.cancelSignIn() } } }
            } else { Text("GitHub sign-in is not configured in this build. Build with the publisher’s GitHub App settings to connect.").font(.callout).foregroundStyle(.secondary).frame(maxWidth: 460).multilineTextAlignment(.center) }
            if !workspace.authStatus.error.isEmpty { Text(workspace.authStatus.error).foregroundStyle(.orange).textSelection(.enabled) }
        }.padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private var emptyWorkspace: some View {
        VStack { EmptyBoard(title: workspace.busy ? "Loading repositories" : "Choose your repositories", message: workspace.snapshot.coverage.message.isEmpty ? "Discover repositories in Settings, choose what to track, and save your workspace." : workspace.snapshot.coverage.message, symbol: "shippingbox"); Button("Open Settings") { navigation.panel = .settings }.buttonStyle(.borderedProminent).padding(.bottom, 50) }
    }
    private func handleKey(_ key: String) {
        if navigation.handlePopoverShortcut(key) { return }
        if navigation.panel != nil { if key == "\u{1b}" { navigation.panel = nil }; return }
        switch key {
        case "a": navigation.switchBoard(.activity)
        case "c": navigation.switchBoard(.contributors)
        case "p": navigation.switchBoard(.pullRequests)
        case "r": if !workspace.busy { Task { await workspace.refresh() } }
        case "/": searchFocused = true
        case "f", "t": navigation.panel = .filters
        case "e": navigation.panel = .status
        case "i": navigation.panel = .repositories
        case ",": navigation.panel = .settings
        case "?", "F1": navigation.panel = .help
        case "x": navigation.resetBoard()
        case "y": copyView()
        case "\u{1b}", "left": navigation.back()
        case "1", "2", "3", "4", "5": if navigation.board == .activity { navigation.setLens(["1": .commits, "2": .people, "3": .related, "4": .directories, "5": .heat][key]!) }
        case "b": navigation.currentFilters.hideBots.toggle()
        case "m": if navigation.board == .contributors { navigation.panel = .identities } else if navigation.board == .activity { navigation.relatedMode = navigation.relatedMode == "commits" ? "people" : "commits" } else { navigation.prs.mine = navigation.prs.mine == ["attention"] ? [] : ["attention"] }
        case "M": navigation.prs.mine = navigation.prs.mine == ["authored"] ? [] : ["authored"]
        case "n": navigation.prs.mine = navigation.prs.mine == ["requested"] ? [] : ["requested"]
        case "v": if navigation.board == .activity && !navigation.scope.repo.isEmpty { navigation.showPRs(repo: navigation.scope.repo) } else if navigation.board == .pullRequests { navigation.panel = .filters }
        case "u", "k", "d", "z": if navigation.board == .pullRequests { navigation.panel = .filters }
        case "s":
            let sorts = navigation.board == .pullRequests ? ["updated", "repository", "author", "title", "attention"] : navigation.board == .contributors ? ["change", "commits", "added", "removed", "net", "ai", "name", "recent"] : ["change", "commits", "recent", "name"]
            navigation.currentFilters.sort = sorts[((sorts.firstIndex(of: navigation.currentFilters.sort) ?? -1) + 1) % sorts.count]
        case "A": if navigation.board == .pullRequests { navigation.prs.sort = "author" }
        case "o": openSelection()
        case "S": navigation.currentFilters.reversed.toggle()
        default: break
        }
    }
    private func openSelection() {
        if navigation.board == .pullRequests {
            let rows = navigation.pullRequests(workspace.snapshot)
            if let pr = rows.first(where: { $0.id == navigation.prSelection }) ?? rows.first { openGitHub(pr.url) }
        } else if navigation.board == .activity {
            let scope = navigation.activityRows(workspace.snapshot, configuration: workspace.configuration).first { $0.id == navigation.activitySelection }?.scope ?? navigation.scope
            guard !scope.repo.isEmpty else { return }
            Task { do { let url = try await workspace.openPath(scope); NSWorkspace.shared.open(url) } catch { workspace.error = error.localizedDescription } }
        }
    }
    private func copyView() {
        let text = BoardMarkdown.render(snapshot: workspace.snapshot, configuration: workspace.configuration, navigation: navigation)
        NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string)
    }
}
private struct ToolbarControlLabel: View {
    var title: String? = nil
    var symbol: String
    var disclosure = false
    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).frame(width: 14).accessibilityHidden(true)
            if let title { Text(title).lineLimit(1) }
            if disclosure { Image(systemName: "chevron.down").font(.system(size: 9, weight: .semibold)).accessibilityHidden(true) }
        }
    }
}
private struct ToolbarControlStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    func makeBody(configuration: Configuration) -> some View { ToolbarControlBody(configuration: configuration, enabled: isEnabled) }
}
private struct ToolbarControlBody: View {
    var configuration: ButtonStyleConfiguration
    var enabled: Bool
    @State private var hovered = false
    private var backgroundOpacity: Double {
        guard enabled else { return 0 }
        return configuration.isPressed ? 0.10 : hovered ? 0.05 : 0
    }
    var body: some View {
        configuration.label
            .font(.system(size: 13))
            .foregroundStyle(enabled ? Color.primary : Color.secondary)
            .padding(.horizontal, 8)
            .frame(height: 30)
            .contentShape(Rectangle())
            .background(RoundedRectangle(cornerRadius: 5).fill(Color.primary.opacity(backgroundOpacity)))
            .onHover { hovered = $0 }
    }
}
private struct ToolbarChoiceRow: View {
    var title: String
    var selected = false
    var action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: "checkmark").frame(width: 12).opacity(selected ? 1 : 0).accessibilityHidden(true)
                Text(title)
                Spacer(minLength: 0)
            }
        }
        .buttonStyle(ToolbarControlStyle())
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}
struct DataStatusPresentation: Equatable {
    var label: String
    var symbol: String
    var loading = false
    var warning = false
    init(snapshot: Snapshot, busy: Bool, checkingSignIn: Bool, signedIn: Bool, error: String) {
        if checkingSignIn { label = "Checking saved sign-in"; symbol = "arrow.triangle.2.circlepath"; loading = true }
        else if busy { label = "Refreshing repository evidence"; symbol = "arrow.triangle.2.circlepath"; loading = true }
        else if !error.isEmpty || !snapshot.coverage.errors.isEmpty || snapshot.coverage.phase == "partial" || snapshot.repositories.contains(where: { $0.stale || $0.prStale || (!snapshot.cachedOnly && (!$0.prInventoryComplete || !$0.prFilesComplete || !$0.prReviewersComplete)) }) {
            label = "Some evidence is incomplete or stale"; symbol = "exclamationmark.circle"; warning = true
        } else if snapshot.cachedOnly { label = "Cached history"; symbol = "externaldrive" }
        else if !signedIn { label = "Sign in to load repository evidence"; symbol = "info.circle" }
        else if snapshot.repositories.isEmpty { label = "No repository evidence loaded"; symbol = "info.circle" }
        else { label = "Repository evidence loaded"; symbol = "checkmark.circle" }
    }
}
struct KeyCommandMonitor: NSViewRepresentable {
    var action: @MainActor (String) -> Void
    func makeNSView(context: Context) -> NSView { context.coordinator.start(action); return NSView() }
    func updateNSView(_ view: NSView, context: Context) { context.coordinator.action = action }
    func makeCoordinator() -> Coordinator { Coordinator() }
    static func dismantleNSView(_ nsView: NSView, coordinator: Coordinator) { coordinator.stop() }
    @MainActor final class Coordinator {
        var monitor: Any?
        var action: (@MainActor (String) -> Void)?
        func start(_ action: @escaping @MainActor (String) -> Void) {
            self.action = action
            monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
                let handled = MainActor.assumeIsolated {
                    guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
                    if let text = event.window?.firstResponder as? NSTextView, text.isEditable { return false }
                    let key = event.keyCode == 122 ? "F1" : event.keyCode == 123 ? "left" : event.characters ?? ""
                    let keys: Set<String> = ["a","c","p","r","/","f","t","e","i",",","?","F1","x","y","\u{1b}","left","1","2","3","4","5","b","m","M","n","v","S","s","A","o","u","k","d","z"]
                    guard keys.contains(key) else { return false }
                    self?.action?(key); return true
                }
                return handled ? nil : event
            }
        }
        func stop() { if let monitor { NSEvent.removeMonitor(monitor) }; monitor = nil }
    }
}
