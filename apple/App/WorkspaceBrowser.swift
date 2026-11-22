import SwiftUI

struct WorkspaceSidebar: View {
  @ObservedObject var workspace: Workspace
  let selected: String?
  let activate: (WorkspaceSession) -> Void
  var openElsewhere: ((WorkspaceSession, Bool) -> Void)?
  @Binding var pairing: Pairing?
  @AppStorage("workspace-list") private var flat = false
  @AppStorage("workspace-group") private var grouping = "Server"
  @State private var collapsed = Set<[String]>()
  @State private var renameTarget: ServerProfile?
  @State private var name = ""
  @FocusState private var editingName: Bool
  private func rename(_ server: ServerProfile) {
    name = server.name
    renameTarget = server
  }

  private func disclosure<C: View>(
    _ text: String, symbol: String?, path: [String], @ViewBuilder children: @escaping () -> C
  ) -> some View {
    DisclosureGroup(
      isExpanded: Binding(
        get: { !collapsed.contains(path) },
        set: {
          if $0 { collapsed.remove(path) } else { collapsed.insert(path) }
        })
    ) {
      children()
    } label: {
      if symbol == "server.rack", let id = path.last,
        let server = workspace.profiles.first(where: { $0.id == id }) {
        Label(text, systemImage: "server.rack")
          .accessibilityLabel("Server " + text)
          .contextMenu { Button("Rename…") { rename(server) } }
      } else if let symbol {
        Label(text, systemImage: symbol)
          .accessibilityLabel((symbol == "server.rack" ? "Server " : "Host ") + text)
      } else { Text(text) }
    }
  }
  private func leaf(_ row: WorkspaceSession, title: String, detail: String = "") -> some View {
    let duplicate =
      workspace.rows[row.server.id]?.filter { $0.Name == row.item.Name && $0.Host == row.item.Host }
      .count ?? 0
    return Button {
      activate(row)
    } label: {
      VStack(alignment: .leading) {
        Text(title + (duplicate > 1 ? " · " + row.item.id.prefix(8) : ""))
        if !detail.isEmpty { Text(detail).font(.caption).foregroundStyle(.secondary) }
      }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
    }.buttonStyle(.plain)
      .accessibilityAddTraits(selected == row.id ? [.isSelected] : [])
      .accessibilityIdentifier("session-" + row.item.id)
      .listRowBackground(selected == row.id ? Color.accentColor.opacity(0.23) : Color.clear)
      .contextMenu {
        if let openElsewhere {
          Button("Open in New Tab") { openElsewhere(row, true) }
          Button("Open in New Window") { openElsewhere(row, false) }
        }
      }
  }
  @ViewBuilder private func group(
    _ text: String, symbol: String?, path: [String], @ViewBuilder content: @escaping () -> some View
  ) -> some View {
    if flat {
      Section(text) { content() }
    } else {
      disclosure(text, symbol: symbol, path: path, children: content)
    }
  }
  var body: some View {
    List {
      if workspace.profiles.isEmpty {
        Text("Add a server to see its CLI apps.").foregroundStyle(.secondary)
      }
      if grouping == "Server" {
        ForEach(workspace.profiles) { server in
          let rows = workspace.sessions.filter { $0.server.id == server.id }
          group(server.name, symbol: "server.rack", path: [server.id]) {
            if let error = workspace.errors[server.id], !error.isEmpty {
              HStack {
                Text("Offline").font(.body).foregroundStyle(.secondary)
                  .fixedSize(horizontal: false, vertical: true)
                Button { workspace.retry(server) } label: {
                  #if os(iOS)
                    Label("Retry", systemImage: "arrow.clockwise")
                      .labelStyle(.iconOnly).frame(minWidth: 44, minHeight: 44)
                  #else
                    Text("Retry")
                  #endif
                }
                .accessibilityLabel("Retry").accessibilityIdentifier("retry-server")
              }.help(error)
            }
            if flat {
              ForEach(rows) { leaf($0, title: $0.item.Name, detail: $0.item.Host) }
            } else {
              ForEach(Set(rows.map { $0.item.Host }).sorted(), id: \.self) { host in
                disclosure(host, symbol: "desktopcomputer", path: [server.id, host]) {
                  ForEach(rows.filter { $0.item.Host == host }) { leaf($0, title: $0.item.Name) }
                }
              }
            }
          }
        }
      } else {
        let byHost = grouping == "Host"
        let groups = Dictionary(grouping: workspace.sessions) {
          byHost ? $0.item.Host : $0.item.Name
        }
        ForEach(groups.keys.sorted(), id: \.self) { key in
          let rows = groups[key] ?? []
          group(key, symbol: byHost ? "desktopcomputer" : nil, path: [grouping, key]) {
            ForEach(
              workspace.profiles.filter { profile in rows.contains { $0.server.id == profile.id } }
            ) { server in
              let matches = rows.filter { $0.server.id == server.id }
              if flat {
                ForEach(matches) {
                  leaf($0, title: byHost ? $0.item.Name : $0.item.Host, detail: server.name)
                }
              } else {
                disclosure(server.name, symbol: "server.rack", path: [grouping, key, server.id]) {
                  ForEach(matches) { leaf($0, title: byHost ? $0.item.Name : $0.item.Host) }
                }
              }
            }
          }
        }
      }
    }.listStyle(.sidebar).navigationTitle("Servers")
      #if os(macOS)
        .onChange(of: grouping) { _, _ in workspace.focus(selected) }
        .onChange(of: flat) { _, _ in workspace.focus(selected) }
        .toolbar { ToolbarItemGroup(placement: .navigation) { sidebarControls } }
      #else
        .toolbar { ToolbarItemGroup(placement: .topBarTrailing) { sidebarControls } }
      #endif
      .sheet(item: $renameTarget) { target in
        WorkspaceSheet(title: "Rename Server", confirmation: "Save",
                       enabled: !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                       cancel: { renameTarget = nil }, confirm: {
          workspace.rename(target, to: name)
          renameTarget = nil
        }) {
          TextField("Name", text: $name)
            .focused($editingName).onAppear { editingName = true }
            .accessibilityIdentifier("server-name-field")
        }
      }
  }
  @ViewBuilder private var sidebarControls: some View {
    Button("Add Server", systemImage: "plus") { pairing = Pairing() }.keyboardShortcut("k")
      .accessibilityIdentifier("workspace-add-server")
    Menu("View", systemImage: "line.3.horizontal.decrease") {
      Menu("Rename Server…") {
        ForEach(workspace.profiles) { server in
          Button(server.name) { rename(server) }
        }
      }
      Picker("Layout", selection: $flat) {
        Text("Tree").tag(false)
        Text("List").tag(true)
      }.pickerStyle(.inline)
      Picker("Group by", selection: $grouping) {
        ForEach(["Server", "Host", "CLI Name"], id: \.self) { Text($0).tag($0) }
      }.pickerStyle(.inline)
    }.accessibilityIdentifier("workspace-view-options")
  }
}

