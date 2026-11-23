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
    terminal.typeText("printf 'shell-ready\n'\n")
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
    try await captureMenu("server-menu", in: app, shown: app.menuItems["Rename…"].firstMatch) {
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
    try await captureMenu("open-session-menu", in: app, shown: app.menuItems["Show Ports"].firstMatch) {
      comparison.rightClick()
    }
    ports.click()

    // A context action uses the clicked CLI even when a different terminal has focus.
    for (action, heading) in [("Restart…", "Restart “comm”"), ("Stop…", "Stop “comm”?")] {
      pipe.click()
      comparison.rightClick()
      let item = app.menuItems[action].firstMatch
      XCTAssertTrue(item.waitForExistence(timeout: 10))
      item.click()
      XCTAssertTrue(app.staticTexts[heading].firstMatch.waitForExistence(timeout: 10))
      app.typeKey(.escape, modifierFlags: [])
    }
    pipe.click()
    for (menu, item) in [("File", "Paste Invitation"), ("Edit", "Copy"), ("View", "Show Ports"),
                         ("Session", "Restart…"), ("Server", "Remove Server…")] {
      try await captureMenu("menu-" + menu.lowercased(), in: app, shown: app.menuItems[item].firstMatch) {
        app.menuBarItems[menu].click()
      }
    }

    let oneShotID = try XCTUnwrap(env["CLISTREAM_PTY_ID"])
    let oneShot = app.descendants(matching: .any)["session-" + oneShotID].firstMatch
    oneShot.rightClick()
    app.menuItems["Open in New Window"].click()
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
    app.menuItems["Paste Invitation"].click()
    let name = try await fixtureResponse("SECOND-ADDRESS")
    XCTAssertTrue(app.staticTexts[name].firstMatch.waitForExistence(timeout: 10))
    confirmPairing(pairingButton(in: app), in: app)
    pasteboard.clearContents()
    let other = app.descendants(matching: .any)["session-" + secondID].firstMatch
    XCTAssertTrue(other.waitForExistence(timeout: 15))
    let header = app.descendants(matching: .any)["Server " + name].firstMatch
    header.rightClick()
    app.menuItems["Rename…"].click()
    let field = app.textFields["server-name-field"].firstMatch
    XCTAssertTrue(field.waitForExistence(timeout: 10))
    field.typeKey("a", modifierFlags: .command)
    field.typeText("unsaved-name")
    field.typeKey(.escape, modifierFlags: [])
    XCTAssertTrue(header.waitForExistence(timeout: 10))
    header.rightClick()
    app.menuItems["Remove Server…"].click()
    XCTAssertTrue(app.staticTexts["Remove “" + name + "”?"].firstMatch.waitForExistence(timeout: 10))
    try await capture("remove-server", app)
    app.typeKey(.escape, modifierFlags: [])
    XCTAssertTrue(other.exists)
    header.rightClick()
    app.menuItems["Remove Server…"].click()
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
  }

  @MainActor private func captureMenu(
    _ name: String, in app: XCUIApplication, shown: XCUIElement, open: () -> Void
  ) async throws {
    let gallery = ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1"
    let device = XCUIDevice.shared
    let original = device.appearance
    defer { if gallery { device.appearance = original } }
    for dark in gallery ? [false, true] : [false] {
      if gallery { device.appearance = dark ? .dark : .light }
      open()
      XCTAssertTrue(shown.waitForExistence(timeout: 10))
      if gallery {
        // A menu lives in its own system window, outside the app window screenshot.
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "gallery-" + name + (dark ? "-dark" : "")
        shot.lifetime = .keepAlways
        add(shot)
      }
      app.typeKey(.escape, modifierFlags: [])
      XCTAssertTrue(shown.waitForNonExistence(timeout: 5))
    }
  }
}
#endif
