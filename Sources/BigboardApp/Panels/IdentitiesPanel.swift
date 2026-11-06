import SwiftUI

struct IdentitiesPanel: View {
    @Bindable var workspace: WorkspaceModel
    @Environment(\.dismiss) private var dismiss
    @State private var rules: [String: IdentityRule] = [:]
    @State private var selected: Set<String> = []
    @State private var primary = ""
    @State private var name = ""
    @State private var custom = ""
    @State private var query = ""
    @State private var editing: String?
    @State private var confirmingDiscard = false
    private var pending: Bool { !selected.isEmpty || !custom.isEmpty || editing != nil }
    private var canStage: Bool { !primary.isEmpty && selected.contains(primary) }
    private var changed: Bool { rules != workspace.configuration.identities }
    var emails: [String] {
        Set(
            workspace.snapshot.commits.map(\.email).filter { !$0.isEmpty } + rules.keys
                + rules.values.flatMap(\.aliases) + parseListEntries(custom)
        )
        .sorted()
    }
    var body: some View {
        VStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text(
                        "Combine email identities belonging to the same person. Saved identities apply to history across every repository."
                    )
                    .foregroundStyle(.secondary)
                    SectionTitle(title: "Saved identities")
                    ForEach(rules.keys.sorted(), id: \.self) { key in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(rules[key]?.name.isEmpty == false ? rules[key]!.name : key)
                                Text(([key] + (rules[key]?.aliases ?? [])).joined(separator: ", "))
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button("Edit") {
                                editing = key
                                primary = key
                                selected = Set([key] + (rules[key]?.aliases ?? []))
                                name = rules[key]?.name ?? ""
                            }
                            Button("Unlink", role: .destructive) { rules.removeValue(forKey: key) }
                        }
                    }
                    Divider()
                    TextField("Search emails", text: $query).textFieldStyle(.roundedBorder)
                    ForEach(
                        emails.filter { query.isEmpty || $0.localizedCaseInsensitiveContains(query) },
                        id: \.self
                    ) { email in Toggle(email, isOn: membership(email, $selected)).toggleStyle(.checkbox)
                    }
                    TextField("Add another email (or comma-separated emails)", text: $custom)
                        .textFieldStyle(.roundedBorder)
                    Picker("Primary email", selection: $primary) {
                        Text("Choose an email").tag("")
                        ForEach(selected.sorted(), id: \.self) { Text($0).tag($0) }
                    }
                    TextField("Display name", text: $name).textFieldStyle(.roundedBorder)
                    HStack {
                        Button(editing == nil ? "Add identity" : "Update identity") { stage() }
                            .disabled(!canStage)
                        if pending { Button("Clear") { clear() } }
                    }
                }
                .padding(24)
            }
            HStack {
                if pending && !canStage {
                    Text("Choose a primary email to include the checked emails.")
                        .font(.callout).foregroundStyle(.orange)
                }
                Spacer()
                Button("Cancel") {
                    if changed || pending { confirmingDiscard = true } else { dismiss() }
                }
                .keyboardShortcut(.cancelAction)
                Button("Save identities") {
                    if canStage { stage() }
                    Task {
                        await workspace.saveIdentities(rules)
                        if workspace.configuration.identities == rules { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled((pending && !canStage) || (!changed && !pending))
            }
            .padding()
        }
        .confirmationDialog("Discard identity changes?", isPresented: $confirmingDiscard) {
            Button("Discard Changes", role: .destructive) { dismiss() }
            Button("Keep Editing", role: .cancel) {}
        }
        .onAppear { rules = workspace.configuration.identities }
    }
    private func stage() {
        IdentityStaging.stage(
            selected, primary: primary, name: name, replacing: editing, into: &rules)
        clear()
    }
    private func clear() {
        selected = []
        primary = ""
        name = ""
        custom = ""
        editing = nil
    }
}

enum IdentityStaging {
    static func stage(
        _ selected: Set<String>, primary: String, name: String, replacing editing: String?,
        into rules: inout [String: IdentityRule]
    ) {
        if let editing { rules.removeValue(forKey: editing) }
        var merged = selected
        for (key, rule) in rules where !Set([key] + rule.aliases).isDisjoint(with: selected) {
            merged.formUnion([key] + rule.aliases)
            rules.removeValue(forKey: key)
        }
        rules[primary] = IdentityRule(
            aliases: merged.subtracting([primary]).sorted(),
            name: name.trimmingCharacters(in: .whitespacesAndNewlines))
    }
}
