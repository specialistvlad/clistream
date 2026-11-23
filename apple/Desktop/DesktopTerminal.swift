import SwiftUI

/// These are the three app-drawn status colors in the approved Mac design.
enum DesktopStatusColor {
  static func connected(_ appearance: ColorScheme) -> Color {
    appearance == .dark ? Color(red: 48/255, green: 209/255, blue: 88/255)
      : Color(red: 40/255, green: 200/255, blue: 64/255)
  }
  static func stopped(_ appearance: ColorScheme) -> Color {
    Color(white: appearance == .dark ? 108/255 : 166/255)
  }
  static func warning(_ appearance: ColorScheme) -> Color {
    appearance == .dark ? Color(red: 1, green: 193/255, blue: 46/255)
      : Color(red: 245/255, green: 163/255, blue: 0)
  }
}

struct DesktopTerminal: View {
  let row: WorkspaceSession
  @ObservedObject var workspace: Workspace
  @ObservedObject var connection: Connection
  @ObservedObject var state: DesktopSelection
  let actions: DesktopContext
  @Environment(\.colorScheme) private var appearance

  static func latency(_ sample: Int64) -> String {
    sample < 1000 ? "<1 ms" : String(Int((Double(sample) / 1000).rounded())) + " ms"
  }
  var body: some View {
    let item = row.item
    let online = !actions.revoked && (connection.attached || !item.running && workspace.errors[row.server.id] == "")
    VStack(spacing: 0) {
      Group {
        if !item.running {
          ScrollView([.vertical, .horizontal]) {
            Text(String(decoding: connection.stdout, as: UTF8.self))
              .font(.system(.body, design: .monospaced)).textSelection(.enabled)
              .frame(maxWidth: .infinity, alignment: .leading)
          }
        } else if connection.selected != nil {
          LiveTerminal(connection: connection, suppressCursor: actions.revoked)
            .id(item.ID)
        } else { ProgressView() }
      }
      .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
      .padding(.horizontal, 16).padding(.vertical, 10)
      .background(Color(nsColor: .textBackgroundColor))
      if actions.revoked {
        strip(symbol: "exclamationmark.triangle.fill", warning: true,
          text: "This device’s access to " + row.server.name + " was revoked. Pair again with a fresh invitation.",
          action: "Pair Again…") { state.pair("") }
      } else if !item.running {
        let message = item.exitCode.map { "Exited with status " + String($0) } ?? "Stopped"
        strip(symbol: "stop", warning: false, text: message + " · showing the last run’s output", action: "Start…") {
          actions.perform(.restart)
        }
      } else if !connection.status.isEmpty {
        Text(connection.status).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
      }
      Divider()
      HStack(spacing: 6) {
        Circle().fill(online ? DesktopStatusColor.connected(appearance) : DesktopStatusColor.stopped(appearance))
          .frame(width: 8, height: 8).accessibilityHidden(true)
        Text(online ? "Connected" + (workspace.samples[row.server.id].map { " · " + Self.latency($0) } ?? "") : "Disconnected")
          .accessibilityIdentifier("desktop-session-status")
        Spacer()
        Text((item.Mode == "pipe" ? "Pipe" : "PTY") + (item.running ? "" : " · Stopped"))
      }.font(.caption).foregroundStyle(.secondary).padding(.horizontal, 12).frame(minHeight: 26)
    }
    .onReceive(connection.focusInput) { connection.focusTerminal() }
    .onChange(of: item.ID) { previous, next in
      // A different client can replace the child while this window watches it.
      if let selected = connection.selected, selected.ID == previous && next != previous {
        workspace.activate(row)
      }
    }
  }
  private func strip(symbol: String, warning: Bool, text: String, action: String, perform: @escaping () -> Void) -> some View {
    VStack(spacing: 0) {
      Divider()
      HStack {
        Image(systemName: symbol).foregroundStyle(warning ? DesktopStatusColor.warning(appearance) : Color.secondary)
        Text(text)
        Spacer()
        Button(action, action: perform).controlSize(.small)
      }.font(.callout).padding(.horizontal, 16).padding(.vertical, 6)
    }
  }
}

struct DesktopPorts: View {
  @ObservedObject var connection: Connection
  let item: SessionInfo
  @State private var changing = Set<String>()
  var body: some View {
    Form {
      Section {
        ForEach((item.Ports ?? [:]).keys.sorted(), id: \.self) { name in
          VStack(alignment: .leading, spacing: 4) {
            Toggle(isOn: Binding(get: { connection.forwarding[name] != nil }, set: { _ in
              guard changing.insert(name).inserted else { return }
              Task {
                await connection.forward(name, item: item)
                changing.remove(name)
              }
            })) {
              VStack(alignment: .leading) {
                Text(name)
                Text([item.Ports?[name]?.uppercased(), item.PortAddresses?[name]].compactMap { $0 }.joined(separator: " · "))
                  .font(.caption).foregroundStyle(.secondary)
              }
            }
            .toggleStyle(.switch).controlSize(.mini)
            .disabled(!connection.attached || changing.contains(name)).accessibilityIdentifier("forward-" + name)
            if let address = connection.forwarding[name] {
              HStack {
                Text(address).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                  .accessibilityIdentifier("forward-address-" + name)
                Spacer()
                Button("Copy") {
                  NSPasteboard.general.clearContents()
                  NSPasteboard.general.setString(address, forType: .string)
                }.controlSize(.small)
              }
            }
          }
        }
        if item.Ports?.isEmpty != false { Text("This session has no shared ports.").foregroundStyle(.secondary) }
      } header: {
        Text("Ports").font(.headline).accessibilityAddTraits(.isHeader)
      } footer: {
        if item.Ports?.isEmpty == false { Text("Forwards keep running when you close this inspector.") }
      }
    }.formStyle(.grouped)
  }
}
