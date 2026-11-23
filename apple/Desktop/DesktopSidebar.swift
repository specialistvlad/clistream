import SwiftUI

extension Workspace {
  func revoked(_ server: ServerProfile) -> Bool {
    errors[server.id]?.localizedCaseInsensitiveContains("revoked") == true
  }
}

struct DesktopSidebar: View {
  @ObservedObject var workspace: Workspace
  @ObservedObject var state: DesktopSelection
  let flat: Bool
  let grouping: String
  @Environment(\.openWindow) private var openWindow
  @Environment(\.colorScheme) private var appearance
  @State private var collapsed = Set<[String]>()

  private var matches: [WorkspaceSession] {
    workspace.sessions.filter { state.query.isEmpty || $0.item.Name.localizedCaseInsensitiveContains(state.query) }
      .sorted { ($0.item.Name, $0.id) < ($1.item.Name, $1.id) }
  }
  private func expanded(_ key: [String]) -> Binding<Bool> {
    Binding(get: { !collapsed.contains(key) || !state.query.isEmpty }, set: {
      if $0 { collapsed.remove(key) } else { collapsed.insert(key) }
    })
  }
  var body: some View {
    List(selection: Binding(get: { state.selection }, set: { value in
      // A section header isn't a selectable outline row. AppKit may restore the
      // previous CLI selection as the header switches into editing mode.
      guard state.renaming == nil else { return }
      switch value {
      case .session(let id):
        if let row = workspace.sessions.first(where: { $0.id == id }) { state.activate(row) }
      case .server, .none: state.selection = value
      }
    })) {
      if workspace.profiles.isEmpty {
        Text("Add a server to see its CLI apps.").foregroundStyle(.secondary)
      }
      if grouping == "Server" {
        ForEach(workspace.profiles) { server in
          let rows = matches.filter { $0.server.id == server.id }
          if state.query.isEmpty || !rows.isEmpty {
            Section(isExpanded: expanded(["Server", server.id])) {
              let offline = !(workspace.errors[server.id] ?? "").isEmpty
              if offline { unavailable(server) }
              if !offline || workspace.revoked(server) {
                if flat { leaves(rows, detail: { $0.item.Host }) }
                else {
                  let byHost = Dictionary(grouping: rows, by: { $0.item.Host })
                  ForEach(byHost.keys.sorted(), id: \.self) { host in
                    let children = byHost[host] ?? []
                    let os = children.first?.item.OS ?? ""
                    DisclosureGroup(isExpanded: expanded(["Server", server.id, host])) {
                      leaves(children)
                    } label: {
                      HStack {
                        Label(host, systemImage: os == "darwin" || os == "macOS" ? "laptopcomputer" : "desktopcomputer")
                        Spacer()
                        Text(os == "darwin" ? "macOS" : os == "linux" ? "Linux" : os)
                          .font(.caption).foregroundStyle(.secondary)
                      }.accessibilityLabel("Host " + host)
                    }
                  }
                }
              }
            } header: { serverHeader(server, group: ["Server", server.id]) }
          }
        }
      } else {
        let byHost = grouping == "Host"
        let visible = matches.filter { (workspace.errors[$0.server.id] ?? "").isEmpty || workspace.revoked($0.server) }
        let sections = Dictionary(grouping: visible, by: { byHost ? $0.item.Host : $0.item.Name })
        ForEach(sections.keys.sorted(), id: \.self) { key in
          let rows = sections[key] ?? []
          Section(isExpanded: expanded([grouping, key])) {
            if flat { leaves(rows, detail: { byHost ? $0.server.name : $0.item.Host + " · " + $0.server.name }) }
            else {
              ForEach(workspace.profiles.filter { server in rows.contains { $0.server.id == server.id } }) { server in
                DisclosureGroup(isExpanded: expanded([grouping, key, server.id])) {
                  leaves(rows.filter { $0.server.id == server.id }, detail: { byHost ? nil : $0.item.Host })
                } label: { serverHeader(server, group: [grouping, key, server.id]) }
              }
            }
          } header: { Text(key) }
        }
        if state.query.isEmpty {
          ForEach(workspace.profiles.filter { !(workspace.errors[$0.id] ?? "").isEmpty }) { server in
            Section { unavailable(server) } header: {
              serverHeader(server, group: ["Unavailable", server.id])
            }
          }
        }
      }
    }
    .listStyle(.sidebar)
  }

