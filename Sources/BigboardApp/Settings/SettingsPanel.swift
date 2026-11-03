import SwiftUI

struct SettingsPanel: View {
    private enum Section {
        case repositories, organization, general, advanced, storage
    }

    @Bindable var workspace: WorkspaceModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = WorkspaceConfiguration()
    @State private var selected: Set<String> = []
    @State private var applying = false
    @State private var initialized = false
    @State private var repositorySort = SettingsRepositorySort.recent
    @State private var section = Section.repositories
    @State private var query = ""
    @State private var groupEditor = SettingsGroupEditor()
    @State private var areaEditor = SettingsWorkAreaEditor()
    private var repositoryRows: [Repository] {
        SettingsRepositories.rows(
            available: workspace.discovered, saved: Array(Set(draft.repositories).union(selected)),
            sort: repositorySort, query: query)
    }
    private var availableIDs: Set<String> { Set(workspace.discovered.map(\.id)) }
    private var selectableVisibleIDs: Set<String> { Set(repositoryRows.map(\.id)).intersection(availableIDs) }
    var body: some View {
        VStack(spacing: 0) {
            Picker("Settings section", selection: $section) {
                Text("Repositories").tag(Section.repositories)
                Text("Groups & areas").tag(Section.organization)
                Text("General").tag(Section.general)
                Text("Advanced").tag(Section.advanced)
                Text("Storage").tag(Section.storage)
            }
            .pickerStyle(.segmented).padding()
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    switch section {
                    case .repositories: repositories
                    case .organization: organization
                    case .general: general
                    case .advanced: advanced
                    case .storage: StorageSettingsPanel(workspace: workspace)
                    }
                }
                .padding(20)
            }
            .disabled(applying)
            Divider()
            HStack {
                if applying { ProgressView("Applying settings…").controlSize(.small) }
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction).disabled(applying)
                if section != .storage {
                    Button("Apply") {
                        let candidate = preparedConfiguration()
                        applying = true
                        Task {
                            let saved = await workspace.saveConfiguration(candidate)
                            applying = false
                            if saved { dismiss() }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(
                        applying || workspace.busy || !workspace.authStatus.signedIn || selected.isEmpty)
                }
            }
            .padding()
        }
        .interactiveDismissDisabled(applying)
        .onAppear {
            guard !initialized else { return }
            initialized = true
            draft = workspace.configuration
            selected = Set(draft.repositories)
        }
    }
    private var repositories: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                VStack(alignment: .leading) {
                    Text(
                        workspace.authStatus.signedIn
                            ? "Signed in as \(workspace.authStatus.login)" : "GitHub account"
                    )
                    .font(.headline)
                    Text("Only repositories available through the Bigboard GitHub App are listed.")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if workspace.checkingSignIn {
                    ProgressView("Checking saved sign-in…").controlSize(.small)
                } else if workspace.authStatus.signedIn {
                    Button("Manage access…") { Task { await workspace.manageAccess() } }
                    Button("Sign out") { Task { if await workspace.signOut() { dismiss() } } }
                        .disabled(workspace.signingOut)
                } else {
                    Button("Sign in") { Task { await workspace.signIn() } }
                        .disabled(!workspace.authStatus.configured)
                }
            }
            HStack {
                Button("Refresh repository list", systemImage: "arrow.clockwise") {
                    Task { await workspace.discover() }
                }
                .disabled(workspace.busy || !workspace.authStatus.signedIn)
                Text("\(selected.count) selected").foregroundStyle(.secondary)
                Spacer()
                Button(query.isEmpty ? "Select all" : "Select visible") {
                    selected.formUnion(selectableVisibleIDs)
                }
                .disabled(selectableVisibleIDs.isEmpty)
                Button("Clear selection") { selected = [] }.disabled(selected.isEmpty)
            }
            HStack {
                TextField("Search repositories", text: $query).textFieldStyle(.roundedBorder)
                Picker("Sort", selection: $repositorySort) {
                    Text("Recent push").tag(SettingsRepositorySort.recent)
                    Text("Name").tag(SettingsRepositorySort.name)
                }
                .frame(width: 180)
            }
            if repositoryRows.isEmpty {
                Text("Refresh the repository list, choose what to track, then apply your settings.")
                    .foregroundStyle(.secondary).padding(.vertical, 30)
            }
            ForEach(repositoryRows) { repo in
                HStack(alignment: .top) {
                    Toggle(repo.id, isOn: membership(repo.id, $selected)).toggleStyle(.checkbox)
                    Spacer()
                    VStack(alignment: .trailing, spacing: 3) {
                        if let pushed = repo.pushedAt {
                            Text("Last push \(pushed.formatted(date: .abbreviated, time: .omitted))")
                        } else {
                            Text("Push date unavailable")
                        }
                        if !availableIDs.contains(repo.id) {
                            Text("Not in current access list").foregroundStyle(.orange)
                        }
                    }
                    .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
    private var organization: some View {
        VStack(alignment: .leading, spacing: 14) {
            SectionTitle(title: "Repository groups", detail: "Switch groups in the toolbar")
            ForEach(draft.groups.keys.sorted(), id: \.self) { name in
                HStack {
                    VStack(alignment: .leading) {
                        Text(name).fontWeight(.medium)
                        Text((draft.groups[name] ?? []).joined(separator: ", ")).font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Edit") { groupEditor.edit(name: name, repositories: draft.groups[name] ?? []) }
                    Button("Remove", role: .destructive) {
                        groupEditor.remove(name: name, from: &draft.groups)
                    }
                }
            }
            TextField("Group name", text: $groupEditor.name)
            TextField("Repository IDs, separated by commas", text: $groupEditor.repositories)
            if !groupEditor.error.isEmpty { Text(groupEditor.error).font(.callout).foregroundStyle(.orange) }
            HStack {
                Button(groupEditor.isEditing ? "Update group" : "Add or update group") {
                    groupEditor.save(to: &draft.groups)
                }
                .disabled(!groupEditor.canSave)
                if groupEditor.isEditing { Button("Cancel edit") { groupEditor.reset() } }
            }
            Divider()
            SectionTitle(title: "Work areas", detail: "Named groups of literal paths")
            ForEach(draft.workAreas.keys.sorted(), id: \.self) { repo in
                ForEach(draft.workAreas[repo] ?? [], id: \.name) { area in
                    HStack {
                        VStack(alignment: .leading) {
                            Text("\(repo) · \(area.name)").fontWeight(.medium)
                            Text(area.paths.joined(separator: ", ")).font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("Edit") { areaEditor.edit(repository: repo, area: area) }
                        Button("Remove", role: .destructive) {
                            areaEditor.remove(repository: repo, name: area.name, from: &draft.workAreas)
                        }
                    }
                }
            }
            TextField("Repository owner/name", text: $areaEditor.repository)
            TextField("Area name", text: $areaEditor.name)
            TextField("Paths, separated by commas (for example src/ui, docs)", text: $areaEditor.paths)
            if !areaEditor.error.isEmpty { Text(areaEditor.error).font(.callout).foregroundStyle(.orange) }
            HStack {
                Button(areaEditor.isEditing ? "Update area" : "Add or update area") {
                    areaEditor.save(to: &draft.workAreas)
                }
                .disabled(!areaEditor.canSave)
                if areaEditor.isEditing { Button("Cancel edit") { areaEditor.reset() } }
            }
        }
        .textFieldStyle(.roundedBorder)
    }
    private var general: some View {
        VStack(alignment: .leading, spacing: 18) {
            SectionTitle(
                title: "Startup defaults", detail: "Choose the initial view when opening a workspace")
            LabeledContent("Date range (days; 0 for all time)") {
                TextField("Days", value: $draft.sinceDays, format: .number).frame(width: 90)
            }
            Picker("Contributor sort", selection: $draft.contributorSort) {
                Text("Lines changed").tag("change")
                Text("Commits").tag("commits")
                Text("Name").tag("name")
                Text("Additions").tag("added")
                Text("Removals").tag("removed")
                Text("Net lines").tag("net")
                Text("AI share").tag("ai")
                Text("Recent").tag("recent")
            }
            Divider()
            Toggle("Use cached history only", isOn: $draft.cachedOnly)
            Text(
                "Read local history without refreshing GitHub. Live pull-request information is unavailable in this mode."
            )
            .font(.caption).foregroundStyle(.secondary)
        }
    }
    private var advanced: some View {
        VStack(alignment: .leading, spacing: 18) {
            Toggle("Include generated, vendored, and bulk-data line changes", isOn: $draft.allFiles)
            Text("These changes are excluded from line totals by default; commits remain visible.")
                .font(.caption).foregroundStyle(.secondary)
            LabeledContent("Pull-request request budget") {
                TextField("Requests", value: $draft.requestBudget, format: .number).frame(width: 90)
            }
            Text("Maximum API requests for pull-request collection during a refresh.").font(.caption)
                .foregroundStyle(.secondary)
            stringList("Exclude repository patterns", values: $draft.exclude)
            stringList("Additional AI identities", values: $draft.aiIdentities)
            stringList("Additional bot identities", values: $draft.botIdentities)
            Text(
                "Enter one rule per line. Repository glob patterns exclude matches from your saved selection. AI rules use author/coauthor email addresses or @domain; bot rules also accept exact names."
            )
            .font(.caption).foregroundStyle(.secondary)
        }
    }
    private func preparedConfiguration() -> WorkspaceConfiguration {
        var candidate = SettingsRepositories.applying(selected, available: availableIDs, to: draft)
        candidate.exclude = parseListEntries(draft.exclude.joined(separator: "\n"))
        candidate.aiIdentities = parseListEntries(draft.aiIdentities.joined(separator: "\n"))
        candidate.botIdentities = parseListEntries(draft.botIdentities.joined(separator: "\n"))
        return candidate
    }
    private func stringList(_ label: String, values: Binding<[String]>) -> some View {
        VStack(alignment: .leading) {
            Text(label)
            TextEditor(
                text: Binding(
                    get: { values.wrappedValue.joined(separator: "\n") },
                    set: { values.wrappedValue = $0.components(separatedBy: .newlines) })
            )
            .font(.body.monospaced()).frame(height: 65).border(.quaternary)
        }
    }
}


private struct StorageSettingsPanel: View {
    @Bindable var workspace: WorkspaceModel
    @State private var removal: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SectionTitle(title: "Local storage")
            Text(workspace.storageDescription).font(.callout.monospaced()).textSelection(.enabled)
            SectionTitle(title: "Unlisted caches")
            Text(
                "Caches can be removed after live access confirms that a repository is no longer available to the app. Removing a cache does not change the repository on GitHub."
            )
            .font(.caption).foregroundStyle(.secondary)
            ForEach(workspace.cachedRepositoryRows.filter { workspace.removableCacheIDs.contains($0.id) }) {
                repo in
                HStack {
                    Text(repo.id)
                    Spacer()
                    Button("Remove cache…", role: .destructive) { removal = repo.id }.disabled(workspace.busy)
                }
            }
            if workspace.removableCacheIDs.isEmpty {
                Text("No removable caches.").foregroundStyle(.secondary)
            }
        }
        .confirmationDialog(
            "Remove local cache?",
            isPresented: Binding(get: { removal != nil }, set: { if !$0 { removal = nil } }),
            titleVisibility: .visible
        ) {
            if let id = removal {
                Button("Remove \(id)", role: .destructive) {
                    Task { await workspace.removeCache(id) }
                    removal = nil
                }
            }
        } message: {
            Text("The local mirror will be deleted. It can be downloaded again if access is restored.")
        }
    }
}

func parseListEntries(_ text: String) -> [String] {
    Array(
        Set(
            text.components(separatedBy: CharacterSet(charactersIn: ",\n"))
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
    )
    .sorted()
}
