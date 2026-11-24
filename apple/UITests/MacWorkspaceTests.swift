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
    assertMacElement(terminal, in: app)
    terminal.click()
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
    pipe.click()
    comparison.rightClick()
    try clickMacMenuItem("Edit Arguments…", in: app)
    assertMacElement(macText("Edit Arguments for “comm”", in: app), in: app)
    app.typeKey(.escape, modifierFlags: [])
    comparison.rightClick()
    try clickMacMenuItem("Stop", in: app)
    XCTAssertTrue(app.buttons["Start"].firstMatch.waitForExistence(timeout: 12))
    comparison.rightClick()
    try clickMacMenuItem("Start", in: app)
    XCTAssertTrue(app.buttons["Restart"].firstMatch.waitForExistence(timeout: 12))
    XCTAssertTrue(app.descendants(matching: .any)["project-github"].firstMatch.exists)
  }

  @MainActor func testKeyWindowMenuCommands() async throws {
    let app = try await openPipe()
    let env = ProcessInfo.processInfo.environment
    let pipeID = try XCTUnwrap(env["CLISTREAM_PIPE_ID"])
    let pipe = app.descendants(matching: .any)["session-" + pipeID].firstMatch
    let search = app.searchFields.firstMatch
    for (menu, item) in [("File", "Paste Invitation"), ("Edit", "Copy"), ("View", "as Tree"),
                         ("Session", "Restart"), ("Server", "Remove Server…")] {
      if menu == "Edit" {
        search.click()
        search.typeText("sh")
        search.typeKey("a", modifierFlags: .command)
      }
      try await captureMenu("menu-" + menu.lowercased(), in: app, title: item) {
        app.menuBarItems[menu].click()
      }
      if menu == "Edit" {
        search.click()
        search.typeKey("a", modifierFlags: .command)
        search.typeKey(.delete, modifierFlags: [])
      }
    }

    let oneShotID = try XCTUnwrap(env["CLISTREAM_PTY_ID"])
    let oneShot = app.descendants(matching: .any)["session-" + oneShotID].firstMatch
    oneShot.rightClick()
    try clickMacMenuItem("Open in New Window", in: app)
    let newWindow = app.windows.containing(.any, identifier: "session-attach-only").firstMatch
    assertMacElement(newWindow, in: app)
    app.menuBarItems["Session"].click()
    let restart = app.menuBarItems["Session"].menuItems["Restart"].firstMatch
    assertMacElement(restart, in: app)
    if !restart.wait(for: \.isEnabled, toEqual: false, timeout: 6) {
      let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
      shot.name = "failed-desktop-window-commands"
      shot.lifetime = .keepAlways
      add(shot)
      XCTFail("The active attach-only window enabled Restart")
    }
    app.typeKey(.escape, modifierFlags: [])
    app.typeKey("w", modifierFlags: .command)
    pipe.click()
    app.menuBarItems["Session"].click()
    try clickMacMenuItem("Edit Arguments…", in: app)
    XCTAssertTrue(app.textFields["command-line"].firstMatch.waitForExistence(timeout: 10))
    app.typeKey(.escape, modifierFlags: [])
  }

  @MainActor func testPasteInvitationAndRemoveServer() async throws {
    let app = try await openPipe()
    let env = ProcessInfo.processInfo.environment
    let link = try await fixtureResponse("SECOND")
    let secondID = try await fixtureResponse("SECOND-ID")
    try pasteMacInvitation(link, in: app)
    let name = try await fixtureResponse("SECOND-ADDRESS")
    XCTAssertTrue(app.staticTexts[name].firstMatch.waitForExistence(timeout: 10))
    confirmPairing(pairingButton(in: app), in: app)
    let other = app.descendants(matching: .any)["session-" + secondID].firstMatch
    XCTAssertTrue(other.waitForExistence(timeout: 15))
    let header = app.descendants(matching: .any)["Server " + name].firstMatch
    header.rightClick()
    try clickMacMenuItem("Rename…", in: app)
    let field = serverNameEditor(in: app)
    assertMacElement(field, in: app)
    app.typeKey("a", modifierFlags: .command)
    app.typeText("unsaved-name")
    app.typeKey(.escape, modifierFlags: [])
    XCTAssertTrue(header.waitForExistence(timeout: 10))
    header.rightClick()
    try clickMacMenuItem("Remove Server…", in: app)
    assertMacElement(macText("Remove “" + name + "”?", in: app), in: app)
    try await capture("remove-server", app)
    let cancel = app.buttons["Cancel"].firstMatch
    cancel.click()
    XCTAssertTrue(cancel.waitForNonExistence(timeout: 10))
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
    expectOutput("preview-ready", in: app)
    app.typeText("surviving-profile\n")
    expectOutput("out:surviving-profile", in: app)
    app.menuBarItems["Server"].click()
    try clickMacMenuItem("Remove Server…", in: app)
    app.buttons["Remove"].firstMatch.click()
    XCTAssertTrue(app.buttons["Paste Invitation"].firstMatch.waitForExistence(timeout: 10))
    XCTAssertTrue(pipe.waitForNonExistence(timeout: 10))
    try await capture("welcome", app)
  }

  @MainActor private func macText(_ text: String, in app: XCUIApplication) -> XCUIElement {
    app.descendants(matching: .any).matching(NSPredicate(
      format: "label == %@ OR title == %@ OR value == %@", text, text, text)).firstMatch
  }

  @MainActor func serverNameEditor(in app: XCUIApplication) -> XCUIElement {
    app.descendants(matching: .any).matching(NSPredicate(
      format: "identifier == %@ OR label == %@", "server-name-field", "Server name")).firstMatch
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
    if !waitForMacMenu(title, in: app, visible: true) {
      let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
      attachment.name = "failed-desktop-menu-" + title.filter(\.isLetter)
      attachment.lifetime = .keepAlways
      add(attachment)
      XCTFail("The menu did not present " + title)
    }
    return try XCTUnwrap(app.menuItems.matching(identifier: title).allElementsBoundByIndex.first(where: { $0.isHittable }))
  }

  @MainActor func pasteMacInvitation(_ link: String, in app: XCUIApplication) throws {
    let pasteboard = NSPasteboard.general
    pasteboard.clearContents()
    pasteboard.setString(link, forType: .string)
    defer { pasteboard.clearContents() }
    app.menuBarItems["File"].click()
    try clickMacMenuItem("Paste Invitation", in: app)
    XCTAssertTrue(pairingButton(in: app).waitForExistence(timeout: 12))
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