  private func serverHeader(_ server: ServerProfile, group: [String]) -> some View {
    DesktopServerHeader(server: server, group: group, state: state, workspace: workspace)
      .frame(maxWidth: .infinity, minHeight: 22)
      .tag(SidebarSelection.server(server.id))
  }
  private func unavailable(_ server: ServerProfile) -> some View {
    let revoked = workspace.revoked(server)
    return HStack {
      Label(revoked ? "Access revoked" : "Offline", systemImage: "wifi.slash").foregroundStyle(.secondary)
      Spacer()
      Button(revoked ? "Pair Again…" : "Retry") {
        if revoked { state.pair("") } else { workspace.retry(server) }
      }.controlSize(.small).buttonStyle(.bordered).accessibilityIdentifier(revoked ? "repair-server" : "retry-server")
    }
  }

  private func leaves(_ rows: [WorkspaceSession], detail: @escaping (WorkspaceSession) -> String? = { _ in nil }) -> some View {
    ForEach(rows) { row in
      let item = row.item
      let revoked = workspace.revoked(row.server)
      let duplicate = workspace.rows[row.server.id]?.filter { $0.Name == item.Name && $0.Host == item.Host }.count ?? 0
      HStack {
        Image(systemName: "terminal")
          .foregroundStyle(item.running && !revoked ? Color.accentColor : DesktopStatusColor.stopped(appearance))
        Text(item.Name)
        Spacer()
        if !revoked {
          let status = !item.running ? item.exitCode.map { "Exit " + String($0) } ?? "Stopped"
            : (item.revision ?? 0) == 0 ? "Attach only" : nil
          let caption = [status, duplicate > 1 ? String(item.id.prefix(8)) : nil, detail(row)]
            .compactMap { $0 }.joined(separator: " · ")
          if !caption.isEmpty { Text(caption).font(.caption).foregroundStyle(.secondary) }
        }
      }
      .tag(SidebarSelection.session(row.id))
      .accessibilityElement(children: .combine)
      .accessibilityIdentifier("session-" + item.id)
      .contentShape(Rectangle())
      .contextMenu {
        Button("Open in New Tab") { state.open(row, tab: true, using: openWindow) }
        Button("Open in New Window") { state.open(row, tab: false, using: openWindow) }
        if !revoked && (item.revision ?? 0) > 0 {
          Divider()
          Button(item.running ? "Restart…" : "Start…", systemImage: item.running ? "arrow.counterclockwise" : "play.fill") {
            state.perform(.restart, on: row, using: openWindow)
          }
          if item.running {
            Button("Stop…", systemImage: "stop") { state.perform(.stop, on: row, using: openWindow) }
          }
        }
        Divider()
        Button("Show Ports") { state.perform(.ports, on: row, using: openWindow) }
        if !revoked && item.workspace == true {
          Button("Show in Finder") { state.perform(.finder, on: row, using: openWindow) }
        }
      }
    }
  }
}

/// A native toolbar search field keeps its cancel button and search accessibility role.
struct DesktopSearchField: NSViewRepresentable {
  @Binding var query: String
  final class Coordinator: NSObject, NSSearchFieldDelegate {
    var field: DesktopSearchField
    init(_ field: DesktopSearchField) { self.field = field }
    func controlTextDidChange(_ notification: Notification) {
      if let control = notification.object as? NSSearchField { field.query = control.stringValue }
    }
    @objc func submit(_ control: NSSearchField) { field.query = control.stringValue }
  }
  func makeCoordinator() -> Coordinator { Coordinator(self) }
  func makeNSView(context: Context) -> NSSearchField {
    let view = NSSearchField()
    view.placeholderString = "Search"
    view.delegate = context.coordinator
    view.target = context.coordinator
    view.action = #selector(Coordinator.submit(_:))
    view.sendsSearchStringImmediately = true
    view.setAccessibilityLabel("Search CLIs")
    view.setAccessibilityIdentifier("workspace-search")
    return view
  }
  func updateNSView(_ view: NSSearchField, context: Context) {
    context.coordinator.field = self
    if view.stringValue != query { view.stringValue = query }
  }
}
