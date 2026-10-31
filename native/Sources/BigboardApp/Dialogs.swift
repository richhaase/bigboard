import SwiftUI

struct WorkspacePanel: View {
    var panel: Panel
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    var title: String { switch panel { case .settings: "Workspace settings"; case .filters: "Filters"; case .status: "Data status"; case .help: "Keyboard guide"; case .identities: "Contributor identities"; case .repositories: "Repository controls" } }
    var body: some View {
        VStack(spacing: 0) {
            HStack { Text(title).font(.title2.bold()); Spacer(); Button("Done") { dismiss() }.keyboardShortcut(.cancelAction) }.padding(20)
            Divider()
            if !workspace.error.isEmpty { Text(workspace.error).foregroundStyle(.orange).textSelection(.enabled).padding(12) }
            if workspace.busy { ProgressView().controlSize(.small).padding(8) }
            Group {
                switch panel {
                case .settings: SettingsPanel(workspace: workspace)
                case .filters: FiltersPanel(workspace: workspace, navigation: navigation)
                case .status: StatusPanel(workspace: workspace)
                case .help: HelpPanel()
                case .identities: IdentitiesPanel(workspace: workspace)
                case .repositories: RepositoriesPanel(workspace: workspace)
                }
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
        }.frame(width: 780, height: 700)
    }
}
struct SettingsPanel: View {
    @Bindable var workspace: WorkspaceModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = WorkspaceConfiguration()
    @State private var selected: Set<String> = []
    @State private var discovered = false
    @State private var reviewing = false
    @State private var tab = "repositories"
    @State private var query = ""
    @State private var groupName = ""
    @State private var groupRepositories = ""
    @State private var areaRepo = ""
    @State private var areaName = ""
    @State private var areaPaths = ""
    var repositoryIDs: [String] { Set(workspace.discovered.map(\.id) + draft.repositories).sorted().filter { query.isEmpty || $0.localizedCaseInsensitiveContains(query) } }
    var body: some View {
        VStack(spacing: 0) {
            if reviewing { review }
            else {
                Picker("Settings section", selection: $tab) { Text("Repositories").tag("repositories"); Text("Groups & areas").tag("organization"); Text("Analysis").tag("analysis") }.pickerStyle(.segmented).padding()
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        if tab == "repositories" { repositories }
                        else if tab == "organization" { organization }
                        else { analysis }
                    }.padding(20)
                }
            }
            Divider()
            HStack {
                if reviewing { Button("Back") { reviewing = false } }
                Spacer()
                Button("Cancel") { dismiss() }
                Button(reviewing ? "Save workspace" : "Review changes") {
                    if reviewing {
                        prepare()
                        Task { await workspace.saveConfiguration(draft); if workspace.configuration == draft { dismiss() } }
                    } else { prepare(); reviewing = true }
                }.buttonStyle(.borderedProminent).disabled(workspace.busy || !workspace.authStatus.signedIn || selected.isEmpty || (Set(workspace.configuration.repositories) != selected && !discovered))
            }.padding()
        }.onAppear { draft = workspace.configuration; selected = Set(draft.repositories) }
    }
    private var repositories: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                VStack(alignment: .leading) { Text(workspace.authStatus.signedIn ? "Signed in as \(workspace.authStatus.login)" : "GitHub account").font(.headline); Text("Only repositories available through the Bigboard GitHub App are listed.").foregroundStyle(.secondary) }
                Spacer()
                if workspace.checkingSignIn { ProgressView("Checking saved sign-in…").controlSize(.small) }
                else if workspace.authStatus.signedIn { Button("Manage access…") { Task { await workspace.manageAccess() } }; Button("Sign out") { Task { await workspace.signOut(); dismiss() } } }
                else { Button("Sign in") { Task { await workspace.signIn() } }.disabled(!workspace.authStatus.configured) }
            }
            HStack {
                Button("Discover repositories", systemImage: "arrow.clockwise") { Task { await workspace.discover(); discovered = workspace.error.isEmpty; if discovered { selected.formIntersection(workspace.discovered.map(\.id)) } } }.disabled(workspace.busy || !workspace.authStatus.signedIn)
                Text("\(selected.count) selected").foregroundStyle(.secondary)
                Spacer()
                Button("Select visible") { selected.formUnion(repositoryIDs) }.disabled(!discovered)
                Button("Clear") { selected = [] }
            }
            TextField("Search repositories", text: $query).textFieldStyle(.roundedBorder)
            if repositoryIDs.isEmpty { Text("Discover repositories, select the ones to track, then review and save.").foregroundStyle(.secondary).padding(.vertical, 30) }
            ForEach(repositoryIDs, id: \.self) { id in Toggle(id, isOn: membership(id, $selected)).toggleStyle(.checkbox) }
        }
    }
    private var organization: some View {
        VStack(alignment: .leading, spacing: 14) {
            SectionTitle(title: "Repository groups", detail: "Switch groups in the toolbar")
            ForEach(draft.groups.keys.sorted(), id: \.self) { name in
                HStack { VStack(alignment: .leading) { Text(name).fontWeight(.medium); Text((draft.groups[name] ?? []).joined(separator: ", ")).font(.caption).foregroundStyle(.secondary) }; Spacer(); Button("Edit") { groupName = name; groupRepositories = (draft.groups[name] ?? []).joined(separator: "\n") }; Button("Remove", role: .destructive) { draft.groups.removeValue(forKey: name) } }
            }
            TextField("Group name", text: $groupName)
            TextField("Repository IDs, separated by commas", text: $groupRepositories)
            Button("Add or update group") { draft.groups[groupName.trimmingCharacters(in: .whitespaces)] = entries(groupRepositories); groupName = ""; groupRepositories = "" }.disabled(groupName.trimmingCharacters(in: .whitespaces).isEmpty || entries(groupRepositories).isEmpty)
            Divider()
            SectionTitle(title: "Work areas", detail: "Named groups of literal paths")
            ForEach(draft.workAreas.keys.sorted(), id: \.self) { repo in
                ForEach(Array((draft.workAreas[repo] ?? []).enumerated()), id: \.offset) { index, area in
                    HStack { VStack(alignment: .leading) { Text("\(repo) · \(area.name)").fontWeight(.medium); Text(area.paths.joined(separator: ", ")).font(.caption).foregroundStyle(.secondary) }; Spacer(); Button("Edit") { areaRepo = repo; areaName = area.name; areaPaths = area.paths.joined(separator: ", ") }; Button("Remove", role: .destructive) { draft.workAreas[repo]?.remove(at: index); if draft.workAreas[repo]?.isEmpty == true { draft.workAreas.removeValue(forKey: repo) } } }
                }
            }
            TextField("Repository owner/name", text: $areaRepo)
            TextField("Area name", text: $areaName)
            TextField("Paths, separated by commas (for example src/ui, docs)", text: $areaPaths)
            Button("Add or update area") {
                let repo = areaRepo.trimmingCharacters(in: .whitespaces), name = areaName.trimmingCharacters(in: .whitespaces)
                var areas = draft.workAreas[repo] ?? []; areas.removeAll { $0.name == name }; areas.append(WorkArea(name: name, paths: entries(areaPaths))); draft.workAreas[repo] = areas
                areaRepo = ""; areaName = ""; areaPaths = ""
            }.disabled(areaRepo.isEmpty || areaName.isEmpty || entries(areaPaths).isEmpty)
        }.textFieldStyle(.roundedBorder)
    }
    private var analysis: some View {
        VStack(alignment: .leading, spacing: 18) {
            Toggle("Use cached history only", isOn: $draft.cachedOnly)
            Text("Cached mode reads local history and leaves live PR context unavailable.").font(.caption).foregroundStyle(.secondary)
            Toggle("Include generated, vendored, and bulk-data line changes", isOn: $draft.allFiles)
            LabeledContent("Startup date range (days; 0 for all time)") { TextField("Days", value: $draft.sinceDays, format: .number).frame(width: 90) }
            LabeledContent("Pull-request request budget") { TextField("Requests", value: $draft.requestBudget, format: .number).frame(width: 90) }
            Picker("Startup contributor sort", selection: $draft.contributorSort) { Text("Lines changed").tag("change"); Text("Commits").tag("commits"); Text("Name").tag("name"); Text("Additions").tag("added"); Text("Removals").tag("removed"); Text("Net lines").tag("net"); Text("AI share").tag("ai"); Text("Recent").tag("recent") }
            stringList("Exclude repository patterns", values: $draft.exclude)
            stringList("Additional AI identities", values: $draft.aiIdentities)
            stringList("Additional bot identities", values: $draft.botIdentities)
            Text("Enter one value per line. Repository exclusions support glob patterns. Identity patterns are matched against author names and emails.").font(.caption).foregroundStyle(.secondary)
        }
    }
    private var review: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text("Review your workspace").font(.title2)
                Text("\(selected.count) repositories · \(draft.groups.count) groups · \(draft.workAreas.values.flatMap { $0 }.count) work areas").font(.headline)
                ForEach(selected.sorted(), id: \.self) { Text($0) }
                Divider()
                Text(draft.cachedOnly ? "Read cached history only" : "Refresh history and live pull requests")
                Text(draft.allFiles ? "Include all line changes" : "Exclude generated, vendored, and bulk-data line changes")
                Text("Startup range: \(draft.sinceDays == 0 ? "all time" : "\(draft.sinceDays) days") · Request budget: \(draft.requestBudget)")
                Text("Saving refreshes the workspace with these settings.").foregroundStyle(.secondary)
            }.padding(24).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
    private func prepare() {
        draft.repositories = selected.sorted()
        draft.exclude = entries(draft.exclude.joined(separator: "\n"))
        draft.aiIdentities = entries(draft.aiIdentities.joined(separator: "\n"))
        draft.botIdentities = entries(draft.botIdentities.joined(separator: "\n"))
        if discovered { draft.excludedRepositories = Set(workspace.discovered.map(\.id)).subtracting(selected).sorted() }
    }
    private func stringList(_ label: String, values: Binding<[String]>) -> some View { VStack(alignment: .leading) { Text(label); TextEditor(text: Binding(get: { values.wrappedValue.joined(separator: "\n") }, set: { values.wrappedValue = $0.components(separatedBy: .newlines) })).font(.body.monospaced()).frame(height: 65).border(.quaternary) } }
}
func membership(_ value: String, _ values: Binding<Set<String>>) -> Binding<Bool> { Binding(get: { values.wrappedValue.contains(value) }, set: { if $0 { values.wrappedValue.insert(value) } else { values.wrappedValue.remove(value) } }) }
func entries(_ text: String) -> [String] { Array(Set(text.components(separatedBy: CharacterSet(charactersIn: ",\n")).map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })).sorted() }

