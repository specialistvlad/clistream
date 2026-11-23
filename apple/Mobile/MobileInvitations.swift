import SwiftUI

@MainActor final class MobileInvitations: ObservableObject {
  enum Page { case review, scan }
  @Published var page: Page?
  @Published var link = ""
  private(set) var confirmed: String?

  func add(_ value: String = "") {
    link = value
    page = .review
  }
  func paste() {
    let board = UIPasteboard.general
    add(board.string ?? board.url?.absoluteString ?? "")
  }
  func scan() { page = .scan }
  @discardableResult func scanned(_ value: String) -> Bool {
    let info = Core.inspect(value)
    guard info.Error == nil, info.Address != nil, info.Pin != nil else { return false }
    add(value)
    return true
  }
  func accept() {
    confirmed = link.trimmingCharacters(in: .whitespacesAndNewlines)
    page = nil
  }
  func takeConfirmation() -> String? {
    defer {
      confirmed = nil
      if page == nil { link = "" }
    }
    return confirmed
  }
}

struct MobileInvitationPresentation: ViewModifier {
  @ObservedObject var flow: MobileInvitations
  @ObservedObject var connection: Connection
  let enroll: (String) async -> Void

  func body(content: Content) -> some View {
    content.onOpenURL { url in
      guard url.scheme == "clistream" else { return }
      if !flow.scanned(url.absoluteString) { connection.pairingError = "Invalid invitation" }
    }
    .sheet(
      isPresented: Binding(get: { flow.page != nil }, set: { if !$0 { flow.page = nil } }),
      onDismiss: { if let link = flow.takeConfirmation() { Task { await enroll(link) } } }
    ) {
      MobileInvitationSheet(flow: flow)
    }
    .alert(
      "Couldn’t Pair This Device",
      isPresented: Binding(
        get: { connection.pairingError != nil }, set: { if !$0 { connection.pairingError = nil } }
      )
    ) {
      Button("OK") { connection.pairingError = nil }
    } message: {
      Text(
        "The invitation expired or was replaced. Ask the server operator for a new one with clistreamd invite."
      )
    }
  }
}

private struct MobileInvitationSheet: View {
  @ObservedObject var flow: MobileInvitations
  var body: some View {
    NavigationStack {
      if flow.page == .scan {
        MobileScanner(flow: flow)
      } else {
        let info = Core.inspect(flow.link.trimmingCharacters(in: .whitespacesAndNewlines))
        Form {
          Section {
            SecureField("Pairing link", text: $flow.link, prompt: Text("clistream://host/?token=…"))
              .textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
              .accessibilityIdentifier("pairing-link")
            Button("Paste Invitation", systemImage: "doc.on.clipboard", action: flow.paste)
            Button("Scan QR Code", systemImage: "qrcode.viewfinder", action: flow.scan)
          } header: {
            Text("Invitation")
          } footer: {
            Text("Scan or paste the invitation from clistreamd. Invitations last three minutes.")
          }
          if let address = info.Address, let pin = info.Pin, info.Error == nil {
            Section {
              LabeledContent("Address", value: address)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Address").accessibilityValue(address)
                .accessibilityIdentifier("pairing-address")
              LabeledContent("Identity") {
                Text(pin.uppercased()).font(.system(.caption, design: .monospaced))
                  .textSelection(.enabled)
              }.accessibilityElement(children: .combine)
                .accessibilityLabel("Identity").accessibilityValue(pin.uppercased())
                .accessibilityIdentifier("pairing-identity")
            } header: {
              Text("Server")
            } footer: {
              Text("Check the server before pairing. This device can then control every CLI on it.")
            }
          }
        }.navigationTitle("Add Server").navigationBarTitleDisplayMode(.inline)
          .toolbar {
            ToolbarItem(placement: .cancellationAction) { close }
            ToolbarItem(placement: .confirmationAction) {
              Button("Pair", systemImage: "checkmark", action: flow.accept)
                .labelStyle(.iconOnly).disabled(
                  info.Address == nil || info.Pin == nil || info.Error != nil
                )
                .accessibilityIdentifier("workspace-confirm")
            }
          }
      }
    }.presentationDetents([.large]).presentationDragIndicator(.visible)
  }
  private var close: some View {
    Button("Close", systemImage: "xmark") { flow.page = nil }
      .labelStyle(.iconOnly).accessibilityIdentifier("workspace-cancel")
  }
}
