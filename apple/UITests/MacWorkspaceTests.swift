#if os(macOS)
import AppKit
import XCTest

extension NativeSessionTests {
  @MainActor func testDesktopSearchAndCommands() async throws {
    let app = try await openPipe()
    let env = ProcessInfo.processInfo.environment
    let pipeID = try XCTUnwrap(env["CLISTREAM_PIPE_ID"])
    let searchID = try XCTUnwrap(env["CLISTREAM_SEARCH_ID"])
    let pipe = app.descendants(matching: .any)["session-" + pipeID].firstMatch
    let comparison = app.descendants(matching: .any)["session-" + searchID].firstMatch
    let shellID = try XCTUnwrap(env["CLISTREAM_SHELL_ID"])
    let shell = app.descendants(matching: .any)["session-" + shellID].firstMatch
    XCTAssertTrue(shell.waitForExistence(timeout: 10))
    shell.click()
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    terminal.typeText("echo shell-ready\n")
    expectOutput("shell-ready", in: app)
    try await capture("servers-tree", app)
    let search = app.searchFields.firstMatch
    XCTAssertTrue(search.waitForExistence(timeout: 12))
    search.click()
    search.typeText("co")
    XCTAssertTrue(comparison.waitForExistence(timeout: 10))
    XCTAssertTrue(pipe.waitForNonExistence(timeout: 10))

    let address = try XCTUnwrap(env["CLISTREAM_ENDPOINT"])
    let server = app.descendants(matching: .any)["Server " + address].firstMatch
    try await captureMenu("server-menu", in: app, title: "Rename…") {
      server.rightClick()
    }
    search.click()
    search.typeKey("a", modifierFlags: .command)
    search.typeKey(.delete, modifierFlags: [])
    XCTAssertTrue(pipe.waitForExistence(timeout: 10))
    pipe.click()
    let ports = app.descendants(matching: .any)["session-ports"].firstMatch
    ports.click()
    XCTAssertTrue(app.descendants(matching: .any)["forward-tcp"].firstMatch.waitForExistence(timeout: 10))
    try await captureMenu("open-session-menu", in: app, title: "Show Ports") {
      comparison.rightClick()
    }
    ports.click()

    // A context action uses the clicked CLI even when a different terminal has focus.
    for (action, heading) in [("Restart…", "Restart “comm”"), ("Stop…", "Stop “comm”?")] {
      pipe.click()
      comparison.rightClick()
      try clickMacMenuItem(action, in: app)
      assertMacElement(app.staticTexts[heading].firstMatch, in: app)
      app.typeKey(.escape, modifierFlags: [])
    }
    pipe.click()
    for (menu, item) in [("File", "Paste Invitation"), ("Edit", "Copy"), ("View", "Show Ports"),
                         ("Session", "Restart…"), ("Server", "Remove Server…")] {
      try await captureMenu("menu-" + menu.lowercased(), in: app, title: item) {
        app.menuBarItems[menu].click()
      }
    }

    let oneShotID = try XCTUnwrap(env["CLISTREAM_PTY_ID"])
    let oneShot = app.descendants(matching: .any)["session-" + oneShotID].firstMatch
    oneShot.rightClick()
    try clickMacMenuItem("Open in New Window", in: app)
    let newWindow = app.windows.firstMatch
    XCTAssertTrue(newWindow.staticTexts["Attach only"].firstMatch.waitForExistence(timeout: 10))
    app.menuBarItems["Session"].click()
    XCTAssertFalse(app.menuItems["Restart…"].firstMatch.isEnabled)
    app.typeKey(.escape, modifierFlags: [])
    app.typeKey("w", modifierFlags: .command)
    pipe.click()
    app.typeKey("r", modifierFlags: .command)
    XCTAssertTrue(app.textFields["command-line"].firstMatch.waitForExistence(timeout: 10))
    app.typeKey(.escape, modifierFlags: [])
  }

