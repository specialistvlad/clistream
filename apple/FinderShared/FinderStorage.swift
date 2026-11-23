import FileProvider
import Foundation
import Security

// The acceptance app has its own bundle and group identifiers.
enum FinderStorage {
  #if os(iOS)
  private final class BundleMarker: NSObject {}
  #endif
  static func container() throws -> URL {
    #if os(iOS)
    let bundle = Bundle(for: BundleMarker.self)
    let group: String
    if let extensionInfo = bundle.object(forInfoDictionaryKey: "NSExtension") as? [String: Any] {
      guard let declared = extensionInfo["NSExtensionFileProviderDocumentGroup"] as? String else {
        throw CocoaError(.fileReadNoPermission)
      }
      group = declared
    } else {
      guard let appID = bundle.bundleIdentifier else { throw CocoaError(.fileReadNoPermission) }
      group = "group." + appID + ".workspace"
    }
    #else
    guard let task = SecTaskCreateFromSelf(nil),
      let groups = SecTaskCopyValueForEntitlement(
        task, "com.apple.security.application-groups" as CFString, nil) as? [String],
      let group = groups.first(where: { $0.hasSuffix(".workspace") })
    else { throw CocoaError(.fileReadNoPermission) }
    #endif
    guard let folder = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
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
    try existingEnrollment(pin, in: container())
  }

  static func existingEnrollment(_ pin: String, in root: URL) throws -> URL {
    struct Record: Decodable { let Pin: String }
    // The first profile can live at the root. Match its pin before using it,
    // just as Workspace.load does when it adopts a legacy enrollment in place.
    let canonical = try credentialDirectory(pin, in: root)
    for folder in [canonical, root] {
      let record = folder.appendingPathComponent("server.json")
      guard FileManager.default.fileExists(atPath: record.path) else { continue }
      if try JSONDecoder().decode(Record.self, from: Data(contentsOf: record)).Pin == pin {
        return folder
      }
    }
    throw NSFileProviderError(.notAuthenticated)
  }

  static func credentialDirectory(_ pin: String, in root: URL) throws -> URL {
    guard pin.count == 64, pin.allSatisfy(\.isHexDigit) else {
      throw CocoaError(.fileReadCorruptFile)
    }
    return root.appendingPathComponent("Servers").appendingPathComponent(pin)
  }

  struct Server: Codable, Equatable {
    let pin: String
    let name: String
  }

  static func servers() throws -> [Server] {
    let url = try container().appendingPathComponent("finder-servers.json")
    do { return try JSONDecoder().decode([Server].self, from: Data(contentsOf: url)) } catch let
      error as CocoaError where error.code == .fileReadNoSuchFile
    { return [] }
  }

  static var domain: NSFileProviderDomain {
    let domain = NSFileProviderDomain(
      identifier: NSFileProviderDomainIdentifier("workspaces"), displayName: "CLIStream")
    domain.supportsSyncingTrash = false
    #if DEBUG && os(macOS)
      if let task = SecTaskCreateFromSelf(nil),
        SecTaskCopyValueForEntitlement(
          task, "com.apple.developer.fileprovider.testing-mode" as CFString, nil) as? Bool == true
      {
        domain.testingModes = [.alwaysEnabled]
      }
    #endif
    return domain
  }
}

enum FinderID {
  static let root = NSFileProviderItemIdentifier.rootContainer.rawValue
  static func make(_ parts: String...) -> String {
    parts.map { Data($0.utf8).base64EncodedString() }.joined(separator: ".")
  }
  static func workspace(_ pin: String, _ id: String) -> String { make("workspace", pin, id) }
  static func locationName(_ name: String) -> String {
    #if os(iOS)
    // These are virtual location names. Keep host:port addresses and leading
    // dots visible in Files without changing identities or remote filenames.
    let visible = name.replacingOccurrences(of: ":", with: "：")
    return visible.hasPrefix(".") ? "․" + visible.dropFirst() : visible
    #else
    return name
    #endif
  }
  static func displayNames(_ pairs: [(String, String)]) -> [String: String] {
    var used = Set<String>()
    var names = [String: String]()
    for (id, name) in pairs.sorted(by: { $0.0 < $1.0 }) {
      let raw = name.replacingOccurrences(of: "/", with: "∕")
      var stem = raw.isEmpty || raw == "." || raw == ".." ? "(\(raw))" : raw
      while stem.utf8.count > 240 { stem.removeLast() }
      var candidate = stem
      var suffix = 1
      while !used.insert(candidate.folding(options: [.caseInsensitive], locale: Locale(identifier: "en_US_POSIX"))
        .precomposedStringWithCanonicalMapping).inserted {
        suffix += 1
        candidate = "\(stem) (\(suffix))"
      }
      names[id] = candidate
    }
    return names
  }
  static func server(of identifier: String) -> String? {
    let parts = identifier.split(separator: ".", omittingEmptySubsequences: false)
    guard parts.count >= 2, let pin = Data(base64Encoded: String(parts[1])) else { return nil }
    return String(data: pin, encoding: .utf8)
  }
}
