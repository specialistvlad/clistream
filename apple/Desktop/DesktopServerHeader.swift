import AppKit
import SwiftUI

/// Observe rename on the section header itself so that changes reach a live header
/// even when the outline reuses the surrounding section.
struct DesktopServerHeader: View {
  let server: ServerProfile
  let group: [String]
  @ObservedObject var state: DesktopSelection
  let workspace: Workspace
  @FocusState private var focused: Bool

  var body: some View {
    if state.renaming == server.id && (state.renamingTarget == nil || state.renamingTarget == group) {
      InlineServerName(name: server.name) { value in
        if let value { workspace.rename(server, to: value) }
        state.renaming = nil
      }
      .accessibilityIdentifier("server-name-field")
    } else {
      HStack {
        Text(server.name)
        Spacer()
        HeaderLatency(workspace: workspace, server: server.id)
      }
      .contentShape(Rectangle())
      .accessibilityElement(children: .combine)
      .accessibilityLabel("Server " + server.name)
      .focusable()
      .focused($focused)
      .onTapGesture {
        state.selection = .server(server.id)
        state.renamingTarget = group
        focused = true
      }
      .onKeyPress(.return) {
        state.rename(server, in: group)
        return .handled
      }
      .contextMenu {
        Button("Rename…") { state.rename(server, in: group) }
        Button("Reconnect") { workspace.retry(server) }
        Divider()
        Button("Remove Server…", systemImage: "trash") { state.alert = .remove(server) }
      }
    }
  }
}

private struct HeaderLatency: View {
  let workspace: Workspace
  let server: String
  @State private var sample: String?
  var body: some View {
    Text(sample ?? "")
      .font(.caption).monospacedDigit()
      .onReceive(workspace.latencySamples.map { $0[server].map(DesktopTerminal.latency) }.removeDuplicates()) {
        sample = $0
      }
  }
}

private struct InlineServerName: NSViewRepresentable {
  let name: String
  let finish: (String?) -> Void

  final class Field: NSTextField {
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      guard let window else { return }
      DispatchQueue.main.async { [weak self, weak window] in
        guard let self, let window, self.window === window else { return }
        window.makeFirstResponder(self)
        self.selectText(nil)
      }
    }
  }
  final class Coordinator: NSObject, NSTextFieldDelegate {
    var finish: (String?) -> Void
    init(_ finish: @escaping (String?) -> Void) { self.finish = finish }
    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
      guard !textView.hasMarkedText() else { return false }
      if selector == #selector(NSResponder.insertNewline(_:)) {
        finish(textView.string)
        return true
      }
      if selector == #selector(NSResponder.cancelOperation(_:)) {
        finish(nil)
        return true
      }
      return false
    }
  }
  func makeCoordinator() -> Coordinator { Coordinator(finish) }
  func makeNSView(context: Context) -> Field {
    let field = Field(string: name)
    field.usesSingleLineMode = true
    field.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
    field.delegate = context.coordinator
    field.setAccessibilityElement(true)
    field.setAccessibilityRole(.textField)
    field.setAccessibilityLabel("Server name")
    field.setAccessibilityIdentifier("server-name-field")
    return field
  }
  func updateNSView(_ field: Field, context: Context) { context.coordinator.finish = finish }
}
