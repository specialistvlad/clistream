import XCTest
#if os(macOS)
  import AppKit
#endif

final class NativeSessionTests: XCTestCase {
  @MainActor func testPairPreviewAndSend() async throws {
    let app = try await openPipe()
    func togglePort(_ name: String) {
      let row = app.descendants(matching: .any)["forward-" + name].firstMatch
      XCTAssertTrue(row.waitForExistence(timeout: 15))
      #if os(iOS)
        let control = row.switches.firstMatch
        activate(control.exists ? control : row)
      #else
        activate(row)
      #endif
    }
    func closePorts() {
      #if os(macOS)
        activate(app.descendants(matching: .any)["session-ports"].firstMatch)
        XCTAssertTrue(app.descendants(matching: .any)["forward-tcp"].firstMatch.waitForNonExistence(timeout: 10))
      #else
        let done = app.buttons["dismiss-ports"].firstMatch
        activate(done)
        XCTAssertTrue(done.waitForNonExistence(timeout: 10))
      #endif
    }
    let closedDone = app.buttons["dismiss-ports"].firstMatch
    XCTAssertFalse(closedDone.exists && closedDone.isHittable)
    for proto in ["tcp", "udp"] {
      activate(app.descendants(matching: .any)["session-ports"].firstMatch)
      togglePort(proto)
      let address = app.descendants(matching: .any)["forward-address-" + proto].firstMatch
      guard address.waitForExistence(timeout: 15) else {
        let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
        attachment.name = "ports-inspector-window"
        attachment.lifetime = .keepAlways
        add(attachment)
        XCTFail("Missing local listener address after toggling " + proto)
        return
      }
      #if os(macOS)
        let endpoint = (address.value as? String) ?? address.label
      #else
        let endpoint = address.label
      #endif
      let port = try XCTUnwrap(endpoint.split(separator: ":").last.flatMap { UInt16($0) })
      try await capture("port-forwards", app)
      closePorts()
      try exchange(port: port, datagram: proto == "udp")
      activate(app.descendants(matching: .any)["session-ports"].firstMatch)
      togglePort(proto)
      XCTAssertTrue(address.waitForNonExistence(timeout: 5))
      closePorts()
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
    #if os(macOS)
      try await enterInvitation(invitation, in: app)
    #else
      app.open(try XCTUnwrap(URL(string: invitation)))
    #endif
    let confirm = pairingButton(in: app)
    XCTAssertTrue(confirm.waitForExistence(timeout: 12))
    try await capture("confirm-server", app)
    confirmPairing(confirm, in: app)
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
    captureCurrent("multiple-servers", app)
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
    // Discovery can start its ten-second connection attempt after the next refresh.
    let retry = app.buttons["retry-server"].firstMatch
    XCTAssertTrue(retry.waitForExistence(timeout: 25), "The offline server must offer Retry")
    captureCurrent("server-offline", app)
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
      name.typeKey("a", modifierFlags: .command)
      name.typeText(alias)
      name.typeKey(.return, modifierFlags: [])
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
    try await capture("stop-confirmation", app)
    activate(app.buttons["Stop"].firstMatch)
    #if os(macOS)
      let endpoint = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_ENDPOINT"])
      let header = app.descendants(matching: .any)["Server " + endpoint].firstMatch
      XCTAssertTrue(app.buttons["Start…"].firstMatch.waitForExistence(timeout: 15))
      header.rightClick()
      app.menuItems["Rename…"].firstMatch.click()
      let name = app.textFields["server-name-field"].firstMatch
      XCTAssertTrue(name.waitForExistence(timeout: 10))
      try await capture("rename-server", app)
      name.typeKey(.escape, modifierFlags: [])
    #endif
    action("Start…", app)
    XCTAssertTrue(app.buttons["Start"].firstMatch.waitForExistence(timeout: 12))
    try await capture("start-command", app)
    activate(app.buttons["Start"].firstMatch)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(terminal)
    terminal.typeText("after-start\n")
    expectOutput("out:after-start", in: app)
    action("Restart…", app)
    XCTAssertTrue(app.textFields["command-line"].waitForExistence(timeout: 10))
    activate(app.buttons["Cancel"].firstMatch)
    expectOutput("out:after-start", in: app)
  }

  @MainActor func testEOFAndCompletedOutput() async throws {
    let app = try await openPipe(eof: true)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(terminal)
    // cat echoes through the remote pipe before Return; a line-buffered input form cannot pass.
    terminal.typeText("finished-child")
    expectOutput("finished-child", in: app)
    terminal.typeText("\n")
    #if os(macOS)
      let eof = app.toolbars.buttons["session-send-eof"].firstMatch
      XCTAssertTrue(eof.waitForExistence(timeout: 12))
      XCTAssertTrue(eof.isEnabled)
      eof.click()
    #else
      action("Send EOF", app)
    #endif
    let result = app.descendants(matching: .any).matching(NSPredicate(
      format: "value CONTAINS %@ OR label CONTAINS %@", "finished-child","finished-child"
    )).firstMatch
    XCTAssertTrue(result.waitForExistence(timeout: 15))
    try await capture("completed-session",app)
    action("Start…", app)
    activate(app.buttons["Start"].firstMatch)
    XCTAssertTrue(terminal.waitForExistence(timeout: 15))
  }

  @MainActor func testEditArgumentsAndRestart() async throws {
    let app = try await openPipe()
    action("Restart…", app)
    let field = app.textFields["command-line"].firstMatch
    if !field.waitForExistence(timeout: 12) {
      let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
      attachment.name = "command-review-window"
      attachment.lifetime = .keepAlways
      add(attachment)
      XCTFail("Command editor did not appear")
      return
    }
    activate(field)
    let saved = try XCTUnwrap(field.value as? String)
    XCTAssertFalse(saved.isEmpty)
    try await capture("edit-command", app)
    field.typeKey("a", modifierFlags: .command)
    field.typeText("'")
    XCTAssertFalse(app.buttons["Restart"].firstMatch.isEnabled)
    field.typeKey("a", modifierFlags: .command)
    field.typeText(saved + " 'two words' ")
    field.typeText("\"\"")
    XCTAssertEqual(field.value as? String, saved + " 'two words' \"\"")
    try await capture("restart-confirmation",app)
    activate(app.buttons["Restart"].firstMatch)
    let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
    expectOutput("args=2", in: app)
    expectOutput("first=<two words>", in: app)
    expectOutput("second=<>", in: app)
    activate(terminal)
    terminal.typeText("after-restart\n")
    expectOutput("out:after-restart", in: app)
    try await capture("pipe-session",app)
  }

  @MainActor func expectOutput(
    _ value: String, in app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line
  ) {
    // Restart and layout transitions can replace the native terminal view.
    let shown = app.descendants(matching: .any).matching(NSPredicate(
      format: "identifier == %@ AND (value CONTAINS %@ OR label CONTAINS %@)",
      "session-terminal", value, value
    )).firstMatch
    if !shown.waitForExistence(timeout: 30) {
      let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
      attachment.name = "terminal-output-window"
      attachment.lifetime = .keepAlways
      add(attachment)
      XCTFail("The terminal did not display " + value, file: file, line: line)
    }
  }

  @MainActor private func action(_ title: String, _ app: XCUIApplication) {
    #if os(iOS)
      activate(app.buttons["Session"].firstMatch)
    #endif
    activate(app.buttons[title].firstMatch)
  }

  @MainActor func capture(_ name: String, _ app: XCUIApplication) async throws {
    guard ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" else { return }
    let device = XCUIDevice.shared
    let original = device.appearance
    defer { device.appearance = original }
    for dark in [false, true] {
      #if os(macOS)
        device.appearance = dark ? .dark : .light
      #else
        let mode = dark ? "dark" : "light"
        let applied = try await fixtureResponse("appearance/" + mode)
        XCTAssertEqual(applied, mode)
        // SpringBoard updates the application, keyboard and system sheets on separate frames.
        try await Task.sleep(for: .seconds(1))
      #endif
      captureCurrent(name + (dark ? "-dark" : ""), app)
    }
  }

  @MainActor func testTerminalKeysAndAttachOnly() async throws {
    let app = try await openPipe(terminal: true)
    _ = try await fixtureResponse("RESET")
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    let ready = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "Terminal ready"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 15), .completed)
    try await capture("terminal", app)
    XCTAssertFalse(app.buttons["Restart"].exists)
    #if os(iOS)
      openTerminalKeys(in: app)
      captureCurrent("terminal-keys", app)
      activate(app.buttons["Control Key"])
      captureCurrent("control-keys", app)
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
      expectOutput("out:layout draft", in: app)
      XCUIDevice.shared.appearance = .light
      expectOutput("out:layout draft", in: app)
      func choose(_ title: String) {
        let menu = app.descendants(matching: .any)["workspace-view-options"].firstMatch
        activate(menu)
        let option = app.menuItems[title].firstMatch
        if title == "List" && option.waitForExistence(timeout: 5) { captureCurrent("view-options", app) }
        activate(option)
        XCTAssertTrue(option.waitForNonExistence(timeout: 5))
      }
      for layout in ["List", "Tree"] {
        choose(layout)
        for (group, screenshot) in [("Host", "group-by-host"), ("CLI Name", "group-by-cli"), ("Server", "servers-list")] {
          choose(group)
          XCTAssertTrue(row.waitForExistence(timeout: 10))
          expectOutput("out:layout draft", in: app)
          if layout == "List" {
            captureCurrent(screenshot, app)
          }
        }
      }
      app.typeText("focus-check\n")
      expectOutput("out:focus-check", in: app)
      app.terminate()
      app.launch()
      activate(row)
      XCTAssertTrue(field.waitForExistence(timeout: 12))
    }
    @MainActor func testOpenAndReuseNativeTab() async throws {
      let app = try await openPipe(capturePairing: true)
      XCTAssertTrue(
        app.staticTexts["desktop-session-status"].firstMatch.waitForExistence(timeout: 10))
      activate(app.buttons["Hide Sidebar"].firstMatch)
      try await capture("sidebar-hidden", app)
      activate(app.buttons["Show Sidebar"].firstMatch)
      let input = app.descendants(matching: .any)["session-terminal"].firstMatch
      activate(input)
      input.typeText("kept draft\n")
      let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
      let row = app.descendants(matching: .any)["session-" + pipe].firstMatch
      let pty = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_ID"])
      let other = app.descendants(matching: .any)["session-" + pty].firstMatch
      other.rightClick()
      let newTab = app.menuItems["Open in New Tab"]
      XCTAssertTrue(newTab.waitForExistence(timeout: 5))
      activate(newTab)
      XCTAssertTrue(
        app.descendants(matching: .any)["session-terminal"].firstMatch.waitForExistence(timeout: 12)
      )
      captureCurrent("native-tabs", app)
      other.rightClick()
      activate(app.menuItems["Open in New Window"])
      captureCurrent("separate-window", app)
      activate(row)
      XCTAssertTrue(input.waitForExistence(timeout: 10))
      expectOutput("out:kept draft", in: app)
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
      try await capture("pairing-error", app)
      app.activate()
      app.typeKey(.return, modifierFlags: [])
    #else
      XCTAssertTrue(app.alerts["Couldn’t pair this device"].waitForExistence(timeout: 12))
      captureCurrent("pairing-error", app)
      activate(app.buttons["OK"].firstMatch)
    #endif
    XCTAssertFalse(pairingButton(in: app).exists)
  }

  @MainActor func testRevocationAndRepair() async throws {
    let app = try await openPipe()
    #if os(macOS)
      let last = app.descendants(matching: .any)["session-terminal"].firstMatch
      last.click()
      last.typeText("before-revocation\n")
      expectOutput("out:before-revocation", in: app)
    #endif
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
    try await capture("disconnected", app)
    #if os(macOS)
      expectOutput("out:before-revocation", in: app)
      XCTAssertFalse(app.buttons["Show in Finder"].exists)
      let repair = app.buttons["Pair Again…"].firstMatch
      XCTAssertTrue(repair.exists)
      repair.click()
      let fresh = try await fixtureResponse("QR")
      let link = app.secureTextFields["pairing-link"].firstMatch
      XCTAssertTrue(link.waitForExistence(timeout: 10))
      link.typeText(fresh)
      confirmPairing(pairingButton(in: app), in: app)
      let id = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
      let session = app.descendants(matching: .any)["session-" + id].firstMatch
      XCTAssertTrue(session.waitForExistence(timeout: 12))
      session.click()
      let recovered = app
    #else
      let recovered = try await openPipe()
    #endif
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
    // All viewers stay attached. Wait for each writer's echo before the next one types
    // so keystrokes from separate keyboards cannot interleave within a marker.
    for peer in ["cli", "mac", "phone", "pad"] {
      if peer == participant { input.typeText("shared-" + participant + "\n") }
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
      try await enterInvitation(invitation, in: app)
      confirmPairing(pairingButton(in: app), in: app)
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

  @MainActor func openPipe(
    terminal: Bool = false, eof: Bool = false, capturePairing: Bool = false
  ) async throws
    -> XCUIApplication
  {
    continueAfterFailure = false
    let app = XCUIApplication()
    let id = try XCTUnwrap(
      ProcessInfo.processInfo.environment[
        terminal ? "CLISTREAM_PTY_ID" : (eof ? "CLISTREAM_EOF_ID" : "CLISTREAM_PIPE_ID")])
    let session = app.descendants(matching: .any)["session-" + id].firstMatch
    #if os(macOS)
      app.launchArguments += ["-ApplePersistenceIgnoreState", "YES"]
      if app.state != .notRunning { app.terminate() }
      // A returning operator selects an enrolled child directly from the sidebar.
      if eof {
        app.launch()
        if session.waitForExistence(timeout: 4) {
          session.click()
          return app
        }
      }
    #else
      app.launch()
    #endif
    captureWelcomeIfEmpty(app)
    let link = try await invitation()
    #if os(macOS)
      if app.state == .notRunning { app.launch() }
      try await enterInvitation(link, in: app, captureEmpty: capturePairing)
    #else
      app.open(try XCTUnwrap(URL(string: link)))
    #endif
    let pair = pairingButton(in: app)
    confirmPairing(pair, in: app)
    #if os(macOS)
      XCTAssertTrue(session.waitForExistence(timeout: 15))
      session.click()
    #else
      activate(session)
    #endif
    return app
  }
  private func invitation() async throws -> String { try await fixtureResponse("QR") }
  @MainActor func confirmPairing(_ pair: XCUIElement, in app: XCUIApplication) {
    XCTAssertTrue(pair.waitForExistence(timeout: 12))
    #if os(macOS)
      app.activate()
      XCTAssertTrue(pair.wait(for: \.isHittable, toEqual: true, timeout: 10))
      app.typeKey(.return, modifierFlags: [])
      XCTAssertTrue(pair.waitForNonExistence(timeout: 12))
    #else
      activate(pair)
    #endif
  }
  #if os(macOS)
    @MainActor private func enterInvitation(
      _ link: String, in app: XCUIApplication, captureEmpty: Bool = false
    ) async throws {
      app.typeKey("k", modifierFlags: .command)
      let field = app.secureTextFields.firstMatch
      XCTAssertTrue(field.waitForExistence(timeout: 12))
      if captureEmpty { try await capture("add-server", app) }
      field.typeText(link)
    }

    @MainActor private func clickConfirmation(_ button: XCUIElement, in app: XCUIApplication) {
      if app.state != .runningForeground { app.activate() }
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
        // A sheet can close on mouse-down. Complete the click at the resolved screen coordinate.
        clickConfirmation(element, in: XCUIApplication())
      } else {
        element.click()
      }
    #else
      element.tap()
    #endif
  }
}

