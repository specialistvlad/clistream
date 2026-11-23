#if os(macOS)
import AppKit
import XCTest

extension NativeSessionTests {
  @MainActor func testFinderWorkspaceRoundTrip() async throws {
    let app = try await openPipe()
    let show = app.descendants(matching: .any)["session-show-finder"].firstMatch
    XCTAssertTrue(show.waitForExistence(timeout: 15))
    show.click()
    let finder = XCUIApplication(bundleIdentifier: "com.apple.finder")
    XCTAssertTrue(finder.wait(for: .runningForeground, timeout: 20))
    let window = finder.windows.firstMatch
    XCTAssertTrue(window.waitForExistence(timeout: 10))

    // In Finder, go up one level and copy the selected workspace's file URL.
    // This discovers the actual provider domain even when macOS renames its mount.
    finder.typeKey(.upArrow, modifierFlags: .command)
    let board = NSPasteboard.general
    board.clearContents()
    defer { board.clearContents() }
    finder.typeKey("c", modifierFlags: .command)
    func selectedURL() -> URL? {
      (board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true])?.first as? NSURL) as URL?
    }
    let selected = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
      selectedURL() != nil
    }, object: nil)
    await fulfillment(of: [selected], timeout: 10)
    let folder = try XCTUnwrap(selectedURL())
    XCTAssertTrue(folder.lastPathComponent.hasPrefix("sh"))
    finder.typeKey(.downArrow, modifierFlags: .command)
    finder.typeKey("2", modifierFlags: .command)
    let temp = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: temp, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: temp) }
    let filename = "finder-roundtrip.txt"
    let incoming = temp.appendingPathComponent(filename)
    try Data("written on the Mac\n".utf8).write(to: incoming)
    board.clearContents()
    XCTAssertTrue(board.writeObjects([incoming as NSURL]))
    finder.typeKey("v", modifierFlags: .command)
    let uploaded = try await fixtureResponse("finder/uploaded")
    XCTAssertEqual(uploaded, "uploaded")
    let source = folder.appendingPathComponent(filename)
    let changed = try await fixtureResponse("finder/host-edit")
    XCTAssertEqual(changed, "edited")
    app.activate()
    app.typeKey(".", modifierFlags: .command)
    let stop = app.buttons["Stop"].firstMatch
    XCTAssertTrue(stop.waitForExistence(timeout: 10))
    stop.click()
    XCTAssertTrue(app.buttons["Start…"].firstMatch.waitForExistence(timeout: 15))
    XCTAssertTrue(show.exists, "Stopping a managed child lost its workspace")
    finder.activate()

    // Open as an ordinary document consumer. Coordination asks the extension
    // to materialize the file and receives later versions from the host.
    func contents(of url: URL) throws -> String {
      var observed: Result<String, Error>?
      var error: NSError?
      NSFileCoordinator().coordinate(readingItemAt: url, options: [], error: &error) { path in
        observed = Result { try String(contentsOf: path, encoding: .utf8) }
      }
      if let error { throw error }
      return try XCTUnwrap(observed).get()
    }
    let deadline = ContinuousClock.now + .seconds(40)
    while (try? contents(of: source)) != "edited on the host\n", ContinuousClock.now < deadline {
      try await Task.sleep(for: .milliseconds(400))
    }
    XCTAssertEqual(try contents(of: source), "edited on the host\n")

    finder.typeKey("n", modifierFlags: [.command, .shift])
    finder.typeText("Finder Round Trip")
    finder.typeKey(.return, modifierFlags: [])
    func entry(_ name: String) -> XCUIElement {
      window.descendants(matching: .any).matching(NSPredicate(
        format: "label == %@ OR value == %@ OR title == %@", name, name, name)).firstMatch
    }
    let subfolder = entry("Finder Round Trip")
    let file = entry(filename)
    XCTAssertTrue(file.waitForExistence(timeout: 12))
    file.click()
    finder.typeKey("c", modifierFlags: .command)
    XCTAssertTrue(subfolder.waitForExistence(timeout: 12))
    subfolder.doubleClick()
    finder.typeKey("v", modifierFlags: [.command, .option])
    let moved = try await fixtureResponse("finder/moved")
    XCTAssertEqual(moved, "moved")

    let destination = folder.appendingPathComponent("Finder Round Trip").appendingPathComponent(filename)
    var coordinationError: NSError?
    var save: Result<Void, Error>?
    NSFileCoordinator().coordinate(writingItemAt: destination, options: [], error: &coordinationError) { path in
      save = Result { try Data("saved on the Mac\n".utf8).write(to: path) }
    }
    if let coordinationError { throw coordinationError }
    try XCTUnwrap(save).get()
    let saved = try await fixtureResponse("finder/saved")
    XCTAssertEqual(saved, "saved")

    file.click()
    finder.typeKey(.delete, modifierFlags: .command)
    if finder.buttons["Delete"].firstMatch.waitForExistence(timeout: 2) { finder.buttons["Delete"].firstMatch.click() }
    XCTAssertTrue(file.waitForNonExistence(timeout: 15))
    finder.typeKey(.upArrow, modifierFlags: .command)
    subfolder.click()
    finder.typeKey(.delete, modifierFlags: .command)
    if finder.buttons["Delete"].firstMatch.waitForExistence(timeout: 2) { finder.buttons["Delete"].firstMatch.click() }
    let deleted = try await fixtureResponse("finder/deleted")
    XCTAssertEqual(deleted, "deleted")
    finder.typeKey("w", modifierFlags: .command)
    app.activate()
    app.typeKey("r", modifierFlags: .command)
    let start = app.buttons["Start"].firstMatch
    XCTAssertTrue(start.waitForExistence(timeout: 10))
    start.click()
    XCTAssertTrue(app.descendants(matching: .any)["session-terminal"].firstMatch.waitForExistence(timeout: 15))
    XCTAssertTrue(show.exists)

    let env = ProcessInfo.processInfo.environment
    if let id = env["CLISTREAM_PTY_ID"] {
      app.descendants(matching: .any)["session-" + id].firstMatch.click()
      XCTAssertTrue(show.waitForNonExistence(timeout: 10), "A disabled workspace was offered in Finder")
    }
  }
}
#endif
