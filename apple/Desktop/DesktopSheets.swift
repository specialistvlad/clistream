import SwiftUI

private struct DesktopFormSheet<Fields: View>: View {
  let title: String
  let message: String
  let action: String
  let enabled: Bool
  let submit: () -> Void
  @ViewBuilder let fields: () -> Fields
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text(title).font(.headline).accessibilityAddTraits(.isHeader)
        .accessibilityLabel(title).accessibilityIdentifier("desktop-sheet-title")
      Text(message).font(.callout).foregroundStyle(.secondary)
      Form { fields() }.formStyle(.columns)
      HStack {
        Spacer()
        Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
        Button(action, action: submit).disabled(!enabled).keyboardShortcut(.defaultAction)
      }
    }.padding(20).frame(width: 440)
  }
}

struct DesktopPairSheet: View {
  let confirm: (String) -> Void
  @State private var link: String
  @State private var inspected: Reply
  @FocusState private var focused: Bool
  init(link: String, confirm: @escaping (String) -> Void) {
    _link = State(initialValue: link)
    _inspected = State(initialValue: Core.inspect(link))
    self.confirm = confirm
  }
  private var valid: Bool { inspected.Error == nil && inspected.Address != nil && inspected.Pin != nil }
  var body: some View {
    DesktopFormSheet(title: "Pair with a server",
      message: valid ? "Check the server before pairing. This device can then control every CLI on it."
        : "Paste the invitation from clistreamd. Invitations last three minutes.",
      action: "Pair", enabled: valid, submit: {
        guard valid else { return }
        confirm(link.trimmingCharacters(in: .whitespacesAndNewlines))
      }) {
      SecureField("Invitation", text: $link, prompt: Text("clistream://host/?token=…"))
        .focused($focused).accessibilityIdentifier("pairing-link")
      if valid, let address = inspected.Address, let pin = inspected.Pin {
        LabeledContent("Server", value: address)
        LabeledContent("Identity") {
          Text(pin.uppercased()).font(.system(.caption, design: .monospaced))
            .textSelection(.enabled).lineLimit(nil).fixedSize(horizontal: false, vertical: true)
        }
      }
    }
    .onAppear { focused = !valid }
    .onChange(of: link) { _, value in inspected = Core.inspect(value.trimmingCharacters(in: .whitespacesAndNewlines)) }
  }
}

struct DesktopCommandSheet: View {
  let row: WorkspaceSession
  let submit: ([Data]) -> Void
  @State private var line: String
  @State private var parsed: Reply
  init(row: WorkspaceSession, initialLine: String, submit: @escaping ([Data]) -> Void) {
    self.row = row
    self.submit = submit
    _line = State(initialValue: initialLine)
    _parsed = State(initialValue: Core.call(["Op": "parse-command", "Line": initialLine]))
  }
  var body: some View {
    let action = row.item.running ? "Restart" : "Start"
    DesktopFormSheet(title: action + " “" + row.item.Name + "”",
      message: row.item.running ? "Restarting ends the current run for everyone connected." : "Starts a new run with this command.",
      action: action, enabled: parsed.Command != nil, submit: launch) {
      LabeledContent("Command") {
        VStack(alignment: .leading) {
          CommandField(line: $line, submit: launch).frame(minHeight: 24)
          Text("Quote arguments that contain spaces. Use \"\" for an empty argument.").font(.caption).foregroundStyle(.secondary)
          if let error = parsed.Error { Text(error).font(.caption).foregroundStyle(.red) }
        }
      }
    }
    .onChange(of: line) { _, value in parsed = Core.call(["Op": "parse-command", "Line": value]) }
  }
  private func launch() {
    if let args = Core.call(["Op": "parse-command", "Line": line]).Command { submit(args) }
  }
}
