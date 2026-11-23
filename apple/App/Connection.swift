import Combine
import Foundation

@MainActor
protocol TerminalRenderer: AnyObject {
  func receive(_ bytes: Data)
  var columns: Int { get }
  var rows: Int { get }
}

@MainActor
final class Connection: ObservableObject {
  let readOnly: Bool
  let profile: String?
  init(readOnly: Bool = false, profile: String? = nil) {
    self.readOnly = readOnly
    self.profile = profile
  }
  let focusInput = PassthroughSubject<Void, Never>()
  var latency: Int64?
  func measureLatency() async {
    guard let key = handle else {
      latency = nil
      return
    }
    let reply = await Core.request(["Op": "stats", "Handle": key])
    if handle == key { latency = reply.RTTMicros }
  }

  @Published private(set) var pairingGeneration = UUID()
  @Published private(set) var paired = false
  @Published var pairingError: String?
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
  @Published private(set) var changingForwards = Set<String>()
  @Published var stdout = Data()
  private(set) var renderer: (any TerminalRenderer)?
  private var refreshTicket = UUID()
  private var pending = Data()
  @Published private var handle: String?
  var attached: Bool { handle != nil }
  private var forwards = [String: String]()
  private var generation = UUID()
  private let inputQueue = DispatchQueue(label: "com.vlk.clistream.input")
  var directory: String {
    get throws {
      if let profile { return profile }
      var dir = try FileManager.default.url(
        for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true
      ).appendingPathComponent("CLIStream", isDirectory: true)
      dir = try FinderWorkspace.enrollmentRoot(legacy: dir)
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
        pairingError = listError
        return
      }
      pairingError = nil
      pairingGeneration = UUID()
      leave()
      await refresh()
    } catch { pairingError = error.localizedDescription }
  }

  func watchSupervisors() async {
    while !Task.isCancelled {
      await refresh()
      do { try await Task.sleep(for: .seconds(2)) } catch { return }
    }
  }

  @discardableResult func refresh() async -> [SessionInfo]? {
    let ticket = UUID()
    refreshTicket = ticket
    do {
      let dir = try directory
      guard FileManager.default.fileExists(atPath: dir + "/server.json") else { return nil }
      paired = true
      let r = await Core.request(["Op": "supervisors", "Dir": dir])
      if let error = r.Error {
        guard refreshTicket == ticket else { return nil }
        listError =
          error.contains("client authorization required")
          ? "Device access was revoked. Pair again with a fresh invitation." : error
        status = listError
        return nil
      }
      let observed = (r.Supervisors ?? []).map { row in
        var item = row.Info
        item.supervisor = row.ID
        item.revision = row.Revision
        item.editable = row.EditableCommand
        item.hasResult = row.RunResult
        item.workspace = row.Workspace
        item.exitCode = row.Exit
        return item
      }
      if refreshTicket == ticket {
        sessions = observed
        listError = ""
        latency = r.RTTMicros
        status = observed.isEmpty ? "No active sessions" : ""
      }
      // A lifecycle action consumes its own post-acknowledgement listing.
      // Another reader may have superseded publication to our UI cache.
      return observed
    } catch {
      if refreshTicket == ticket { status = error.localizedDescription }
      return nil
    }
  }

  func leave() {
    generation = UUID()
    let closing = [handle].compactMap { $0 } + Array(forwards.values)
    handle = nil
    latency = nil
    selected = nil
    forwards.removeAll()
    forwarding.removeAll()
    changingForwards.removeAll()
    renderer = nil
    pending = Data()
    stdout = Data()
    status = ""
    // Flush accepted keystrokes and pointer reports before closing their stream.
    inputQueue.async {
      for key in closing { _ = Core.call(["Op": "close", "Handle": key]) }
    }
  }

  func attach(_ item: SessionInfo) async {
    leave()
    guard item.running else {
      selected = item
      status = "Stopped"
      let ticket = generation
      if item.hasResult == true {
        let result = await manage(.result, item: item)
        guard generation == ticket else { return }
        stdout = result.Data ?? Data()
        status =
          result.Exit.map { "Process exited with status " + String($0) } ?? result.Error ?? status
      }
      return
    }
    let ticket = generation
    do {
      let r = await Core.request([
        "Op": readOnly ? "preview" : "attach", "Dir": try directory, "ID": item.ID,
      ])
      guard let key = r.Handle else {
        if ticket == generation {
          selected = item
          status = r.Error ?? "Attach failed"
        }
        return
      }
      guard ticket == generation else {
        _ = await Core.request(["Op": "close", "Handle": key])
        return
      }
      handle = key
      selected = item
      status = ""
      var childExited = false
      while handle == key {
        let event = await Core.request(["Op": "next", "Handle": key])
        guard handle == key else { return }
        if let error = event.Error {
          status = error
          break
        }
        if let exit = event.Exit {
          childExited = true
          status = "Process exited with status \(exit)"
          break
        }
        if let data = event.Data {
          var bytes = data
          if item.Mode == "pipe" {
            bytes = Data()
            for byte in data {
              if byte == 10 { bytes.append(13) }
              bytes.append(byte)
            }
          }
          if let renderer { renderer.receive(bytes) }
          else {
            pending.append(bytes)
            pending = pending.suffix(1024 * 1024)
          }
        }
      }
      if handle == key {
        let finalStatus = status
        let closing = Array(forwards.values) + [key]
        handle = nil
        forwards.removeAll()
        forwarding.removeAll()
        for id in closing { _ = await Core.request(["Op": "close", "Handle": id]) }
        guard ticket == generation else { return }
        // The child sends its exit on the output stream before the supervisor
        // publishes its next revision to the broker.
        let deadline = ContinuousClock.now + .seconds(6)
        repeat {
          guard ticket == generation else { return }
          await refresh()
          guard ticket == generation else { return }
          guard childExited, listError.isEmpty, (item.revision ?? 0) > 0,
            sessions.contains(where: { $0.id == item.id && $0.ID == item.ID }),
            ContinuousClock.now < deadline
          else { break }
          try? await Task.sleep(for: .milliseconds(150))
        } while !Task.isCancelled
        guard ticket == generation else { return }
        if let current = sessions.first(where: { $0.id == item.id }), !current.running {
          selected = current
          if current.hasResult == true {
            let result = await manage(.result, item: current)
            guard ticket == generation else { return }
            if let data = result.Data { stdout = data }
          }
        }
        status = finalStatus
      }
    } catch { status = error.localizedDescription }
  }

  func manage(_ action: SessionAction, item: SessionInfo, command: [Data]? = nil) async -> Reply {
    do {
      return await Core.action(
        action, directory: try directory, target: item, arguments: command,
        columns: renderer?.columns, rows: renderer?.rows)
    } catch { return Reply(Error: error.localizedDescription) }
  }

  func changeRun(_ action: SessionAction, item: SessionInfo, arguments: [Data]?) async -> String? {
    let ticket = generation
    let reply = await manage(action, item: item, command: arguments)
    guard generation == ticket else { return nil }
    if let error = reply.Error {
      await refresh()
      return error
    }
    // Stop the old reader before its exit event can replace the new run's state.
    leave()
    selected = item
    let replacement = generation
    let observed = await refresh()
    guard replacement == generation else { return nil }
    guard let current = observed?.first(where: { $0.id == item.id }) else {
      status = listError.isEmpty ? "The supervisor is unavailable." : listError
      return status
    }
    if let acknowledged = reply.Run, current.ID != acknowledged {
      status = "The supervisor changed again. Reconnect to its current run."
      return status
    }
    await attach(current)
    return nil
  }

  func reconnect(_ item: SessionInfo) async {
    let ticket = generation
    await refresh()
    guard ticket == generation else { return }
    guard listError.isEmpty, let current = sessions.first(where: { $0.id == item.id }) else {
      status = listError.isEmpty ? "This supervisor is unavailable." : listError
      return
    }
    await attach(current)
  }

  func install(_ view: any TerminalRenderer) {
    renderer = view
    if !pending.isEmpty {
      view.receive(pending)
      pending.removeAll()
    }
  }

  func send(_ input: TerminalInput) {
    guard !readOnly, let key = handle else { return }
    let request = input.fields(handle: key)
    inputQueue.async {
      let r = Core.call(request)
      if let error = r.Error {
        Task { @MainActor in if self.handle == key { self.status = error } }
      }
    }
  }

  func setForward(_ name: String, enabled: Bool, item: SessionInfo) async {
    guard attached, enabled != (forwards[name] != nil), changingForwards.insert(name).inserted else { return }
    let ticket = generation
    defer { if ticket == generation { changingForwards.remove(name) } }
    if !enabled, let key = forwards.removeValue(forKey: name) {
      forwarding.removeValue(forKey: name)
      _ = await Core.request(["Op": "close", "Handle": key])
      return
    }
    do {
      let r = await Core.request([
        "Op": "forward", "Dir": try directory, "ID": item.ID, "Name": name,
      ])
      if let key = r.Handle, let address = r.Address {
        guard ticket == generation, attached, forwards[name] == nil else {
          _ = await Core.request(["Op": "close", "Handle": key])
          return
        }
        forwards[name] = key
        forwarding[name] = address
      } else if ticket == generation {
        status = r.Error ?? "Forward failed"
      }
    } catch { if ticket == generation { status = error.localizedDescription } }
  }
}
