#if os(macOS)
import AppKit
import XCTest

extension NativeSessionTests {
  @MainActor private func rememberProviderWindow(_ app: XCUIApplication, name: String) {
    let image = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
    image.name = name + "-window"
    image.lifetime = .keepAlways
    add(image)
    let detail: String
    if name == "finder-open-error" {
      let dialog = [app.alerts.firstMatch, app.dialogs.firstMatch, app.sheets.firstMatch].first { $0.exists }
      detail = dialog?.debugDescription ?? "No error dialog appeared."
    } else {
      detail = app.windows.firstMatch.debugDescription
    }
    let hierarchy = XCTAttachment(string: detail)
    hierarchy.name = name + "-message"
    hierarchy.lifetime = .keepAlways
    add(hierarchy)
  }

  @MainActor private func enableFinderDomain(_ finder: XCUIApplication) throws {
    // Fresh CI accounts must give the same consent as someone installing the app.
    finder.activate()
    finder.typeKey("n", modifierFlags: .command)
    let window = finder.windows.firstMatch
    let name = NSPredicate(format: "label == %@ OR value == %@ OR title == %@",
                           "CLIStream", "CLIStream", "CLIStream")
    let location = window.outlines.descendants(matching: .any).matching(name).firstMatch
    if location.waitForExistence(timeout: 5) {
      location.click()
      let enable = window.buttons["Enable"].firstMatch
      if enable.waitForExistence(timeout: 5) {
        enable.click()
        let confirm = finder.sheets.buttons["Enable"].firstMatch
        if confirm.waitForExistence(timeout: 2) { confirm.click() }
        return
      }
    }
    rememberProviderWindow(finder, name: "finder-locations")

    let settings = XCUIApplication(bundleIdentifier: "com.apple.systempreferences")
    let url = try XCTUnwrap(URL(string: "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"))
    XCTAssertTrue(NSWorkspace.shared.open(url))
    XCTAssertTrue(settings.wait(for: .runningForeground, timeout: 15))
    let pane = settings.windows.firstMatch
    let category = pane.descendants(matching: .any)["By Category"].firstMatch
    if category.waitForExistence(timeout: 3) { category.click() }
    let providers = pane.staticTexts.matching(
      NSPredicate(format: "label == %@ OR value == %@", "File Providers", "File Providers")).firstMatch
    if providers.waitForExistence(timeout: 8) {
      // The category's trailing info button is in the same settings row.
      let rows = pane.descendants(matching: .any).containing(.staticText, identifier: "File Providers")
      let row = rows.allElementsBoundByIndex.reversed().first { $0.buttons.count == 1 }
      if let row { row.buttons.firstMatch.click() } else { providers.doubleClick() }
    }
    let control = pane.descendants(matching: .any).matching(NSPredicate(
      format: "(elementType == %d OR elementType == %d) AND (label CONTAINS[c] %@ OR title CONTAINS[c] %@ OR identifier CONTAINS[c] %@)",
      XCUIElement.ElementType.switch.rawValue, XCUIElement.ElementType.checkBox.rawValue,
      "CLIStream", "CLIStream", "CLIStream")).firstMatch
    guard control.waitForExistence(timeout: 8) else {
      rememberProviderWindow(settings, name: "finder-settings")
      throw NSError(domain: "FinderAcceptance", code: 1, userInfo: [
        NSLocalizedDescriptionKey: "System Settings did not offer CLIStream in File Providers."
      ])
    }
    if control.value as? String != "1" { control.click() }
    let done = pane.buttons["Done"].firstMatch
    if done.exists { done.click() }
  }

  @MainActor func testFinderWorkspaceRoundTrip() async throws {
    let app = try await openPipe()
    let show = app.descendants(matching: .any)["session-show-finder"].firstMatch
    XCTAssertTrue(show.waitForExistence(timeout: 15))
    show.click()
    let finder = XCUIApplication(bundleIdentifier: "com.apple.finder")
    if !finder.wait(for: .runningForeground, timeout: 10) {
      let permission = app.staticTexts.matching(NSPredicate(
        format: "value CONTAINS %@ OR label CONTAINS %@", "File Providers", "File Providers")).firstMatch
      if permission.exists {
        app.buttons["OK"].firstMatch.click()
        try enableFinderDomain(finder)
        app.activate()
        show.click()
      }
    }
    guard finder.wait(for: .runningForeground, timeout: 15) else {
      rememberProviderWindow(app, name: "finder-open-error")
      XCTFail("Finder did not open the workspace. See the window and accessibility attachments.")
      return
    }
    let window = finder.windows.firstMatch
    XCTAssertTrue(window.waitForExistence(timeout: 10))
    func checkpoint(_ name: String, file: StaticString = #filePath, line: UInt = #line) async throws -> String {
      try await fixtureResponse("finder/" + name, file: file, line: line) {
        self.rememberProviderWindow(finder, name: "finder-checkpoint")
      }
    }

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
    let uploaded = try await checkpoint("uploaded")
    XCTAssertEqual(uploaded, "uploaded")
    let source = folder.appendingPathComponent(filename)
    let changed = try await checkpoint("host-edit")
    XCTAssertEqual(changed, "edited")
    app.activate()
    if ProcessInfo.processInfo.environment["CLISTREAM_EXPECT_OFFLINE_SERVER"] == "1" {
      // The Mac-hosted fixture has shut down, while the Linux session stays active.
      XCTAssertTrue(app.buttons["retry-server"].firstMatch.waitForExistence(timeout: 15),
                    "Expected the saved Mac server to be offline during the Linux Finder round trip")
    }
    app.typeKey(".", modifierFlags: .command)
    XCTAssertTrue(app.buttons["Start"].firstMatch.waitForExistence(timeout: 15))
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
    let moved = try await checkpoint("moved")
    XCTAssertEqual(moved, "moved")

    let destination = folder.appendingPathComponent("Finder Round Trip").appendingPathComponent(filename)
    var coordinationError: NSError?
    var save: Result<Void, Error>?
    NSFileCoordinator().coordinate(writingItemAt: destination, options: [], error: &coordinationError) { path in
      save = Result { try Data("saved on the Mac\n".utf8).write(to: path) }
    }
    if let coordinationError { throw coordinationError }
    try XCTUnwrap(save).get()
    let saved = try await checkpoint("saved")
    XCTAssertEqual(saved, "saved")

    file.click()
    finder.typeKey(.delete, modifierFlags: .command)
    if finder.buttons["Delete"].firstMatch.waitForExistence(timeout: 2) { finder.buttons["Delete"].firstMatch.click() }
    XCTAssertTrue(file.waitForNonExistence(timeout: 15))
    finder.typeKey(.upArrow, modifierFlags: .command)
    subfolder.click()
    finder.typeKey(.delete, modifierFlags: .command)
    if finder.buttons["Delete"].firstMatch.waitForExistence(timeout: 2) { finder.buttons["Delete"].firstMatch.click() }
    let deleted = try await checkpoint("deleted")
    XCTAssertEqual(deleted, "deleted")
    finder.typeKey("w", modifierFlags: .command)
    app.activate()
    app.typeKey("r", modifierFlags: .command)
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