struct FiltersPanel: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    @State private var filters = BoardFilters()
    @State private var days = 14
    @State private var since = false
    @State private var repositories: Set<String> = []
    var body: some View {
        VStack {
            Form {
                Section("Shared scope") {
                    Toggle("Since last look", isOn: $since).disabled(workspace.snapshot.bookmark == nil)
                    LabeledContent("Last days (0 for all time)") { TextField("Days", value: $days, format: .number).frame(width: 100).disabled(since) }
                    DisclosureGroup("Repositories (\(repositories.isEmpty ? "all" : "\(repositories.count)"))") {
                        Button("All repositories") { repositories = [] }
                        ForEach(workspace.snapshot.repositories) { repo in Toggle(repo.id, isOn: membership(repo.id, $repositories)) }
                    }
                }
                Section(navigation.board.rawValue) {
                    TextField("Search", text: $filters.query)
                    Picker("Sort", selection: $filters.sort) {
                        if navigation.board == .pullRequests { Text("Updated").tag("updated"); Text("Repository").tag("repository"); Text("Author").tag("author"); Text("Title").tag("title"); Text("Attention").tag("attention") }
                        else { Text("Lines changed").tag("change"); Text("Commits").tag("commits"); Text("Name").tag("name"); Text("Recent").tag("recent"); if navigation.board == .contributors { Text("Additions").tag("added"); Text("Removals").tag("removed"); Text("Net lines").tag("net"); Text("AI share").tag("ai") } }
                    }
                    Toggle("Reverse sort", isOn: $filters.reversed)
                    if navigation.board != .pullRequests {
                        Toggle("Hide bots", isOn: $filters.hideBots)
                        DisclosureGroup("People (\(filters.people.isEmpty ? "all" : "\(filters.people.count)"))") {
                            Button("All people") { filters.people = [] }
                            ForEach(Analysis.people(workspace.snapshot.commits)) { person in Toggle("\(person.name) · \(person.email)", isOn: membership(person.id, $filters.people)) }
                        }
                    } else { Text("Review, check, author, and attention facets are also available above the PR list.").foregroundStyle(.secondary) }
                }
            }.formStyle(.grouped)
            HStack { Button("Reset this board") { filters = BoardFilters(sort: filters.sort, reversed: filters.reversed) }; Button("Reset shared scope") { days = workspace.configuration.sinceDays; since = false; repositories = [] }; Spacer(); Button("Cancel") { dismiss() }; Button("Apply") { navigation.currentFilters = filters; navigation.days = max(0, min(3650, days)); navigation.sinceLastLook = since; navigation.repositories = repositories; dismiss() }.buttonStyle(.borderedProminent) }.padding()
        }.onAppear { filters = navigation.currentFilters; days = navigation.days; since = navigation.sinceLastLook; repositories = navigation.repositories }
    }
}
struct StatusPanel: View {
    @Bindable var workspace: WorkspaceModel
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack { Text(workspace.snapshot.coverage.phase.capitalized).font(.title2.bold()); Spacer(); Button("Refresh") { Task { await workspace.refresh() } }.disabled(workspace.busy) }
                Text(workspace.snapshot.coverage.message)
                LabeledContent("Snapshot", value: workspace.snapshot.generatedAt.formatted())
                LabeledContent("Since-last-look baseline", value: workspace.snapshot.bookmark?.formatted() ?? "Not established yet")
                Text("A successful workspace snapshot becomes the next baseline when the app quits normally.").font(.caption).foregroundStyle(.secondary)
                LabeledContent("Requests", value: "\(workspace.snapshot.coverage.probeRequests) probes · \(workspace.snapshot.coverage.inventoryRequests) inventory · \(workspace.snapshot.coverage.fileRequests) files · \(workspace.snapshot.coverage.accountRequests) account")
                SectionTitle(title: "Repository evidence")
                ForEach(workspace.snapshot.repositories) { repo in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack { Text(repo.id).fontWeight(.medium); Spacer(); Text(repo.stale ? "Stale history" : "History loaded").foregroundStyle(repo.stale ? .orange : .secondary) }
                        Text("History: \(repo.scannedAt?.formatted() ?? "unknown") · PRs: \(repo.prCheckedAt?.formatted() ?? "unknown")").font(.caption)
                        Text("PR inventory \(repo.prInventoryComplete ? "complete" : "incomplete") · files \(repo.prFilesComplete ? "complete" : "incomplete") · reviewers \(repo.prReviewersComplete ? "complete" : "incomplete")\(repo.prStale ? " · stale" : "")").font(.caption).foregroundStyle(.secondary)
                        if !repo.error.isEmpty { Text(repo.error).font(.caption).foregroundStyle(.orange).textSelection(.enabled) }
                    }
                    Divider()
                }
                if !workspace.snapshot.coverage.errors.isEmpty { SectionTitle(title: "Issues"); ForEach(Array(workspace.snapshot.coverage.errors.enumerated()), id: \.offset) { _, error in Text(error).foregroundStyle(.orange).textSelection(.enabled) } }
                SectionTitle(title: "Local storage")
                Text(workspace.storageDescription).font(.callout.monospaced()).textSelection(.enabled)
            }.padding(24)
        }
    }
}
struct RepositoriesPanel: View {
    @Bindable var workspace: WorkspaceModel
    @State private var selected: Set<String> = []
    @State private var removal: String?
    var ids: [String] { Set(workspace.discovered.map(\.id) + workspace.snapshot.repositories.map(\.id) + workspace.configuration.repositories).sorted() }
    var body: some View {
        VStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Session tracking").font(.headline)
                    Text("Choose which repositories load during this session. Use Settings to change your saved workspace.").foregroundStyle(.secondary)
                    ForEach(ids, id: \.self) { Toggle($0, isOn: membership($0, $selected)).toggleStyle(.checkbox) }
                    Button("Apply session tracking") { Task { await workspace.setTracked(selected) } }.disabled(workspace.busy)
                    SectionTitle(title: "Unlisted caches")
                    Text("Caches can be removed after live access confirms that a repository is no longer available to the app. Removing a cache does not change the repository on GitHub.").font(.caption).foregroundStyle(.secondary)
                    ForEach(workspace.cachedRepositoryRows.filter { workspace.removableCacheIDs.contains($0.id) }) { repo in HStack { Text(repo.id); Spacer(); Button("Remove cache…", role: .destructive) { removal = repo.id }.disabled(workspace.busy) } }
                    if workspace.removableCacheIDs.isEmpty { Text("No removable caches.").foregroundStyle(.secondary) }
                }.padding(24)
            }
        }.onAppear { selected = Set(ids).subtracting(workspace.sessionExcluded) }
        .confirmationDialog("Remove local cache?", isPresented: Binding(get: { removal != nil }, set: { if !$0 { removal = nil } }), titleVisibility: .visible) {
            if let id = removal { Button("Remove \(id)", role: .destructive) { Task { await workspace.removeCache(id) }; removal = nil } }
        } message: { Text("The local mirror will be deleted. It can be downloaded again if access is restored.") }
    }
}
struct IdentitiesPanel: View {
    @Bindable var workspace: WorkspaceModel
    @Environment(\.dismiss) private var dismiss
    @State private var rules: [String: IdentityRule] = [:]
    @State private var selected: Set<String> = []
    @State private var primary = ""
    @State private var name = ""
    @State private var custom = ""
    @State private var query = ""
    @State private var reviewing = false
    var emails: [String] { Set(workspace.snapshot.commits.map(\.email).filter { !$0.isEmpty } + rules.keys + rules.values.flatMap(\.aliases) + entries(custom)).sorted() }
    var body: some View {
        VStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text("Combine email identities belonging to the same person. Saved identities apply to history across every repository.").foregroundStyle(.secondary)
                    if reviewing {
                        Text("Review identity changes").font(.title2)
                        ForEach(rules.keys.sorted(), id: \.self) { key in Text("\(rules[key]?.name.isEmpty == false ? rules[key]!.name : key) · \(key)\nAliases: \((rules[key]?.aliases ?? []).joined(separator: ", "))") }
                    } else {
                        SectionTitle(title: "Saved identities")
                        ForEach(rules.keys.sorted(), id: \.self) { key in HStack { VStack(alignment: .leading) { Text(rules[key]?.name.isEmpty == false ? rules[key]!.name : key); Text(([key] + (rules[key]?.aliases ?? [])).joined(separator: ", ")).font(.caption).foregroundStyle(.secondary) }; Spacer(); Button("Edit") { primary = key; selected = Set([key] + (rules[key]?.aliases ?? [])); name = rules[key]?.name ?? "" }; Button("Unlink", role: .destructive) { rules.removeValue(forKey: key) } } }
                        Divider()
                        TextField("Search emails", text: $query).textFieldStyle(.roundedBorder)
                        ForEach(emails.filter { query.isEmpty || $0.localizedCaseInsensitiveContains(query) }, id: \.self) { email in Toggle(email, isOn: membership(email, $selected)).toggleStyle(.checkbox) }
                        TextField("Add another email (or comma-separated emails)", text: $custom).textFieldStyle(.roundedBorder)
                        Picker("Primary email", selection: $primary) { Text("Choose an email").tag(""); ForEach(selected.sorted(), id: \.self) { Text($0).tag($0) } }
                        TextField("Display name", text: $name).textFieldStyle(.roundedBorder)
                        Button("Stage identity") { stage() }.disabled(primary.isEmpty || !selected.contains(primary))
                    }
                }.padding(24)
            }
            HStack { if reviewing { Button("Back") { reviewing = false } }; Spacer(); Button("Cancel") { dismiss() }; Button(reviewing ? "Save identities" : "Review changes") { if reviewing { Task { await workspace.saveIdentities(rules); if workspace.configuration.identities == rules { dismiss() } } } else { reviewing = true } }.buttonStyle(.borderedProminent).disabled(workspace.busy || rules == workspace.configuration.identities) }.padding()
        }.onAppear { rules = workspace.configuration.identities }
    }
    private func stage() {
        var merged = selected
        for (key, rule) in rules where !Set([key] + rule.aliases).isDisjoint(with: selected) { merged.formUnion([key] + rule.aliases); rules.removeValue(forKey: key) }
        rules[primary] = IdentityRule(aliases: merged.subtracting([primary]).sorted(), name: name.trimmingCharacters(in: .whitespacesAndNewlines))
        selected = []; primary = ""; name = ""; custom = ""
    }
}
struct HelpPanel: View {
    let commands: [(String, String)] = [("A / C / P", "Activity / Contributors / Pull requests"), ("⌘R or R", "Refresh repository evidence"), ("/", "Focus search"), ("F or T", "Open filters"), ("E", "Data status"), ("I", "Repository controls"), ("⌘, or ,", "Workspace settings"), ("? or F1", "Keyboard guide"), ("X", "Reset this board’s filters"), ("Y", "Copy the current view as Markdown"), ("B", "Show or hide bots"), ("S / Shift-S", "Cycle sort / reverse sort order"), ("Return / →", "Directories: explore the selected row"), ("O", "Open the selected path or PR on GitHub"), ("1 / 2 / 3 / 4 / 5", "Activity: commits / people / related / directories / heat"), ("Escape or ←", "Move back through Activity context"), ("V", "Activity: open repository pull requests"), ("M", "Activity: switch related evidence; Contributors: edit identities; PRs: needs attention"), ("Shift-M / N", "PRs: authored by me / review requested")]
    var body: some View {
        ScrollView { VStack(alignment: .leading, spacing: 14) { Text("Shortcuts work when you are not editing text. Use standard macOS list navigation, window controls, and menu commands throughout the app.").foregroundStyle(.secondary); ForEach(commands, id: \.0) { key, action in HStack { Text(key).font(.body.monospaced()).frame(width: 180, alignment: .leading); Text(action); Spacer() }; Divider() }; Text("Heat compares each area with its own recent history. Contribution totals describe changes; they do not measure productivity. Pull-request evidence may be partial; Data status explains coverage.").foregroundStyle(.secondary) }.padding(24) }
    }
}
