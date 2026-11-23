import SwiftUI

struct MobileFormSheet<Fields: View>: View {
  let title: String
  let confirmation: String
  let enabled: Bool
  let cancel: () -> Void
  let confirm: () -> Void
  @ViewBuilder let fields: () -> Fields

  var body: some View {
    NavigationStack {
      Form { fields() }.formStyle(.grouped)
        .navigationTitle(title).navigationBarTitleDisplayMode(.inline)
        .toolbarVisibility(.visible, for: .navigationBar)
        .toolbarMinimizationBehavior(.never, for: .navigationBar)
        .toolbar {
          ToolbarItem(placement: .cancellationAction) {
            Button("Close", systemImage: "xmark", action: cancel).labelStyle(.iconOnly)
              .accessibilityIdentifier("workspace-cancel")
          }
          ToolbarItem(placement: .confirmationAction) {
            Button(confirmation, systemImage: "checkmark", action: confirm).labelStyle(.iconOnly)
              .disabled(!enabled).keyboardShortcut(.defaultAction).accessibilityIdentifier(
                "workspace-confirm")
          }
        }
    }.presentationDetents([.medium, .large]).presentationDragIndicator(.visible)
  }
}

struct CommandSheet: View {
  let draft: MobileLifecycle.Draft
  let submit: ([Data]) -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var line: String
  init(draft: MobileLifecycle.Draft, submit: @escaping ([Data]) -> Void) {
    self.draft = draft
    self.submit = submit
    _line = State(initialValue: draft.line)
  }

  var body: some View {
    let item = draft.row.item
    let action = item.running ? "Restart" : "Start"
    let parsed = Core.call(["Op": "parse-command", "Line": line])
    MobileFormSheet(
      title: action + " “" + item.Name + "”", confirmation: action,
      enabled: parsed.Command != nil, cancel: { dismiss() }, confirm: launch
    ) {
      Section {
        CommandField(line: $line, submit: launch).frame(minHeight: 32)
        if let error = parsed.Error { Text(error).foregroundStyle(.red) }
      } header: {
        Text("Command")
      } footer: {
        Text(
          item.running
            ? "Quote arguments that contain spaces. Use \"\" for an empty argument. Restarting ends the current run for everyone connected."
            : "Quote arguments that contain spaces. Use \"\" for an empty argument.")
      }
    }
  }
  private func launch() {
    if let command = Core.call(["Op": "parse-command", "Line": line]).Command { submit(command) }
  }
}

struct PortsPresentation: ViewModifier {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  @Binding var presented: Bool
  @Environment(\.regularWorkspace) private var regular
  @Environment(\.horizontalSizeClass) private var columnWidth

  func body(content: Content) -> some View {
    content.inspector(isPresented: presentation(regular: true)) {
      VStack(alignment: .leading, spacing: 0) {
        HStack {
          Text("Ports").font(.title2.bold()).accessibilityAddTraits(.isHeader)
          Spacer()
          close
        }.padding()
        portList
      }.inspectorColumnWidth(min: 240, ideal: columnWidth == .compact ? 300 : 360, max: 400)
    }
    .sheet(isPresented: presentation(regular: false)) {
      NavigationStack {
        portList.navigationTitle("Ports").navigationBarTitleDisplayMode(.inline)
          .toolbar { ToolbarItem(placement: .cancellationAction) { close } }
      }.presentationDetents([.medium, .large]).presentationDragIndicator(.visible)
    }
  }
  private var close: some View {
    Button("Close", systemImage: "xmark") { presented = false }.labelStyle(.iconOnly)
      .accessibilityIdentifier("dismiss-ports")
  }
  private func presentation(regular requested: Bool) -> Binding<Bool> {
    Binding(
      get: { presented && regular == requested },
      set: { if regular == requested { presented = $0 } })
  }
  private var portList: some View {
    List {
      Section {
        if session.Ports?.isEmpty != false {
          Text("This session has no shared ports.").foregroundStyle(.secondary)
        }
        ForEach((session.Ports ?? [:]).keys.sorted(), id: \.self) { name in
          Toggle(
            isOn: Binding(
              get: { connection.forwarding[name] != nil },
              set: { enabled in
                Task { await connection.setForward(name, enabled: enabled, item: session) }
              })
          ) {
            VStack(alignment: .leading) {
              Text(name)
              Text(
                [session.Ports?[name]?.uppercased(), session.PortAddresses?[name]].compactMap { $0 }
                  .joined(separator: " · ")
              )
              .font(.caption).foregroundStyle(.secondary)
            }
          }.disabled(!connection.attached || connection.changingForwards.contains(name))
            .accessibilityIdentifier("forward-" + name)
          if let address = connection.forwarding[name] {
            HStack {
              Text(address).font(.system(.body, design: .monospaced))
                .textSelection(.enabled).accessibilityIdentifier("forward-address-" + name)
              Spacer()
              Button("Copy Address", systemImage: "doc.on.doc") {
                UIPasteboard.general.string = address
              }
              .labelStyle(.iconOnly).buttonStyle(.borderless)
            }
          }
        }
      } footer: {
        Text(
          regular
            ? "Forwards keep running when you close this inspector."
            : "Forwards keep running when you close this sheet.")
      }
    }.listStyle(.insetGrouped)
  }
}
