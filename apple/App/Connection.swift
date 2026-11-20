import Foundation
import SwiftTerm
import SwiftUI

struct SessionInfo: Codable, Identifiable {
  let ID, Name, Mode, Driver, Version, Host, OS: String
  let Ports: [String: String]?
  var id: String { self.ID }
}

struct WorkspaceEntry: Decodable, Identifiable {
  let Name: String
  let Directory: Bool
  let Size, Modified: Int64
  var id: String { Name }
}
struct Reply: Decodable {
  var Entries: [WorkspaceEntry]?
  var Error: String?
  var Handle: String?
  var Sessions: [SessionInfo]?
  var Address: String?
  var Data: Foundation.Data?
  var Channel: String?
  var Exit: Int?
}

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

@MainActor
final class Connection: ObservableObject {
  @Published private(set) var paired = false
  @Published private(set) var listError = ""
  var listTitle: String {
    !listError.isEmpty
      ? "Connection unavailable" : (paired ? "No active sessions" : "Pair a server")
  }
  var listDetail: String {
    !listError.isEmpty
      ? listError
      : (paired
        ? "Start a supervisor and refresh the list." : "Open a server invitation or choose Pair.")
  }
  @Published var sessions: [SessionInfo] = []
  @Published var selected: SessionInfo?
  @Published var status = ""
  @Published var forwarding = [String: String]()
  @Published var stdout = Data()
  @Published var stderr = Data()
  private(set) var terminal: TerminalView?
  private var refreshTicket = UUID()
  private var pending = Data()
  private var handle: String?
  private var forwards = [String: String]()
  private var generation = UUID()
  private let inputQueue = DispatchQueue(label: "com.vlk.clistream.input")
  var directory: String {
    get throws {
      var dir = try FileManager.default.url(
        for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true
      ).appendingPathComponent("CLIStream", isDirectory: true)
      try FileManager.default.createDirectory(
        at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
      var attributes = URLResourceValues()
      attributes.isExcludedFromBackup = true
      try dir.setResourceValues(attributes)
      return dir.path
    }
  }

  func pair(_ link: String) async {
    do {
      let r = await Core.request(["Op": "pair", "Link": link, "Dir": try directory])
      if let error = r.Error {
        listError = error + ". Request a fresh invitation from the running server."
        status = listError
        return
      }
      leave()
      await refresh()
    } catch { status = error.localizedDescription }
  }

  func refresh() async {
    let ticket = UUID()
    refreshTicket = ticket
    do {
      let dir = try directory
      guard FileManager.default.fileExists(atPath: dir + "/server.json") else { return }
      paired = true
      let r = await Core.request(["Op": "list", "Dir": dir])
      guard refreshTicket == ticket else { return }
      listError = r.Error ?? ""
      sessions = r.Sessions ?? []
      status = r.Error ?? (sessions.isEmpty ? "No active sessions" : "")
    } catch { status = error.localizedDescription }
  }

  func leave() {
    generation = UUID()
    let closing = [handle].compactMap { $0 } + Array(forwards.values)
    handle = nil
    selected = nil
    forwards.removeAll()
    forwarding.removeAll()
    terminal = nil
    pending = Data()
    stdout = Data()
    stderr = Data()
    Task { for key in closing { _ = await Core.request(["Op": "close", "Handle": key]) } }
  }

  func attach(_ item: SessionInfo) async {
    leave()
    let ticket = generation
    do {
      let r = await Core.request(["Op": "attach", "Dir": try directory, "ID": item.ID])
      guard let key = r.Handle else {
        if ticket == generation { status = r.Error ?? "Attach failed" }
        return
      }
      guard ticket == generation else {
        _ = await Core.request(["Op": "close", "Handle": key])
        return
      }
      handle = key
      selected = item
      status = ""
      while handle == key {
        let event = await Core.request(["Op": "next", "Handle": key])
        guard handle == key else { return }
        if let error = event.Error {
          status = error
          break
        }
        if let exit = event.Exit {
          status = "Process exited with status \(exit)"
          break
        }
        if let data = event.Data {
          if item.Mode == "pty" {
            if let terminal {
              terminal.feed(byteArray: Array(data)[...])
            } else {
              pending.append(data)
              pending = pending.suffix(1024 * 1024)
            }
          } else if event.Channel == "stderr" {
            stderr = (stderr + data).suffix(65536)
          } else {
            stdout = (stdout + data).suffix(65536)
          }
        }
      }
      if handle == key {
        handle = nil
        _ = await Core.request(["Op": "close", "Handle": key])
      }
    } catch { status = error.localizedDescription }
  }

  func install(_ view: TerminalView) {
    terminal = view
    if !pending.isEmpty {
      view.feed(byteArray: Array(pending)[...])
      pending.removeAll()
    }
  }

  func send(_ fields: [String: Any]) {
    guard let key = handle else { return }
    let request = fields.merging(["Handle": key]) { _, new in new }
    inputQueue.async {
      let r = Core.call(request)
      if let error = r.Error {
        Task { @MainActor in if self.handle == key { self.status = error } }
      }
    }
  }

  func workspace(_ fields: [String: Any], session: SessionInfo) async -> Reply {
    guard selected?.id == session.id, let handle else { return Reply(Error: "Session detached") }
    return await Core.request(
      fields.merging(["Handle": handle, "ID": session.id]) { _, new in new })
  }

  func forward(_ name: String, item: SessionInfo) async {
    if let key = forwards.removeValue(forKey: name) {
      forwarding.removeValue(forKey: name)
      _ = await Core.request(["Op": "close", "Handle": key])
      return
    }
    let ticket = generation
    do {
      let r = await Core.request([
        "Op": "forward", "Dir": try directory, "ID": item.ID, "Name": name,
      ])
      if let key = r.Handle, let address = r.Address {
        guard ticket == generation, forwards[name] == nil else {
          _ = await Core.request(["Op": "close", "Handle": key])
          return
        }
        forwards[name] = key
        forwarding[name] = address
      } else {
        status = r.Error ?? "Forward failed"
      }
    } catch { status = error.localizedDescription }
  }
}
