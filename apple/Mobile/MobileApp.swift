import SwiftUI

@main struct MobileApp: App {
  private let workspace: Workspace = {
    #if DEBUG && targetEnvironment(simulator)
      if CommandLine.arguments.contains("--ui-test-first-launch") {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        return Workspace(directory: root.path)
      }
    #endif
    return .shared
  }()

  var body: some Scene { WindowGroup { BrowserView(workspace: workspace) } }
}

struct BrowserView: View {
  @ObservedObject var workspace: Workspace
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
        WorkspaceTerminal(connection: workspace.terminal(row), workspace: workspace, row: row) {
          workspace.detach(row.id)
          selected = nil
          column = .sidebar
        }.id(row.id)
        .navigationTitle(row.item.Name)
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
