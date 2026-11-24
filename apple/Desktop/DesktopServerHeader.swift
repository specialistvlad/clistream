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
        desktopMenuButton("Remove Server…", symbol: "trash") { state.alert = .remove(server) }
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

private struct InlineServerName: View {
  @State private var draft: String
  @FocusState private var focused: Bool
  let finish: (String?) -> Void

  init(name: String, finish: @escaping (String?) -> Void) {
    _draft = State(initialValue: name)
    self.finish = finish
  }

  var body: some View {
    TextField("Server name", text: $draft)
      .textFieldStyle(.roundedBorder)
      .labelsHidden()
      .focused($focused)
      .accessibilityLabel("Server name")
      .accessibilityIdentifier("server-name-field")
      .onSubmit { finish(draft) }
      .onExitCommand { finish(nil) }
      .onAppear { focused = true }
  }
}
