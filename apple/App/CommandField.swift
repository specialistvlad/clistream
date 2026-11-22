import SwiftUI

#if os(macOS)
  private typealias FieldColor = NSColor
#else
  private typealias FieldColor = UIColor
#endif

/// Decorations are independent of parsing. The shared Go parser decides which bytes to launch.
private func highlighted(_ line: String) -> NSAttributedString {
  let value = NSMutableAttributedString(string: line)
  var word = 0
  var quote: Character?
  var inWord = false
  var escaped = false
  var option = false
  var offset = 0
  for char in line {
    let length = String(char).utf16.count
    let color: FieldColor?
    if escaped {
      color = .systemOrange
      escaped = false
    } else if char == "\\" && quote != "'" {
      color = .systemOrange
      escaped = true
    } else if let delimiter = quote {
      color = .systemGreen
      if delimiter == char { quote = nil }
    } else if char.isWhitespace {
      if inWord { word += 1 }
      inWord = false
      option = false
      offset += length
      continue
    } else if char == "'" || char == "\"" {
      quote = char
      color = .systemGreen
    } else {
      if !inWord { option = char == "-" }
      color = word == 0 ? .systemBlue : (option ? .systemPurple : nil)
    }
    if let color {
      value.addAttribute(
        .foregroundColor, value: color, range: NSRange(location: offset, length: length))
    }
    offset += length
    inWord = true
  }
  return value
}

#if os(macOS)
  struct CommandField: NSViewRepresentable {
    @Binding var line: String
    let submit: () -> Void
    final class Coordinator: NSObject, NSTextFieldDelegate {
      var field: CommandField
      init(_ field: CommandField) { self.field = field }
      func controlTextDidChange(_ notification: Notification) {
        guard let control = notification.object as? NSTextField else { return }
        field.line = control.stringValue
        paint(control)
      }
      func controlTextDidBeginEditing(_ notification: Notification) {
        if let control = notification.object as? NSTextField { paint(control) }
      }
      func paint(_ control: NSTextField) {
        guard let editor = control.currentEditor() as? NSTextView, !editor.hasMarkedText(),
          let layout = editor.layoutManager
        else { return }
        let styled = highlighted(editor.string)
        let all = NSRange(location: 0, length: styled.length)
        layout.removeTemporaryAttribute(.foregroundColor, forCharacterRange: all)
        styled.enumerateAttribute(.foregroundColor, in: all) { color, range, _ in
          if let color {
            layout.addTemporaryAttribute(.foregroundColor, value: color, forCharacterRange: range)
          }
        }
      }
      @objc func commit() { field.submit() }
    }
    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeNSView(context: Context) -> NSTextField {
      let view = NSTextField(string: line)
      view.font = .monospacedSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)
      view.usesSingleLineMode = true
      view.delegate = context.coordinator
      view.target = context.coordinator
      view.action = #selector(Coordinator.commit)
      view.setAccessibilityLabel("Command")
      view.setAccessibilityIdentifier("command-line")
      view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
      DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
      return view
    }
    func updateNSView(_ view: NSTextField, context: Context) {
      context.coordinator.field = self
      if view.stringValue != line { view.stringValue = line }
    }
  }
#else
  struct CommandField: UIViewRepresentable {
    @Binding var line: String
    let submit: () -> Void
    final class Coordinator: NSObject, UITextFieldDelegate {
      var field: CommandField
      init(_ field: CommandField) { self.field = field }
      @objc func changed(_ view: UITextField) {
        let text = view.text ?? ""
        field.line = text
        guard view.markedTextRange == nil else { return }
        let selection = view.selectedTextRange
        view.attributedText = highlighted(text)
        view.selectedTextRange = selection
      }
      func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        field.submit()
        return false
      }
    }
    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeUIView(context: Context) -> UITextField {
      let view = UITextField()
      view.borderStyle = .roundedRect
      view.font = .monospacedSystemFont(
        ofSize: UIFont.preferredFont(forTextStyle: .body).pointSize, weight: .regular)
      view.autocorrectionType = .no
      view.autocapitalizationType = .none
      view.smartDashesType = .no
      view.smartQuotesType = .no
      view.smartInsertDeleteType = .no
      view.attributedText = highlighted(line)
      view.accessibilityLabel = "Command"
      view.accessibilityIdentifier = "command-line"
      view.delegate = context.coordinator
      view.addTarget(
        context.coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
      view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
      DispatchQueue.main.async { view.becomeFirstResponder() }
      return view
    }
    func updateUIView(_ view: UITextField, context: Context) {
      context.coordinator.field = self
      if view.text != line { view.attributedText = highlighted(line) }
    }
  }
#endif
