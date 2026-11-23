#if os(macOS)
import AppKit
#endif
import FileProvider
import OSLog

@MainActor enum FinderWorkspace {
  private static let logger = Logger(subsystem: "com.vlk.clistream", category: "Finder")
  private static var registration: Task<NSFileProviderManager, Error>?
  private static var migrated: URL?
  private static var lastSignature: [String]?

  static func enrollmentRoot(legacy: URL) throws -> URL {
    if let migrated { return migrated }
    let root = try FinderStorage.container()
    try migrate(legacy: legacy, to: root)
    migrated = root
    return root
  }

  static func migrate(legacy: URL, to root: URL) throws {
    let fm = FileManager.default
    let marker = root.appendingPathComponent(".profiles-migrated")
    if !fm.fileExists(atPath: marker.path) {
      let oldServers = legacy.appendingPathComponent("Servers")
      var previous = [legacy]
      if fm.fileExists(atPath: oldServers.path) {
        previous += try fm.contentsOfDirectory(at: oldServers, includingPropertiesForKeys: nil)
      }
      for source in previous {
        let metadata = source.appendingPathComponent("server.json")
        guard fm.fileExists(atPath: metadata.path) else { continue }
        struct Saved: Decodable { let Pin: String }
        let pin = try JSONDecoder().decode(Saved.self, from: Data(contentsOf: metadata)).Pin
        let destination = try FinderStorage.credentialDirectory(pin, in: root)
        try fm.createDirectory(
          at: destination.deletingLastPathComponent(), withIntermediateDirectories: true,
          attributes: [.posixPermissions: 0o700])
        let key = source.appendingPathComponent("key.pem")
        if !fm.fileExists(atPath: destination.path) {
          // Publish the two credential files together. Never expose a profile with a new key.
          let stage = root.appendingPathComponent(UUID().uuidString)
          try fm.createDirectory(
            at: stage, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
          defer { try? fm.removeItem(at: stage) }
          for url in [key, metadata] {
            try fm.copyItem(at: url, to: stage.appendingPathComponent(url.lastPathComponent))
          }
          try fm.moveItem(at: stage, to: destination)
        }
        let currentKey = try Data(contentsOf: destination.appendingPathComponent("key.pem"))
        if fm.contents(atPath: key.path) == currentKey {
          try fm.removeItem(at: key)
        }
        // If a previous launch stopped between the two unlinks, the destination is
        // already complete. Remove the surviving metadata only after checking it.
        if !fm.fileExists(atPath: key.path),
          try Data(contentsOf: metadata)
            == Data(contentsOf: destination.appendingPathComponent("server.json"))
        {
          try fm.removeItem(at: metadata)
        }
      }
      try Data().write(to: marker, options: .atomic)
    }
  }

  private static func manager() async throws -> NSFileProviderManager {
    if let registration { return try await registration.value }
    let task = Task { () throws -> NSFileProviderManager in
      let domain = FinderStorage.domain
      let registered = try await NSFileProviderManager.domains()
      if !registered.contains(where: { $0.identifier == domain.identifier }) {
        try await NSFileProviderManager.add(domain)
      }
      guard let manager = NSFileProviderManager(for: domain) else {
        throw NSFileProviderError(.providerNotFound)
      }
      return manager
    }
    registration = task
    do { return try await task.value } catch {
      registration = nil
      throw error
    }
  }

  static func update(
    servers: [ServerProfile], sessions: [WorkspaceSession], errors: [String: String]
  ) throws {
    let entries = servers.map { FinderStorage.Server(pin: $0.id, name: $0.name) }.sorted {
      $0.pin < $1.pin
    }
    let previous = try FinderStorage.servers()
    if previous != entries {
      let url = try FinderStorage.container().appendingPathComponent("finder-servers.json")
      try JSONEncoder().encode(entries).write(to: url, options: .atomic)
    }
    let workspaces = sessions.filter { $0.item.workspace == true }
    let signature =
      (entries.map { FinderID.make($0.pin, $0.name, errors[$0.pin] ?? "") }
      + workspaces.map { FinderID.make($0.id, $0.item.Host, $0.item.Name) }).sorted()
    guard lastSignature != signature else { return }
    lastSignature = signature
    guard !servers.isEmpty || !previous.isEmpty else { return }
    Task {
      do {
        let manager = try await manager()
        try await manager.signalEnumerator(for: .workingSet)
      } catch {
        let reason = error as NSError
        logger.error("Finder registration or refresh failed: \(reason.domain, privacy: .public) \(reason.code)")
      }
    }
  }

  #if os(iOS)
  static func prepareForBrowsing() async throws {
    _ = try await manager()
  }

  static func filesPath(_ row: WorkspaceSession, in workspace: Workspace) -> String {
    let pin = row.server.id
    let rows = workspace.sessions.filter { $0.server.id == pin && $0.item.workspace == true }
    let servers = FinderID.displayNames(workspace.profiles.map { (FinderID.make("server", $0.id), $0.name) })
    let hosts = FinderID.displayNames(Set(rows.map { $0.item.Host }).map { (FinderID.make("host", pin, $0), $0) })
    let programs = FinderID.displayNames(rows.filter { $0.item.Host == row.item.Host }
      .map { (FinderID.workspace(pin, $0.item.id), $0.item.Name) })
    return ["Browse", "CLIStream", servers[FinderID.make("server", pin)] ?? row.server.name,
            hosts[FinderID.make("host", pin, row.item.Host)] ?? row.item.Host,
            programs[FinderID.workspace(pin, row.item.id)] ?? row.item.Name].joined(separator: " → ")
  }
  #else
  static func show(serverID: String, workspaceID: String) async throws {
    do {
      #if DEBUG
        logger.notice("Resolving selected Finder workspace")
      #endif
      let manager = try await manager()
      let id = NSFileProviderItemIdentifier(FinderID.workspace(serverID, workspaceID))
      let folder = try await manager.getUserVisibleURL(for: id)
      let acquired = folder.startAccessingSecurityScopedResource()
      defer { if acquired { folder.stopAccessingSecurityScopedResource() } }
      guard NSWorkspace.shared.selectFile(nil, inFileViewerRootedAtPath: folder.path) else {
        throw CocoaError(.fileReadUnknown)
      }
      #if DEBUG
        logger.notice("Opened selected Finder workspace")
      #endif
    } catch {
      let reason = error as NSError
      logger.error("Could not open Finder workspace: \(reason.domain, privacy: .public) \(reason.code)")
      if reason.domain == NSFileProviderErrorDomain && reason.code == NSFileProviderError.Code.domainDisabled.rawValue {
        throw NSError(domain: reason.domain, code: reason.code, userInfo: [
          NSLocalizedDescriptionKey:
            "In Finder, select CLIStream under Locations and click Enable. You can also turn on CLIStream in System Settings › General › Login Items & Extensions › File Providers. Then choose Show in Finder again."
        ])
      }
      throw error
    }
  }
  #endif
}
