import AppKit

func openGitHub(_ raw: String) {
    guard let url = URL(string: raw), url.scheme == "https", url.host == "github.com", url.user == nil,
        url.password == nil, !raw.contains("\n"), !raw.contains("\r")
    else { return }
    NSWorkspace.shared.open(url)
}
@MainActor func openGitHub(_ scope: ActivityScope, workspace: WorkspaceModel) {
    Task {
        do {
            let url = try await workspace.openPath(scope)
            NSWorkspace.shared.open(url)
        } catch {
            workspace.error = error.localizedDescription
        }
    }
}
