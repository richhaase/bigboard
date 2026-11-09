import AppKit

enum GitHubTarget: Equatable {
    case url(String)
    case scope(ActivityScope)
}

func commitURL(_ commit: Commit) -> String {
    "https://github.com/\(commit.repo)/commit/\(commit.id)"
}

func copyToPasteboard(_ text: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
}

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
@MainActor func copyGitHubLink(_ scope: ActivityScope, workspace: WorkspaceModel) {
    Task {
        do {
            copyToPasteboard(try await workspace.openPath(scope).absoluteString)
        } catch {
            workspace.error = error.localizedDescription
        }
    }
}
@MainActor func openGitHub(_ target: GitHubTarget, workspace: WorkspaceModel) {
    switch target {
    case .url(let url): openGitHub(url)
    case .scope(let scope): openGitHub(scope, workspace: workspace)
    }
}
@MainActor func copyGitHubLink(_ target: GitHubTarget, workspace: WorkspaceModel) {
    switch target {
    case .url(let url): copyToPasteboard(url)
    case .scope(let scope): copyGitHubLink(scope, workspace: workspace)
    }
}
