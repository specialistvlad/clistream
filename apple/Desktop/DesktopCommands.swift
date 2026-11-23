import AppKit
import SwiftUI

@MainActor struct DesktopContext {
  let state: DesktopSelection
  let row: WorkspaceSession?
  let server: ServerProfile?
  let revoked: Bool
  let attached: Bool
  let openWindow: OpenWindowAction
  var managed: Bool { (row?.item.revision ?? 0) > 0 }
  var finder: Bool { row?.item.workspace == true && !revoked }
  var reconnect: Bool { revoked || (row?.item.running == true && !attached) }
  func perform(_ action: DesktopAction) {
    if let row { state.perform(action, on: row, using: openWindow) }
  }
  func rename(_ server: ServerProfile) {
    // A flat Host or CLI Name layout has no individual server header to edit.
    // Reveal it in the server grouping before making it the first responder.
    let grouping = UserDefaults.standard.string(forKey: "workspace-group") ?? "Server"
    let target = state.renamingTarget
    if target?.first == grouping && target?.contains(server.id) == true {
      state.rename(server, in: target)
    } else {
      UserDefaults.standard.set("Server", forKey: "workspace-group")
      state.rename(server, in: ["Server", server.id])
    }
  }
  func pasteInvitation() {
    let board = NSPasteboard.general
    state.pair(board.string(forType: .string) ?? board.string(forType: .URL) ?? "")
  }
}
private struct DesktopContextKey: FocusedValueKey { typealias Value = DesktopContext }
extension FocusedValues {
  var desktopContext: DesktopContext? {
    get { self[DesktopContextKey.self] }
    set { self[DesktopContextKey.self] = newValue }
  }
}

@MainActor @ViewBuilder var groupChoices: some View {
  ForEach(["Server", "Host", "CLI Name"], id: \.self) { Text($0).tag($0) }
}

struct DesktopCommands: Commands {
  @FocusedValue(\.desktopContext) private var context
  @AppStorage("workspace-list") private var flat = false
  @AppStorage("workspace-group") private var grouping = "Server"
  @Environment(\.openWindow) private var openWindow
  var body: some Commands {
    CommandGroup(replacing: .newItem) {
      Button("New Window") {
        if let context { context.state.open(nil, tab: false, using: openWindow) }
        else { openWindow(id: "workspace", value: UUID()) }
      }.keyboardShortcut("n")
      Button("New Tab") { context?.state.open(nil, tab: true, using: openWindow) }
        .disabled(context == nil).keyboardShortcut("t")
      Divider()
      Button("Add Server…") { context?.state.pair("") }.disabled(context == nil).keyboardShortcut("k")
      Button("Paste Invitation") { context?.pasteInvitation() }
        .disabled(context == nil)
    }
    CommandGroup(after: .sidebar) {
      Button(context?.state.ports == true ? "Hide Ports" : "Show Ports") { context?.state.ports.toggle() }
        .disabled(context?.row == nil).keyboardShortcut("i", modifiers: [.option, .command])
      Divider()
      Picker("Layout", selection: $flat) {
        Text("as Tree").tag(false).keyboardShortcut("1")
        Text("as List").tag(true).keyboardShortcut("2")
      }.pickerStyle(.inline)
      Menu("Group By") { Picker("Group By", selection: $grouping) { groupChoices }.pickerStyle(.inline) }
    }
    CommandMenu("Session") {
      let running = context?.row?.item.running ?? true
      Button(running ? "Restart…" : "Start…", systemImage: running ? "arrow.counterclockwise" : "play.fill") {
        context?.perform(.restart)
      }.keyboardShortcut("r").disabled(context?.managed != true || context?.revoked == true)
      Button("Stop…", systemImage: "stop") { context?.perform(.stop) }
        .keyboardShortcut(".").disabled(!running || context?.managed != true || context?.revoked == true)
      Button("Send EOF", systemImage: "text.append") { context?.perform(.eof) }
        .disabled(context?.row?.item.Mode != "pipe" || context?.attached != true)
      Button("Reconnect", systemImage: "arrow.clockwise") { context?.perform(.reconnect) }
        .disabled(context?.reconnect != true)
      Divider()
      Button("Open in New Tab") { context?.perform(.tab) }.disabled(context?.row == nil)
      Button("Open in New Window") { context?.perform(.window) }.disabled(context?.row == nil)
      Divider()
      Button("Show in Finder") { context?.perform(.finder) }
        .keyboardShortcut("r", modifiers: [.option, .command]).disabled(context?.finder != true)
    }
    CommandMenu("Server") {
      Button("Rename…") { if let context, let server = context.server { context.rename(server) } }
        .disabled(context?.server == nil)
      Button("Reconnect") { if let server = context?.server { Workspace.shared.retry(server) } }
        .disabled(context?.server == nil)
      Divider()
      Button("Remove Server…", systemImage: "trash") {
        if let server = context?.server { context?.state.alert = .remove(server) }
      }.keyboardShortcut(.delete).disabled(context?.server == nil)
    }
  }
}
