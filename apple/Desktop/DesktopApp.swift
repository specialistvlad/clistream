import AppKit
import SwiftUI

@main struct DesktopApp: App {
  var body: some Scene {
    WindowGroup("CLIStream", id: "workspace", for: UUID.self) { $windowID in
      DesktopBrowser(windowID: windowID)
    }
    .defaultSize(width: 1200, height: 740)
    .defaultLaunchBehavior(.presented)
    .commands { SidebarCommands(); DesktopCommands() }
  }
}

private struct DesktopBrowser: View {
  @ObservedObject private var workspace = Workspace.shared
  @StateObject private var state: DesktopSelection
  init(windowID: UUID?) {
    _state = StateObject(wrappedValue: DesktopSelection.take(windowID))
  }
  var body: some View {
    let current: WorkspaceSession? = state.row.flatMap { selected in
      guard workspace.profiles.contains(where: { $0.id == selected.server.id }) else { return nil }
      return workspace.sessions.first { $0.id == selected.id } ?? selected
    }
    DesktopWindow(state: state, workspace: workspace,
                  connection: current.map(workspace.terminal) ?? workspace.pairing, row: current)
      .background(DesktopWindowReader(state: state))
      .task { workspace.load() }
      .onReceive(NotificationCenter.default.publisher(for: NSWindow.willCloseNotification)) { event in
        if let closed = event.object as? NSWindow, closed === state.window { state.closed() }
      }
      .onChange(of: workspace.profiles.map(\.id)) { _, ids in
        if let row = state.row, !ids.contains(row.server.id) {
          state.row = nil
          state.selection = nil
          state.ports = false
          state.sheet = nil
        }
      }
  }
}

private struct DesktopWindow: View {
  @ObservedObject var state: DesktopSelection
  @ObservedObject var workspace: Workspace
  @ObservedObject var connection: Connection
  let row: WorkspaceSession?
  @AppStorage("workspace-list") private var flat = false
  @AppStorage("workspace-group") private var grouping = "Server"
  @Environment(\.openWindow) private var openWindow
  private var selectedServer: ServerProfile? {
    if case .server(let id) = state.selection { return workspace.profiles.first { $0.id == id } }
    return row?.server
  }
  private var actions: DesktopContext {
    DesktopContext(state: state, row: row, server: selectedServer,
      revoked: row.map { workspace.revoked($0.server) } == true, attached: connection.attached,
      openWindow: openWindow)
  }
  var body: some View {
    NavigationSplitView {
      DesktopSidebar(workspace: workspace, state: state, flat: flat, grouping: grouping)
        .navigationSplitViewColumnWidth(min: 210, ideal: 256, max: 400)
    } detail: {
      Group {
        if let row {
          DesktopTerminal(row: row, workspace: workspace, connection: connection, state: state, actions: actions)
            .id(row.id)
        } else { emptyContent }
      }
      .navigationTitle(row?.item.Name ?? "Servers")
      .navigationSubtitle(row.map { $0.item.Host + " — " + $0.server.name }
        ?? (workspace.profiles.isEmpty ? "None paired" : ""))
      .toolbar { toolbar }

    }
    .inspector(isPresented: $state.ports) {
      if let row {
        DesktopPorts(connection: connection, item: row.item)
          .inspectorColumnWidth(min: 230, ideal: 280, max: 400)
      }
    }
    .focusedSceneValue(\.desktopContext, actions)
    .onChange(of: grouping) { _, _ in workspace.focus(row?.id) }
    .onChange(of: flat) { _, _ in workspace.focus(row?.id) }
    .onOpenURL { url in
      guard url.scheme == "clistream" else { return }
      NSApp.activate()
      state.window?.makeKeyAndOrderFront(nil)
      state.pair(url.absoluteString)
    }
    .sheet(item: $state.sheet) { sheet in
      switch sheet {
      case .pair(let link):
        DesktopPairSheet(link: link) { value in
          state.sheet = nil
          Task {
            await workspace.enroll(value)
            if let error = workspace.pairing.pairingError {
              workspace.pairing.pairingError = nil
              state.pairingFailed(error)
            }
          }
        }
      case .command(let row, let line):
        DesktopCommandSheet(row: row, initialLine: line) { argv in
          state.sheet = nil
          state.change(.launch, row: row, arguments: argv)
        }
      }
    }
    .alert(state.alert?.title ?? "", isPresented: Binding(get: { state.alert != nil },
      set: { if !$0 { state.alert = nil } }), presenting: state.alert) { alert in
      switch alert {
      case .stop(let row):
        Button("Stop", role: .destructive) { state.change(.stop, row: row) }
        Button("Cancel", role: .cancel) {}.keyboardShortcut(.defaultAction)
      case .restart(let row):
        Button("Restart", role: .destructive) { state.change(.restart, row: row) }
        Button("Cancel", role: .cancel) {}.keyboardShortcut(.defaultAction)
      case .remove(let server):
        Button("Remove", role: .destructive) {
          do { try workspace.remove(server) }
          catch { let message = error.localizedDescription; Task { state.alert = .error(message) } }
        }
        Button("Cancel", role: .cancel) {}.keyboardShortcut(.defaultAction)
      case .pairError, .error:
        Button("OK") {}.keyboardShortcut(.defaultAction)
      }
    } message: { alert in Text(alert.message) }
  }

