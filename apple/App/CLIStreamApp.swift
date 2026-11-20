import SwiftUI

@main
struct CLIStreamApp: App {
  var body: some Scene {
    WindowGroup { BrowserView() }
      #if os(macOS)
        .commands { SessionCommands() }
      #endif
  }
}

private struct Pairing: Identifiable {
  let id = UUID()
  var link: URL?
  var address: String?
}

struct BrowserView: View {
  @StateObject private var connection = Connection()
  @State private var invitation = ""
  @State private var pastedAddress: String?
  @State private var browsing = false
  @State private var pairing: Pairing?
  @State private var line = ""
  @State private var column = NavigationSplitViewColumn.sidebar
  @State private var selection: String?
  @State private var folders = [String: WorkspaceLocation]()
  @FocusState private var inputFocused: Bool
  @FocusState private var invitationFocused: Bool
  #if os(macOS)
    @FocusState private var sidebarFocused: Bool
  #endif

  var body: some View {
    NavigationSplitView(preferredCompactColumn: $column) {
      List(selection: $selection) {
        ForEach(connection.sessions) { item in
          NavigationLink(value: item.id) {
            VStack(alignment: .leading) {
              Text(item.Name).font(.headline)
              Text("\(item.Host) · \(item.Mode.uppercased()) · \(item.Driver) \(item.Version)")
                .font(.caption).foregroundStyle(.secondary)
            }
          }.accessibilityIdentifier("session-" + item.id)
        }
      }
      #if os(macOS)
        .focused($sidebarFocused)
      #endif
      .overlay {
        if connection.sessions.isEmpty {
          ContentUnavailableView(
            connection.listTitle, systemImage: "terminal",
            description: Text(connection.listDetail))
        }
      }
      .navigationTitle("Sessions")
      .toolbar {
        Button("Refresh") { Task { await connection.refresh() } }
        Button("Pair") { pairing = Pairing() }
      }
    } detail: {
      if let session = connection.selected {
        VStack(alignment: .leading) {
          if session.Mode == "pty" {
            LiveTerminal(connection: connection).id(session.id)
          } else {
            stream("Standard output", connection.stdout)
            stream("Standard error", connection.stderr)
            HStack {
              TextField("Input", text: $line).accessibilityIdentifier("session-input").focused(
                $inputFocused
              ).onSubmit(submit)
                .autocorrectionDisabled()
                #if os(iOS)
                  .textInputAutocapitalization(.never)
                #endif
              Button("Send", action: submit)
              Button("EOF") { connection.send(["Op": "eof"]) }
            }.padding()
          }
          if let ports = session.Ports, !ports.isEmpty {
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
        .navigationTitle(session.Name)
        .toolbar {
          Button("Files") { browsing = true }
          #if os(iOS)
            if session.Mode == "pty" { terminalKeys }
          #endif
          Button("Detach", action: detach)
        }
      } else {
        ContentUnavailableView(
          "Select a session", systemImage: "terminal",
          description: Text(
            connection.status.isEmpty ? "Open a running CLI from Sessions." : connection.status))
      }
    }
    .onChange(of: selection) { _, id in
      if let item = connection.sessions.first(where: { $0.id == id }) {
        line = ""
        column = .detail
        Task { await connection.attach(item) }
      } else {
        connection.leave()
      }
    }
    .onChange(of: connection.selected?.id) { _, id in
      if id != nil, let session = connection.selected {
        #if os(macOS)
          inputFocused = session.Mode == "pipe"
        #else
          // Let the user read incoming output before bringing up the keyboard.
          inputFocused = false
        #endif
        AccessibilityNotification.Announcement(session.Name + ", " + session.Host).post()
      }
    }
    .sheet(isPresented: $browsing, onDismiss: restoreFocus) {
      if let session = connection.selected {
        WorkspaceBrowser(
          connection: connection, session: session,
          location: Binding(
            get: { folders[session.id] ?? WorkspaceLocation() },
            set: { folders[session.id] = $0 }))
      }
    }
    #if os(macOS)
      .focusedSceneValue(
        \.sessionActions,
        SessionActions(
          pair: { pairing = Pairing() },
          sessions: { sidebarFocused = true },
          refresh: { Task { await connection.refresh() } }, files: { browsing = true },
          detach: detach, attached: connection.selected != nil))
    #endif
    .task {
      #if CLISTREAM_SHARED_TEST_HOST
        await SharedParticipant.run(connection)
      #else
        await connection.refresh()
      #endif
    }
    .onOpenURL { url in
      guard url.scheme == "clistream", url.host == "pair" else { return }
      let info = Core.call(["Op": "inspect-invitation", "Link": url.absoluteString])
      if let error = info.Error {
        connection.pairingError = error
        return
      }
      pairing = Pairing(link: url, address: info.Address)
    }
    .alert(
      "Couldn’t pair this device",
      isPresented: Binding(
        get: { connection.pairingError != nil },
        set: { if !$0 { connection.pairingError = nil } }
      )
    ) {
      Button("OK") { connection.pairingError = nil }
    } message: {
      Text(connection.pairingError ?? "")
    }
    .sheet(item: $pairing, onDismiss: {
      invitation = ""
      pastedAddress = nil
    }) { request in
      VStack(alignment: .leading, spacing: 16) {
        Text("Pair a server").font(.title2)
        if let address = request.address {
          Text(address)
        } else {
          SecureField("Pairing link", text: $invitation).textFieldStyle(.roundedBorder)
            .focused($invitationFocused).onAppear { invitationFocused = true }
            .onChange(of: invitation) { _, text in
              let link = text.trimmingCharacters(in: .whitespacesAndNewlines)
              pastedAddress = Core.call(["Op": "inspect-invitation", "Link": link]).Address
            }
          if let pastedAddress { Text(pastedAddress) }
        }
        HStack {
          Button("Cancel") { pairing = nil }.keyboardShortcut(.cancelAction)
          Button("Pair device") {
            let link =
              request.link?.absoluteString
              ?? invitation.trimmingCharacters(in: .whitespacesAndNewlines)
            pairing = nil
            Task { await connection.pair(link) }
          }.keyboardShortcut(.defaultAction).disabled(request.link == nil && invitation.isEmpty)
        }
      }.padding(24).frame(minWidth: 290).presentationDetents([.medium])
    }
  }

  private func restoreFocus() {
    if connection.selected?.Mode == "pipe" {
      inputFocused = true
    } else {
      connection.focusTerminal()
    }
  }

  private func detach() {
    selection = nil
    column = .sidebar
    connection.leave()
    Task { await connection.refresh() }
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
    let data = Data((line + "\n").utf8)
    connection.send(["Op": "input", "Data": data.base64EncodedString()])
    line = ""
  }

  func stream(_ title: String, _ data: Data) -> some View {
    VStack(alignment: .leading) {
      Text(title).font(.caption).foregroundStyle(.secondary)
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

#if os(macOS)
  private struct SessionActions {
    let pair, sessions, refresh, files, detach: () -> Void
    let attached: Bool
  }
  private struct SessionActionKey: FocusedValueKey { typealias Value = SessionActions }
  extension FocusedValues {
    fileprivate var sessionActions: SessionActions? {
      get { self[SessionActionKey.self] }
      set { self[SessionActionKey.self] = newValue }
    }
  }
  private struct SessionCommands: Commands {
    @FocusedValue(\.sessionActions) private var actions
    var body: some Commands {
      CommandMenu("Session") {
        Button("Pair…") { actions?.pair() }.keyboardShortcut("k")
        Button("Focus Sessions") { actions?.sessions() }.keyboardShortcut("1")
        Button("Refresh Sessions") { actions?.refresh() }.keyboardShortcut("r")
        Divider()
        Button("Files") { actions?.files() }.keyboardShortcut("e").disabled(
          actions?.attached != true)
        Button("Detach") { actions?.detach() }.keyboardShortcut("d", modifiers: [.command, .shift])
          .disabled(actions?.attached != true)
      }
    }
  }
#endif
