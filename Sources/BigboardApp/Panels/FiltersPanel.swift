import SwiftUI

struct FiltersPanel: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    @State private var filters = BoardFilters()
    @State private var dateRange = DateRange.days(14)
    @State private var repositories: Set<String> = []
    var body: some View {
        VStack {
            Form {
                Section("Shared scope") {
                    Picker("Date range", selection: $dateRange) {
                        ForEach(DateRange.presets, id: \.self) { range in
                            Text(range.label).tag(range)
                        }
                        if !DateRange.presets.contains(dateRange) {
                            Text(dateRange.label).tag(dateRange)
                        }
                    }
                    if case .days(let days) = dateRange {
                        LabeledContent("Custom days (0 for all time)") {
                            TextField("Days", value: Binding(
                                get: { days },
                                set: { dateRange = .days(max(0, min(3650, $0))) }
                            ), format: .number).frame(width: 100)
                        }
                    }
                    DisclosureGroup(
                        "Repository filter (\(repositories.isEmpty ? "all" : "\(repositories.count) selected"))"
                    ) {
                        RepositoryFilterSelection(
                            repositories: workspace.snapshot.repositories, selection: $repositories)
                    }
                }
                Section(navigation.board.rawValue) {
                    TextField("Search", text: $filters.query)
                    if navigation.board != .heat {
                        Picker("Sort", selection: $filters.sort) {
                            if navigation.board == .pullRequests {
                                Text("Updated").tag("updated")
                                Text("Repository").tag("repository")
                                Text("Author").tag("author")
                                Text("Title").tag("title")
                                Text("Attention").tag("attention")
                            } else {
                                Text("Lines changed").tag("change")
                                Text("Commits").tag("commits")
                                Text("Name").tag("name")
                                Text("Recent").tag("recent")
                                if navigation.board == .contributors {
                                    Text("Additions").tag("added")
                                    Text("Removals").tag("removed")
                                    Text("Net lines").tag("net")
                                    Text("AI share").tag("ai")
                                }
                            }
                        }
                        Toggle("Reverse sort", isOn: $filters.reversed)
                    }
                    if navigation.board != .pullRequests {
                        Toggle("Hide bots", isOn: $filters.hideBots)
                        DisclosureGroup(
                            "People (\(filters.people.isEmpty ? "all" : "\(filters.people.count)"))"
                        ) {
                            Button("All people") { filters.people = [] }
                            ForEach(Analysis.people(workspace.snapshot.commits)) { person in
                                Toggle(
                                    "\(person.name) · \(person.email)",
                                    isOn: membership(person.id, $filters.people))
                            }
                        }
                    } else {
                        Text(
                            "Review, check, author, and attention facets are also available above the PR list."
                        )
                        .foregroundStyle(.secondary)
                    }
                }
            }
            .formStyle(.grouped)
            HStack {
                Button("Reset this board") {
                    filters = BoardFilters(sort: filters.sort, reversed: filters.reversed)
                }
                Button("Reset shared scope") {
                    dateRange = .days(workspace.configuration.sinceDays)
                    repositories = []
                }
                Spacer()
                Button("Cancel") { dismiss() }
                Button("Apply") {
                    navigation.currentFilters = filters
                    navigation.dateRange = dateRange
                    navigation.setRepositoryFilter(repositories, available: workspace.snapshot.repositories)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
            }
            .padding()
        }
        .onAppear {
            filters = navigation.currentFilters
            dateRange = navigation.dateRange
            repositories = navigation.repositories
        }
    }
}
