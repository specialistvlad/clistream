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
      #if os(macOS)
        if connection.selected?.Mode == "pipe", bytes == Data([13]),
          let event = NSApp.currentEvent, event.type == .keyDown, [36, 76].contains(event.keyCode) {
          connection.send(.bytes(Data([10])))
          return
        }
      #endif
      connection.send(.bytes(bytes))
    }
  }
  func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
    // During rotation UIKit can temporarily allocate less than one text row.
    // The PTY keeps its last usable size until the layout has a complete cell.
    let valid = 1...Int(UInt16.max)
    guard valid.contains(newCols), valid.contains(newRows) else { return }
    MainActor.assumeIsolated {
      guard connection.selected?.Mode == "pty" else { return }
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
    private var keyMonitor: Any?
    var focusOnMount = false

    func focusIfMounted() {
      guard focusOnMount, let window else { return }
      DispatchQueue.main.async { [weak self, weak window] in
        guard let self, let window, self.window === window, self.focusOnMount else { return }
        self.focusOnMount = false
        window.makeFirstResponder(self)
      }
    }

    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      if let keyMonitor { NSEvent.removeMonitor(keyMonitor); self.keyMonitor = nil }
      guard window != nil else { return }
      focusIfMounted()
      // AppKit dispatches key equivalents before sending keyDown to the
      // first responder. Only consume conventional control letters belonging
      // to this terminal. SwiftTerm handles negotiated keyboard protocols.
      keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
        guard let self, let window = self.window, event.window === window,
          window.firstResponder === self, self.getTerminal().keyboardEnhancementFlags.isEmpty,
          event.modifierFlags.intersection([.command, .option, .control, .shift]) == .control
        else { return event }
        if let byte = event.characters?.utf8.first, event.characters?.utf8.count == 1,
          (1...26).contains(byte) {
          self.send([byte])
          return nil
        }
        guard let letter = event.charactersIgnoringModifiers?.lowercased().utf8,
          letter.count == 1, let byte = letter.first, (97...122).contains(byte)
        else { return event }
        self.send([byte - 96])
        return nil
      }
    }
    override func mouseDown(with event: NSEvent) {
      window?.makeFirstResponder(self)
      super.mouseDown(with: event)
    }
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
    var suppressCursor = false
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeNSView(context: Context) -> TerminalView {
      let view = connection.terminal ?? AccessibleTerminal(
        frame: .zero, font: .monospacedSystemFont(ofSize: 12.5, weight: .regular))
      if connection.terminal == nil { view.getTerminal().setCursorStyle(.steadyBlock) }
      view.terminalDelegate = context.coordinator
      view.setAccessibilityElement(true)
      view.setAccessibilityRole(.textArea)
      view.setAccessibilityLabel("Terminal")
      view.setAccessibilityIdentifier("session-terminal")
      connection.install(view)
      if let terminal = view as? AccessibleTerminal {
        terminal.focusOnMount = !connection.readOnly
        terminal.focusIfMounted()
      }
      return view
    }
    func updateNSView(_ view: TerminalView, context: Context) {
      // The Metal renderer reads the emulator's cursor state during a redraw.
      if suppressCursor { view.getTerminal().hideCursor() }
    }
  }
#else
  private final class TouchTerminal: TerminalView {
    override func didMoveToWindow() {
      super.didMoveToWindow()
      guard window != nil else { return }
      if !(inputAccessoryView is TerminalKeyBar) { inputAccessoryView = TerminalKeyBar(terminal: self) }
      applyPalette()
      applyTextSize()
    }

    override init(frame: CGRect) {
      super.init(frame: frame)
      registerForTraitChanges(UITraitCollection.systemTraitsAffectingColorAppearance) {
        (view: TouchTerminal, _) in view.applyPalette()
      }
      registerForTraitChanges([UITraitPreferredContentSizeCategory.self, UITraitHorizontalSizeClass.self]) {
        (view: TouchTerminal, _) in view.applyTextSize()
      }
    }

    required init?(coder: NSCoder) { super.init(coder: coder) }

    private func applyPalette() {
      let foreground = UIColor.label.resolvedColor(with: traitCollection)
      let background = UIColor.systemBackground.resolvedColor(with: traitCollection)
      nativeForegroundColor = foreground
      nativeBackgroundColor = background
      backgroundColor = background
      caretColor = foreground
      caretTextColor = background
      setNeedsDisplay()
    }

    private func applyTextSize() {
      let base = UIFont.monospacedSystemFont(ofSize: traitCollection.horizontalSizeClass == .regular ? 13 : 12, weight: .regular)
      let scaled = UIFontMetrics(forTextStyle: .body).scaledFont(for: base, compatibleWith: traitCollection)
      if font != scaled { font = scaled }
    }

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
      view.returnByteSequence = connection.selected?.Mode == "pipe" ? [10] : [13]
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
