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
    @State private var reviewing = false
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
                    if reviewing {
                        Text("Review identity changes").font(.title2)
                        ForEach(rules.keys.sorted(), id: \.self) { key in
                            Text(
                                "\(rules[key]?.name.isEmpty == false ? rules[key]!.name : key) · \(key)\nAliases: \((rules[key]?.aliases ?? []).joined(separator: ", "))"
                            )
                        }
                    } else {
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
                        Button("Stage identity") { stage() }
                            .disabled(primary.isEmpty || !selected.contains(primary))
                    }
                }
                .padding(24)
            }
            HStack {
                if reviewing { Button("Back") { reviewing = false } }
                Spacer()
                Button("Cancel") { dismiss() }
                Button(reviewing ? "Save identities" : "Review changes") {
                    if reviewing {
                        Task {
                            await workspace.saveIdentities(rules)
                            if workspace.configuration.identities == rules { dismiss() }
                        }
                    } else {
                        reviewing = true
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(workspace.busy || rules == workspace.configuration.identities)
            }
            .padding()
        }
        .onAppear { rules = workspace.configuration.identities }
    }
    private func stage() {
        var merged = selected
        for (key, rule) in rules where !Set([key] + rule.aliases).isDisjoint(with: selected) {
            merged.formUnion([key] + rule.aliases)
            rules.removeValue(forKey: key)
        }
        rules[primary] = IdentityRule(
            aliases: merged.subtracting([primary]).sorted(),
            name: name.trimmingCharacters(in: .whitespacesAndNewlines))
        selected = []
        primary = ""
        name = ""
        custom = ""
    }
}
