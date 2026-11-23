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
  private var discoveryGeneration = [String: Int]()
  private var unavailableUntil = [String: ContinuousClock.Instant]()

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
  @discardableResult func store(_ value: WorkspaceNode) -> WorkspaceNode {
    var node = value
    let prior = state.nodes[node.id]
    if node.contentVersionAcrossMove == nil, node.version == prior?.version {
      node.contentVersionAcrossMove = prior?.contentVersionAcrossMove
    }
    guard prior != node else { return node }
    state.nodes[node.id] = node
    record(node.id, old: prior?.parent, new: node.parent)
    return node
  }
  func storeMove(_ moved: WorkspaceNode, from original: WorkspaceNode, basedOn version: String) -> WorkspaceNode {
    var node = moved
    if moved.id == original.id, original.version == version,
      moved.bytes == original.bytes, moved.modified == original.modified
    {
      node.contentVersionAcrossMove = original.contentRevision
    }
    return store(node)
  }
  func remove(_ id: String) {
    guard let node = state.nodes.removeValue(forKey: id) else { return }
    if node.kind == .server {
      discoveryGeneration[node.server, default: 0] += 1
      unavailableUntil.removeValue(forKey: node.server)
    }
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
    for child in children { store(child) }
  }

  // Finder names must fit on a case-insensitive volume. Keep the exact remote component
  // separately so addressing never depends on a display name or duplicate suffix.
  private func displayNames(_ nodes: [WorkspaceNode]) -> [WorkspaceNode] {
    let names = FinderID.displayNames(nodes.map { ($0.id, $0.name) })
    return nodes.sorted { $0.id < $1.id }.map { value in
      var node = value
      node.name = names[node.id] ?? node.name
      return node
    }
  }
  @discardableResult func refreshCatalog() throws -> [WorkspaceNode] {
    let servers = try FinderStorage.servers()
    let roots = displayNames(
      servers.map {
        WorkspaceNode(
          id: FinderID.make("server", $0.pin), parent: FinderID.root, name: $0.name, kind: .server,
          server: $0.pin)
      })
    reconcile(roots, parent: FinderID.root)
    return roots
  }

  static func supervisors(on server: WorkspaceNode) throws -> [WorkspaceService.Supervisor] {
    let dir = try FinderStorage.credentialDirectory(server.server)
    return try WorkspaceService.call(["Op": "supervisors", "Dir": dir.path]).Supervisors ?? []
  }
  func generation(for server: WorkspaceNode) -> Int { discoveryGeneration[server.server, default: 0] }

  private func noteFailure(_ error: NSError, on pin: String) {
    if error.domain == NSFileProviderErrorDomain && error.code == NSFileProviderError.Code.serverUnreachable.rawValue {
      unavailableUntil[pin] = .now + .seconds(15)
    }
  }
  private func metadata<T>(on pin: String, _ operation: () throws -> T) throws -> T {
    if let retry = unavailableUntil[pin], retry > .now { throw NSFileProviderError(.serverUnreachable) }
    do { return try operation() } catch let failure as NSError {
      noteFailure(failure, on: pin)
      throw failure
    }
  }
  func apply(_ result: Result<[WorkspaceService.Supervisor], Error>, to server: WorkspaceNode) throws {
    discoveryGeneration[server.server, default: 0] += 1
    let available: [WorkspaceService.Supervisor]
    do { available = try result.get().filter { $0.Workspace == true } } catch let error as NSError {
      noteFailure(error, on: server.server)
      if error.domain == NSFileProviderErrorDomain && error.code == NSFileProviderError.Code.notAuthenticated.rawValue {
        reconcile([], parent: server.id)
      }
      throw error
    }
    unavailableUntil.removeValue(forKey: server.server)
    let byHost = Dictionary(grouping: available, by: { $0.Info.Host })
    let hosts = displayNames(byHost.keys.map { name in
      WorkspaceNode(id: FinderID.make("host", server.server, name), parent: server.id,
                    name: name, kind: .host, server: server.server, remoteName: name)
    })
    reconcile(hosts, parent: server.id)
    for host in hosts {
      let workspaces = displayNames((byHost[host.remoteName] ?? []).map { run in
        WorkspaceNode(id: FinderID.workspace(server.server, run.ID), parent: host.id, name: run.Info.Name,
                      kind: .workspace, server: server.server, workspace: run.ID)
      })
      reconcile(workspaces, parent: host.id)
    }
  }
  func discover(only serverID: String?) throws {
    let roots = try refreshCatalog()
    if let server = roots.first(where: { $0.server == serverID }) {
      try metadata(on: server.server) {
        try apply(Result { try Self.supervisors(on: server) }, to: server)
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
    let list = try metadata(on: node.server) {
      try WorkspaceService.request("list", parent: node, path: path(of: node))
    }
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
        for child in incoming { store(child) }
      }
    case .file:
      let parent = try require(node.parent)
      let entry = try metadata(on: node.server) {
        try WorkspaceService.request("stat", parent: node, path: path(of: node)).entry()
      }
      var result = entry.node(in: parent)
      guard result.id == node.id else {
        remove(node.id)
        throw NSFileProviderError(.noSuchItem)
      }
      result.name = node.name
      store(result)
    }
  }

  func refreshFolders(server: String, workspace: String? = nil) throws {
    let watched = state.enumerated.compactMap(cached).filter {
      server == $0.server && (workspace == nil || workspace == $0.workspace)
    }.sorted { ((try? path(of: $0))?.count ?? 0) < ((try? path(of: $1))?.count ?? 0) }
    var seen = [String: Set<String>]()
    for item in watched {
      guard let current = cached(item.id) else { continue }
      do {
        let contents = try readChildren(current)
        seen[current.id] = Set(contents.map(\.id))
        for child in contents { store(child) }
      } catch let error as NSError
        where error.domain == NSFileProviderErrorDomain
        && error.code == NSFileProviderError.Code.noSuchItem.rawValue
      {
        continue
      }
    }
    for (parent, present) in seen {
      for child in children(of: parent) where !present.contains(child.id) { remove(child.id) }
    }
  }
}
