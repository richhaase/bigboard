import SwiftUI

struct WorkspacePanel: View {
    var panel: Panel
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    var title: String {
        switch panel {
        case .settings, .groups: "Settings"
        case .dateRange: "Custom date range"
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
                if !panel.isSettings && panel != .dateRange && panel != .identities {
                    Button("Done") { dismiss() }.keyboardShortcut(.cancelAction)
                }
            }
            .padding(20)
            Divider()
            if !workspace.error.isEmpty {
                Text(workspace.error).foregroundStyle(.orange).textSelection(.enabled).padding(12)
            }
            if workspace.busy, !panel.isSettings && panel != .dateRange { ProgressView().controlSize(.small).padding(8) }
            Group {
                switch panel {
                case .settings, .groups: SettingsPanel(workspace: workspace, showGroups: panel == .groups)
                case .dateRange: CustomDateRangePanel(navigation: navigation)
                case .status: DataStatusPanel(workspace: workspace)
                case .help: KeyboardGuide()
                case .identities: IdentitiesPanel(workspace: workspace)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: panel == .dateRange ? 420 : 780, height: panel == .dateRange ? 260 : 700)
    }
}
