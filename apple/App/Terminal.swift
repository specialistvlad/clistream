import SwiftTerm
import SwiftUI

final class TerminalDelegate: NSObject, TerminalViewDelegate {
  let connection: Connection
  init(_ connection: Connection) { self.connection = connection }
  func send(source: TerminalView, data: ArraySlice<UInt8>) {
    let bytes = Data(data)
    MainActor.assumeIsolated {
      connection.send(["Op": "input", "Data": bytes.base64EncodedString()])
    }
  }
  func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
    MainActor.assumeIsolated {
      connection.send(["Op": "resize", "Cols": newCols, "Rows": newRows])
    }
  }
  func setTerminalTitle(source: TerminalView, title: String) {}
  func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
  func scrolled(source: TerminalView, position: Double) {}
  func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {}
  func bell(source: TerminalView) {}
  func clipboardCopy(source: TerminalView, content: Data) {}
  func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) {}
  func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

#if os(macOS)
  struct LiveTerminal: NSViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeNSView(context: Context) -> TerminalView {
      let view = connection.terminal ?? TerminalView(frame: .zero)
      view.terminalDelegate = context.coordinator
      view.setAccessibilityIdentifier("session-terminal")
      connection.install(view)
      DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
      return view
    }
    func updateNSView(_ view: TerminalView, context: Context) {}
  }
#else
  struct LiveTerminal: UIViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeUIView(context: Context) -> TerminalView {
      let view = connection.terminal ?? TerminalView(frame: .zero)
      view.terminalDelegate = context.coordinator
      view.accessibilityIdentifier = "session-terminal"
      connection.install(view)
      DispatchQueue.main.async { _ = view.becomeFirstResponder() }
      return view
    }
    func updateUIView(_ view: TerminalView, context: Context) {}
  }
#endif

extension Connection {
  func focusTerminal() {
    #if os(macOS)
      if let terminal { terminal.window?.makeFirstResponder(terminal) }
    #else
      _ = terminal?.becomeFirstResponder()
    #endif
  }

  #if os(iOS)
    func armControlKey() {
      focusTerminal()
      if let accessory = terminal?.inputAccessoryView as? TerminalAccessory {
        accessory.controlModifier = true
      } else {
        terminal?.controlModifier = true
      }
    }

    func sendArrow(_ suffix: String) {
      guard let terminal else { return }
      terminal.send(
        txt: (terminal.getTerminal().applicationCursor ? "\u{1b}O" : "\u{1b}[") + suffix)
    }
  #endif
}
