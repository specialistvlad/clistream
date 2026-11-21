import SwiftTerm
import SwiftUI

extension TerminalView: TerminalRenderer {
  func receive(_ bytes: Data) { feed(byteArray: Array(bytes)[...]) }
  var columns: Int { getTerminal().cols }
  var rows: Int { getTerminal().rows }
}

final class TerminalDelegate: NSObject, TerminalViewDelegate {
  let connection: Connection
  init(_ connection: Connection) { self.connection = connection }
  func send(source: TerminalView, data: ArraySlice<UInt8>) {
    let bytes = Data(data)
    MainActor.assumeIsolated {
      connection.send(.bytes(bytes))
    }
  }
  func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
    // During rotation UIKit can temporarily allocate less than one text row.
    // The PTY keeps its last usable size until the layout has a complete cell.
    let valid = 1...Int(UInt16.max)
    guard valid.contains(newCols), valid.contains(newRows) else { return }
    MainActor.assumeIsolated {
      connection.send(.resize(columns: newCols, rows: newRows))
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

private func visibleText(in view: TerminalView) -> String {
  let terminal = view.getTerminal()
  return (0..<terminal.rows).compactMap {
    terminal.getLine(row: $0)?.translateToString(trimRight: true)
  }
  .joined(separator: "\n").trimmingCharacters(in: .newlines)
}

#if os(macOS)
  private final class AccessibleTerminal: TerminalView {
    override func viewDidChangeEffectiveAppearance() {
      super.viewDidChangeEffectiveAppearance()
      effectiveAppearance.performAsCurrentDrawingAppearance {
        configureNativeColors()
        caretColor = .textColor
        selectedTextBackgroundColor = .selectedTextBackgroundColor
      }
      needsDisplay = true
    }
    override func accessibilityValue() -> Any? { visibleText(in: self) }
  }

  struct LiveTerminal: NSViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeNSView(context: Context) -> TerminalView {
      let view = connection.terminal ?? AccessibleTerminal(frame: .zero)
      view.terminalDelegate = context.coordinator
      view.setAccessibilityElement(true)
      view.setAccessibilityRole(.textArea)
      view.setAccessibilityLabel("Terminal")
      view.setAccessibilityIdentifier("session-terminal")
      connection.install(view)
      if !connection.readOnly { DispatchQueue.main.async { view.window?.makeFirstResponder(view) } }
      return view
    }
    func updateNSView(_ view: TerminalView, context: Context) {}
  }
#else
  private final class TouchTerminal: TerminalView {
    private lazy var controlLetters: [UIKeyCommand] = (97...122).map { code in
      let command = UIKeyCommand(
        input: String(UnicodeScalar(code)!), modifierFlags: .control,
        action: #selector(sendControlLetter(_:)))
      command.wantsPriorityOverSystemBehavior = true
      return command
    }

    override var keyCommands: [UIKeyCommand]? {
      let inherited = super.keyCommands ?? []
      guard isFirstResponder else { return inherited }
      // Applications that negotiate enhanced keyboard reporting own their
      // modifier encoding. In a conventional PTY these are ASCII controls.
      return getTerminal().keyboardEnhancementFlags.isEmpty ? inherited + controlLetters : inherited
    }

    @objc private func sendControlLetter(_ command: UIKeyCommand) {
      guard let byte = command.input?.utf8.first, (97...122).contains(byte) else { return }
      send(txt: String(UnicodeScalar(byte - 96)))
    }

    override var accessibilityValue: String? {
      get { visibleText(in: self) }
      set { super.accessibilityValue = newValue }
    }

    override func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
      if recognizer is UITapGestureRecognizer, !isFirstResponder, allowMouseReporting,
        getTerminal().mouseMode != .off
      {
        _ = becomeFirstResponder()
      }
      return super.gestureRecognizerShouldBegin(recognizer)
    }
  }

  struct LiveTerminal: UIViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeUIView(context: Context) -> TerminalView {
      let view = connection.terminal ?? TouchTerminal(frame: .zero)
      if connection.terminal == nil {
        // A steady initial caret also respects Reduce Motion. A child can
        // subsequently select its own cursor style with DECSCUSR.
        view.getTerminal().setCursorStyle(.steadyBlock)
      }
      view.terminalDelegate = context.coordinator
      view.accessibilityLabel = "Terminal"
      view.accessibilityIdentifier = "session-terminal"
      connection.install(view)
      DispatchQueue.main.async { _ = view.becomeFirstResponder() }
      return view
    }
    func updateUIView(_ view: TerminalView, context: Context) {}
  }
#endif

extension Connection {
  var terminal: TerminalView? { renderer as? TerminalView }
  func focusTerminal() {
    #if os(macOS)
      if let terminal { terminal.window?.makeFirstResponder(terminal) }
    #else
      _ = terminal?.becomeFirstResponder()
    #endif
  }

  #if os(iOS)
    func sendArrow(_ suffix: String) {
      guard let terminal else { return }
      terminal.send(
        txt: (terminal.getTerminal().applicationCursor ? "\u{1b}O" : "\u{1b}[") + suffix)
    }
  #endif
}
