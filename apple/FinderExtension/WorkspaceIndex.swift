import FileProvider
import Foundation

// This index contains metadata only. File Provider owns downloaded bytes and pending local edits.
// All access is serialized by WorkspaceProvider's domain queue.
final class WorkspaceIndex {
  struct Change: Codable {
    var sequence: Int
    var id: String
    var oldParent: String?
    var parent: String?
  }
  private struct State: Codable {
    var epoch = UUID()
    var sequence = 0
    var nodes: [String: WorkspaceNode] = [:]
    var enumerated: Set<String> = []
    var changes: [Change] = []
    var creationRequests = [String]()
  }
  private var state = State()
  private var loaded = false
  private var dirty = false

  func load() throws {
    guard !loaded else { return }
    let file = try FinderStorage.container().appendingPathComponent("finder-index.plist")
    if FileManager.default.fileExists(atPath: file.path) {
      // Metadata can be rebuilt from the server. A fresh epoch expires prior sync anchors.
      state =
        (try? PropertyListDecoder().decode(State.self, from: Data(contentsOf: file))) ?? State()
    }
    loaded = true
  }
  func save() throws {
    guard dirty else { return }
    if state.changes.count > 8192 { state.changes.removeFirst(state.changes.count - 8192) }
    let file = try FinderStorage.container().appendingPathComponent("finder-index.plist")
    let encoder = PropertyListEncoder()
    encoder.outputFormat = .binary
    try encoder.encode(state).write(to: file, options: .atomic)
    dirty = false
  }
  var sequence: Int { state.sequence }
  // File Provider preserves the template identifier across crashes and redelivery.
  func beginCreation(_ id: String) throws -> Bool {
    if state.creationRequests.contains(id) { return true }
    state.creationRequests.append(id)
    if state.creationRequests.count > 4096 { state.creationRequests.removeFirst() }
    dirty = true
    try save()
    return false
  }
  func clearCreation(_ id: String) {
    state.creationRequests.removeAll { $0 == id }
    dirty = true
  }
  var anchor: NSFileProviderSyncAnchor {
    .init(Data("\(state.epoch.uuidString):\(sequence)".utf8))
  }
  func changes(since anchor: NSFileProviderSyncAnchor) throws -> [Change] {
    let parts = String(decoding: anchor.rawValue, as: UTF8.self).split(separator: ":")
    guard parts.count == 2, parts[0] == state.epoch.uuidString, let since = Int(parts[1]),
      since >= (state.changes.first?.sequence ?? 1) - 1, since <= sequence
    else {
      throw NSFileProviderError(.syncAnchorExpired)
    }
    return state.changes.filter { $0.sequence > since }
  }
  var all: [WorkspaceNode] { Array(state.nodes.values) }
  private func record(_ id: String, old: String?, new: String?) {
    state.sequence += 1
    dirty = true
    state.changes.append(Change(sequence: state.sequence, id: id, oldParent: old, parent: new))
  }
  func store(_ node: WorkspaceNode) {
    let prior = state.nodes[node.id]
    guard prior != node else { return }
    state.nodes[node.id] = node
    record(node.id, old: prior?.parent, new: node.parent)
  }
  func remove(_ id: String) {
    guard let node = state.nodes.removeValue(forKey: id) else { return }
    state.enumerated.remove(id)
    record(id, old: node.parent, new: nil)
    if node.kind != .file {
      for child in children(of: id) { remove(child.id) }
    }
  }
  func cached(_ id: String) -> WorkspaceNode? { id == FinderID.root ? .root : state.nodes[id] }
  func require(_ id: String) throws -> WorkspaceNode {
    guard let node = cached(id) else {
      throw NSError.fileProviderErrorForNonExistentItem(withIdentifier: .init(id))
    }
    return node
  }
  func children(of id: String) -> [WorkspaceNode] { state.nodes.values.filter { $0.parent == id } }

  private func reconcile(_ children: [WorkspaceNode], parent: String) {
    let next = Set(children.map(\.id))
    for old in self.children(of: parent) where !next.contains(old.id) { remove(old.id) }
    children.forEach(store)
  }