extension XCTestCase {
  @MainActor func pairingButton(in app: XCUIApplication) -> XCUIElement {
    #if os(macOS)
      return app.buttons["Pair"].firstMatch
    #else
      return app.buttons["Pair device"].firstMatch
    #endif
  }

  @MainActor private static var inspectedFirstLaunch = false

  @MainActor func captureWelcomeIfEmpty(_ app: XCUIApplication) {
    guard ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1",
      !Self.inspectedFirstLaunch else { return }
    Self.inspectedFirstLaunch = true
    if app.state == .notRunning { app.launch() }
    if app.staticTexts["Add a server to see its CLI apps."].firstMatch.waitForExistence(timeout: 3) {
      #if os(macOS)
        let appearance = XCUIDevice.shared.appearance
        defer { XCUIDevice.shared.appearance = appearance }
        for dark in [false, true] {
          XCUIDevice.shared.appearance = dark ? .dark : .light
          captureCurrent(dark ? "welcome-dark" : "welcome", app)
        }
      #else
        captureCurrent("welcome", app)
      #endif
    }
  }

  #if os(iOS)
    @MainActor func openTerminalKeys(in app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
      let menu = app.buttons["Session"].firstMatch
      XCTAssertTrue(menu.waitForExistence(timeout: 12), file: file, line: line)
      menu.tap()
      let keys = app.buttons["Keys"].firstMatch
      XCTAssertTrue(keys.waitForExistence(timeout: 12), file: file, line: line)
      keys.tap()
    }
  #endif

  @MainActor func captureCurrent(_ name: String, _ app: XCUIApplication) {
    guard ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" else { return }
    #if os(macOS)
      let screenshot = app.windows.firstMatch.screenshot()
    #else
      let screenshot = XCUIScreen.main.screenshot()
    #endif
    let attachment = XCTAttachment(screenshot: screenshot)
    attachment.name = "gallery-" + name
    attachment.lifetime = .keepAlways
    add(attachment)
  }
}
