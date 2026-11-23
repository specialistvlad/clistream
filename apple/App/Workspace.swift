import Combine
import SwiftUI

struct ServerProfile: Identifiable {
  let id: String
  let address: String
  let directory: String
  var name: String
}

struct WorkspaceSession: Identifiable {
  let server: ServerProfile
  let item: SessionInfo
  var id: String { server.id + "/" + item.id }
}

@MainActor final class Workspace: ObservableObject {
  static let shared = Workspace()
  let pairing: Connection
  init(directory: String? = nil) { pairing = Connection(profile: directory) }
  deinit { for task in watchers.values { task.cancel() } }
  @Published private(set) var profiles = [ServerProfile]()
  @Published private(set) var rows = [String: [SessionInfo]]()
  @Published private(set) var errors = [String: String]()
  #if os(macOS)
    // Latency updates belong to the two labels displaying them. Invalidating
    // the entire window here also rebuilds its focused menu and outline.
    let latencySamples = CurrentValueSubject<[String: Int64], Never>([:])
    var samples: [String: Int64] { latencySamples.value }
  #else
    // The mobile status bar samples latency on its own timer. Publishing every
    // RTT here rebuilds its parent toolbar and closes the native Keys submenu.
    private(set) var samples = [String: Int64]()
  #endif
  private var watchers = [String: Task<Void, Never>]()
  private var terminals = [String: Connection]()
  var sessions: [WorkspaceSession] {
    profiles.flatMap { server in
      (rows[server.id] ?? []).map { WorkspaceSession(server: server, item: $0) }
    }
  }
  func load() {
    do {
      let root = URL(fileURLWithPath: try pairing.directory)
      let folder = root.appendingPathComponent("Servers")
      try FileManager.default.createDirectory(
        at: folder, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
      struct Saved: Decodable { let Address, Pin: String }
      // Adopt the original directory in place so its enrolled device key stays valid.
      for url in [root]
        + (try FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil))
      {
        guard let data = try? Data(contentsOf: url.appendingPathComponent("server.json")),
          let saved = try? JSONDecoder().decode(Saved.self, from: data),
          !profiles.contains(where: { $0.id == saved.Pin })
        else { continue }
        let profile = ServerProfile(
          id: saved.Pin, address: saved.Address, directory: url.path,
          name: UserDefaults.standard.string(forKey: "server-name-" + saved.Pin) ?? saved.Address)
        profiles.append(profile)
        watch(profile)
      }
      if let recent = UserDefaults.standard.string(forKey: "last-paired-server"),
        let index = profiles.firstIndex(where: { $0.id == recent }), index != 0
      {
        let profile = profiles.remove(at: index)
        profiles.insert(profile, at: 0)
      }
      updateFinder()
    } catch { pairing.pairingError = error.localizedDescription }
  }
  func rename(_ profile: ServerProfile, to value: String) {
    let name = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !name.isEmpty, let i = profiles.firstIndex(where: { $0.id == profile.id }) else { return }
    profiles[i].name = name
    UserDefaults.standard.set(name, forKey: "server-name-" + profile.id)
    updateFinder()
  }
  func enroll(_ link: String) async {
    pairing.pairingError = nil
    let info = Core.inspect(link)
    guard let pin = info.Pin, pin.count == 64, pin.allSatisfy(\.isHexDigit) else {
      pairing.pairingError = info.Error ?? "Invalid server invitation"
      return
    }
    do {
      load()
      let dir =
        try profiles.first(where: { $0.id == pin })?.directory ?? pairing.directory + "/Servers/"
        + pin
      let reply = await Core.request(["Op": "pair", "Dir": dir, "Link": link])
      if let error = reply.Error {
        pairing.pairingError = error + ". Request a fresh invitation from the running server."
        return
      }
      load()
      if let index = profiles.firstIndex(where: { $0.id == pin }), let address = info.Address {
        let old = profiles[index]
        profiles.remove(at: index)
        let updated = ServerProfile(
          id: pin, address: address, directory: old.directory,
          name: UserDefaults.standard.string(forKey: "server-name-" + pin) ?? address)
        profiles.insert(updated, at: 0)
        UserDefaults.standard.set(pin, forKey: "last-paired-server")
        watch(updated, resumeAttachments: true)
        updateFinder()
      }
    } catch { pairing.pairingError = error.localizedDescription }
  }
  func retry(_ profile: ServerProfile) { watch(profile) }
  private func watch(_ profile: ServerProfile, resumeAttachments: Bool = false) {
    watchers[profile.id]?.cancel()
    watchers[profile.id] = Task { [weak self] in
      let connection = Connection(profile: profile.directory)
      var resume = resumeAttachments
      while !Task.isCancelled {
        await connection.refresh()
        guard !Task.isCancelled else { return }
        self?.receive(connection, from: profile, resumeAttachments: resume)
        if connection.listError.isEmpty { resume = false }
        try? await Task.sleep(for: .seconds(2))
      }
    }
  }
  private func receive(_ connection: Connection, from profile: ServerProfile, resumeAttachments: Bool) {
    guard watchers[profile.id] != nil else { return }
    let restored = connection.listError.isEmpty && (resumeAttachments || revoked(profile))
    let errorChanged = errors[profile.id] != connection.listError
    let rowsChanged = connection.listError.isEmpty && rows[profile.id] != connection.sessions
    if errorChanged { errors[profile.id] = connection.listError }
    if rowsChanged { rows[profile.id] = connection.sessions }
    if errorChanged || rowsChanged { updateFinder() }
    if restored {
      // Pairing can return directly to an open terminal on any Apple device.
      for item in connection.sessions where item.running {
        let id = WorkspaceSession(server: profile, item: item).id
        if let terminal = terminals[id], terminal.selected != nil, !terminal.attached {
          Task { await terminal.attach(item) }
        }
      }
    }
    let sample = connection.listError.isEmpty ? connection.latency : nil
    setSample(sample, for: profile.id)
  }

