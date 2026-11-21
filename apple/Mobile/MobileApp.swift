import SwiftUI

@main
struct MobileApp: App {
  var body: some Scene { WindowGroup { BrowserView() } }
}

struct BrowserView: View {
  @StateObject private var connection = Connection()
  @State private var pairing: Pairing?
  @State private var tabs: [String: Connection] = [:]
  @State private var column = NavigationSplitViewColumn.sidebar
  @State private var selection: String?

  var body: some View {
    NavigationSplitView(preferredCompactColumn: $column) {
      List(selection: $selection) {
        if !connection.listError.isEmpty && !connection.sessions.isEmpty {
          Text(connection.listError).accessibilityIdentifier("catalog-error")
        }
        ForEach(connection.sessions) { item in
          NavigationLink(value: item.id) {
            VStack(alignment: .leading) {
              Text(item.Name).font(.headline)
              Text("\(item.Host) · \(item.running ? item.Mode.uppercased() : "Stopped")")
                .font(.caption).foregroundStyle(.primary)
            }
          }.accessibilityIdentifier("session-" + item.id)
        }
      }

      .overlay {
        if connection.sessions.isEmpty {
          ContentUnavailableView(
            connection.listTitle, systemImage: "terminal",
            description: Text(connection.listDetail))
        }
      }
      .navigationTitle("Sessions")
      .toolbar {
        Button("Refresh", systemImage: "arrow.clockwise") { Task { await connection.refresh() } }
          .labelStyle(.iconOnly)
        Button("Pair", systemImage: "link") { pairing = Pairing() }
          .labelStyle(.iconOnly)
      }
    } detail: {
      if let id = selection, let tab = tabs[id] {
        MobileSession(tab: tab, detach: detach)
          .id(id)
      } else {
        ContentUnavailableView(
          "Select a session", systemImage: "terminal",
          description: Text(
            connection.status.isEmpty ? "Open a running CLI from Sessions." : connection.status))
      }
    }
    .onChange(of: selection) { _, id in
      if let item = connection.sessions.first(where: { $0.id == id }) {
        column = .detail
        if tabs[item.id] == nil {
          let tab = Connection()
          tabs[item.id] = tab
          Task { await tab.attach(item) }
        }
      }
    }
    .onChange(of: connection.pairingGeneration) { _, _ in
      tabs.values.forEach { $0.leave() }
      tabs.removeAll()
      selection = nil
      column = .sidebar
    }
    .modifier(PairingPresentation(connection: connection, pairing: $pairing))
    .task { await connection.watchSupervisors() }
  }
  private func detach() {
    if let id = selection { tabs.removeValue(forKey: id)?.leave() }
    selection = nil
    column = .sidebar
    Task { await connection.refresh() }
  }

}

private struct MobileSession: View {
  @ObservedObject var tab: Connection
  let detach: () -> Void

  var body: some View {
    Group {
      if let item = tab.selected {
        SessionPane(connection: tab, session: item)
          .navigationTitle(item.Name)
      } else {
        Text(tab.status.isEmpty ? "Connecting…" : tab.status)
      }
    }.toolbar {
      Button("Detach", systemImage: "rectangle.portrait.and.arrow.right", action: detach)
        .labelStyle(.iconOnly)
    }
  }
}
