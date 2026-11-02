import SwiftUI
import AppKit

@MainActor final class ApplicationDelegate: NSObject, NSApplicationDelegate {
    var workspace: WorkspaceModel?
    private var terminating = false
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard !terminating else { return .terminateNow }
        terminating = true
        Task { await workspace?.shutdown(); sender.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}
@main struct BigboardApp: App {
    @NSApplicationDelegateAdaptor(ApplicationDelegate.self) private var delegate
    @State private var workspace = WorkspaceModel()
    @State private var navigation = BoardState()
    var body: some Scene {
        Window("Bigboard", id: "main") {
            MainView(workspace: workspace, navigation: navigation)
                .frame(minWidth: 900, minHeight: 620)
                .task {
                    delegate.workspace = workspace
                    await workspace.start()
                }
                .onOpenURL { url in Task { await workspace.handleCallback(url) } }
        }
        .defaultSize(width: 1360, height: 900)
        .commands {
            CommandGroup(replacing: .appSettings) { Button("Settings…") { navigation.panel = .settings }.keyboardShortcut(",") }
            CommandMenu("Boards") {
                ForEach(Board.allCases) { board in Button(board.rawValue) { navigation.switchBoard(board) } }
                Divider()
                Button("Refresh") { Task { await workspace.refresh() } }.keyboardShortcut("r").disabled(workspace.busy)
                Button("Filters…") { navigation.panel = .filters }.keyboardShortcut("f", modifiers: [.command, .shift])
                Button("Data Status…") { navigation.panel = .status }
                Button("Repository Filter…") { navigation.panel = .repositories }
                Button("Contributor Identities…") { navigation.panel = .identities }
            }
            CommandGroup(replacing: .help) { Button("Keyboard Guide") { navigation.panel = .help } }
        }
    }
}
