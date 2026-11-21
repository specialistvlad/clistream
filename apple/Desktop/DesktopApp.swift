import AppKit
import SwiftUI

@main
struct DesktopApp: App {
  var body: some Scene {
    WindowGroup("Sessions") { DesktopBrowser() }
      .defaultLaunchBehavior(.presented)
      .commands { SidebarCommands() }
  }
}

@MainActor
private final class SessionTabs: NSObject, NSWindowDelegate {
  static let shared = SessionTabs()
  static let showAttachment = Notification.Name("CLIStreamShowAttachment")
  private var windows: [String: (NSWindow, Connection)] = [:]

  func open(_ session: SessionInfo) {
    if let existing = windows[session.id] {
      if let current = existing.1.selected, current.ID != session.ID {
        Task { await existing.1.attach(session) }
      }
      existing.0.tabGroup?.selectedWindow = existing.0
      existing.0.makeKeyAndOrderFront(nil)
      NotificationCenter.default.post(name: Self.showAttachment, object: existing.1)
      return
    }
    let connection = Connection()
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 800, height: 520),
      styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false
    )
    window.contentView = NSHostingView(
      rootView: DesktopBrowser(activeTab: connection, initialSession: session))
    window.title = session.Name
    window.tabbingIdentifier = "CLIStreamSessions"
    window.tabbingMode = .preferred
    window.isReleasedWhenClosed = false
    window.delegate = self
    if let sibling = NSApp.keyWindow ?? windows.values.first?.0 {
      sibling.tabbingIdentifier = window.tabbingIdentifier
      sibling.tabbingMode = .preferred
      sibling.addTabbedWindow(window, ordered: .above)
    } else {
      window.center()
    }
    windows[session.id] = (window, connection)
    Task { await connection.attach(session) }
    window.makeKeyAndOrderFront(nil)
  }

  func closeAll() {
    for (window, _) in Array(windows.values) { window.close() }
  }

  func windowWillClose(_ notification: Notification) {
    guard let window = notification.object as? NSWindow,
      let key = windows.first(where: { $0.value.0 === window })?.key
    else { return }
    windows.removeValue(forKey: key)?.1.leave()
  }
}

private struct DesktopSession: View {
  @ObservedObject var connection: Connection
  let session: SessionInfo

  var body: some View {
    VStack(spacing: 0) {
      if let attached = connection.selected {
        SessionPane(connection: connection, session: attached)
      } else {
        Text(connection.status.isEmpty ? "Connecting…" : connection.status)
      }
      Divider()
      let current = connection.selected ?? session
      let detail = current.running ? "Run " + String(current.ID.prefix(8)) : "Stopped"
      Text("\(current.Host) · \(current.Mode.uppercased()) · \(detail)")
        .accessibilityIdentifier("desktop-session-status")
        .frame(maxWidth: .infinity, alignment: .leading).padding(6)
    }

  }
}

private struct DesktopBrowser: View {
  var activeTab: Connection? = nil
  var initialSession: SessionInfo? = nil
  @StateObject private var catalog = Connection()
  @StateObject private var preview = Connection(readOnly: true)
  @State private var selected: String?
  @State private var pairing: Pairing?
  private var item: SessionInfo? { catalog.sessions.first { $0.id == selected } }

  var body: some View {
    NavigationSplitView {
      List(selection: $selected) {
        if !catalog.listError.isEmpty && !catalog.sessions.isEmpty {
          Text(catalog.listError).accessibilityIdentifier("catalog-error")
        }
        ForEach(Set(catalog.sessions.map(\.Host)).sorted(), id: \.self) { host in
          Section(host) {
            ForEach(catalog.sessions.filter { $0.Host == host }) { app in
              Label(app.Name, systemImage: "terminal").tag(app.id)
                .accessibilityIdentifier("session-" + app.id)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
                .highPriorityGesture(TapGesture(count: 2).onEnded {
                  // Finish the list's selection event before revealing the active attachment.
                  DispatchQueue.main.async { SessionTabs.shared.open(app) }
                })
            }
          }
        }
      }
      .onKeyPress(.return) {
        guard let item else { return .ignored }
        SessionTabs.shared.open(item)
        return .handled
      }
      .navigationTitle("Sessions")
      .toolbar {
        Button("Pair") { pairing = Pairing() }.keyboardShortcut("k")
        Button("Refresh") { Task { await catalog.refresh() } }.keyboardShortcut("r")
      }
    } detail: {
      if let item {
        VStack(alignment: .leading) {
          if item.Mode == "pty", preview.selected?.ID == item.ID, item.running {
            LiveTerminal(connection: preview).id(item.id).allowsHitTesting(false)
          } else {
            ScrollView {
              Text(String(decoding: preview.stdout + preview.stderr, as: UTF8.self))
                .font(.system(.body, design: .monospaced)).textSelection(.enabled)
            }
          }
          if !preview.status.isEmpty {
            Text(preview.status).font(.callout).foregroundStyle(.secondary)
          }
          Spacer()
          Divider()
          HStack {
            Text("\(item.Host) · \(item.running ? "Running" : "Stopped") · Read-only preview")
            Spacer()
            Button("Open in Tab") { SessionTabs.shared.open(item) }.keyboardShortcut(.defaultAction)
          }.padding(8)
        }.navigationTitle(item.Name)
      } else if let activeTab, let initialSession {
        DesktopSession(connection: activeTab, session: initialSession)
          .navigationTitle(initialSession.Name)
      } else {
        ContentUnavailableView(
          catalog.sessions.isEmpty ? catalog.listTitle : "Select a CLI app",
          systemImage: "terminal",
          description: Text(
            catalog.sessions.isEmpty
              ? catalog.listDetail : "Select a supervisor to preview its terminal."))
      }
    }
    .onReceive(NotificationCenter.default.publisher(for: SessionTabs.showAttachment)) { event in
      if let owner = event.object as? Connection, owner === activeTab { selected = nil }
    }
    .onChange(of: catalog.pairingGeneration) { _, _ in
      selected = nil
      preview.leave()
      SessionTabs.shared.closeAll()
    }
    .modifier(PairingPresentation(connection: catalog, pairing: $pairing))
    .task { await catalog.watchSupervisors() }
    .task(id: item.map { $0.id + $0.ID }) {
      preview.leave()
      if let item { await preview.attach(item) }
    }
    .onDisappear { preview.leave() }
  }
}
