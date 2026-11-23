import SwiftUI

/// Each presentation retains the supervisor revision that the operator reviewed.
struct SessionActions: ViewModifier {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  let detach: (() -> Void)?
  @State private var confirmation: SessionInfo?
  @State private var confirmingRestart = false
  @State private var editor: CommandDraft?
  @State private var error: String?
  @State private var ports = false

  func body(content: Content) -> some View {
    content.toolbar {
      #if os(macOS)
        ToolbarItemGroup { actions }
        ToolbarSpacer(.fixed)
      #else
        ToolbarItem {
          Menu("Session", systemImage: "ellipsis.circle") {
            if connection.attached { terminalKeys }
            actions
            if let detach {
              Divider()
              Button("Detach", systemImage: "rectangle.portrait.and.arrow.right", action: detach)
            }
          }
        }
      #endif
      ToolbarItem {
        Button("Ports", systemImage: "sidebar.right") { ports.toggle() }
          .accessibilityLabel("Ports").accessibilityIdentifier("session-ports")
          .help("Show or hide port forwarding")
      }
    }
    .modifier(PortsPresentation(connection: connection, session: session, presented: $ports))
    .sheet(item: $editor) { draft in
      CommandSheet(draft: draft) { argv in
        editor = nil
        perform(.launch, target: draft.session, argv: argv)
      }
    }
    .alert(
      (confirmingRestart ? "Restart “" : "Stop “") + (confirmation?.Name ?? "") + "”?",
      isPresented: Binding(
        get: { confirmation != nil }, set: { if !$0 { confirmation = nil } }
      ), presenting: confirmation
    ) { target in
      Button("Cancel", role: .cancel) {}
      Button(confirmingRestart ? "Restart" : "Stop", role: .destructive) {
        perform(confirmingRestart ? .restart : .stop, target: target)
      }
    } message: { _ in
      Text("This ends the current run for everyone connected.")
    }
    .alert(
      "Session action failed",
      isPresented: Binding(
        get: { error != nil }, set: { if !$0 { error = nil } }
      )
    ) {
      Button("OK") {}
    } message: {
      Text(error ?? "")
    }
  }

