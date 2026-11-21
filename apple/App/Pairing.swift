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
        }
      ) { request in
        VStack(alignment: .leading, spacing: 16) {
          Text("Pair a server").font(.title2)
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

}
