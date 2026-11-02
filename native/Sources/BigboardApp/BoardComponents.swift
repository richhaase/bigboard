import SwiftUI

struct DeltaView: View {
    var added: Int
    var removed: Int
    var unknown = false
    var body: some View {
        HStack(spacing: 5) {
            Text("+\(added.formatted())").foregroundStyle(.green)
            Text("−\(removed.formatted())").foregroundStyle(.red)
            if unknown { Text("?").help("Some line counts are unavailable") }
        }
        .font(.system(.callout, design: .monospaced)).monospacedDigit()
    }
}
struct StateBadge: View {
    var value: String
    var color: Color {
        ["SUCCESS", "APPROVED", "MERGEABLE"].contains(value)
            ? .green
            : ["FAILURE", "ERROR", "CHANGES_REQUESTED", "CONFLICTING"].contains(value) ? .orange : .secondary
    }
    var body: some View {
        Text(value.replacingOccurrences(of: "_", with: " ").localizedCapitalized).font(.caption)
            .foregroundStyle(color).padding(.horizontal, 7).padding(.vertical, 3)
            .background(color.opacity(0.1), in: Capsule())
    }
}
struct EmptyBoard: View {
    var title: String
    var message: String
    var symbol = "tray"
    var body: some View {
        ContentUnavailableView(title, systemImage: symbol, description: Text(message))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
struct SectionTitle: View {
    var title: String
    var detail = ""
    var body: some View {
        HStack {
            Text(title).font(.headline)
            Spacer()
            Text(detail).font(.caption).foregroundStyle(.secondary)
        }
        .padding(.top, 12)
    }
}
struct MetricView: View {
    var title: String
    var value: String
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundStyle(.secondary)
            Text(value).font(.title2).monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

func membership(_ value: String, _ values: Binding<Set<String>>) -> Binding<Bool> {
    Binding(
        get: { values.wrappedValue.contains(value) },
        set: { if $0 { values.wrappedValue.insert(value) } else { values.wrappedValue.remove(value) } })
}
