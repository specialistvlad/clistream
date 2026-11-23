#if os(iOS)
import XCTest

extension NativeSessionTests {
  @MainActor func testFilesWorkspaceRoundTrip() async throws {
    continueAfterFailure = false
    if UIDevice.current.userInterfaceIdiom == .pad {
      let device = XCUIDevice.shared
      let initial = device.orientation
      device.orientation = .landscapeLeft
      addTeardownBlock { @MainActor in if initial != .unknown { device.orientation = initial } }
    }
    let app = try await openPipe()
    func workspacePath(afterEnabling: Bool = false) throws -> [String] {
      let show = app.buttons["session-show-files"].firstMatch
      if !show.isHittable { app.buttons["Session"].firstMatch.tap() }
      XCTAssertTrue(show.waitForExistence(timeout: 10))
      show.tap()
      let alert = app.alerts["Show in Files"].firstMatch
      XCTAssertTrue(alert.waitForExistence(timeout: 20))
      let message = alert.staticTexts.allElementsBoundByIndex.map(\.label).joined(separator: " ")
      if afterEnabling {
        XCTAssertTrue(alert.staticTexts.matching(NSPredicate(
          format: "label BEGINSWITH %@", "Open Files and go to")).firstMatch.exists,
          "Files could not resolve the selected workspace after it was enabled")
      }
      let prefix = try XCTUnwrap(message.range(of: "Open Files and go to Browse → CLIStream → "))
      let path = try XCTUnwrap(message[prefix.upperBound...].components(separatedBy: ". If CLIStream").first)
      let components = path.components(separatedBy: " → ")
      XCTAssertEqual(components.count, 3)
      alert.buttons["OK"].tap()
      return components
    }
    var components = try workspacePath()
    let prepared = try await fixtureResponse("files/prepare")
    XCTAssertEqual(prepared, "ready")
    let files = XCUIApplication(bundleIdentifier: "com.apple.DocumentsApp")
    files.launch()
    addTeardownBlock { @MainActor in
      if self.testRun?.hasSucceeded == false && files.state == .runningForeground {
        if let state = try? await fixtureResponse("files/provider-state") {
          let report = XCTAttachment(string: state)
          report.name = "files-provider-state"
          report.lifetime = .keepAlways
          self.add(report)
        }
        let screen = XCTAttachment(screenshot: files.screenshot())
        screen.name = "files-checkpoint-window"
        screen.lifetime = .keepAlways
        self.add(screen)
      }
      files.terminate()
      app.activate()
    }
    func element(_ title: String) -> XCUIElement {
      files.descendants(matching: .any).matching(NSPredicate(format: "label ==[c] %@", title)).firstMatch
    }
    func tap(_ title: String, file: StaticString = #filePath, line: UInt = #line) {
      let target = element(title)
      XCTAssertTrue(target.wait(for: \.isHittable, toEqual: true, timeout: 12), title, file: file, line: line)
      target.tap()
    }
    func fileItem(_ stem: String) -> XCUIElement {
      files.descendants(matching: .any).matching(NSPredicate(
        format: "label ==[c] %@ OR label BEGINSWITH[c] %@ OR label BEGINSWITH[c] %@",
        stem, stem + ".", stem + ",")).firstMatch
    }
    func more() {
      let menu = files.buttons.matching(NSPredicate(format: "label IN %@", ["More", "More Options", "More Actions"])).firstMatch
      XCTAssertTrue(menu.waitForExistence(timeout: 10))
      menu.tap()
    }
    func checkpoint(_ step: String) async throws {
      let observed = try await fixtureResponse("files/" + step)
      XCTAssertEqual(observed, step)
    }
    if element("Continue").waitForExistence(timeout: 2) { tap("Continue") }
    if element("Browse").isHittable { tap("Browse") }
    if element("Show Sidebar").isHittable { tap("Show Sidebar") }
    if !element("CLIStream").waitForExistence(timeout: 4) {
      if element("Browse").isHittable { tap("Browse") }
      more()
      if element("Edit Sidebar").isHittable { tap("Edit Sidebar") } else { tap("Edit") }
      let toggle = files.switches.matching(NSPredicate(format: "label CONTAINS[c] %@", "CLIStream")).firstMatch
      if toggle.waitForExistence(timeout: 3) {
        if (toggle.value as? String) != "1" { toggle.tap() }
      } else { tap("CLIStream") }
      tap("Done")
    }
    tap("CLIStream")
    // A visible location can still be disabled on first use. Files asks for
    // consent after the user selects it, including in the iPad sidebar.
    let turnOn = files.buttons["Turn On"].firstMatch
    if turnOn.wait(for: \.isHittable, toEqual: true, timeout: 4) {
      turnOn.tap()
      XCTAssertTrue(turnOn.waitForNonExistence(timeout: 10))
      // Enabling a provider grants the app access to the system-assigned path.
      app.activate()
      components = try workspacePath(afterEnabling: true)
      files.activate()
    }
    for part in components {
      let directory = fileItem(part)
      XCTAssertTrue(directory.wait(for: \.isHittable, toEqual: true, timeout: 20), part)
      directory.tap()
    }
    let original = fileItem("CLIStream Marker")
    XCTAssertTrue(original.wait(for: \.isHittable, toEqual: true, timeout: 20))
    original.press(forDuration: 0.8)
    tap("Duplicate")
    let copy = files.descendants(matching: .any).matching(NSPredicate(
      format: "label BEGINSWITH[c] %@ OR label BEGINSWITH[c] %@", "CLIStream Marker 2", "CLIStream Marker copy")).firstMatch
    XCTAssertTrue(copy.wait(for: \.isHittable, toEqual: true, timeout: 20))
    copy.press(forDuration: 0.8)
    if !element("Rotate Left").isHittable { tap("Quick Actions") }
    tap("Rotate Left")
    copy.press(forDuration: 0.8)
    tap("Rename")
    let field = files.textFields.firstMatch
    XCTAssertTrue(field.waitForExistence(timeout: 8))
    field.typeText("CLIStream Edited\n")
    try await checkpoint("renamed")
    more()
    tap("New Folder")
    XCTAssertTrue(field.waitForExistence(timeout: 8))
    field.typeText("CLIStream Folder\n")
    try await checkpoint("folder")
    let edited = fileItem("CLIStream Edited")
    edited.press(forDuration: 0.8)
    tap("Move")
    let folder = fileItem("CLIStream Folder")
    if !folder.waitForExistence(timeout: 3) {
      tap("CLIStream")
      for part in components { fileItem(part).tap() }
    }
    folder.tap()
    tap("Move")
    try await checkpoint("moved")
    folder.tap()
    edited.press(forDuration: 0.8)
    tap("Delete")
    if files.alerts.buttons["Delete"].firstMatch.exists { files.alerts.buttons["Delete"].firstMatch.tap() }
    try await checkpoint("deleted")
  }
}
#endif
