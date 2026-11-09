import SwiftUI

struct WorkspacePanel: View {
    var panel: Panel
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var title: String {
        switch panel {
        case .dateRange: "Custom date range"
        case .identities: "Contributor identities"
        }
    }
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(title).font(.title2.bold())
                Spacer()
            }
            .padding(20)
            Divider()
            if !workspace.error.isEmpty {
                Text(workspace.error).foregroundStyle(.orange).textSelection(.enabled).padding(12)
            }
            if workspace.busy, panel != .dateRange { ProgressView().controlSize(.small).padding(8) }
            Group {
                switch panel {
                case .dateRange: CustomDateRangePanel(navigation: navigation)
                case .identities: IdentitiesPanel(workspace: workspace)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: panel == .dateRange ? 420 : 780, height: panel == .dateRange ? 260 : 700)
    }
}
