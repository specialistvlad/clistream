import SwiftUI

struct MobileServers: View {
  @ObservedObject var workspace: Workspace
  @ObservedObject var actions: MobileLifecycle
  let selected: String?
  let activate: (WorkspaceSession) -> Void
  let add: () -> Void
  @Environment(\.horizontalSizeClass) private var width
  @AppStorage("workspace-list") private var flat = false
  @AppStorage("workspace-group") private var grouping = "Server"
  @State private var query = ""
  @State private var collapsed = Set<[String]>()
  @State private var renaming: ServerProfile?
  @State private var name = ""

  private var matches: [WorkspaceSession] {
    workspace.sessions.filter { query.isEmpty || $0.item.Name.localizedCaseInsensitiveContains(query) }
  }

  var body: some View {
    List(selection: Binding(get: { selected }, set: { id in
      if let row = workspace.sessions.first(where: { $0.id == id }) { activate(row) }
    })) {
      if grouping == "Server" {
        ForEach(workspace.profiles) { server in
          let rows = matches.filter { $0.server.id == server.id }
          if query.isEmpty || !rows.isEmpty {
            Section(isExpanded: expanded([server.id])) {
              if let error = workspace.errors[server.id], !error.isEmpty {
                HStack {
                  Label("Offline", systemImage: "wifi.slash").foregroundStyle(.secondary)
                  Spacer()
                  Button("Retry") { workspace.retry(server) }.buttonStyle(.borderless)
                    .accessibilityIdentifier("retry-server")
                }
              }
              if flat { ForEach(rows) { leaf($0, detail: $0.item.Host) } }
              else {
                ForEach(Set(rows.map(\.item.Host)).sorted(), id: \.self) { host in
                  let children = rows.filter { $0.item.Host == host }
                  DisclosureGroup(isExpanded: expanded([server.id, host])) {
                    ForEach(children) { leaf($0) }
                  } label: { hostLabel(host, os: children.first?.item.OS ?? "") }
                }
              }
            } header: { serverHeader(server) }
          }
        }
      } else {
        let byHost = grouping == "Host"
        let groups = Dictionary(grouping: matches) { byHost ? $0.item.Host : $0.item.Name }
        ForEach(groups.keys.sorted(), id: \.self) { group in
          let rows = groups[group] ?? []
          Section(isExpanded: expanded([grouping, group])) {
            ForEach(workspace.profiles) { server in
              let children = rows.filter { $0.server.id == server.id }
              if !children.isEmpty {
                if flat {
                  ForEach(children) { leaf($0, detail: server.name) }
                } else {
                  DisclosureGroup(isExpanded: expanded([grouping, group, server.id])) {
                    ForEach(children) { leaf($0, detail: byHost ? "" : $0.item.Host) }
                  } label: { serverHeader(server) }
                }
              }
            }
          } header: { Text(group) }
        }
      }
    }
    .listStyle(.insetGrouped)
    .searchable(text: $query, placement: width == .compact ? .toolbar : .sidebar, prompt: "Search")
    .navigationTitle("Servers")
    .toolbar {
      ToolbarItemGroup(placement: .topBarTrailing) {
        Button("Add Server", systemImage: "plus", action: add).keyboardShortcut("k")
          .accessibilityIdentifier("workspace-add-server")
        Menu("Layout and grouping", systemImage: "list.bullet.indent") {
          Picker("Layout", selection: $flat) { Text("Tree").tag(false); Text("List").tag(true) }
          Picker("Group by", selection: $grouping) {
            ForEach(["Server", "Host", "CLI Name"], id: \.self) { Text($0) }
          }
          Menu("Rename Server…") {
            ForEach(workspace.profiles) { server in Button(server.name) { rename(server) } }
          }
        }.accessibilityIdentifier("workspace-view-options")
      }
    }
    .sheet(item: $renaming) { server in
      WorkspaceSheet(title: "Rename Server", confirmation: "Save",
                     enabled: !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                     cancel: { renaming = nil }, confirm: {
        workspace.rename(server, to: name)
        renaming = nil
      }) { TextField("Name", text: $name).accessibilityIdentifier("server-name-field") }
    }
  }

  private func expanded(_ path: [String]) -> Binding<Bool> {
    Binding(get: { !query.isEmpty || !collapsed.contains(path) }, set: {
      if $0 { collapsed.remove(path) } else { collapsed.insert(path) }
    })
  }
  private func rename(_ server: ServerProfile) { name = server.name; renaming = server }
  private func serverHeader(_ server: ServerProfile) -> some View {
    HStack {
      Text(server.name).accessibilityLabel("Server " + server.name)
      Spacer()
      TimelineView(.periodic(from: .now, by: 2)) { _ in
        if let us = workspace.samples[server.id] {
          Text(Self.latency(us)).monospacedDigit().font(.caption)
        }
      }
    }.textCase(nil).contextMenu {
      Button("Rename…", systemImage: "pencil") { rename(server) }
      Button("Remove Server…", systemImage: "trash", role: .destructive) { actions.confirmation = .remove(server) }
    }
  }
  static func latency(_ us: Int64) -> String {
    us < 1000 ? "<1 ms" : "\(Int((Double(us) / 1000).rounded())) ms"
  }
  private func hostLabel(_ host: String, os: String) -> some View {
    HStack {
      Label(host, systemImage: os == "darwin" ? "laptopcomputer" : "desktopcomputer")
        .accessibilityLabel("Host " + host)
      Spacer()
      Text(os == "darwin" ? "macOS" : os.capitalized).font(.caption).foregroundStyle(.secondary)
    }
  }
  private func leaf(_ row: WorkspaceSession, detail: String = "") -> some View {
    NavigationLink(value: row.id) {
      HStack {
        Image(systemName: "terminal").foregroundStyle(row.item.running ? Color.accentColor : .secondary)
        Text(row.item.Name)
        Spacer()
        let label = !row.item.running ? row.item.exitCode.map { "Exit \($0)" } ?? "Stopped"
          : (row.item.revision ?? 0) == 0 ? "Attach only" : detail
        if !label.isEmpty { Text(label).font(.caption).foregroundStyle(.secondary) }
      }
    }.accessibilityIdentifier("session-" + row.item.id)
      .swipeActions(edge: .trailing, allowsFullSwipe: false) {
        if (row.item.revision ?? 0) > 0 {
          if row.item.running {
            Button("Stop", systemImage: "stop.fill", role: .destructive) { actions.confirmation = .stop(row) }
          }
          Button(row.item.running ? "Restart" : "Start",
                 systemImage: row.item.running ? "arrow.counterclockwise" : "play") {
            Task { await actions.review(row, in: workspace) }
          }.tint(.accentColor)
        }
      }
  }
}
