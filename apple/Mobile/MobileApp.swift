import SwiftUI

@main struct MobileApp: App {
  var body: some Scene { WindowGroup { BrowserView() } }
}

struct BrowserView: View {
  @ObservedObject private var workspace = Workspace.shared
  @State private var pairing: Pairing?
  @State private var selected: WorkspaceSession?
  @State private var column = NavigationSplitViewColumn.sidebar
  var body: some View {
    NavigationSplitView(preferredCompactColumn: $column) {
      WorkspaceSidebar(
        workspace: workspace, selected: selected?.id,
        activate: { row in
          workspace.activate(row)
          selected = row
          column = .detail
        }, pairing: $pairing)
    } detail: {
      if let row = selected {
        WorkspaceTerminal(connection: workspace.terminal(row), workspace: workspace, row: row).id(
          row.id
        )
        .navigationTitle(row.item.Name)
        .toolbar {
          Button("Detach", systemImage: "rectangle.portrait.and.arrow.right") {
            workspace.detach(row.id)
            selected = nil
            column = .sidebar
          }
        }
      } else {
        Text("Select a CLI")
      }
    }.modifier(
      PairingPresentation(
        connection: workspace.pairing, pairing: $pairing, enroll: workspace.enroll)
    )
    .task { workspace.load() }
  }
}
