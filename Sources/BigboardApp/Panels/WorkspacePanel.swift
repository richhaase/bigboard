import SwiftUI

struct WorkspacePanel: View {
    var panel: Panel
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    var title: String {
        switch panel {
        case .settings: "Workspace settings"
        case .filters: "Filters"
        case .status: "Data status"
        case .help: "Keyboard guide"
        case .identities: "Contributor identities"
        }
    }
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(title).font(.title2.bold())
                Spacer()
                if panel != .settings {
                    Button("Done") { dismiss() }.keyboardShortcut(.cancelAction)
                }
            }
            .padding(20)
            Divider()
            if !workspace.error.isEmpty {
                Text(workspace.error).foregroundStyle(.orange).textSelection(.enabled).padding(12)
            }
            if workspace.busy, panel != .settings { ProgressView().controlSize(.small).padding(8) }
            Group {
                switch panel {
                case .settings: SettingsPanel(workspace: workspace)
                case .filters: FiltersPanel(workspace: workspace, navigation: navigation)
                case .status: DataStatusPanel(workspace: workspace)
                case .help: KeyboardGuide()
                case .identities: IdentitiesPanel(workspace: workspace)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: 780, height: 700)
    }
}