  func revoked(_ server: ServerProfile) -> Bool {
    errors[server.id]?.localizedCaseInsensitiveContains("revoked") == true
  }

  private func setSample(_ sample: Int64?, for server: String) {
    guard samples[server] != sample else { return }
    #if os(macOS)
      latencySamples.value[server] = sample
    #else
      samples[server] = sample
    #endif
  }

  private func updateFinder() {
    #if os(macOS)
      guard pairing.profile == nil else { return }
      do {
        try FinderWorkspace.update(servers: profiles, sessions: sessions, errors: errors)
      } catch {
        // A Finder registration failure must not interrupt a live terminal.
        NSLog("CLIStream Finder catalog: %@", error.localizedDescription)
      }
    #endif
  }

  #if os(macOS)
    func remove(_ profile: ServerProfile) throws {
      guard profiles.contains(where: { $0.id == profile.id }) else { return }
      let root = URL(fileURLWithPath: try pairing.directory).standardizedFileURL
      let directory = URL(fileURLWithPath: profile.directory).standardizedFileURL
      let fm = FileManager.default
      if directory.path == root.path {
        // A legacy profile shares this directory with all subsequent enrollments.
        for file in ["key.pem", "server.json"] {
          let url = directory.appendingPathComponent(file)
          if fm.fileExists(atPath: url.path) { try fm.removeItem(at: url) }
        }
      } else {
        guard directory.deletingLastPathComponent().path == root.appendingPathComponent("Servers").path
        else { throw CocoaError(.fileWriteNoPermission) }
        if fm.fileExists(atPath: directory.path) { try fm.removeItem(at: directory) }
      }
      watchers.removeValue(forKey: profile.id)?.cancel()
      for id in Array(terminals.keys) where id.hasPrefix(profile.id + "/") { detach(id) }
      profiles.removeAll { $0.id == profile.id }
      rows.removeValue(forKey: profile.id)
      setSample(nil, for: profile.id)
      errors.removeValue(forKey: profile.id)
      UserDefaults.standard.removeObject(forKey: "server-name-" + profile.id)
      if UserDefaults.standard.string(forKey: "last-paired-server") == profile.id {
        UserDefaults.standard.removeObject(forKey: "last-paired-server")
      }
      updateFinder()
    }
  #endif

  func terminal(_ row: WorkspaceSession) -> Connection {
    if let current = terminals[row.id] { return current }
    let current = Connection(profile: row.server.directory)
    terminals[row.id] = current
    Task { await current.attach(row.item) }
    return current
  }
  func activate(_ row: WorkspaceSession) {
    let current = terminal(row)
    if let previous = current.selected,
      previous.ID != row.item.ID || (row.item.running && !current.attached)
    {
      Task { await current.attach(row.item) }
    }
  }
  #if os(macOS)
    func focus(_ id: String?) {
      guard let id, let connection = terminals[id] else { return }
      connection.focusTerminal()
      connection.focusInput.send()
    }
  #endif
  func detach(_ id: String) { terminals.removeValue(forKey: id)?.leave() }
}
