import CryptoKit
import FileProvider
import UniformTypeIdentifiers

struct WorkspaceNode: Codable, Equatable {
  enum Kind: String, Codable { case root, server, host, workspace, file, directory }
  let id: String
  var parent: String
  var name: String
  let kind: Kind
  var server = ""
  var workspace = ""
  var remoteName = ""
  var version = ""
  var bytes: Int64 = 0
  var modified: Int64 = 0
  var isRemote: Bool { kind == .file || kind == .directory }
  static let root = Self(id: FinderID.root, parent: FinderID.root, name: "CLIStream", kind: .root)
}

final class WorkspaceItem: NSObject, NSFileProviderItem {
  let node: WorkspaceNode
  init(_ node: WorkspaceNode) { self.node = node }
  var itemIdentifier: NSFileProviderItemIdentifier { .init(node.id) }
  var parentItemIdentifier: NSFileProviderItemIdentifier { .init(node.parent) }
  var filename: String { node.name }
  var contentType: UTType {
    if node.kind != .file { return .folder }
    return UTType(filenameExtension: (filename as NSString).pathExtension, conformingTo: .data)
      ?? .data
  }
  var documentSize: NSNumber? { node.kind == .file ? NSNumber(value: node.bytes) : nil }
  var contentModificationDate: Date? {
    node.modified == 0 ? nil : Date(timeIntervalSince1970: Double(node.modified) / 1_000_000_000)
  }
  var capabilities: NSFileProviderItemCapabilities {
    switch node.kind {
    case .file, .directory:
      return [.allowsReading, .allowsWriting, .allowsReparenting, .allowsRenaming, .allowsDeleting]
    case .workspace: return [.allowsContentEnumerating, .allowsAddingSubItems]
    default: return [.allowsContentEnumerating]
    }
  }
  var itemVersion: NSFileProviderItemVersion {
    // File Provider caps both components at 128 bytes. Keep the supervisor's
    // short version for conditional writes, and hash variable-length UI metadata.
    let content = node.isRemote ? Data(node.version.utf8) : Data(SHA256.hash(data: Data(node.id.utf8)))
    let metadata = Data(FinderID.make(node.parent, node.name, node.version).utf8)
    return .init(
      contentVersion: content,
      metadataVersion: Data(SHA256.hash(data: metadata)))
  }
}

struct WorkspaceEntry: Decodable {
  let ID, Name, Version: String
  let Directory: Bool
  let Size, Modified: Int64

  func node(in parent: WorkspaceNode) -> WorkspaceNode {
    WorkspaceNode(
      id: FinderID.make("item", parent.server, parent.workspace, ID),
      parent: parent.id, name: Name, kind: Directory ? .directory : .file, server: parent.server,
      workspace: parent.workspace, remoteName: Name, version: Version, bytes: Size,
      modified: Modified)
  }
}

enum WorkspaceService {
  struct Supervisor: Decodable {
    struct Info: Decodable { let Name, Host: String }
    let ID: String
    let Info: Info
    let Workspace: Bool?
  }
  struct Reply: Decodable {
    let Entries: [WorkspaceEntry]?
    let Entry: WorkspaceEntry?
    let Supervisors: [Supervisor]?
    let Error, ErrorKind: String?

    func entry() throws -> WorkspaceEntry {
      guard let Entry else { throw CocoaError(.fileReadCorruptFile) }
      return Entry
    }
  }

  static func call(_ fields: [String: Any]) throws -> Reply {
    let json = try JSONSerialization.data(withJSONObject: fields)
    guard let result = CSCall(String(decoding: json, as: UTF8.self)) else {
      throw NSFileProviderError(.serverUnreachable)
    }
    defer { free(result) }
    let reply = try JSONDecoder().decode(
      Reply.self, from: Data(bytes: result, count: strlen(result)))
    if let message = reply.Error, !message.isEmpty {
      let code: NSFileProviderError.Code
      switch reply.ErrorKind {
      case "conflict": code = .localVersionConflictingWithServer
      case "exists": code = .filenameCollision
      case "missing": code = .noSuchItem
      case "denied": code = .notAuthenticated
      case "permission":
        let reading = ["workspace-list", "workspace-stat", "workspace-fetch"].contains(
          fields["Op"] as? String ?? "")
        throw CocoaError(reading ? .fileReadNoPermission : .fileWriteNoPermission)
      case "noSpace": code = .insufficientQuota
      case "notEmpty": code = .directoryNotEmpty
      case "invalid": throw CocoaError(.fileWriteInvalidFileName)
      default:
        code = message.contains("authorization required") ? .notAuthenticated : .serverUnreachable
      }
      throw NSError(
        domain: NSFileProviderErrorDomain, code: code.rawValue,
        userInfo: [NSLocalizedDescriptionKey: message])
    }
    return reply
  }
  static func request(
    _ action: String, parent: WorkspaceNode, path: String, local: URL? = nil,
    destination: String? = nil, version: String? = nil, recursive: Bool = false,
    requestID: String? = nil
  ) throws -> Reply {
    var fields: [String: Any] = [
      "Op": "workspace-" + action, "Dir": try FinderStorage.credentialDirectory(parent.server).path,
      "ID": parent.workspace, "Path": path,
    ]
    fields["LocalPath"] = local?.path
    fields["Destination"] = destination
    fields["Version"] = version
    fields["Handle"] = requestID
    fields["Recursive"] = recursive
    return try call(fields)
  }
}
