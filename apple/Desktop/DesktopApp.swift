import AppKit
import SwiftUI

@main struct DesktopApp: App {
  var body: some Scene {
    WindowGroup("CLIStream") { DesktopBrowser() }
      .defaultSize(width: 960, height: 560)
      .defaultLaunchBehavior(.presented)
      .commands { SidebarCommands() }
  }
}

@MainActor private final class DesktopSelection: ObservableObject {
  @Published var row: WorkspaceSession?
  weak var window: NSWindow?
  private static var auxiliaryWindows = [ObjectIdentifier: NSWindow]()
  func closed(_ window: NSWindow) {
    Self.auxiliaryWindows.removeValue(forKey: ObjectIdentifier(window))
    // A window owns its suspended terminals until another window opens them.
    for case let key as NSString in Self.owners.keyEnumerator().allObjects
    where Self.owners.object(forKey: key) === self {
      Self.owners.removeObject(forKey: key)
      Workspace.shared.detach(key as String)
    }
    row = nil
  }
  private static let owners = NSMapTable<NSString, DesktopSelection>.strongToWeakObjects()
  init(_ row: WorkspaceSession? = nil) { self.row = row }
  func bind(_ window: NSWindow) {
    self.window = window
    window.tabbingIdentifier = "CLIStream"
    if let row { Self.owners.setObject(self, forKey: row.id as NSString) }
  }
  func activate(_ next: WorkspaceSession) {
    Workspace.shared.activate(next)
    if let owner = Self.owners.object(forKey: next.id as NSString), owner.row?.id == next.id,
      let existing = owner.window,
      existing.isVisible || existing.tabGroup != nil
    {
      existing.tabGroup?.selectedWindow = existing
      existing.makeKeyAndOrderFront(nil)
      let connection = Workspace.shared.terminal(next)
      connection.focusInput.send()
      if let terminal = connection.terminal { existing.makeFirstResponder(terminal) }
      return
    }
    row = next
    Self.owners.setObject(self, forKey: next.id as NSString)
  }
  func open(_ next: WorkspaceSession, tab: Bool) {
    if let owner = Self.owners.object(forKey: next.id as NSString), owner.row?.id == next.id,
      let existing = owner.window
    {
      if tab, let window, window !== existing,
        window.tabGroup !== existing.tabGroup || window.tabGroup == nil
      {
        window.addTabbedWindow(existing, ordered: .above)
      } else if !tab, let group = existing.tabGroup, group.windows.count > 1 {
        group.removeWindow(existing)
      }
      existing.tabGroup?.selectedWindow = existing
      existing.makeKeyAndOrderFront(nil)
      return
    }
    if tab && row == nil {
      activate(next)
      return
    }
    let state = DesktopSelection(next)
    let child = NSWindow(
      contentViewController: NSHostingController(rootView: DesktopBrowser(state: state)))
    child.setContentSize(window?.contentLayoutRect.size ?? NSSize(width: 960, height: 560))
    child.styleMask.formUnion([.resizable, .closable, .miniaturizable])
    child.isReleasedWhenClosed = false
    Self.auxiliaryWindows[ObjectIdentifier(child)] = child
    state.bind(child)
    child.title = next.item.Name
    if tab, let window { window.addTabbedWindow(child, ordered: .above) } else { child.center() }
    child.makeKeyAndOrderFront(nil)
  }
}

private struct WindowReader: NSViewRepresentable {
  let state: DesktopSelection
  final class Reader: NSView {
    var state: DesktopSelection?
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
  func updateNSView(_ nsView: Reader, context: Context) {}
}

private struct DesktopBrowser: View {
  @ObservedObject private var workspace = Workspace.shared
  @StateObject private var state: DesktopSelection
  @State private var pairing: Pairing?
  init(state: DesktopSelection? = nil) {
    _state = StateObject(wrappedValue: state ?? DesktopSelection())
  }
  var body: some View {
    NavigationSplitView {
      WorkspaceSidebar(
        workspace: workspace, selected: state.row?.id, activate: state.activate,
        openElsewhere: state.open, pairing: $pairing
      )
      .navigationSplitViewColumnWidth(min: 210, ideal: 265, max: 400)
    } detail: {
      if let row = state.row {
        WorkspaceTerminal(connection: workspace.terminal(row), workspace: workspace, row: row).id(
          row.id
        )
        .navigationTitle(row.item.Name)
        .navigationSubtitle(
          row.item.Host + " · "
            + (workspace.profiles.first { $0.id == row.server.id }?.name ?? row.server.name))
      } else {
        ContentUnavailableView {
          Text(workspace.profiles.isEmpty ? "Pair with a server" : "Select a CLI")
        } description: {
          Text(
            workspace.profiles.isEmpty
              ? "Use an invitation from clistreamd."
              : "Choose a program in the sidebar to open its terminal.")
        } actions: {
          if workspace.profiles.isEmpty { Button("Add Server…") { pairing = Pairing() } }
        }
      }
    }.background(WindowReader(state: state))
      .onReceive(NotificationCenter.default.publisher(for: NSWindow.willCloseNotification)) {
        event in
        if let closed = event.object as? NSWindow, closed === state.window {
          state.closed(closed)
        }
      }
      .modifier(
        PairingPresentation(
          connection: workspace.pairing, pairing: $pairing, enroll: workspace.enroll)
      )
      .task { workspace.load() }
  }
}
