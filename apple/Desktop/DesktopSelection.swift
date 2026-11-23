import AppKit
import SwiftUI

enum SidebarSelection: Hashable {
  case server(String)
  case session(String)
}

enum DesktopAction {
  case restart, stop, eof, reconnect, ports, finder, tab, window
}

enum DesktopSheet: Identifiable {
  case pair(String), command(WorkspaceSession, String)

  var id: String {
    switch self {
    case .pair: return "pair"
    case .command(let row, _): return row.id
    }
  }
}

enum DesktopAlert: Identifiable {
  case stop(WorkspaceSession), restart(WorkspaceSession), remove(ServerProfile), pairError(String), error(String)

  var id: String { title }
  var title: String {
    switch self {
    case .stop(let row): return "Stop “" + row.item.Name + "”?"
    case .restart(let row): return "Restart “" + row.item.Name + "”?"
    case .remove(let server): return "Remove “" + server.name + "”?"
    case .pairError: return "Couldn’t pair this device"
    case .error: return "Action failed"
    }
  }
  var message: String {
    switch self {
    case .stop, .restart: return "This ends the current run for everyone connected."
    case .remove: return "This Mac forgets the server and its pairing. The device stays enrolled until the server operator revokes it."
    case .error(let message), .pairError(let message): return message
    }
  }
}

@MainActor final class DesktopSelection: ObservableObject {
  @Published var row: WorkspaceSession?
  @Published var selection: SidebarSelection?
  @Published var renaming: String?
  var renamingTarget: [String]?
  @Published var query = ""
  @Published var ports = false
  @Published var sheet: DesktopSheet?
  @Published var alert: DesktopAlert?
  weak var window: NSWindow?
  private weak var tabParent: NSWindow?
  private var pendingWindow = false
  private static var pending = [UUID: DesktopSelection]()
  private static let owners = NSMapTable<NSString, DesktopSelection>.strongToWeakObjects()
  static func take(_ id: UUID?) -> DesktopSelection {
    if let id, let value = pending.removeValue(forKey: id) { return value }
    return DesktopSelection()
  }
  private static func owner(_ row: WorkspaceSession) -> DesktopSelection? {
    guard let owner = owners.object(forKey: row.id as NSString), owner.row?.id == row.id,
      owner.window?.isVisible == true || owner.window?.tabGroup != nil else { return nil }
    return owner
  }
  func bind(_ window: NSWindow) {
    guard self.window !== window else { return }
    self.window = window
    window.tabbingIdentifier = "CLIStream"
    if let row { Self.owners.setObject(self, forKey: row.id as NSString) }
    guard pendingWindow else { return }
    pendingWindow = false
    DispatchQueue.main.async { [weak self, weak window] in
      guard let self, let window else { return }
      if let parent = self.tabParent, parent !== window {
        if window.tabGroup !== parent.tabGroup || parent.tabGroup == nil {
          parent.addTabbedWindow(window, ordered: .above)
        }
        window.tabGroup?.selectedWindow = window
      } else if let group = window.tabGroup, group.windows.count > 1 {
        group.removeWindow(window)
      }
      window.makeKeyAndOrderFront(nil)
    }
  }
  func closed() {
    for case let key as NSString in Self.owners.keyEnumerator().allObjects
    where Self.owners.object(forKey: key) === self {
      Self.owners.removeObject(forKey: key)
      Workspace.shared.detach(key as String)
    }
    row = nil
  }
  @discardableResult func activate(_ next: WorkspaceSession) -> DesktopSelection {
    let owner = Self.owner(next) ?? self
    if !Workspace.shared.revoked(next.server) { Workspace.shared.activate(next) }
    owner.row = next
    owner.selection = .session(next.id)
    owner.renaming = nil
    owner.renamingTarget = nil
    Self.owners.setObject(owner, forKey: next.id as NSString)
    if let window = owner.window, owner !== self {
      window.tabGroup?.selectedWindow = window
      window.makeKeyAndOrderFront(nil)
    }
    Workspace.shared.focus(next.id)
    return owner
  }
  func open(_ next: WorkspaceSession?, tab: Bool, using openWindow: OpenWindowAction) {
    if let next, let owner = Self.owner(next), let existing = owner.window {
      if tab, let window, existing !== window,
        window.tabGroup !== existing.tabGroup || window.tabGroup == nil {
        window.addTabbedWindow(existing, ordered: .above)
      } else if !tab, let group = existing.tabGroup, group.windows.count > 1 {
        group.removeWindow(existing)
      }
      existing.tabGroup?.selectedWindow = existing
      existing.makeKeyAndOrderFront(nil)
      return
    }
    let child = DesktopSelection()
    child.row = next
    child.selection = next.map { .session($0.id) }
    child.pendingWindow = true
    if tab { child.tabParent = window }
    let id = UUID()
    Self.pending[id] = child
    openWindow(id: "workspace", value: id)
  }
  func pair(_ link: String) {
    let value = link.trimmingCharacters(in: .whitespacesAndNewlines)
    if !value.isEmpty, let error = Core.inspect(value).Error {
      pairingFailed(error)
    } else { sheet = .pair(value) }
  }
  func pairingFailed(_ error: String) {
    let message = ["invitation", "token", "expired"].contains { error.localizedCaseInsensitiveContains($0) }
      ? "The invitation expired or was replaced. Ask the server operator for a new one with clistreamd invite."
      : error
    alert = .pairError(message)
  }
  func perform(_ action: DesktopAction, on row: WorkspaceSession, using openWindow: OpenWindowAction) {
    switch action {
    case .tab, .window:
      open(row, tab: action == .tab, using: openWindow)
    default:
      let owner = row.id == self.row?.id ? self : activate(row)
      owner.selection = .session(row.id)
      let connection = Workspace.shared.terminal(row)
      switch action {
      case .restart:
        if row.item.editable == true {
          Task {
            let reply = await connection.manage(.command, item: row.item)
            guard owner.row?.id == row.id else { return }
            if let argv = reply.Command, let line = Core.call(["Op": "format-command",
              "Command": argv.map { $0.base64EncodedString() }]).Line {
              owner.sheet = .command(row, line)
            } else { owner.alert = .error(reply.Error ?? "Command unavailable") }
          }
        } else if row.item.running { owner.alert = .restart(row) }
        else { owner.change(.start, row: row) }
      case .stop: owner.alert = .stop(row)
      case .eof: connection.send(.eof)
      case .ports: owner.ports = true
      case .reconnect:
        Workspace.shared.retry(row.server)
        Task { await connection.reconnect(row.item) }
      case .finder:
        Task {
          do { try await FinderWorkspace.show(serverID: row.server.id, host: row.item.Host, workspaceID: row.item.id) }
          catch { owner.alert = .error(error.localizedDescription) }
        }
      case .tab, .window: break
      }
    }
  }
  func change(_ action: SessionAction, row: WorkspaceSession, arguments: [Data]? = nil) {
    let connection = Workspace.shared.terminal(row)
    Task {
      if let error = await connection.changeRun(action, item: row.item, arguments: arguments) {
        alert = .error(error)
      }
    }
  }
  func rename(_ server: ServerProfile, in target: [String]? = nil) {
    renamingTarget = target
    selection = .server(server.id)
    renaming = server.id
  }
}

struct DesktopWindowReader: NSViewRepresentable {
  let state: DesktopSelection
  final class Reader: NSView {
    weak var state: DesktopSelection?
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      if let window { state?.bind(window) }
    }
  }
  func makeNSView(context: Context) -> Reader {
    let view = Reader()
    view.state = state
    return view
  }
  func updateNSView(_ view: Reader, context: Context) {}
}
