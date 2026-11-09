import SwiftUI

struct SettingsView: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @State private var draft = WorkspaceConfiguration()
    @State private var selected: Set<String> = []
    @State private var saving = false
    @State private var initialized = false
    @State private var repositorySort = SettingsRepositorySort.recent
    @State private var query = ""
    @State private var groupEditor = SettingsGroupEditor()
    @State private var areaEditor = SettingsWorkAreaEditor()
    @State private var baseline = WorkspaceConfiguration()
    @State private var opened = WorkspaceConfiguration()
    @State private var saveError = ""
    private var hasChanges: Bool { preparedConfiguration() != baseline }
    private var repositoryRows: [Repository] {
        SettingsRepositories.rows(
            available: workspace.discovered, saved: Array(Set(draft.repositories).union(selected)),
            sort: repositorySort, query: query)
    }
    private var availableIDs: Set<String> { Set(workspace.discovered.map(\.id)) }
    private var selectableVisibleIDs: Set<String> { Set(repositoryRows.map(\.id)).intersection(availableIDs) }
    var body: some View {
        TabView(selection: $navigation.settingsTab) {
            Tab("Repositories", systemImage: "shippingbox", value: SettingsTab.repositories) {
                pane { repositories }
            }
            Tab("Groups & Areas", systemImage: "square.grid.2x2", value: SettingsTab.organization) {
                pane { organization }
            }
            Tab("General", systemImage: "gearshape", value: SettingsTab.general) {
                pane { general }
            }
            Tab("Advanced", systemImage: "slider.horizontal.3", value: SettingsTab.advanced) {
                pane { advanced }
            }
            Tab("Storage", systemImage: "internaldrive", value: SettingsTab.storage) {
                pane { StorageSettingsPanel(workspace: workspace) }
            }
        }
        .frame(width: 720, height: 580)
        .task(id: preparedConfiguration()) {
            guard initialized, hasChanges else { return }
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            await save()
        }
        .onAppear {
            if !initialized || !hasChanges || workspace.configuration != opened { load() }
            initialized = true
        }
        .onDisappear {
            _ = commitPendingEditors()
            if hasChanges { Task { await save() } }
        }
        .onChange(of: workspace.configuration) { _, _ in
            if !saving, !hasChanges { load() }
        }
    }
    private func pane(@ViewBuilder _ content: () -> some View) -> some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) { content() }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(20)
            }
            Divider()
            status.padding(.horizontal, 20).padding(.vertical, 10)
        }
    }
    private var status: some View {
        HStack(spacing: 8) {
            if saving {
                ProgressView().controlSize(.small)
                Text("Saving and refreshing…")
            } else if !saveError.isEmpty {
                Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange)
                Text(saveError).textSelection(.enabled)
            } else if !workspace.authStatus.signedIn {
                Text("Connect the GitHub CLI to save changes.")
            } else if selected.isEmpty {
                Text("Select at least one repository to save changes.")
            } else if hasChanges {
                Text("Saving shortly…")
            } else {
                Text("Changes save automatically and start a refresh.")
            }
            Spacer()
        }
        .font(.callout).foregroundStyle(.secondary)
    }
    private func save() async {
        while saving { try? await Task.sleep(for: .milliseconds(200)) }
        let candidate = preparedConfiguration()
        guard candidate != opened, workspace.authStatus.signedIn, !candidate.repositories.isEmpty else { return }
        saving = true
        let saved = await workspace.saveConfiguration(candidate, basedOn: opened)
        saving = false
        if saved {
            opened = candidate
            baseline = candidate
            saveError = ""
        } else {
            saveError = workspace.error
            workspace.error = ""
        }
    }
    private func load() {
        opened = workspace.configuration
        draft = opened
        selected = Set(draft.repositories)
        baseline = preparedConfiguration()
    }
    private var repositories: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                VStack(alignment: .leading) {
                    Text(
                        workspace.authStatus.signedIn
                            ? "GitHub CLI: \(workspace.authStatus.login)" : "GitHub CLI not connected"
                    )
                    .font(.headline)
                    Text("GitHub clones in your source folders are listed. Bigboard only fetches their default branch.")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if workspace.checkingSignIn {
                    ProgressView("Checking GitHub CLI…").controlSize(.small)
                } else {
                    Button("Check Again") { Task { await workspace.checkConnection() } }
                }
            }
            sourceFolders
            HStack {
                Button("Refresh repository list", systemImage: "arrow.clockwise") {
                    Task { await workspace.discover(roots: draft.sourceRoots) }
                }
                .disabled(saving || !workspace.authStatus.signedIn || draft.sourceRoots.isEmpty)
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
                    Text("Latest commit").tag(SettingsRepositorySort.recent)
                    Text("Name").tag(SettingsRepositorySort.name)
                }
                .frame(width: 180)
            }
            if repositoryRows.isEmpty {
                Text("No GitHub clones found yet. Check your source folders, refresh the list, then choose what to track.")
                    .foregroundStyle(.secondary).padding(.vertical, 30)
            }
            ForEach(repositoryRows) { repo in
                HStack(alignment: .top) {
                    Toggle(repo.id, isOn: membership(repo.id, $selected)).toggleStyle(.checkbox)
                    Spacer()
                    VStack(alignment: .trailing, spacing: 3) {
                        if let pushed = repo.pushedAt {
                            Text("Latest commit \(pushed.formatted(date: .abbreviated, time: .omitted))")
                        }
                        if !repo.path.isEmpty { Text(SettingsRepositories.displayPath(repo.path)) }
                        if repo.shallow { Text("Shallow clone: partial history").foregroundStyle(.orange) }
                        if repo.partial { Text("Partial clone: line counts unknown").foregroundStyle(.orange) }
                        if !availableIDs.contains(repo.id) {
                            Text("No local clone found").foregroundStyle(.orange)
                        }
                    }
                    .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
    private var sourceFolders: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Source folders").font(.subheadline.bold())
                Spacer()
                Button("Add folder…", systemImage: "plus") {
                    for folder in SettingsRepositories.chooseFolders() where !draft.sourceRoots.contains(folder) {
                        draft.sourceRoots.append(folder)
                    }
                }
            }
            if draft.sourceRoots.isEmpty {
                Text("No source folders. Add the folders that hold your Git clones.")
                    .font(.caption).foregroundStyle(.orange)
            }
            ForEach(draft.sourceRoots, id: \.self) { folder in
                HStack {
                    Text(SettingsRepositories.displayPath(folder)).font(.callout.monospaced())
                    Spacer()
                    Button("Remove", systemImage: "minus.circle") {
                        draft.sourceRoots.removeAll { $0 == folder }
                    }
                    .labelStyle(.iconOnly).buttonStyle(.borderless).help("Remove \(folder)")
                    .accessibilityLabel("Remove \(folder)")
                }
            }
        }
        .padding(10).background(.quaternary.opacity(0.4), in: .rect(cornerRadius: 8))
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
            Picker("Automatically refresh", selection: $draft.refreshIntervalMinutes) {
                Text("Manually").tag(0)
                ForEach([5, 15, 30, 60], id: \.self) { minutes in
                    Text("Every \(minutes) minutes").tag(minutes)
                }
            }
            .disabled(draft.cachedOnly)
            Text("Refresh while Bigboard is visible. If overdue, refresh when you return. Command-R refreshes immediately.")
                .font(.caption).foregroundStyle(.secondary)
            Toggle("Use cached history only", isOn: $draft.cachedOnly)
            Text(
                "Read local history without refreshing GitHub. Live pull-request information is unavailable in this mode."
            )
            .font(.caption).foregroundStyle(.secondary)
        }
    }
    private var advanced: some View {
        VStack(alignment: .leading, spacing: 18) {
            Toggle("Include generated, vendored, fixture, and bulk-data line changes", isOn: $draft.allFiles)
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
    private func commitPendingEditors() -> Bool {
        if groupEditor.hasPendingInput, !groupEditor.save(to: &draft.groups) { return false }
        if areaEditor.hasPendingInput, !areaEditor.save(to: &draft.workAreas) { return false }
        return true
    }
    private func preparedConfiguration() -> WorkspaceConfiguration {
        var candidate = draft
        candidate.repositories = selected.sorted()
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
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SectionTitle(title: "Local storage")
            Text(workspace.storageDescription).font(.callout.monospaced()).textSelection(.enabled)
            Text(
                "History is read from your own clones. Bigboard only updates their remote-tracking default branch; it never changes local branches, the index, or working files. Parsed history caches can be rebuilt."
            )
            .font(.caption).foregroundStyle(.secondary)
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
