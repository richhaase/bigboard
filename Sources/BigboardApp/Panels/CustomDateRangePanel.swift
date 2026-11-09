import SwiftUI

struct CustomDateRangePanel: View {
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    @Environment(\.calendar) private var calendar
    @State private var mode = Mode.recent
    @State private var days = 14
    @State private var from = Date()
    @State private var through = Date()
    @FocusState private var focused: Bool

    enum Mode { case recent, dates }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Picker("Range", selection: $mode) {
                Text("Recent days").tag(Mode.recent)
                Text("Between dates").tag(Mode.dates)
            }
            .pickerStyle(.segmented).labelsHidden()
            if mode == .recent {
                LabeledContent("Last days") {
                    TextField("Days", value: $days, format: .number)
                        .frame(width: 100).focused($focused)
                }
                Text("Use 0 for all time, up to 3650 days.").foregroundStyle(.secondary)
            } else {
                DatePicker("From", selection: $from, displayedComponents: .date)
                DatePicker("Through", selection: $through, in: from..., displayedComponents: .date)
                Text("Includes both days.").foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Apply") {
                    apply()
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .onAppear {
            switch navigation.dateRange {
            case .days(let count): days = count
            case .hours: days = 1
            case .between(let interval, _):
                mode = .dates
                from = interval.start
                through = max(interval.start, interval.end.addingTimeInterval(-1))
            }
            focused = mode == .recent
        }
    }
    private func apply() {
        switch mode {
        case .recent:
            navigation.dateRange = .days(max(0, min(3650, days)))
        case .dates:
            let start = calendar.startOfDay(for: min(from, through))
            let last = calendar.startOfDay(for: max(from, through))
            let end = calendar.date(byAdding: .day, value: 1, to: last) ?? last
            navigation.dateRange = .between(DateInterval(start: start, end: end), includesStart: true)
        }
    }
}
