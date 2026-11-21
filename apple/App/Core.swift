import Foundation

enum Core {
  static func call(_ fields: [String: Any]) -> Reply {
    do {
      let data = try JSONSerialization.data(withJSONObject: fields)
      guard let raw = CSCall(String(decoding: data, as: UTF8.self)) else {
        return Reply(Error: "Client returned no response")
      }
      defer { free(raw) }
      return try JSONDecoder().decode(Reply.self, from: Data(bytes: raw, count: strlen(raw)))
    } catch { return Reply(Error: error.localizedDescription) }
  }

  static func request(_ fields: [String: Any]) async -> Reply {
    await withCheckedContinuation { resume in
      DispatchQueue.global(qos: .userInitiated).async { resume.resume(returning: call(fields)) }
    }
  }
}

enum SessionAction: String {
  case start, stop, restart, command, result
  case launch = "run-command"
}

enum TerminalInput {
  case bytes(Data)
  case resize(columns: Int, rows: Int)
  case eof

  func fields(handle: String) -> [String: Any] {
    switch self {
    case .bytes(let bytes):
      return ["Op": "input", "Handle": handle, "Data": bytes.base64EncodedString()]
    case .resize(let columns, let rows):
      return ["Op": "resize", "Handle": handle, "Cols": columns, "Rows": rows]
    case .eof: return ["Op": "eof", "Handle": handle]
    }
  }
}

extension Core {
  static func inspect(_ invitation: String) -> Reply {
    call(["Op": "inspect-invitation", "Link": invitation])
  }

  static func action(
    _ action: SessionAction, directory: String, target: SessionInfo,
    arguments: [Data]?, columns: Int?, rows: Int?
  ) async -> Reply {
    var fields: [String: Any] = [
      "Op": action.rawValue, "Dir": directory, "ID": target.id,
      "Run": target.ID, "Revision": target.revision ?? 0,
    ]
    if let arguments { fields["Command"] = arguments.map { $0.base64EncodedString() } }
    if let columns, let rows {
      fields["Cols"] = columns
      fields["Rows"] = rows
    }
    return await request(fields)
  }
}