struct WorkspaceTerminal: View {
  @ObservedObject var connection: Connection
  @ObservedObject var workspace: Workspace
  let row: WorkspaceSession
  var detach: (() -> Void)? = nil
  var body: some View {
    VStack(spacing: 0) {
      if let session = connection.selected {
        SessionPane(connection: connection, session: session, detach: detach)
      } else {
        ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
      }
      Divider()
      WorkspaceStatusBar(connection: connection, workspace: workspace, row: row)
    }
  }
}

private struct WorkspaceStatusBar: View {
  @ObservedObject var connection: Connection
  @ObservedObject var workspace: Workspace
  let row: WorkspaceSession
  @State private var sample: Int64?
  var body: some View {
    let online =
      connection.attached
      || (connection.selected?.running == false && workspace.errors[row.server.id] == "")
    let us = sample
    HStack {
      Circle().fill(online ? Color.green : Color.secondary).frame(width: 8, height: 8)
      Text(
        online
          ? "Connected"
            + (us.map { $0 < 1000 ? " · <1 ms" : " · \(Int((Double($0)/1000).rounded())) ms" }
              ?? "") : "Disconnected"
      )
      .accessibilityIdentifier("desktop-session-status")
      Spacer()
    }.font(.caption).padding(.horizontal, 12).padding(.vertical, 6)
      .task(id: connection.attached) {
        sample = nil
        while !Task.isCancelled {
          if connection.attached { await connection.measureLatency() }
          sample = connection.attached ? connection.latency : workspace.samples[row.server.id]
          try? await Task.sleep(for: .seconds(1))
        }
      }
  }
}