  // Finder names must fit on a case-insensitive volume. Keep the exact remote component
  // separately so addressing never depends on a display name or duplicate suffix.
  private func displayNames(_ nodes: [WorkspaceNode]) -> [WorkspaceNode] {
    var used = Set<String>()
    return nodes.sorted { $0.id < $1.id }.map { value in
      var node = value
      let raw = node.name.replacingOccurrences(of: "/", with: "∕")
      var stem = raw.isEmpty || raw == "." || raw == ".." ? "(\(raw))" : raw
      while stem.utf8.count > 240 { stem.removeLast() }
      node.name = stem
      var suffix = 1
      while !used.insert(
        node.name.folding(options: [.caseInsensitive], locale: Locale(identifier: "en_US_POSIX"))
          .precomposedStringWithCanonicalMapping
      ).inserted {
        suffix += 1
        node.name = "\(stem) (\(suffix))"
      }
      return node
    }
  }
  func discover(only serverID: String? = nil) throws {
    let servers = try FinderStorage.servers()
    let roots = displayNames(
      servers.map {
        WorkspaceNode(
          id: FinderID.make("server", $0.pin), parent: FinderID.root, name: $0.name, kind: .server,
          server: $0.pin)
      })
    reconcile(roots, parent: FinderID.root)
    for server in roots where serverID == nil || server.server == serverID {
      do {
        let dir = try FinderStorage.credentialDirectory(server.server)
        let reply = try WorkspaceService.call(["Op": "supervisors", "Dir": dir.path])
        let available = (reply.Supervisors ?? []).filter { $0.Workspace == true }
        let byHost = Dictionary(grouping: available, by: { $0.Info.Host })
        let hosts = displayNames(
          byHost.keys.map { name in
            WorkspaceNode(
              id: FinderID.make("host", server.server, name), parent: server.id,
              name: name, kind: .host, server: server.server, remoteName: name)
          })
        reconcile(hosts, parent: server.id)
        for host in hosts {
          let workspaces = displayNames(
            (byHost[host.remoteName] ?? []).map { run in
              WorkspaceNode(
                id: FinderID.workspace(server.server, run.ID), parent: host.id, name: run.Info.Name,
                kind: .workspace, server: server.server, workspace: run.ID)
            })
          reconcile(workspaces, parent: host.id)
        }
      } catch let error as NSError {
        if error.domain == NSFileProviderErrorDomain
          && error.code == NSFileProviderError.Code.notAuthenticated.rawValue
        {
          reconcile([], parent: server.id)
        }
        // An unavailable server must not erase materialized files or hide other servers.
        if serverID != nil { throw error }
      }
    }
  }
  func path(of node: WorkspaceNode) throws -> String {
    var components = [String]()
    var current = node
    while current.isRemote {
      components.append(current.remoteName)
      current = try require(current.parent)
    }
    guard current.kind == .workspace else { throw CocoaError(.fileWriteNoPermission) }
    return components.isEmpty ? "." : components.reversed().joined(separator: "/")
  }
  func childPath(_ name: String, in parent: WorkspaceNode) throws -> String {
    guard !name.isEmpty, name != ".", name != "..", !name.contains("/"), !name.contains("\0"),
      parent.kind == .directory || parent.kind == .workspace
    else { throw CocoaError(.fileWriteInvalidFileName) }
    let root = try path(of: parent)
    return root == "." ? name : root + "/" + name
  }
  private func readChildren(_ node: WorkspaceNode) throws -> [WorkspaceNode] {
    let list = try WorkspaceService.request("list", parent: node, path: path(of: node))
    guard let entries = list.Entries else { throw CocoaError(.fileReadCorruptFile) }
    for entry in entries where entry.Name == "." || entry.Name == ".." || entry.Name.contains("/") {
      throw CocoaError(.fileReadCorruptFile)
    }
    if state.enumerated.insert(node.id).inserted { dirty = true }
    return displayNames(entries.map { $0.node(in: node) })
  }

  func refresh(_ node: WorkspaceNode) throws {
    switch node.kind {
    case .root, .server, .host:
      try discover(only: node.kind == .root ? nil : node.server)
    case .workspace, .directory:
      let incoming = try readChildren(node)
      let ids = Set(incoming.map(\.id))
      if children(of: node.id).contains(where: { !ids.contains($0.id) }) {
        // Read known destinations before emitting deletions. A move of an open directory
        // must carry its materialized descendants and their identifiers to the new parent.
        try refreshFolders(server: node.server, workspace: node.workspace)
      } else {
        incoming.forEach(store)
      }
    case .file:
      let parent = try require(node.parent)
      let entry = try WorkspaceService.request("stat", parent: node, path: path(of: node)).entry()
      var result = entry.node(in: parent)
      guard result.id == node.id else {
        remove(node.id)
        throw NSFileProviderError(.noSuchItem)
      }
      result.name = node.name
      store(result)
    }
  }

  func refreshWorkingSet() throws {
    try discover()
    try refreshFolders()
  }

  private func refreshFolders(server: String? = nil, workspace: String? = nil) throws {
    let watched = state.enumerated.compactMap(cached).filter {
      (server == nil || server == $0.server) && (workspace == nil || workspace == $0.workspace)
    }.sorted { ((try? path(of: $0))?.count ?? 0) < ((try? path(of: $1))?.count ?? 0) }
    var seen = [String: Set<String>]()
    for item in watched {
      guard let current = cached(item.id) else { continue }
      do {
        let contents = try readChildren(current)
        seen[current.id] = Set(contents.map(\.id))
        contents.forEach(store)
      } catch let error as NSError
        where error.domain == NSFileProviderErrorDomain
        && [.serverUnreachable, .notAuthenticated, .noSuchItem].contains(
          NSFileProviderError.Code(rawValue: error.code))
      {
        continue
      }
    }
    for (parent, present) in seen {
      for child in children(of: parent) where !present.contains(child.id) { remove(child.id) }
    }
  }
}
