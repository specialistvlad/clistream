import SwiftUI

struct SessionPane: View {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  let detach: (() -> Void)?

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      if session.running {
        LiveTerminal(connection: connection).id(session.ID)
      } else {
        ScrollView {
          Text(String(decoding: connection.stdout, as: UTF8.self))
            .font(.system(.body, design: .monospaced)).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
      }
      if !connection.status.isEmpty { Text(connection.status).font(.callout).padding(.horizontal) }
    }
    .onReceive(connection.focusInput) { connection.focusTerminal() }
    .toolbar {
      #if os(iOS)
        if connection.attached { ToolbarItem { terminalKeys } }
      #endif
    }
    .modifier(SessionActions(connection: connection, session: session, detach: detach))
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

}
