import SwiftUI

/// Every confirmation captures a specific server, supervisor, run and revision.
@MainActor final class MobileLifecycle: ObservableObject {
  struct Draft: Identifiable {
    let row: WorkspaceSession
    let line: String
    var id: String { row.id }
  }
  enum Confirmation: Identifiable {
    case remove(ServerProfile)
    case error(String)
    var id: String { title }
    var title: String {
      switch self {
      case .remove(let server): return "Remove “\(server.name)”?"
      case .error: return "Session action failed"
      }
    }
  }
  @Published var draft: Draft?
  @Published var confirmation: Confirmation?

  func review(_ row: WorkspaceSession) async {
    guard row.item.editable == true else { return }
    let reply = await Core.action(
      .command, directory: row.server.directory, target: row.item,
      arguments: nil, columns: nil, rows: nil)
    guard let argv = reply.Command else {
      confirmation = .error(reply.Error ?? "Command unavailable")
      return
    }
    let formatted = Core.call([
      "Op": "format-command", "Command": argv.map { $0.base64EncodedString() },
    ])
    if let line = formatted.Line {
      draft = Draft(row: row, line: line)
    } else {
      confirmation = .error(formatted.Error ?? "Command unavailable")
    }
  }

  func perform(
    _ action: SessionAction, _ row: WorkspaceSession, in workspace: Workspace,
    arguments: [Data]? = nil
  ) async {
    let error: String?
    if let existing = workspace.existingTerminal(row) {
      error = await existing.changeRun(action, item: row.item, arguments: arguments)
    } else {
      let reply = await Core.action(
        action, directory: row.server.directory, target: row.item,
        arguments: arguments, columns: nil, rows: nil)
      error = reply.Error
    }
    workspace.retry(row.server)
    if let error { confirmation = .error(error) }
  }
}

struct MobileLifecyclePresentation: ViewModifier {
  @ObservedObject var actions: MobileLifecycle
  let workspace: Workspace
  func body(content: Content) -> some View {
    content.sheet(item: $actions.draft) { draft in
      CommandSheet(draft: draft) { argv in
        actions.draft = nil
        Task { await actions.perform(.launch, draft.row, in: workspace, arguments: argv) }
      }
    }.alert(
      actions.confirmation?.title ?? "",
      isPresented: Binding(
        get: { actions.confirmation != nil }, set: { if !$0 { actions.confirmation = nil } }
      ), presenting: actions.confirmation
    ) { confirmation in
      switch confirmation {
      case .remove(let server):
        Button("Cancel", role: .cancel) {}
        Button("Remove", role: .destructive) {
          do { try workspace.remove(server) } catch {
            actions.confirmation = .error(error.localizedDescription)
          }
        }
      case .error: Button("OK") {}
      }
    } message: { value in
      switch value {
      case .remove:
        Text(
          "This device forgets the server and its pairing. The device stays enrolled until the server operator revokes it."
        )
      case .error(let message): Text(message)
      }
    }
  }
}