  #if os(iOS)
    private var terminalKeys: some View {
      Menu("Keys") {
        Button("Show Keyboard") { connection.focusTerminal() }
        Button("Hide Keyboard") { _ = connection.terminal?.resignFirstResponder() }
        Button("Escape") { connection.send(.bytes(Data([0x1b]))) }
        Menu("Control Key") {
          ForEach(Array(65...90), id: \.self) { letter in
            Button("Control-" + String(UnicodeScalar(letter)!)) {
              connection.send(.bytes(Data([UInt8(letter - 64)])))
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

  @ViewBuilder private var actions: some View {
    if session.running && !connection.attached {
      Button("Reconnect", systemImage: "arrow.clockwise") {
        Task { await connection.reconnect(session) }
      }
    }
    if (session.revision ?? 0) > 0 {
      Button(
        session.running ? "Restart…" : "Start…",
        systemImage: session.running ? "arrow.counterclockwise" : "play"
      ) {
        let target = session
        if target.editable == true {
          Task {
            let reply = await connection.manage(.command, item: target)
            guard let argv = reply.Command else {
              error = reply.Error ?? "Command unavailable"
              return
            }
            let formatted = Core.call([
              "Op": "format-command", "Command": argv.map { $0.base64EncodedString() },
            ])
            guard let line = formatted.Line else {
              error = formatted.Error ?? "Command unavailable"
              return
            }
            editor = CommandDraft(session: target, line: line)
          }
        } else if target.running {
          confirmingRestart = true
          confirmation = target
        } else {
          perform(.start, target: target)
        }
      }
      if session.running {
        Button("Stop…", systemImage: "stop") {
          confirmingRestart = false
          confirmation = session
        }
      }
    }
    if session.Mode == "pipe" {
      #if os(macOS)
        sendEOF.disabled(!connection.attached)
      #else
        if connection.attached { sendEOF }
      #endif
    }
  }

  private var sendEOF: some View {
    Button("Send EOF", systemImage: "text.append") { connection.send(.eof) }
      .accessibilityIdentifier("session-send-eof")
  }

  private func perform(_ action: SessionAction, target: SessionInfo, argv: [Data]? = nil) {
    Task { error = await connection.changeRun(action, item: target, arguments: argv) }
  }
}

private struct CommandDraft: Identifiable {
  let id = UUID()
  let session: SessionInfo
  let line: String
}

private struct CommandSheet: View {
  let draft: CommandDraft
  let submit: ([Data]) -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var line: String

  init(draft: CommandDraft, submit: @escaping ([Data]) -> Void) {
    self.draft = draft
    self.submit = submit
    _line = State(initialValue: draft.line)
  }

  var body: some View {
    let validation = Core.call(["Op": "parse-command", "Line": line])
    WorkspaceSheet(
      title: (draft.session.running ? "Restart “" : "Start “") + draft.session.Name + "”",
      confirmation: draft.session.running ? "Restart" : "Start",
      enabled: validation.Command != nil, cancel: { dismiss() }, confirm: launch
    ) {
      if draft.session.running { Text("Restarting ends the current run for everyone connected.") }
      VStack(alignment: .leading) {
        Text("Command")
        CommandField(line: $line, submit: launch).frame(minHeight: 32)
        if let error = validation.Error { Text(error).foregroundStyle(.red) }
      }
    }
  }
  private func launch() {
    if let argv = Core.call(["Op": "parse-command", "Line": line]).Command { submit(argv) }
  }
}

private struct PortsPresentation: ViewModifier {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  @Binding var presented: Bool
  #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
  #endif

  func body(content: Content) -> some View {
    #if os(iOS)
      // Keep the terminal mounted as the window changes size. A compact
      // sheet owns its navigation bar and Done button; the regular-width
      // inspector owns its own header.
      content
        .inspector(isPresented: presentation(compact: false)) { inspectorColumn }
        .sheet(isPresented: presentation(compact: true)) {
          NavigationStack {
            portList.navigationTitle("Ports").navigationBarTitleDisplayMode(.inline)
              .toolbar { ToolbarItem(placement: .confirmationAction) { done } }
          }
          .presentationDetents([.medium, .large])
        }
    #else
      content.inspector(isPresented: $presented) { inspectorColumn }
    #endif
  }

  #if os(iOS)
    private func presentation(compact: Bool) -> Binding<Bool> {
      Binding(get: { presented && (sizeClass == .compact) == compact },
              set: { value in if (sizeClass == .compact) == compact { presented = value } })
    }
  #endif

  private var inspectorColumn: some View {
    VStack(alignment: .leading, spacing: 0) {
      HStack {
        Text("Ports").font(.headline).accessibilityAddTraits(.isHeader)
        Spacer()
        done
      }.padding()
      Divider()
      portList
    }
    .inspectorColumnWidth(min: 230, ideal: 280, max: 400)
  }

  private var done: some View {
    Button("Done") { presented = false }.accessibilityIdentifier("dismiss-ports")
  }

  private var portList: some View {
    PortList(connection: connection, session: session)
  }
}

private struct PortList: View {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  @State private var changing = Set<String>()

  var body: some View {
    List {
      ForEach((session.Ports ?? [:]).keys.sorted(), id: \.self) { name in
        VStack(alignment: .leading) {
          Toggle(
            isOn: Binding(
              get: { connection.forwarding[name] != nil },
              set: { _ in
                guard changing.insert(name).inserted else { return }
                Task {
                  await connection.forward(name, item: session)
                  changing.remove(name)
                }
              })
          ) {
            Text(name)
            Text((session.Ports?[name] ?? "").uppercased()).font(.caption)
          }.disabled(!connection.attached || changing.contains(name))
            .accessibilityIdentifier("forward-" + name)
          if let address = connection.forwarding[name] {
            Text(address).textSelection(.enabled).accessibilityIdentifier(
              "forward-address-" + name)
            Button("Copy Address") {
              #if os(macOS)
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(address, forType: .string)
              #else
                UIPasteboard.general.string = address
              #endif
            }.buttonStyle(.borderless)
          }
        }
      }
    }.overlay {
      if session.Ports?.isEmpty != false {
        Text("This session has no shared ports.").foregroundStyle(.secondary)
      }
    }
  }
}
