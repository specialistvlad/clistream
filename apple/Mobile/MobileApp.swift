import SwiftUI
import FileProvider

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
  @Environment(\.horizontalSizeClass) private var width
  @Environment(\.scenePhase) private var scenePhase
  @StateObject private var actions = MobileLifecycle()
  @StateObject private var invitations = MobileInvitations()
  @State private var selected: WorkspaceSession?
  @State private var closedHinge = false
  @State private var visibility = NavigationSplitViewVisibility.all
  @State private var column = NavigationSplitViewColumn.sidebar
  var body: some View {
    GeometryReader { geometry in
      NavigationSplitView(columnVisibility: $visibility, preferredCompactColumn: $column) {
        MobileServers(
          workspace: workspace, actions: actions, selected: selected?.id,
          activate: { row in
            workspace.activate(row)
            selected = row
            column = .detail
          }, invitations: invitations
        )
        .navigationSplitViewColumnWidth(
          min: 220, ideal: geometry.size.width < 1024 ? 272 : 330, max: 380)
      } detail: {
        if let row = selected {
          MobileTerminal(
            workspace: workspace, connection: workspace.terminal(row), actions: actions, row: row,
            pairAgain: { invitations.add() }
          ) {
            workspace.detach(row.id)
            selected = nil
            column = .sidebar
          }.id(row.id)

        } else {
          Text("Select a CLI")
        }
      }.navigationSplitViewStyle(.balanced)
    }
    .background { if #available(iOS 27.1, *) { HingeObserver(closed: $closedHinge) } }
    .environment(\.closedHinge, closedHinge)
    .environment(\.regularWorkspace, width == .regular)
    .handlesExternalEvents(preferring: scenePhase == .active ? ["clistream://"] : [], allowing: ["clistream://"])
    .modifier(
      MobileInvitationPresentation(
        flow: invitations, connection: workspace.pairing,
        enroll: { link in
          let previousServers = Set(workspace.profiles.map(\.id))
          await workspace.enroll(link)
          guard workspace.pairing.pairingError == nil else { return }
          let addedServer = workspace.profiles.contains { !previousServers.contains($0.id) }
          if selected == nil || addedServer {
            if addedServer {
              UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder),
                to: nil, from: nil, for: nil)
            }
            visibility = .all
            column = .sidebar
          }
        })
    )
    .modifier(MobileLifecyclePresentation(actions: actions, workspace: workspace))
    .onChange(of: workspace.profiles.map(\.id)) { _, ids in
      if let row = selected, !ids.contains(row.server.id) {
        selected = nil
        column = .sidebar
      }
    }
    .task { workspace.load() }
    .onReceive(NotificationCenter.default.publisher(for: .fileProviderDomainDidChange)) { _ in
      Task { await FinderWorkspace.locationDidChange() }
    }
  }
}
