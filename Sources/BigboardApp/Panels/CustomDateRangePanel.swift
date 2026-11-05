import SwiftUI

struct CustomDateRangePanel: View {
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    @State private var days = 14
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            LabeledContent("Last days") {
                TextField("Days", value: $days, format: .number)
                    .frame(width: 100).focused($focused)
            }
            Text("Use 0 for all time, up to 3650 days.").foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Apply") {
                    navigation.dateRange = .days(max(0, min(3650, days)))
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .onAppear {
            if case .days(let count) = navigation.dateRange { days = count }
            else { days = 1 }
            focused = true
        }
    }
}