  @ViewBuilder private var emptyContent: some View {
    if workspace.profiles.isEmpty {
      ContentUnavailableView {
        Label("Pair with a server", systemImage: "network")
      } description: {
        Text("Open an invitation from clistreamd, or paste one. Invitations last three minutes.")
      } actions: {
        HStack {
          Button("Paste Invitation") { actions.pasteInvitation() }

          Button("Add Server…") { state.pair("") }.buttonStyle(.borderedProminent)
        }
      }
    } else {
      ContentUnavailableView("Select a CLI", systemImage: "terminal",
        description: Text("Choose a program in the sidebar to open its terminal."))
    }
  }

  @ToolbarContentBuilder private var toolbar: some ToolbarContent {
    ToolbarItemGroup {
      Button("Add Server", systemImage: "plus") { state.pair("") }
        .accessibilityIdentifier("workspace-add-server")
      Menu("Layout and grouping", systemImage: "list.bullet.indent") {
        Picker("Layout", selection: $flat) { Text("Tree").tag(false); Text("List").tag(true) }
          .pickerStyle(.inline)
        Picker("Group by", selection: $grouping) { groupChoices }.pickerStyle(.inline)
      }.accessibilityIdentifier("workspace-view-options")
    }
    ToolbarSpacer(.flexible)
    if let row {
      let item = row.item
      if item.running && !actions.revoked {
        ToolbarItemGroup {
          if actions.managed {
            Button("Restart…", systemImage: "arrow.counterclockwise") { actions.perform(.restart) }
            Button("Stop…", systemImage: "stop") { actions.perform(.stop) }
          }
          if item.Mode == "pipe" {
            Button("Send EOF", systemImage: "text.append") { actions.perform(.eof) }
              .disabled(!connection.attached).accessibilityIdentifier("session-send-eof")
          }
        }
      }
      if !item.running && actions.managed && !actions.revoked {
        ToolbarItem {
          Button("Start…", systemImage: "play.fill") { actions.perform(.restart) }
            .labelStyle(.titleAndIcon).buttonStyle(.borderedProminent)
        }
      } else if !actions.managed && !actions.revoked {
        ToolbarItem {
          Text("Attach only").foregroundStyle(.tertiary)
            .accessibilityIdentifier("session-attach-only")
        }
          .sharedBackgroundVisibility(.hidden)
      }
      if actions.reconnect {
        ToolbarItem { Button("Reconnect", systemImage: "arrow.clockwise") { actions.perform(.reconnect) } }
      }
      ToolbarSpacer(.fixed)
      ToolbarItemGroup {
        if actions.finder {
          Button("Show in Finder", systemImage: "folder") { actions.perform(.finder) }
            .accessibilityIdentifier("session-show-finder")
        }
        Toggle("Ports", systemImage: "sidebar.right", isOn: $state.ports)
          .accessibilityIdentifier("session-ports").help("Show or hide port forwarding")
      }
    }
    ToolbarItem {
      DesktopSearchField(query: $state.query).frame(width: 160)
    }.sharedBackgroundVisibility(.hidden)
  }
}
