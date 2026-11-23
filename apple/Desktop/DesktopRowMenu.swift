import AppKit
import Combine
import SwiftUI

/// Keep the secondary-click target bound to the clicked item throughout AppKit menu tracking.
@MainActor enum DesktopMenuEntry {
  case command(String, String? = nil, () -> Void)
  case separator

  @MainActor private final class Action: NSObject {
    let perform: () -> Void
    init(_ perform: @escaping () -> Void) { self.perform = perform }

    @objc func choose(_ sender: NSMenuItem) {
      // A sheet or field editor can take focus once the tracking menu has closed.
      RunLoop.main.perform(inModes: [.default]) { MainActor.assumeIsolated { self.perform() } }
    }
  }

  static func menu(_ entries: [Self]) -> NSMenu {
    let menu = NSMenu()
    menu.autoenablesItems = false
    for entry in entries {
      switch entry {
      case .separator: menu.addItem(.separator())
      case .command(let title, let symbol, let perform):
        let action = Action(perform)
        let item = NSMenuItem(title: title, action: #selector(Action.choose(_:)), keyEquivalent: "")
        item.target = action
        item.representedObject = action
        if let symbol { item.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil) }
        menu.addItem(item)
      }
    }
    return menu
  }
}

/// The List also exposes its contextual commands to the keyboard and VoiceOver.
struct DesktopAccessibleMenu: View {
  let entries: [DesktopMenuEntry]
  var body: some View {
    ForEach(entries.indices, id: \.self) { index in
      switch entries[index] {
      case .separator: Divider()
      case .command(let title, let symbol, let action):
        Button { DispatchQueue.main.async(execute: action) } label: {
          if let symbol { Label(title, systemImage: symbol).labelStyle(.titleAndIcon) }
          else { Text(title) }
        }
      }
    }
  }
}

/// Draw the target using the system's focus ring and current keyboard appearance.
@MainActor private func drawMenuTarget(in view: NSView) {
  NSGraphicsContext.saveGraphicsState()
  NSFocusRingPlacement.only.set()
  let mask = NSButtonCell(textCell: "")
  mask.bezelStyle = .rounded
  mask.drawFocusRingMask(withFrame: view.bounds.insetBy(dx: 2, dy: 2), in: view)
  NSGraphicsContext.restoreGraphicsState()
}

/// Primary clicks and keyboard selection stay with the native sidebar list.
struct DesktopRowMenu: NSViewRepresentable {
  let entries: () -> [DesktopMenuEntry]
  final class Target: NSView, NSMenuDelegate {
    private var tracking = false
    func menuWillOpen(_ menu: NSMenu) { tracking = true; needsDisplay = true }
    func menuDidClose(_ menu: NSMenu) { tracking = false; needsDisplay = true }
    override func draw(_ dirtyRect: NSRect) {
      super.draw(dirtyRect)
      if tracking { drawMenuTarget(in: self) }
    }
    var entries: () -> [DesktopMenuEntry] = { [] }
    override func hitTest(_ point: NSPoint) -> NSView? {
      guard let event = NSApp.currentEvent,
        event.type == .rightMouseDown || event.type == .leftMouseDown && event.modifierFlags.contains(.control)
      else { return nil }
      return super.hitTest(point)
    }
    override func menu(for event: NSEvent) -> NSMenu? {
      let menu = DesktopMenuEntry.menu(entries())
      menu.delegate = self
      return menu
    }
  }
  func makeNSView(context: Context) -> Target { Target() }
  func updateNSView(_ view: Target, context: Context) { view.entries = entries }
}

/// A section header is not a selectable outline row. Keeping one AppKit view
/// alive for its label and editor preserves its identity and first responder.
struct DesktopServerHeader: NSViewRepresentable {
  let server: ServerProfile
  let group: [String]
  let state: DesktopSelection
  let workspace: Workspace

  final class Header: NSStackView, NSTextFieldDelegate, NSMenuDelegate {
    private let title = NSTextField(labelWithString: "")
    private let sample = NSTextField(labelWithString: "")
    private var model: DesktopServerHeader?
    private var subscription: AnyCancellable?
    private var latencySubscription: AnyCancellable?
    private var editing = false

