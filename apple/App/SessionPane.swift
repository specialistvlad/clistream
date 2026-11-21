import SwiftUI

struct SessionPane: View {
  @ObservedObject var connection: Connection
  let initialSession: SessionInfo
  var session: SessionInfo { connection.selected ?? initialSession }
  init(connection: Connection, session: SessionInfo) {
    self.connection = connection
    self.initialSession = session
  }
  @FocusState private var inputFocused: Bool

  var body: some View {
    VStack(alignment: .leading) {
      if (session.revision ?? 0) > 0 { SessionControls(connection: connection, session: session) }
      if session.running && !connection.attached {
        Button("Reconnect") { Task { await connection.reconnect(session) } }
      }
      if !session.running {
        ScrollView {
          Text(String(decoding: connection.stdout, as: UTF8.self)).textSelection(.enabled)
        }
      } else if session.Mode == "pty" {
        LiveTerminal(connection: connection).id(session.ID)
      } else {
        stream("Standard output", connection.stdout)
        stream("Standard error", connection.stderr)
        HStack {
          TextField(
            "Input", text: $connection.draft, prompt: Text("Input").foregroundStyle(.primary)
          ).accessibilityIdentifier("session-input")
            .focused(
              $inputFocused
            ).onSubmit(submit)
            .autocorrectionDisabled()
            #if os(iOS)
              .textInputAutocapitalization(.never)
            #endif
          Button("Send", action: submit)
          Button("EOF") { connection.send(.eof) }
        }.padding().disabled(!connection.attached)
      }
      if connection.attached, let ports = session.Ports, !ports.isEmpty {
        ForEach(ports.keys.sorted(), id: \.self) { name in
          HStack {
            Button(connection.forwarding[name] == nil ? "Forward \(name)" : "Stop \(name)") {
              Task { await connection.forward(name, item: session) }
            }
            if let address = connection.forwarding[name] {
              Text(address).textSelection(.enabled).accessibilityIdentifier(
                "forward-address-" + name)
            }
          }.padding(.horizontal)
        }
      }

      if !connection.status.isEmpty {
        Text(connection.status).font(.callout).padding(.horizontal)
      }
    }
    #if os(iOS)
      .tint(.primary)
    #endif
    #if os(macOS)
      .onChange(of: connection.attached, initial: true) { _, attached in
        if attached && session.Mode == "pipe" { inputFocused = true }
      }
    #endif
    .toolbar {
      #if os(iOS)
        if connection.attached && session.Mode == "pty" { ToolbarItem { terminalKeys } }
        if inputFocused {
          ToolbarItemGroup(placement: .keyboard) {
            Spacer()
            Button("Done") { inputFocused = false }.accessibilityIdentifier(
              "dismiss-input-keyboard")
          }
        }
      #endif
    }
  }
  #if os(iOS)
    private var terminalKeys: some View {
      Menu("Keys") {
        Button("Show Keyboard") { connection.focusTerminal() }
        Button("Hide Keyboard") { _ = connection.terminal?.resignFirstResponder() }
        Button("Escape") { connection.terminal?.send(txt: "\u{1b}") }
        Menu("Control Key") {
          ForEach(Array(65...90), id: \.self) { letter in
            Button("Control-" + String(UnicodeScalar(letter)!)) {
              connection.terminal?.send(txt: String(UnicodeScalar(letter - 64)!))
            }
          }
        }
        ForEach([("Up", "A"), ("Down", "B"), ("Right", "C"), ("Left", "D")], id: \.0) {
          label, suffix in
          Button(label + " Arrow") { connection.sendArrow(suffix) }
        }
      }
    }
  #endif

  func submit() {
    let data = Data((connection.draft + "\n").utf8)
    connection.send(.bytes(data))
    connection.draft = ""
  }

  func stream(_ title: String, _ data: Data) -> some View {
    VStack(alignment: .leading) {
      Text(title).font(.caption.weight(.semibold)).foregroundStyle(.primary)
      #if os(iOS)
        SelectableOutput(text: String(decoding: data, as: UTF8.self), title: title)
          .frame(maxWidth: .infinity, maxHeight: .infinity)
      #else
        ScrollView {
          Text(String(decoding: data, as: UTF8.self)).font(.system(.body, design: .monospaced))
            .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityIdentifier("pipe-" + title)
        }
      #endif
    }.padding(.horizontal)
  }
}

#if os(iOS)
  private struct SelectableOutput: UIViewRepresentable {
    let text: String
    let title: String

    func makeUIView(context: Context) -> UITextView {
      let view = UITextView()
      view.isEditable = false
      view.isSelectable = true
      view.backgroundColor = .clear
      view.adjustsFontForContentSizeCategory = true
      view.font = UIFontMetrics(forTextStyle: .body).scaledFont(
        for: .monospacedSystemFont(ofSize: 17, weight: .regular))
      view.accessibilityLabel = title
      view.accessibilityIdentifier = title
      view.setContentCompressionResistancePriority(.defaultLow, for: .vertical)
      return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
      guard view.text != text else { return }
      // Appending preserves the native selection and its handles while output arrives.
      if text.hasPrefix(view.text) {
        view.textStorage.append(
          NSAttributedString(
            string: String(text.dropFirst(view.text.count)), attributes: view.typingAttributes))
      } else {
        view.text = text
      }
    }
  }
#endif
