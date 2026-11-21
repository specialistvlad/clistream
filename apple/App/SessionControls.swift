import SwiftUI

struct SessionControls: View {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  @State private var action: SessionAction?
  @State private var error: String?
  @State private var arguments: [String] = []
  @State private var editing = false
  @State private var editTarget: SessionInfo?

  var body: some View {
    HStack {
      if session.running {
        Button("Stop") { action = .stop }
        Button("Restart") { action = .restart }
      } else {
        Button("Start") { perform(.start) }
      }
      if session.editable == true {
        Button("Edit Command…") {
          Task {
            let target = session
            let reply = await connection.manage(.command, item: target)
            guard let argv = reply.Command else {
              error = reply.Error ?? "Command unavailable"
              return
            }
            let decoded = argv.compactMap { String(data: $0, encoding: .utf8) }
            guard decoded.count == argv.count else {
              error = "Command contains non-UTF-8 arguments"
              return
            }
            editTarget = target
            arguments = decoded
            editing = true
          }
        }
      }
    }
    .confirmationDialog(
      "Confirm " + (action?.rawValue ?? ""),
      isPresented: Binding(
        get: { action != nil }, set: { if !$0 { action = nil } }
      ), presenting: action
    ) { operation in
      Button(operation.rawValue.capitalized + " Session", role: .destructive) { perform(operation) }
    }
    .sheet(isPresented: $editing) {
      VStack {
        Text("Edit Command").font(.headline)
        ScrollView {
          ForEach(arguments.indices, id: \.self) { i in
            HStack {
              TextField(i == 0 ? "Executable" : "Argument", text: $arguments[i])
                .accessibilityIdentifier("argument-" + String(i))
              if i > 0 { Button("Remove") { arguments.remove(at: i) } }
            }
          }
        }
        HStack {
          Button("Add Argument") { arguments.append("") }
          Button("Cancel") { editing = false }
          Button("Launch") {
            editing = false
            perform(.launch, target: editTarget, argv: arguments.map { Data($0.utf8) })
          }.disabled(arguments.first?.isEmpty != false)
        }
      }.padding()
    }
    .alert(
      "Session action failed",
      isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })
    ) {
      Button("OK") {}
    } message: {
      Text(error ?? "")
    }
  }

  private func perform(_ operation: SessionAction, target: SessionInfo? = nil, argv: [Data]? = nil)
  {
    Task {
      error = await connection.changeRun(operation, item: target ?? session, arguments: argv)
    }
  }
}
