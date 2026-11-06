import SwiftUI
import AppKit

@MainActor final class ApplicationDelegate: NSObject, NSApplicationDelegate {
    var workspace: WorkspaceModel?
    private var terminating = false
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard !terminating else { return .terminateNow }
        terminating = true
        Task {
            await workspace?.shutdown()
            sender.reply(toApplicationShouldTerminate: true)
        }
        return .terminateLater
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}
public struct BigboardApplication: App {
    @NSApplicationDelegateAdaptor(ApplicationDelegate.self) private var delegate
    @State private var workspace = WorkspaceModel()
    @State private var navigation = BoardState()
    public init() {}

    public var body: some Scene {
        Window("Bigboard", id: "main") {
            MainView(workspace: workspace, navigation: navigation)
                .frame(minWidth: 900, minHeight: 620)
                .task {
                    delegate.workspace = workspace
                    await workspace.start()
                }
        }
        .defaultSize(width: 1360, height: 900)
        .commands {
            BoardCommands(workspace: workspace, navigation: navigation)
        }
    }
}

extension FocusedValues {
    @Entry var boardSearch: FocusState<Bool>.Binding?
    @Entry var selectedActivityScope: ActivityScope?
    @Entry var selectedPullRequestURL: String?
}

private struct BoardCommands: Commands {
    let workspace: WorkspaceModel
    let navigation: BoardState
    @FocusedValue(\.boardSearch) private var boardSearch
    @FocusedValue(\.selectedActivityScope) private var selectedActivityScope
    @FocusedValue(\.selectedPullRequestURL) private var selectedPullRequestURL

    private var modalOpen: Bool { navigation.panel != nil }
    private var hasData: Bool { workspace.authStatus.signedIn && !workspace.snapshot.repositories.isEmpty }

    var body: some Commands {
        CommandGroup(replacing: .appSettings) {
            Button("Settings…") { navigation.panel = .settings }
                .keyboardShortcut(",")
                .disabled(modalOpen)
        }
        CommandMenu("Go") {
            Button("Back") { navigation.goBack() }
                .keyboardShortcut("[")
                .disabled(modalOpen || !hasData || !navigation.canGoBack)
            Button("Forward") { navigation.goForward() }
                .keyboardShortcut("]")
                .disabled(modalOpen || !hasData || !navigation.canGoForward)
            Button("Enclosing Scope") { navigation.goUp() }
                .keyboardShortcut(.upArrow)
                .disabled(modalOpen || !hasData || !navigation.canGoUp)
        }
        CommandMenu("Boards") {
            ForEach(Array(Board.allCases.enumerated()), id: \.element) { index, board in
                Button(board.rawValue) { navigation.switchBoard(board) }
                    .keyboardShortcut(KeyEquivalent(Character(String(index + 1))))
                    .disabled(modalOpen)
            }
            Divider()
            Button(navigation.searchPrompt ?? "Find on Board") { boardSearch?.wrappedValue = true }
                .keyboardShortcut("f")
                .disabled(modalOpen || boardSearch == nil)
            Button("Custom Date Range…") { navigation.panel = .dateRange }
                .keyboardShortcut("d", modifiers: [.command, .option])
                .disabled(modalOpen || navigation.board == .pullRequests)
            Button("Repository Filter…") { navigation.repositoryFilterPresented.toggle() }
                .keyboardShortcut("i", modifiers: [.command, .shift])
                .disabled(modalOpen)
            Button("Data Status…") { navigation.panel = .status }
                .keyboardShortcut("d", modifiers: [.command, .shift])
                .disabled(modalOpen)
            Button("Refresh") { Task { await workspace.refresh() } }
                .keyboardShortcut("r")
                .disabled(modalOpen || workspace.busy || !workspace.authStatus.signedIn)
            Button("Contributor Identities…") { navigation.panel = .identities }
                .disabled(modalOpen)
            Menu("Group") {
                Button("All repositories") {
                    navigation.showGroup(nil, available: workspace.snapshot.repositories)
                }
                ForEach(workspace.configuration.groups.keys.sorted(), id: \.self) { name in
                    let members = workspace.configuration.groups[name] ?? []
                    Button(name) {
                        navigation.showGroup(members, available: workspace.snapshot.repositories)
                    }
                    .disabled(!navigation.groupIsAvailable(members, available: workspace.snapshot.repositories))
                }
                Divider()
                Button("Manage Groups…") { navigation.panel = .groups }
            }
            .disabled(modalOpen || !hasData)
            Divider()
            Button("Copy View as Markdown") {
                BoardMarkdown.copy(
                    snapshot: workspace.snapshot, configuration: workspace.configuration,
                    navigation: navigation)
            }
            .keyboardShortcut("c", modifiers: [.command, .shift])
            .disabled(modalOpen || !hasData)
            Button("Open Selection on GitHub") {
                if let scope = selectedActivityScope {
                    openGitHub(scope, workspace: workspace)
                } else if let url = selectedPullRequestURL {
                    openGitHub(url)
                }
            }
            .keyboardShortcut(.return, modifiers: .command)
            .disabled(modalOpen || (selectedActivityScope == nil && selectedPullRequestURL == nil))
            Menu("Activity View") {
                ForEach(Array(ActivityLens.allCases.enumerated()), id: \.element) { index, lens in
                    Button(lens.rawValue) { navigation.setLens(lens) }
                        .keyboardShortcut(
                            KeyEquivalent(Character(String(index + 1))), modifiers: [.command, .option])
                }
            }
            .disabled(modalOpen || !hasData || navigation.board != .activity)
            Menu("My Pull Requests") {
                Button("Needs My Attention") { navigation.togglePRMine("attention") }
                    .keyboardShortcut("m", modifiers: [.command, .option])
                Button("Authored by Me") { navigation.togglePRMine("authored") }
                    .keyboardShortcut("a", modifiers: [.command, .option])
                Button("Review Requested") { navigation.togglePRMine("requested") }
                    .keyboardShortcut("r", modifiers: [.command, .option])
            }
            .disabled(modalOpen || !hasData || navigation.board != .pullRequests)
        }
        CommandGroup(replacing: .help) {
            Button("Keyboard Guide") { navigation.panel = .help }.disabled(modalOpen)
        }
    }
}
