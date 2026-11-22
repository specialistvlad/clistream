import SwiftUI

#if os(macOS)
  import AppKit
#endif

struct Pairing: Identifiable {
  let id = UUID()
  var link: URL?
  var address: String?
}

struct PairingPresentation: ViewModifier {
  @ObservedObject var connection: Connection
  @Binding var pairing: Pairing?
  var enroll: ((String) async -> Void)? = nil
  @State private var confirmedLink: String?
  @State private var invitation = ""
  @State private var pastedAddress: String?
  @FocusState private var invitationFocused: Bool

  func body(content: Content) -> some View {
    content
      .onOpenURL { url in
        guard url.scheme == "clistream", url.host != nil else { return }
        #if os(macOS)
          // A pairing link is a user-initiated request to bring its confirmation forward.
          NSApp.activate()
          (NSApp.keyWindow ?? NSApp.mainWindow)?.makeKeyAndOrderFront(nil)
        #endif
        let info = Core.inspect(url.absoluteString)
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
      .sheet(
        item: $pairing,
        onDismiss: {
          invitation = ""
          pastedAddress = nil
          if let link = confirmedLink {
            confirmedLink = nil
            Task { if let enroll { await enroll(link) } else { await connection.pair(link) } }
          }
        }
      ) { request in
        WorkspaceSheet(title: "Pair a server", confirmation: "Pair device",
                       enabled: request.link != nil || !invitation.isEmpty,
                       cancel: { pairing = nil }, confirm: {
          confirmedLink = request.link?.absoluteString ?? invitation.trimmingCharacters(in: .whitespacesAndNewlines)
          pairing = nil
        }) {
          if let address = request.address {
            Text(address)
          } else {
            SecureField("Pairing link", text: $invitation).textFieldStyle(.roundedBorder)
              .focused($invitationFocused).onAppear { invitationFocused = true }
              .onChange(of: invitation) { _, text in
                let link = text.trimmingCharacters(in: .whitespacesAndNewlines)
                pastedAddress = Core.inspect(link).Address
              }
            if let pastedAddress { Text(pastedAddress) }
          }
        }
      }
  }

}

/// Use the platform's presentation and button ordering for single-task sheets.
struct WorkspaceSheet<Fields: View>: View {
  let title: String
  let confirmation: String
  let enabled: Bool
  let cancel: () -> Void
  let confirm: () -> Void
  @ViewBuilder let fields: () -> Fields

  var body: some View {
    #if os(macOS)
      VStack(alignment: .leading, spacing: 16) {
        Text(title).font(.headline).accessibilityAddTraits(.isHeader)
        fields()
        HStack {
          Spacer()
          Button("Cancel", action: cancel).keyboardShortcut(.cancelAction)
          Button(confirmation, action: confirm).disabled(!enabled).keyboardShortcut(.defaultAction)
        }.padding(.top, 4)
      }.padding(24).frame(width: 510)
    #else
    NavigationStack {
      Form { fields() }.formStyle(.grouped)
        .navigationTitle(title)
        .toolbar {
          ToolbarItem(placement: .cancellationAction) { Button("Cancel", action: cancel) }
          ToolbarItem(placement: .confirmationAction) {
            Button(confirmation, action: confirm).disabled(!enabled)
              .keyboardShortcut(.defaultAction)
          }
        }
    }
      .presentationDetents([.medium, .large])
    #endif
  }
}
