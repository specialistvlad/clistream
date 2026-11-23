import FileProvider
import Foundation
import Security

// Both the signed app and its extension read their group from the same entitlement.
// The acceptance app has its own bundle and group identifiers.
enum FinderStorage {
  static func container() throws -> URL {
    guard let task = SecTaskCreateFromSelf(nil),
      let groups = SecTaskCopyValueForEntitlement(
        task, "com.apple.security.application-groups" as CFString, nil) as? [String],
      let group = groups.first(where: { $0.hasSuffix(".workspace") }),
      let folder = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
    else { throw CocoaError(.fileReadNoPermission) }
    var root = folder.appendingPathComponent("CLIStream", isDirectory: true)
    try FileManager.default.createDirectory(
      at: root, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    var values = URLResourceValues()
    values.isExcludedFromBackup = true
    try root.setResourceValues(values)
    return root
  }

  static func credentialDirectory(_ pin: String) throws -> URL {
    guard pin.count == 64, pin.allSatisfy(\.isHexDigit) else {
      throw CocoaError(.fileReadCorruptFile)
    }
    return try container().appendingPathComponent("Servers").appendingPathComponent(pin)
  }

  struct Server: Codable, Equatable {
    let pin: String
    let name: String
  }

  static func servers() throws -> [Server] {
    let url = try container().appendingPathComponent("finder-servers.json")
    do { return try JSONDecoder().decode([Server].self, from: Data(contentsOf: url)) }
    catch let error as CocoaError where error.code == .fileReadNoSuchFile { return [] }
  }

  static var domain: NSFileProviderDomain {
    let domain = NSFileProviderDomain(
      identifier: NSFileProviderDomainIdentifier("workspaces"), displayName: "CLIStream")
    domain.supportsSyncingTrash = false
    return domain
  }
}

enum FinderID {
  static let root = NSFileProviderItemIdentifier.rootContainer.rawValue
  static func make(_ parts: String...) -> String {
    parts.map { Data($0.utf8).base64EncodedString() }.joined(separator: ".")
  }
  static func workspace(_ pin: String, _ id: String) -> String { make("workspace", pin, id) }
}
