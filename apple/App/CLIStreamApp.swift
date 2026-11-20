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
}

struct BrowserView: View {
  @StateObject private var connection = Connection()
  @State private var invitation = ""
  @State private var browsing = false
  @State private var pairing: Pairing?
  @State private var line = ""
  @State private var column = NavigationSplitViewColumn.sidebar
  @State private var selection: String?
  @State private var folders = [String: WorkspaceLocation]()
  @FocusState private var inputFocused: Bool

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
                  Text(address).textSelection(.enabled)
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
        inputFocused = session.Mode == "pipe"
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
          refresh: { Task { await connection.refresh() } }, files: { browsing = true },
          detach: detach, attached: connection.selected != nil))
    #endif
    .task { await connection.refresh() }
    .onOpenURL { url in
      if url.scheme == "clistream", url.host == "pair" { pairing = Pairing(link: url) }
    }
    .sheet(item: $pairing, onDismiss: { invitation = "" }) { request in
      VStack(alignment: .leading, spacing: 16) {
        Text("Pair a server").font(.title2)
        if let url = request.link {
          let fields = URLComponents(string: "?" + (url.fragment ?? ""))?.queryItems
          Text(fields?.first(where: { $0.name == "address" })?.value ?? "Server invitation")
        } else {
          SecureField("Pairing link", text: $invitation).textFieldStyle(.roundedBorder)
        }
        HStack {
          Button("Cancel") { pairing = nil }
          Button("Pair device") {
            let link =
              request.link?.absoluteString
              ?? invitation.trimmingCharacters(in: .whitespacesAndNewlines)
            pairing = nil
            Task { await connection.pair(link) }
          }.disabled(request.link == nil && invitation.isEmpty)
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
        Button("Control Key") { connection.armControlKey() }
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
      ScrollView {
        Text(String(decoding: data, as: UTF8.self)).font(.system(.body, design: .monospaced))
          .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
      }
    }.padding(.horizontal)
  }
}

#if os(macOS)
  private struct SessionActions {
    let pair, refresh, files, detach: () -> Void
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
