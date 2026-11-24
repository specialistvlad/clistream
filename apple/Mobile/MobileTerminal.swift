import SwiftUI

@available(iOS 27.1, *)
struct HingeObserver: UIViewRepresentable {
  @Binding var closed: Bool
  func makeUIView(context: Context) -> UIView {
    let view = UIView()
    view.isUserInteractionEnabled = false
    view.addInteraction(
      UIHingeInteraction { _, update in
        let value = update.hinge?.status == .closed
        if closed != value { closed = value }
      })
    return view
  }
  func updateUIView(_ view: UIView, context: Context) {}
}

extension EnvironmentValues {
  @Entry var regularWorkspace = false
  @Entry var closedHinge = false
}

struct MobileTerminal: View {
  @ObservedObject var workspace: Workspace
  @ObservedObject var connection: Connection
  @ObservedObject var actions: MobileLifecycle
  let row: WorkspaceSession
  let pairAgain: () -> Void
  let detach: () -> Void
  @Environment(\.regularWorkspace) private var regular
  @Environment(\.closedHinge) private var closedHinge
  @State private var ports = false
  @State private var showingFiles = false
  @State private var filesError: String?
  @State private var filesPath = ""
  @State private var keyboard = false
  @State private var sample: String?
  @State private var grid = ""
  private var item: SessionInfo { connection.selected ?? row.item }
  private var target: WorkspaceSession { WorkspaceSession(server: row.server, item: item) }
  private var outer: Bool { !regular && closedHinge }
  private var revoked: Bool {
    workspace.revoked(row.server) || connection.status.localizedCaseInsensitiveContains("revoked")
  }
  private var online: Bool {
    !revoked && (connection.attached || (!item.running && workspace.errors[row.server.id] == ""))
  }
  private var status: String {
    guard online else { return item.running && !revoked ? "Reconnecting…" : "Disconnected" }
    return ["Connected", sample].compactMap { $0 }.joined(
      separator: " · ")
  }
  private var subtitle: String {
    if !regular { return item.Host + " — " + row.server.name }
    return [item.Host, status, online ? mode : nil].compactMap { $0 }.joined(separator: " · ")
  }
  private var mode: String {
    item.Mode == "pty" ? (regular && !grid.isEmpty ? "PTY " + grid : "PTY") : "Pipe"
  }

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      VStack(alignment: .leading, spacing: 0) { terminalContent }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .overlay(alignment: .bottomTrailing) {
          if !keyboard && !outer && connection.attached {
            showKeyboard.controlSize(.large).buttonStyle(.glass).buttonBorderShape(.circle)
              .padding()
          }
        }
      if !regular {
        HStack {
          Circle().fill(online ? Color.green : .secondary).frame(width: 8, height: 8)
          Text(status).accessibilityIdentifier("desktop-session-status")
          Spacer()
          Text(mode)
        }.font(.footnote).padding(.horizontal).padding(.vertical, 5)
      }
    }
    .safeAreaInset(edge: .trailing, spacing: 0) {
      if outer {
        VStack {
          portsButton
          sessionMenu
          if !keyboard { showKeyboard }
          Spacer()
        }.labelStyle(.iconOnly).buttonStyle(.glass).buttonBorderShape(.circle).controlSize(.large)
          .accessibilityElement(children: .contain).accessibilityIdentifier("duo-terminal-tools")
      }
    }
    .navigationTitle(item.Name).navigationBarTitleDisplayMode(.inline)
    .navigationSubtitle(subtitle)
    .toolbar {
      if !outer {
        if regular {
          ToolbarItemGroup(placement: .topBarTrailing) { lifecycleButtons; editArguments }
          ToolbarSpacer(.fixed, placement: .topBarTrailing)
          ToolbarItemGroup(placement: .topBarTrailing) {
            if item.workspace == true && !revoked { filesButton }
            portsButton
          }
          ToolbarSpacer(.fixed, placement: .topBarTrailing)
          ToolbarItem(placement: .topBarTrailing) { sessionMenu }
        } else {
          ToolbarItemGroup(placement: .topBarTrailing) {
            portsButton
            sessionMenu
          }
        }
      }
    }
    .alert("Show in Files", isPresented: $showingFiles) {
      Button("OK") {}
    } message: {
      Text(
        (filesError.map { $0 + "\n\n" } ?? "") + "Open Files and go to "
          + filesPath
          + ". If CLIStream is hidden, tap Edit in Browse and enable CLIStream under Locations. Tap Turn On if Files asks.")
    }
    .modifier(PortsPresentation(connection: connection, session: item, presented: $ports))
    .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidShowNotification)) {
      _ in keyboard = true
    }
    .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidHideNotification)) {
      _ in keyboard = false
    }
    .task(id: connection.attached) {
      sample = nil
      while !Task.isCancelled {
        if connection.attached { await connection.measureLatency() }
        sample = (connection.attached ? connection.latency : workspace.samples[row.server.id]).map(
          MobileServers.latency)
        if let renderer = connection.renderer { grid = "\(renderer.columns) × \(renderer.rows)" }
        try? await Task.sleep(for: .seconds(2))
      }
    }
  }
  @ViewBuilder private var terminalContent: some View {
    if let current = connection.selected {
      if current.running {
        LiveTerminal(connection: connection).id(current.ID)
          .padding(.horizontal, regular ? 24 : 14)
      } else {
        ScrollView {
          Text(String(decoding: connection.stdout, as: UTF8.self))
            .font(.system(.body, design: .monospaced)).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
      }
      if revoked {
        Text("This device’s access to " + row.server.name + " was revoked. Pair again with a fresh invitation.")
          .font(.footnote).foregroundStyle(.secondary).padding(.horizontal)
      } else if !connection.status.isEmpty {
        Text(connection.status).font(.footnote).foregroundStyle(.secondary).padding(.horizontal)
      }
    } else {
      ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
    }
  }
  private var showKeyboard: some View {
    Button("Show Keyboard", systemImage: "keyboard") { connection.focusTerminal() }
      .labelStyle(.iconOnly).accessibilityIdentifier("show-keyboard")
  }
  private var filesButton: some View {
    Button("Show in Files", systemImage: "folder") {
      Task {
        do {
          filesPath = try await FinderWorkspace.prepareForBrowsing(target, in: workspace)
          filesError = nil
        } catch {
          filesPath = FinderWorkspace.filesPath(target, in: workspace)
          filesError = error.localizedDescription
        }
        showingFiles = true
      }
    }
    .accessibilityIdentifier("session-show-files")
  }
  private var portsButton: some View {
    Button("Ports", systemImage: regular ? "sidebar.right" : "network") { ports.toggle() }
      .accessibilityIdentifier("session-ports")
  }
  @ViewBuilder private var lifecycleButtons: some View {
    let row = target
    Button(
      item.running ? "Restart" : "Start",
      systemImage: item.running ? "arrow.counterclockwise" : "play"
    ) {
      Task { await actions.perform(row.item.running ? .restart : .start, row, in: workspace) }
    }.disabled(revoked || (item.revision ?? 0) == 0)
      .accessibilityIdentifier(item.running ? "session-restart" : "session-start")
    Button("Stop", systemImage: "stop", role: .destructive) {
      Task { await actions.perform(.stop, row, in: workspace) }
    }
      .disabled(revoked || !item.running || (item.revision ?? 0) == 0).accessibilityIdentifier("session-stop")
  }
  @ViewBuilder private var editArguments: some View {
    if item.editable == true && !revoked {
      Button("Edit Arguments…", systemImage: "pencil") {
        let row = target
        Task { await actions.review(row) }
      }.accessibilityIdentifier("session-edit-arguments")
    }
  }
  private var sessionMenu: some View {
    Menu("Session", systemImage: "ellipsis") {
      ControlGroup {
        lifecycleButtons
        Button("Send EOF", systemImage: "text.append") { connection.send(.eof) }
          .disabled(!connection.attached || item.Mode != "pipe").accessibilityIdentifier(
            "session-send-eof")
      }
      Divider()
      editArguments
      if item.running && !connection.attached {
        Button("Reconnect", systemImage: "arrow.clockwise") {
          Task { await connection.reconnect(item) }
        }
      }
      if revoked {
        Button("Pair Again…", systemImage: "link", action: pairAgain)
      }
      Button("Ports", systemImage: "network") { ports = true }
      if item.workspace == true && !revoked { filesButton }
      Divider()
      Button("Detach", systemImage: "rectangle.portrait.and.arrow.right", action: detach)
    }.accessibilityIdentifier("session-menu")
  }
}