    override init(frame frameRect: NSRect) {
      super.init(frame: frameRect)
      orientation = .horizontal
      alignment = .centerY
      spacing = 4
      title.font = .systemFont(ofSize: NSFont.smallSystemFontSize, weight: .semibold)
      title.textColor = .tertiaryLabelColor
      title.setAccessibilityElement(false)
      title.lineBreakMode = .byTruncatingTail
      title.usesSingleLineMode = true
      title.delegate = self
      title.setContentHuggingPriority(.defaultLow, for: .horizontal)
      title.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
      sample.font = .preferredFont(forTextStyle: .caption1)
      sample.textColor = .tertiaryLabelColor
      sample.setAccessibilityElement(false)
      addArrangedSubview(title)
      addArrangedSubview(sample)
    }
    required init?(coder: NSCoder) { super.init(coder: coder) }
    func update(_ value: DesktopServerHeader) {
      let needsObserver = model?.state !== value.state || model?.group != value.group
      model = value
      if needsObserver {
        latencySubscription = value.workspace.latencySamples
          .map { $0[value.server.id].map(DesktopTerminal.latency) }.removeDuplicates()
          .sink { [weak self] latency in
            guard let self else { return }
            self.sample.stringValue = latency ?? ""
            self.sample.isHidden = self.editing || latency == nil
          }
        subscription = value.state.$renaming.sink { [weak self] id in self?.updateEditor(id) }
      }
      updateEditor(value.state.renaming)
    }
    private func updateEditor(_ id: String?) {
      guard let model else { return }
      let active = id == model.server.id && (model.state.renamingTarget == nil || model.state.renamingTarget == model.group)
      if !editing { title.stringValue = model.server.name }
      guard active != editing else {
        if !active { setAccessibilityLabel("Server " + model.server.name) }
        return
      }
      editing = active
      title.isEditable = active
      title.isSelectable = active
      title.isBezeled = active
      title.bezelStyle = .roundedBezel
      title.drawsBackground = active
      title.textColor = active ? .labelColor : .tertiaryLabelColor
      title.setAccessibilityElement(active)
      title.setAccessibilityIdentifier(active ? "server-name-field" : nil)
      title.setAccessibilityLabel(active ? "Server name" : nil)
      setAccessibilityElement(!active)
      setAccessibilityRole(.group)
      setAccessibilityLabel(active ? nil : "Server " + model.server.name)
      sample.isHidden = active || sample.stringValue.isEmpty
      if active {
        title.setAccessibilityRole(.textField)
        RunLoop.main.perform(inModes: [.default]) { [weak self] in
          guard let self, self.editing else { return }
          self.window?.makeFirstResponder(self.title)
          self.title.selectText(nil)
        }
      } else { title.stringValue = model.server.name }
      NSAccessibility.post(element: self, notification: .layoutChanged)
    }
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      setAccessibilityElement(!editing)
      setAccessibilityRole(.group)
    }
    override var acceptsFirstResponder: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? {
      guard let target = super.hitTest(point) else { return nil }
      return editing ? target : self
    }
    override func mouseDown(with event: NSEvent) {
      guard let model else { return }
      if event.modifierFlags.contains(.control), let menu = menu(for: event) {
        NSMenu.popUpContextMenu(menu, with: event, for: self)
      } else {
        window?.makeFirstResponder(self)
        model.state.selection = .server(model.server.id)
        model.state.renamingTarget = model.group
      }
    }
    override func keyDown(with event: NSEvent) {
      if [36, 76].contains(event.keyCode), let model {
        model.state.rename(model.server, in: model.group)
      } else { super.keyDown(with: event) }
    }
    private var tracking = false
    func menuWillOpen(_ menu: NSMenu) { tracking = true; needsDisplay = true }
    func menuDidClose(_ menu: NSMenu) { tracking = false; needsDisplay = true }
    override func draw(_ dirtyRect: NSRect) {
      super.draw(dirtyRect)
      if tracking { drawMenuTarget(in: self) }
    }
    override func accessibilityPerformShowMenu() -> Bool {
      guard let menu = serverMenu() else { return false }
      return menu.popUp(positioning: nil, at: NSPoint(x: bounds.minX, y: bounds.midY), in: self)
    }
    override func menu(for event: NSEvent) -> NSMenu? { serverMenu() }
    private func serverMenu() -> NSMenu? {
      guard let model else { return nil }
      let menu = DesktopMenuEntry.menu([
        .command("Rename…") { model.state.rename(model.server, in: model.group) },
        .command("Reconnect") { model.workspace.retry(model.server) },
        .separator, .command("Remove Server…", "trash") { model.state.alert = .remove(model.server) }
      ])
      menu.delegate = self
      return menu
    }
    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
      guard !textView.hasMarkedText(), let model else { return false }
      switch selector {
      case #selector(NSResponder.insertNewline(_:)):
        model.workspace.rename(model.server, to: textView.string)
        model.state.renaming = nil
        return true
      case #selector(NSResponder.cancelOperation(_:)):
        model.state.renaming = nil
        return true
      default: return false
      }
    }
  }

  func makeNSView(context: Context) -> Header { Header() }
  func updateNSView(_ header: Header, context: Context) { header.update(self) }
}
