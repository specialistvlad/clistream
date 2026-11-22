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
  private(set) var samples = [String: Int64]()
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
    } catch { pairing.pairingError = error.localizedDescription }
  }
  func rename(_ profile: ServerProfile, to value: String) {
    let name = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !name.isEmpty, let i = profiles.firstIndex(where: { $0.id == profile.id }) else { return }
    profiles[i].name = name
    UserDefaults.standard.set(name, forKey: "server-name-" + profile.id)
  }
  func enroll(_ link: String) async {
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
        watch(updated)
      }
    } catch { pairing.pairingError = error.localizedDescription }
  }
  func retry(_ profile: ServerProfile) { watch(profile) }
  private func watch(_ profile: ServerProfile) {
    watchers[profile.id]?.cancel()
    watchers[profile.id] = Task { [weak self] in
      let connection = Connection(profile: profile.directory)
      while !Task.isCancelled {
        await connection.refresh()
        guard !Task.isCancelled, self != nil else { return }
        if self?.errors[profile.id] != connection.listError {
          self?.errors[profile.id] = connection.listError
        }
        if connection.listError.isEmpty && self?.rows[profile.id] != connection.sessions {
          self?.rows[profile.id] = connection.sessions
        }
        self?.samples[profile.id] = connection.listError.isEmpty ? connection.latency : nil
        try? await Task.sleep(for: .seconds(2))
      }
    }
  }
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