  @MainActor func testPasteInvitationAndRemoveServer() async throws {
    let app = try await openPipe()
    let env = ProcessInfo.processInfo.environment
    let link = try await fixtureResponse("SECOND")
    let secondID = try await fixtureResponse("SECOND-ID")
    let pasteboard = NSPasteboard.general
    pasteboard.clearContents()
    pasteboard.setString(link, forType: .string)
    defer { pasteboard.clearContents() }
    app.menuBarItems["File"].click()
    try clickMacMenuItem("Paste Invitation", in: app)
    let name = try await fixtureResponse("SECOND-ADDRESS")
    XCTAssertTrue(app.staticTexts[name].firstMatch.waitForExistence(timeout: 10))
    confirmPairing(pairingButton(in: app), in: app)
    pasteboard.clearContents()
    let other = app.descendants(matching: .any)["session-" + secondID].firstMatch
    XCTAssertTrue(other.waitForExistence(timeout: 15))
    let header = app.descendants(matching: .any)["Server " + name].firstMatch
    header.rightClick()
    try clickMacMenuItem("Rename…", in: app)
    let field = app.textFields["server-name-field"].firstMatch
    assertMacElement(field, in: app)
    field.typeKey("a", modifierFlags: .command)
    field.typeText("unsaved-name")
    field.typeKey(.escape, modifierFlags: [])
    XCTAssertTrue(header.waitForExistence(timeout: 10))
    header.rightClick()
    try clickMacMenuItem("Remove Server…", in: app)
    XCTAssertTrue(app.staticTexts["Remove “" + name + "”?"].firstMatch.waitForExistence(timeout: 10))
    try await capture("remove-server", app)
    app.typeKey(.escape, modifierFlags: [])
    XCTAssertTrue(other.exists)
    header.rightClick()
    try clickMacMenuItem("Remove Server…", in: app)
    app.buttons["Remove"].firstMatch.click()
    XCTAssertTrue(other.waitForNonExistence(timeout: 10))
    app.terminate()
    app.launch()
    let pipeID = try XCTUnwrap(env["CLISTREAM_PIPE_ID"])
    let pipe = app.descendants(matching: .any)["session-" + pipeID].firstMatch
    XCTAssertTrue(pipe.waitForExistence(timeout: 15))
    XCTAssertFalse(header.exists)
    pipe.click()
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    terminal.typeText("surviving-profile\n")
    expectOutput("out:surviving-profile", in: app)
    app.menuBarItems["Server"].click()
    try clickMacMenuItem("Remove Server…", in: app)
    app.buttons["Remove"].firstMatch.click()
    XCTAssertTrue(app.buttons["Paste Invitation"].firstMatch.waitForExistence(timeout: 10))
    XCTAssertTrue(pipe.waitForNonExistence(timeout: 10))
    try await capture("welcome", app)
  }

  @MainActor func assertMacElement(_ element: XCUIElement, in app: XCUIApplication,
    function: String = #function, file: StaticString = #filePath, line: UInt = #line) {
    if element.waitForExistence(timeout: 10) { return }
    let attachment = XCTAttachment(screenshot: app.screenshot())
    attachment.name = "failed-desktop-" + function.prefix(while: { $0.isLetter })
    attachment.lifetime = .keepAlways
    add(attachment)
    XCTFail("Expected desktop control", file: file, line: line)
  }

  // macOS also exposes commands from closed main menus. Resolve the item in the
  // currently tracking menu when a row and the menu bar share the same label.
  @MainActor private func waitForMacMenu(_ title: String, in app: XCUIApplication, visible: Bool) -> Bool {
    let items = app.menuItems.matching(identifier: title)
    let matches = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
      items.allElementsBoundByIndex.contains(where: { $0.isHittable }) == visible
    }, object: nil)
    return XCTWaiter.wait(for: [matches], timeout: 6) == .completed
  }

  @MainActor func visibleMacMenuItem(_ title: String, in app: XCUIApplication) throws -> XCUIElement {
    XCTAssertTrue(waitForMacMenu(title, in: app, visible: true), title)
    return try XCTUnwrap(app.menuItems.matching(identifier: title).allElementsBoundByIndex.first(where: { $0.isHittable }))
  }

  @MainActor func clickMacMenuItem(_ title: String, in app: XCUIApplication) throws {
    // Menu tracking can dismiss the AX element at mouse-down. Resolve its screen
    // position once and finish the physical click there.
    let item = try visibleMacMenuItem(title, in: app)
    let point = item.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).screenPoint
    let origin = app.windows.firstMatch.coordinate(withNormalizedOffset: .zero)
    origin.withOffset(CGVector(dx: point.x - origin.screenPoint.x, dy: point.y - origin.screenPoint.y)).click()
  }

  @MainActor private func captureMenu(
    _ name: String, in app: XCUIApplication, title: String, open: () -> Void
  ) async throws {
    let gallery = ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1"
    let device = XCUIDevice.shared
    let original = device.appearance
    defer { if gallery { device.appearance = original } }
    for dark in gallery ? [false, true] : [false] {
      if gallery { device.appearance = dark ? .dark : .light }
      open()
      _ = try visibleMacMenuItem(title, in: app)
      if gallery {
        // A menu lives in its own system window, outside the app window screenshot.
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "gallery-" + name + (dark ? "-dark" : "")
        shot.lifetime = .keepAlways
        add(shot)
      }
      app.typeKey(.escape, modifierFlags: [])
      XCTAssertTrue(waitForMacMenu(title, in: app, visible: false))
    }
  }
}
#endif
