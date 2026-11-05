import SwiftUI

struct BoardSearch: View {
    @Bindable var navigation: BoardState
    @FocusState private var focused: Bool

    var body: some View {
        if let prompt = navigation.searchPrompt {
            TextField(prompt, text: $navigation.searchQuery)
                .textFieldStyle(.roundedBorder)
                .focused($focused)
                .focusedSceneValue(\.boardSearch, $focused)
                .onSubmit { focused = false }
                .onExitCommand { focused = false }
        }
    }
}

struct PeopleFilter: View {
    var records: [Commit]
    @Binding var filters: BoardFilters

    var body: some View {
        FilterPopover(title: "People: \(filters.people.isEmpty ? "All" : "\(filters.people.count) selected")\(filters.hideBots ? " · Bots hidden" : "")") {
            Toggle("Hide bots", isOn: $filters.hideBots)
            Divider()
            Button("All people") { filters.people = [] }
            let people = Analysis.people(records)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(people) { person in
                        Toggle("\(person.name) · \(person.email)", isOn: membership(person.id, $filters.people))
                    }
                }
            }
            .frame(maxHeight: min(CGFloat(people.count) * 30, 240))
        }
        .keyboardShortcut("f", modifiers: [.command, .shift])
    }
}

struct FilterPopover<Content: View>: View {
    var title: String
    @ViewBuilder var content: () -> Content
    @State private var presented = false

    var body: some View {
        Button { presented.toggle() } label: {
            HStack {
                Text(title)
                Image(systemName: "chevron.down").font(.caption)
            }
        }
        .popover(isPresented: $presented) {
            VStack(alignment: .leading, spacing: 12, content: content)
                .toggleStyle(.checkbox)
                .padding(16)
                .frame(width: 340)
                .fixedSize(horizontal: false, vertical: true)
                .onExitCommand { presented = false }
        }
    }
}

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
