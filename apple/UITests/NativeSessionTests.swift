import XCTest

final class NativeSessionTests: XCTestCase {
  @MainActor func testPairPreviewAndSend() async throws {
    let app = try await openPipe()
    for proto in ["tcp", "udp"] {
      activate(app.buttons["session-ports"].firstMatch)
      let toggle = app.descendants(matching: .any)["forward-" + proto].firstMatch
      #if os(iOS)
        let control = toggle.switches.firstMatch
        activate(control.exists ? control : toggle)
      #else
        activate(toggle)
      #endif
      let address = app.staticTexts["forward-address-" + proto].firstMatch
      XCTAssertTrue(address.waitForExistence(timeout: 12))
      #if os(macOS)
        let endpoint = (address.value as? String) ?? address.label
      #else
        let endpoint = address.label
      #endif
      let port = try XCTUnwrap(endpoint.split(separator: ":").last.flatMap { UInt16($0) })
      capture("port-forwards", app)
      activate(app.buttons["Done"].firstMatch)
      try exchange(port: port, datagram: proto == "udp")
      activate(app.buttons["session-ports"].firstMatch)
      #if os(iOS)
        activate(control.exists ? control : toggle)
      #else
        activate(toggle)
      #endif
      XCTAssertTrue(address.waitForNonExistence(timeout: 5))
      activate(app.buttons["Done"].firstMatch)
    }
    let field = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(field)
    field.typeText("native-client\n")
    let shown = NSPredicate(
      format: "label CONTAINS %@ OR value CONTAINS %@ OR title CONTAINS %@", "out:native-client",
      "out:native-client", "out:native-client")
    XCTAssertTrue(
      app.descendants(matching: .any).matching(shown).firstMatch.waitForExistence(timeout: 15))
    XCTAssertFalse(app.buttons["Files"].exists)
  }
  @MainActor func testTwoServersRouteInputIndependently() async throws {
    let app = try await openPipe()
    let field = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(field)
    field.typeText("first-server-marker\n")
    let invitation = try await fixtureResponse("SECOND")
    let id = try await fixtureResponse("SECOND-ID")
    app.open(try XCTUnwrap(URL(string: invitation)))
    let confirm = app.buttons["Pair device"].firstMatch
    XCTAssertTrue(confirm.waitForExistence(timeout: 12))
    capture("confirm-server", app)
    activate(confirm)
    #if os(iOS)
      func showServerList(for id: String) {
        let row = app.descendants(matching: .any)["session-" + id].firstMatch
        if row.waitForExistence(timeout: 6) && row.isHittable { return }
        let back = app.navigationBars.descendants(matching: .any)["Back"].firstMatch
        if back.exists && back.isHittable {
          back.tap()
        } else {
          let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.45))
          let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.45))
          start.press(forDuration: 0.1, thenDragTo: end)
        }
      }
      showServerList(for: id)
    #endif
    activate(app.descendants(matching: .any)["session-" + id].firstMatch)
    activate(field)
    field.typeText("second-server-marker\n")
    func output(_ text: String) -> XCUIElement {
      app.descendants(matching: .any).matching(
        NSPredicate(
          format: "value CONTAINS %@ OR label CONTAINS %@", text, text)
      ).firstMatch
    }
    XCTAssertTrue(output("second:second-server-marker").waitForExistence(timeout: 10))
    XCTAssertFalse(output("first-server-marker").exists)
    let offline = try await fixtureResponse("SECOND-OFFLINE")
    XCTAssertEqual(offline, "stopped")
    let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
    #if os(iOS)
      showServerList(for: pipe)
    #endif
    activate(app.descendants(matching: .any)["session-" + pipe].firstMatch)
    activate(field)
    field.typeText("still-connected\n")
    XCTAssertTrue(output("out:still-connected").waitForExistence(timeout: 10))
    XCTAssertFalse(output("second-server-marker").exists)
    #if os(macOS)
      let endpoint = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_ENDPOINT"])
      let server = app.descendants(matching: .any)["Server " + endpoint].firstMatch
      XCTAssertTrue(server.waitForExistence(timeout: 12))
      server.rightClick()
      activate(app.menuItems["Rename…"].firstMatch)
      let name = app.textFields["server-name-field"].firstMatch
      let alias = "Build server " + UUID().uuidString.prefix(8)
      activate(name)
      capture("rename-server", app)
      name.typeKey("a", modifierFlags: .command)
      name.typeText(alias)
      activate(app.buttons["Save"].firstMatch)
      app.terminate()
      app.launch()
      XCTAssertTrue(app.descendants(matching: .any).matching(
        NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@", alias, alias)
      ).firstMatch.waitForExistence(timeout: 12))
      activate(app.descendants(matching: .any)["session-" + pipe].firstMatch)
      XCTAssertTrue(field.waitForExistence(timeout: 12))
    #endif
  }
  @MainActor func testManagedLifecycle() async throws {
    let app = try await openPipe()
    action("Stop…", app)
    XCTAssertTrue(app.buttons["Stop"].firstMatch.waitForExistence(timeout: 10))
    capture("stop-confirmation", app)
    activate(app.buttons["Stop"].firstMatch)
    action("Start…", app)
    XCTAssertTrue(app.buttons["Start"].firstMatch.waitForExistence(timeout: 12))
    capture("start-command", app)
    activate(app.buttons["Start"].firstMatch)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(terminal)
    terminal.typeText("after-start\n")
    expectOutput("out:after-start", in: terminal)
    action("Restart…", app)
    XCTAssertTrue(app.textFields["command-line"].waitForExistence(timeout: 10))
    activate(app.buttons["Cancel"].firstMatch)
    expectOutput("out:after-start", in: terminal)
  }

  @MainActor func testEOFAndCompletedOutput() async throws {
    let app = try await openPipe(eof: true)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(terminal)
    // cat echoes through the remote pipe before Return; a line-buffered input form cannot pass.
    terminal.typeText("finished-child")
    expectOutput("finished-child", in: terminal)
    terminal.typeText("\n")
    action("Send EOF",app)
    let result = app.staticTexts.containing(NSPredicate(
      format: "value CONTAINS %@ OR label CONTAINS %@", "finished-child","finished-child"
    )).firstMatch
    XCTAssertTrue(result.waitForExistence(timeout: 15))
    capture("completed-session",app)
    action("Start…", app)
    activate(app.buttons["Start"].firstMatch)
    XCTAssertTrue(terminal.waitForExistence(timeout: 15))
  }

  @MainActor func testEditArgumentsAndRestart() async throws {
    let app = try await openPipe()
    action("Restart…", app)
    let field = app.textFields["command-line"].firstMatch
    activate(field)
    let saved = try XCTUnwrap(field.value as? String)
    capture("edit-command", app)
    field.typeKey("a", modifierFlags: .command)
    field.typeText("'")
    XCTAssertFalse(app.buttons["Restart"].firstMatch.isEnabled)
    field.typeKey("a", modifierFlags: .command)
    field.typeText(saved + " placeholder 'two words' \"\"")
    capture("restart-confirmation",app)
    activate(app.buttons["Restart"].firstMatch)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    expectOutput("args=2 first=<two words> second=<>", in: terminal)
    activate(terminal)
    terminal.typeText("after-restart\n")
    expectOutput("out:after-restart", in: terminal)
    capture("pipe-session",app)
  }

  @MainActor private func expectOutput(_ value: String, in terminal: XCUIElement) {
    let shown = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value CONTAINS %@", value), object: terminal)
    XCTAssertEqual(XCTWaiter.wait(for: [shown], timeout: 20), .completed)
  }

  @MainActor private func action(_ title: String, _ app: XCUIApplication) {
    #if os(iOS)
      activate(app.buttons["Session"].firstMatch)
    #endif
    activate(app.buttons[title].firstMatch)
  }

  @MainActor private func capture(_ name: String, _ app: XCUIApplication) {
    guard ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" else { return }
    let device = XCUIDevice.shared
    let original = device.appearance
    defer { device.appearance = original }
    for dark in [false, true] {
      device.appearance = dark ? .dark : .light
      #if os(macOS)
        let screenshot = app.windows.firstMatch.screenshot()
      #else
        let screenshot = XCUIScreen.main.screenshot()
      #endif
      let attachment = XCTAttachment(screenshot: screenshot)
      attachment.name = "gallery-" + name + (dark ? "-dark" : "")
      attachment.lifetime = .keepAlways
      add(attachment)
    }
  }

  @MainActor func testTerminalKeysAndAttachOnly() async throws {
    let app = try await openPipe(terminal: true)
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    let ready = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "Terminal ready"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 15), .completed)
    capture("terminal", app)
    XCTAssertFalse(app.buttons["Restart"].exists)
    #if os(iOS)
      activate(app.buttons["Keys"])
      activate(app.buttons["Control Key"])
      activate(app.buttons["Control-C"])
    #else
      view.click()
      view.typeKey("c", modifierFlags: .control)
    #endif
    let received = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "[03]"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [received], timeout: 12), .completed)
  }

  #if os(macOS)
    @MainActor func testWorkspaceGroupingAndLatency() async throws {
      let app = try await openPipe()
      let status = app.staticTexts["desktop-session-status"].firstMatch
      let measured = XCTNSPredicateExpectation(
        predicate: NSPredicate(
          format:
            "value CONTAINS %@ AND value ENDSWITH %@", "Connected", " ms"), object: status)
      XCTAssertEqual(XCTWaiter.wait(for: [measured], timeout: 15), .completed)
      let id = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
      let row = app.descendants(matching: .any)["session-" + id].firstMatch
      let field = app.descendants(matching: .any)["session-terminal"].firstMatch
      activate(field)
      field.typeText("layout draft\n")
      let originalAppearance = XCUIDevice.shared.appearance
      defer { XCUIDevice.shared.appearance = originalAppearance }
      XCUIDevice.shared.appearance = .dark
      expectOutput("out:layout draft", in: field)
      XCUIDevice.shared.appearance = .light
      expectOutput("out:layout draft", in: field)
      func choose(_ title: String) {
        let menu = app.descendants(matching: .any)["workspace-view-options"].firstMatch
        activate(menu)
        let option = app.menuItems[title].firstMatch
        activate(option)
        XCTAssertTrue(option.waitForNonExistence(timeout: 5))
      }
      for layout in ["List", "Tree"] {
        choose(layout)
        for group in ["Host", "CLI Name", "Server"] {
          choose(group)
          XCTAssertTrue(row.waitForExistence(timeout: 10))
          expectOutput("out:layout draft", in: field)
        }
      }
      app.typeText("focus-check\n")
      expectOutput("out:focus-check", in: field)
      app.terminate()
      app.launch()
      activate(row)
      XCTAssertTrue(field.waitForExistence(timeout: 12))
    }
    @MainActor func testOpenAndReuseNativeTab() async throws {
      let app = try await openPipe(pairInWindow: true)
      XCTAssertTrue(
        app.staticTexts["desktop-session-status"].firstMatch.waitForExistence(timeout: 10))
      activate(app.buttons["Hide Sidebar"].firstMatch)
      capture("sidebar-hidden", app)
      activate(app.buttons["Show Sidebar"].firstMatch)
      let input = app.descendants(matching: .any)["session-terminal"].firstMatch
      activate(input)
      input.typeText("kept draft\n")
      let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
      let row = app.descendants(matching: .any)["session-" + pipe].firstMatch
      let pty = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_ID"])
      let other = app.descendants(matching: .any)["session-" + pty].firstMatch
      other.rightClick()
      activate(app.menuItems["Open in New Tab"])
      XCTAssertTrue(
        app.descendants(matching: .any)["session-terminal"].firstMatch.waitForExistence(timeout: 12)
      )
      other.rightClick()
      activate(app.menuItems["Open in New Window"])
      activate(row)
      XCTAssertTrue(input.waitForExistence(timeout: 10))
      expectOutput("out:kept draft", in: input)
    }
  #endif

  @MainActor func testMalformedInvitation() async throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    #if os(macOS)
      app.launchArguments += ["-ApplePersistenceIgnoreState", "YES"]
    #endif
    #if os(macOS)
      if app.state != .notRunning { app.terminate() }
    #else
      app.launch()
    #endif
    app.open(try XCTUnwrap(URL(string: "clistream://invalid.example/?token=invalid")))
    #if os(macOS)
      XCTAssertTrue(
        app.staticTexts["Couldn’t pair this device"].firstMatch.waitForExistence(timeout: 12))
      app.activate()
      app.typeKey(.return, modifierFlags: [])
    #else
      XCTAssertTrue(app.alerts["Couldn’t pair this device"].waitForExistence(timeout: 12))
      activate(app.buttons["OK"].firstMatch)
    #endif
    XCTAssertFalse(app.buttons["Pair device"].exists)
  }

  @MainActor func testRevocationAndRepair() async throws {
    let app = try await openPipe()
    let response = try await fixtureResponse("REVOKE")
    XCTAssertEqual(response, "revoked")
    #if os(iOS)
      activate(app.buttons["Session"].firstMatch)
    #endif
    let reconnect = app.buttons["Reconnect"].firstMatch
    XCTAssertTrue(reconnect.waitForExistence(timeout: 15))
    activate(reconnect)
    let denied = app.descendants(matching: .any).matching(
      NSPredicate(format: "label CONTAINS[c] %@ OR value CONTAINS[c] %@", "revoked", "revoked")
    ).firstMatch
    XCTAssertTrue(denied.waitForExistence(timeout: 15))
    let recovered = try await openPipe()
    let input = recovered.descendants(matching: .any)["session-terminal"].firstMatch
    activate(input)
    input.typeText("access-restored\n")
    XCTAssertTrue(
      recovered.descendants(matching: .any).matching(
        NSPredicate(
          format: "value CONTAINS %@ OR label CONTAINS %@", "out:access-restored",
          "out:access-restored")
      ).firstMatch.waitForExistence(timeout: 12))
  }

  @MainActor func testConcurrentViewers() async throws {
    let participant = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PARTICIPANT"])
    let app = try await openPipe()
    defer { app.terminate() }
    let input = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(input)
    let ready = try await fixtureResponse("ready/" + participant)
    XCTAssertEqual(ready, "ready")
    input.typeText("shared-" + participant + "\n")
    for peer in ["cli", "mac", "phone", "pad"] {
      let text = "out:shared-" + peer
      let match = app.descendants(matching: .any).matching(
        NSPredicate(format: "value CONTAINS %@ OR label CONTAINS %@", text, text)
      ).firstMatch
      XCTAssertTrue(match.waitForExistence(timeout: 300))
    }
    #if os(macOS)
      let pty = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_ID"])
      activate(app.descendants(matching: .any)["session-" + pty].firstMatch)
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      activate(terminal)
      terminal.typeKey("c", modifierFlags: .control)
      let received = XCTNSPredicateExpectation(
        predicate: NSPredicate(format: "value CONTAINS %@", "[03]"), object: terminal)
      XCTAssertEqual(XCTWaiter.wait(for: [received], timeout: 12), .completed)
      let invitation = try await fixtureResponse("SECOND")
      let otherSession = try await fixtureResponse("SECOND-ID")
      app.open(try XCTUnwrap(URL(string: invitation)))
      activate(app.buttons["Pair device"].firstMatch)
      activate(app.descendants(matching: .any)["session-" + otherSession].firstMatch)
      activate(input)
      input.typeText("independent-mac\n")
      XCTAssertTrue(
        app.descendants(matching: .any).matching(
          NSPredicate(format: "value CONTAINS %@ OR label CONTAINS %@",
                      "second:independent-mac", "second:independent-mac")
        ).firstMatch.waitForExistence(timeout: 12))
    #endif
    _ = try await fixtureResponse("switch/" + participant)
    if participant == "phone" {
      activate(input)
      input.typeText("independent-phone\n")
    }
    #if !os(macOS)
      let marker = app.descendants(matching: .any).matching(
        NSPredicate(
          format: "value CONTAINS %@ OR label CONTAINS %@", "out:independent-phone",
          "out:independent-phone")
      ).firstMatch
      XCTAssertTrue(marker.waitForExistence(timeout: 20))
    #endif
    _ = try await fixtureResponse("return/" + participant)
    #if os(macOS)
      let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
      activate(app.descendants(matching: .any)["session-" + pipe].firstMatch)
      XCTAssertTrue(app.descendants(matching: .any)["session-terminal"].firstMatch.waitForExistence(timeout: 12))
      XCTAssertTrue(
        app.descendants(matching: .any).matching(
          NSPredicate(
            format: "value CONTAINS %@ OR label CONTAINS %@", "out:independent-phone",
            "out:independent-phone")
        ).firstMatch.waitForExistence(timeout: 12))
    #endif
  }

  @MainActor private func openPipe(
    terminal: Bool = false, eof: Bool = false, pairInWindow: Bool = false
  ) async throws
    -> XCUIApplication
  {
    continueAfterFailure = false
    let app = XCUIApplication()
    #if os(macOS)
      app.launchArguments += ["-ApplePersistenceIgnoreState", "YES"]
      if app.state != .notRunning { app.terminate() }
    #else
      app.launch()
    #endif
    let link = try await invitation()
    #if os(macOS)
      if pairInWindow {
        app.launch()
        app.typeKey("k", modifierFlags: .command)
        let field = app.secureTextFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 12))
        capture("add-server", app)
        field.typeText(link)
      } else {
        app.open(try XCTUnwrap(URL(string: link)))
      }
    #else
      app.open(try XCTUnwrap(URL(string: link)))
    #endif
    let pair = app.buttons["Pair device"].firstMatch
    XCTAssertTrue(pair.waitForExistence(timeout: 12))
    #if os(macOS)
      clickConfirmation(pair, in: app)
    #else
      activate(pair)
    #endif
    let id = try XCTUnwrap(
      ProcessInfo.processInfo.environment[
        terminal ? "CLISTREAM_PTY_ID" : (eof ? "CLISTREAM_EOF_ID" : "CLISTREAM_PIPE_ID")])
    let session = app.descendants(matching: .any)["session-" + id].firstMatch
    activate(session)
    return app
  }
  private func invitation() async throws -> String { try await fixtureResponse("QR") }
  #if os(macOS)
    @MainActor private func clickConfirmation(_ button: XCUIElement, in app: XCUIApplication) {
      let anchor = app.windows.firstMatch.coordinate(withNormalizedOffset: .zero)
      let point = button.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).screenPoint
      let offset = CGVector(dx: point.x - anchor.screenPoint.x, dy: point.y - anchor.screenPoint.y)
      anchor.withOffset(offset).click()
    }
  #endif

  @MainActor private func activate(
    _ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line
  ) {
    XCTAssertTrue(element.waitForExistence(timeout: 15), file: file, line: line)
    #if os(macOS)
      if element.elementType == .button || element.elementType == .textField {
        // A sheet can close on mouse-down. Complete the click at the resolved window coordinate.
        clickConfirmation(element, in: XCUIApplication())
      } else {
        element.click()
      }
    #else
      element.tap()
    #endif
  }
}
