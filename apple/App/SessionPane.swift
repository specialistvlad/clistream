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
    .modifier(SessionActions(connection: connection, session: session, detach: detach))
  }
}
