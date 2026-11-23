#if os(macOS)
  import Foundation
  import XCTest

  @testable import CLIStream

  extension ClientTests {
    func testFinderResolvesOnlyTheMatchingRootEnrollment() throws {
      let files = FileManager.default
      let root = files.temporaryDirectory.appendingPathComponent(UUID().uuidString)
      defer { try? files.removeItem(at: root) }
      let legacy = String(repeating: "a", count: 64)
      let current = String(repeating: "b", count: 64)
      let unknown = String(repeating: "c", count: 64)
      let nested = try FinderStorage.credentialDirectory(current, in: root)
      try files.createDirectory(at: nested, withIntermediateDirectories: true)
      for (dir, pin) in [(root, legacy), (nested, current)] {
        try JSONEncoder().encode(["Pin": pin]).write(to: dir.appendingPathComponent("server.json"))
      }
      XCTAssertEqual(try FinderStorage.existingEnrollment(legacy, in: root).path, root.path)
      XCTAssertEqual(try FinderStorage.existingEnrollment(current, in: root).path, nested.path)
      XCTAssertThrowsError(try FinderStorage.existingEnrollment(unknown, in: root))
    }

    @MainActor func testMigratesMacProfilesWithoutChangingKeysOrRevivingRemovedServers() throws {
      let files = FileManager.default
      let temp = files.temporaryDirectory.appendingPathComponent(UUID().uuidString)
      defer { try? files.removeItem(at: temp) }
      let legacy = temp.appendingPathComponent("old")
      let group = temp.appendingPathComponent("group")
      try files.createDirectory(at: group, withIntermediateDirectories: true)
      let a = String(repeating: "a", count: 64)
      let b = String(repeating: "b", count: 64)
      let profiles = [
        (legacy, a), (legacy.appendingPathComponent("Servers").appendingPathComponent(b), b),
      ]
      for (folder, pin) in profiles {
        try files.createDirectory(at: folder, withIntermediateDirectories: true)
        let record = try JSONSerialization.data(withJSONObject: [
          "Pin": pin, "Address": "localhost:7443",
        ])
        try record.write(to: folder.appendingPathComponent("server.json"))
        try Data(pin.utf8).write(to: folder.appendingPathComponent("key.pem"))
      }
      // Reproduce a crash after publishing both files and removing only the old private key.
      let partial = try FinderStorage.credentialDirectory(a, in: group)
      try files.createDirectory(at: partial, withIntermediateDirectories: true)
      for file in ["key.pem", "server.json"] {
        try files.copyItem(
          at: legacy.appendingPathComponent(file), to: partial.appendingPathComponent(file))
      }
      try files.removeItem(at: legacy.appendingPathComponent("key.pem"))
      try FinderWorkspace.migrate(legacy: legacy, to: group)
      for (source, pin) in profiles {
        let migrated = try FinderStorage.credentialDirectory(pin, in: group)
        XCTAssertEqual(
          try Data(contentsOf: migrated.appendingPathComponent("key.pem")), Data(pin.utf8))
        XCTAssertFalse(files.fileExists(atPath: source.appendingPathComponent("server.json").path))
        XCTAssertFalse(files.fileExists(atPath: source.appendingPathComponent("key.pem").path))
      }
      try files.removeItem(at: partial)
      try FinderWorkspace.migrate(legacy: legacy, to: group)
      XCTAssertFalse(files.fileExists(atPath: partial.path))
      XCTAssertTrue(
        files.fileExists(atPath: try FinderStorage.credentialDirectory(b, in: group).path))
    }
  }
#endif
