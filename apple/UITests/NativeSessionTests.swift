import XCTest
#if os(macOS)
  import AppKit
#endif

final class NativeSessionTests: XCTestCase {
  #if os(iOS)
    private var tapLocation: XCTSourceCodeLocation?

    override func record(_ issue: XCTIssue) {
      var issue = issue
      if let tapLocation {
        issue.sourceCodeContext = XCTSourceCodeContext(
          callStack: issue.sourceCodeContext.callStack, location: tapLocation)
      }
      super.record(issue)
    }

    @MainActor func testAddServerSheet() async throws {
      continueAfterFailure = false
      let app = XCUIApplication()
      app.launchArguments = ["--ui-test-first-launch"]
      app.launch()
      defer { app.terminate() }
      XCTAssertTrue(app.staticTexts["No Servers"].waitForExistence(timeout: 12))
      try await capture("welcome", app)
      activate(app.buttons["workspace-add-server"].firstMatch)
      let invitation = app.secureTextFields["pairing-link"].firstMatch
      XCTAssertTrue(invitation.waitForExistence(timeout: 12))
      XCTAssertFalse(app.buttons["workspace-confirm"].firstMatch.isEnabled)
      activate(app.buttons["workspace-cancel"].firstMatch)
      XCTAssertTrue(invitation.waitForNonExistence(timeout: 12))
    }
  #endif

  @MainActor func testPairPreviewAndSend() async throws {
    let app = try await openPipe()
    func togglePort(_ name: String) {
      let row = app.descendants(matching: .any)["forward-" + name].firstMatch
      XCTAssertTrue(row.waitForExistence(timeout: 15))
      #if os(iOS)
        let nested = row.switches.firstMatch
        let control = nested.exists ? nested : row
        XCTAssertTrue(control.wait(for: \.isEnabled, toEqual: true, timeout: 12))
        XCTAssertTrue(control.wait(for: \.isHittable, toEqual: true, timeout: 10))
        // UIKit can expose the label and switch as one row-wide accessibility
        // frame. Touch the native switch at the trailing edge of that frame.
        let inset = min(24, control.frame.width / 2)
        control.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 0.5))
          .withOffset(CGVector(dx: -inset, dy: 0)).tap()
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
        XCTAssertTrue(done.wait(for: \.isHittable, toEqual: true, timeout: 10))
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
        let sidebar = app.buttons["Show Sidebar"].firstMatch
        if sidebar.isHittable { sidebar.tap() }
        let add = app.buttons["workspace-add-server"].firstMatch
        if !add.exists || !add.isHittable {
          let back = app.navigationBars.descendants(matching: .any)["Back"].firstMatch
          if back.exists && back.isHittable {
            back.tap()
          } else {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.45))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.45))
            start.press(forDuration: 0.1, thenDragTo: end)
          }
        }
        // An expanded offline server can push the live server below the fold.
        // SwiftUI creates its rows as the real sidebar scrolls into view.
        let list = app.collectionViews.firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 8))
        for _ in 0..<12 {
          if row.exists && row.isHittable { return }
          list.swipeUp()
        }
        XCTAssertTrue(row.exists && row.isHittable, "The server list did not reveal the requested session")
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
      try clickMacMenuItem("Rename…", in: app)
      let name = serverNameEditor(in: app)
      let alias = "Build server " + UUID().uuidString.prefix(8)
      assertMacElement(name, in: app)
      app.typeKey("a", modifierFlags: .command)
      app.typeText(alias)
      app.typeKey(.return, modifierFlags: [])
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
      try clickMacMenuItem("Rename…", in: app)
      let name = serverNameEditor(in: app)
      assertMacElement(name, in: app)
      try await capture("rename-server", app)
      app.typeKey(.escape, modifierFlags: [])
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
    #if os(iOS)
      activate(app.buttons["workspace-cancel"].firstMatch)
    #else
      activate(app.buttons["Cancel"].firstMatch)
    #endif
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
    #if os(iOS)
      expectOutput("preview-ready", in: app)
    #endif
    action("Restart…", app)
    let field = app.textFields["command-line"].firstMatch
    // Opening the review sheet may need a second read after a transport timeout.
    // Surface a rejected read immediately, and keep the UI and output assertions.
    let settled = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
      field.exists || app.alerts["Session action failed"].firstMatch.exists
    }, object: nil)
    let outcome = await XCTWaiter.fulfillment(of: [settled], timeout: 60)
    if outcome != .completed || !field.exists {
      let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
      attachment.name = "command-review-window"
      attachment.lifetime = .keepAlways
      add(attachment)
      XCTFail("Command editor did not appear")
      return
    }
    #if os(macOS)
      activate(field)
    #endif
    let saved = try XCTUnwrap(field.value as? String)
    XCTAssertFalse(saved.isEmpty)
    func expectCommand(_ expected: String, file: StaticString = #filePath, line: UInt = #line) {
      let changed = XCTNSPredicateExpectation(
        predicate: NSPredicate(format: "value == %@", expected), object: field)
      guard XCTWaiter.wait(for: [changed], timeout: 8) != .completed else { return }
      let screen = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
      screen.name = "command-review-window"
      screen.lifetime = .keepAlways
      add(screen)
      XCTAssertEqual(field.value as? String, expected, file: file, line: line)
    }
    try await capture("edit-command", app)
    #if os(iOS)
      XCTAssertTrue(field.wait(for: \.isHittable, toEqual: true, timeout: 10))
      activate(field)
    #endif
    field.typeKey("a", modifierFlags: .command)
    field.typeText("'")
    expectCommand("'")
    XCTAssertTrue(app.buttons["Restart"].firstMatch.wait(for: \.isEnabled, toEqual: false, timeout: 5))
    field.typeKey("a", modifierFlags: .command)
    field.typeText(saved + " 'two words' ")
    field.typeText("\"\"")
    expectCommand(saved + " 'two words' \"\"")
    try await capture("restart-confirmation",app)
    #if os(iOS)
      let confirm = app.buttons["workspace-confirm"].firstMatch
      XCTAssertTrue(app.buttons["workspace-cancel"].firstMatch.wait(for: \.isHittable, toEqual: true, timeout: 10))
      XCTAssertTrue(confirm.wait(for: \.isHittable, toEqual: true, timeout: 10))
      activate(confirm)
    #else
      activate(app.buttons["Restart"].firstMatch)
    #endif
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
    #if os(iOS)
      let output = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
        let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
        return terminal.exists && (terminal.value as? String)?.contains(value) == true
      }, object: nil)
      let displayed = XCTWaiter.wait(for: [output], timeout: 30) == .completed
    #else
      let shown = app.descendants(matching: .any).matching(NSPredicate(
        format: "identifier == %@ AND (value CONTAINS %@ OR label CONTAINS %@)",
        "session-terminal", value, value
      )).firstMatch
      let displayed = shown.waitForExistence(timeout: 30)
    #endif
    if !displayed {
      let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
      attachment.name = "terminal-output-window"
      attachment.lifetime = .keepAlways
      add(attachment)
      XCTFail("The terminal did not display " + value, file: file, line: line)
    }
  }

  @MainActor private func action(_ title: String, _ app: XCUIApplication) {
    #if os(iOS)
      let command = title.replacingOccurrences(of: "…", with: "")
      let identifier = "session-" + command.lowercased().replacingOccurrences(of: " ", with: "-")
      // Regular width places lifecycle commands directly in the toolbar. A
      // presented Session menu can expose both copies to accessibility.
      let toolbarAction = app.navigationBars.buttons[identifier].firstMatch
      if toolbarAction.isHittable {
        toolbarAction.tap()
        return
      }
      let menu = app.buttons["Session"].firstMatch
      guard menu.wait(for: \.isHittable, toEqual: true, timeout: 12) else {
        recordDuoFailure("session-menu", in: app)
        XCTFail("The Session toolbar button was unavailable")
        return
      }
      // Keep the gesture on the active display while UIKit replaces the
      // toolbar button's accessibility element with the presented menu.
      let frame = menu.frame
      let appFrame = app.frame
      let origin = appFrame.origin
      app.coordinate(withNormalizedOffset: .zero)
        .withOffset(CGVector(dx: frame.midX - origin.x, dy: frame.midY - origin.y)).tap()
      let labels = [title, command, command + "..."]
      // Read the enabled state and bounds from the same native menu snapshot.
      // UIKit can replace an action between two AX queries during presentation.
      let button = app.buttons[identifier].firstMatch
      var actionSnapshot = try? button.snapshot()
      if actionSnapshot == nil {
        let titled = app.descendants(matching: .any).matching(
          NSPredicate(format: "label IN %@", labels)
        ).firstMatch
        if titled.waitForExistence(timeout: 12) { actionSnapshot = try? titled.snapshot() }
      }
      guard let actionSnapshot, actionSnapshot.isEnabled else {
        recordDuoFailure("session-action", in: app)
        XCTFail("The Session menu did not present " + title)
        return
      }
      let row = actionSnapshot.frame.intersection(appFrame)
      guard !row.isEmpty else {
        recordDuoFailure("session-action", in: app)
        XCTFail("The requested Session command was outside the active display")
        return
      }
      app.coordinate(withNormalizedOffset: .zero)
        .withOffset(CGVector(dx: row.midX - origin.x, dy: row.midY - origin.y)).tap()
    #else
      activate(app.buttons[title].firstMatch)
    #endif
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
      let variant = name + (dark ? "-dark" : "")
      #if os(iOS)
        if name.hasPrefix("duo-") {
          let recorded = try await fixtureResponse("panel-capture/" + variant)
          XCTAssertEqual(recorded, "captured")
        } else if UIDevice.current.userInterfaceIdiom == .pad,
          ["ipad-main", "ipad-ports", "terminal-key-bar", "session-menu"].contains(name) {
          try await captureTabletTerminal(variant, dark: dark)
        } else { captureCurrent(variant, app) }
      #else
        captureCurrent(variant, app)
      #endif
    }
  }

  #if os(iOS)
    @MainActor private func captureTabletTerminal(_ name: String, dark: Bool) async throws {
      // These boards have a predominantly black or white terminal. SpringBoard
      // can acknowledge the new appearance before the window finishes its fade.
      let deadline = ContinuousClock.now + .seconds(25)
      repeat {
        let image = XCUIScreen.main.screenshot().image
        guard let cgImage = image.cgImage,
          let sample = CGContext(data: nil, width: 1, height: 1, bitsPerComponent: 8,
            bytesPerRow: 1, space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGImageAlphaInfo.none.rawValue),
          let pixel = sample.data
        else { XCTFail("Could not read tablet screenshot"); return }
        sample.interpolationQuality = .high
        sample.draw(cgImage, in: CGRect(x: 0, y: 0, width: 1, height: 1))
        let luminance = pixel.load(as: UInt8.self)
        if dark ? luminance < 70 : luminance > 185 {
          // Bake UIKit’s landscape orientation into the pixels for gallery
          // viewers that ignore the orientation metadata in XCTest’s PNG.
          let format = UIGraphicsImageRendererFormat()
          format.scale = image.scale
          let data = UIGraphicsImageRenderer(size: image.size, format: format).pngData { _ in
            image.draw(in: CGRect(origin: .zero, size: image.size))
          }
          let attachment = XCTAttachment(data: data, uniformTypeIdentifier: "public.png")
          attachment.name = "gallery-" + name
          attachment.lifetime = .keepAlways
          add(attachment)
          return
        }
        try await Task.sleep(for: .milliseconds(500))
      } while ContinuousClock.now < deadline
      XCTFail("The iPad terminal did not finish switching appearance for " + name)
    }
  #endif

  @MainActor func testTerminalKeysAndAttachOnly() async throws {
    let app = try await openPipe(terminal: true)
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    XCTAssertTrue(view.wait(for: \.isHittable, toEqual: true, timeout: 15))
    _ = try await fixtureResponse("RESET")
    let ready = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "Terminal ready"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 15), .completed)
    try await capture("terminal", app)
    XCTAssertFalse(app.buttons["Restart"].exists)
    let previous = try await fixtureResponse("CONTROL-COUNT")
    #if os(iOS)
      openTerminalKeys(in: app)
      let control = app.buttons["Control Key"].firstMatch
      XCTAssertTrue(control.waitForExistence(timeout: 8))
      activate(control)
      XCTAssertEqual(control.value as? String, "On")
      app.typeText("c")
      XCTAssertEqual(control.value as? String, "Off")
    #else
      sendControlC(to: view, in: app)
    #endif
    try await verifyControlC(in: view, previous: previous)
  }

  #if os(macOS)
    @MainActor private func sendControlC(to terminal: XCUIElement, in app: XCUIApplication) {
      terminal.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.3)).click()
      app.typeKey("c", modifierFlags: .control)
    }
  #endif

  @MainActor private func verifyControlC(in terminal: XCUIElement, previous: String) async throws {
    let before = try XCTUnwrap(Int(previous))
    // The child prints each received byte in hex. Ignore grid wraps within
    // a four-character marker, including when a remote viewer resizes the PTY.
    let rendered = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
      (terminal.value as? String)?.replacingOccurrences(of: "\n", with: "").contains("[03]") == true
    }, object: nil)
    let screenEcho = await XCTWaiter.fulfillment(of: [rendered], timeout: 12) == .completed
    let rawCount = try await fixtureResponse("CONTROL-COUNT")
    let after = try XCTUnwrap(Int(rawCount))
    guard after == before + 1 && screenEcho else {
      // This fixture also exercises pointer reporting. Keep only the known
      // terminal evidence: no URLs, arbitrary typed text, or other AX content.
      let screen = terminal.value as? String ?? ""
      let unwrapped = screen.replacingOccurrences(of: "\n", with: "")
      let markers = ["[03]", "[63]", "[1b]"].map { marker in
        marker + "=" + String(unwrapped.components(separatedBy: marker).count - 1)
      }
      let receipt = "bytes before \(before), after \(after); echo \(screenEcho); ready \(screen.contains("Terminal ready")); pointer \(screen.contains("pointer-")); markers \(markers.joined(separator: " "))"
      let evidence = XCTAttachment(string: receipt)
      evidence.name = "control-input-receipt"
      evidence.lifetime = .keepAlways
      add(evidence)
      XCTFail("Control-C delivery or display failed. " + receipt)
      return
    }
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
      func choose(_ title: String) throws {
        let menu = app.descendants(matching: .any)["workspace-view-options"].firstMatch
        activate(menu)
        let option = try visibleMacMenuItem(title, in: app)
        if title == "List" && option.waitForExistence(timeout: 5) { captureCurrent("view-options", app) }
        activate(option)

      }
      for layout in ["List", "Tree"] {
        try choose(layout)
        for (group, screenshot) in [("Host", "group-by-host"), ("CLI Name", "group-by-cli"), ("Server", "servers-list")] {
          try choose(group)
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
      try clickMacMenuItem("Open in New Tab", in: app)
      XCTAssertTrue(
        app.descendants(matching: .any)["session-terminal"].firstMatch.waitForExistence(timeout: 12)
      )
      captureCurrent("native-tabs", app)
      other.rightClick()
      try clickMacMenuItem("Open in New Window", in: app)
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
      XCTAssertTrue(app.alerts["Couldn’t Pair This Device"].waitForExistence(timeout: 12))
      try await capture("pairing-error", app)
      activate(app.buttons["OK"].firstMatch)
    #endif
    XCTAssertFalse(pairingButton(in: app).exists)
  }

  @MainActor func testRevocationAndRepair() async throws {
    let app = try await openPipe()
    let last = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(last)
    last.typeText("before-revocation\n")
    expectOutput("out:before-revocation", in: app)
    #if os(iOS)
      func waitForStatus(_ title: String) -> Bool {
        let match = NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@", title, title)
        let compact = app.descendants(matching: .any).matching(identifier: "desktop-session-status").matching(match).firstMatch
        let toolbar = app.navigationBars.descendants(matching: .any).matching(match).firstMatch
        let shown = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
          compact.exists || toolbar.exists
        }, object: nil)
        if XCTWaiter.wait(for: [shown], timeout: 15) == .completed { return true }
        let screen = XCTAttachment(screenshot: app.screenshot())
        screen.name = "mobile-repair-window"
        screen.lifetime = .keepAlways
        add(screen)
        return false
      }
    #endif
    let response = try await fixtureResponse("REVOKE")
    XCTAssertEqual(response, "revoked")
    #if os(iOS)
      // UIKit snapshots the Session menu when the user opens it. Wait for
      // the live transport to close before looking for its Reconnect action.
      guard waitForStatus("Disconnected") else {
        XCTFail("The terminal stayed connected after access was revoked")
        return
      }
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
      let restored = app.staticTexts["desktop-session-status"].firstMatch
      let connected = XCTNSPredicateExpectation(predicate: NSPredicate(
        format: "value BEGINSWITH %@", "Connected"), object: restored)
      XCTAssertEqual(XCTWaiter.wait(for: [connected], timeout: 15), .completed)
    #else
      let receipt = try await fixtureResponse("open-repair-invitation")
      XCTAssertEqual(receipt, "opened")
      let confirm = pairingButton(in: app)
      let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
      let appName = NSPredicate(
        format: "label CONTAINS[c] %@ OR title CONTAINS[c] %@ OR identifier CONTAINS[c] %@",
        "CLIStream", "CLIStream", "CLIStream")
      let systemOpen = springboard.alerts.buttons["Open"].firstMatch
      let namedAlert = springboard.alerts.matching(appName).buttons["Open"].firstMatch
      let namedContent = springboard.alerts.containing(appName).buttons["Open"].firstMatch
      var grantedConsent = false
      var readyToPair = false
      let end = ContinuousClock.now + .seconds(25)
      repeat {
        // Launch Services and the foreground app present independently. The
        // system consent prompt can appear after the first app snapshot.
        if systemOpen.exists {
          if !grantedConsent,
             let open = [namedAlert, namedContent].first(where: { $0.exists && $0.isHittable }) {
            open.tap()
            grantedConsent = true
          }
        } else if confirm.exists && confirm.isHittable {
          readyToPair = true
          break
        }
        try await Task.sleep(for: .milliseconds(300))
      } while ContinuousClock.now < end
      if !readyToPair {
        let detail = XCTAttachment(string: """
          App foreground: \(app.state == .runningForeground)
          App pairing error: \(app.alerts["Couldn’t Pair This Device"].exists)
          Terminal visible: \(app.descendants(matching: .any)["session-terminal"].firstMatch.exists)
          System alert: \(springboard.alerts.firstMatch.exists)
          System Open action: \(systemOpen.exists)
          App named by system alert: \(namedAlert.exists)
          App named within system alert: \(namedContent.exists)
          Consent granted: \(grantedConsent)
          Pair device visible: \(confirm.exists)
          """)
        detail.name = "mobile-repair-state"
        detail.lifetime = .keepAlways
        add(detail)
        let screen = XCTAttachment(screenshot: app.screenshot())
        screen.name = "mobile-repair-window"
        screen.lifetime = .keepAlways
        add(screen)
        XCTFail("The external repair link did not present the pairing confirmation")
        return
      }
      activate(confirm)
      guard waitForStatus("Connected") else {
        XCTFail("The selected terminal did not reconnect after confirming the new invitation")
        return
      }
    #endif
    let input = app.descendants(matching: .any)["session-terminal"].firstMatch
    activate(input)
    input.typeText("access-restored\n")
    XCTAssertTrue(
      app.descendants(matching: .any).matching(
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
    expectOutput("preview-ready", in: app)
    let ready = try await fixtureResponse("ready/" + participant)
    XCTAssertEqual(ready, "ready")
    // All viewers stay attached. Wait for each writer's echo before the next one types
    // so keystrokes from separate keyboards cannot interleave within a marker.
    for peer in ["cli", "mac", "phone", "pad"] {
      if peer == participant { input.typeText("shared-" + participant + "\n") }
      let text = "out:shared-" + peer
      let match = app.descendants(matching: .any).matching(
        NSPredicate(format: "identifier == %@ AND value CONTAINS %@", "session-terminal", text)
      ).firstMatch
      XCTAssertTrue(match.waitForExistence(timeout: 300))
    }
    #if os(macOS)
      let pty = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_ID"])
      _ = try await fixtureResponse("mac-stage/select-pty")
      activate(app.descendants(matching: .any)["session-" + pty].firstMatch)
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      expectOutput("Terminal ready", in: app)
      _ = try await fixtureResponse("mac-stage/pty-ready")
      let previous = try await fixtureResponse("CONTROL-COUNT")
      sendControlC(to: terminal, in: app)
      try await verifyControlC(in: terminal, previous: previous)
      _ = try await fixtureResponse("mac-stage/control-received")
      let invitation = try await fixtureResponse("SECOND")
      let otherSession = try await fixtureResponse("SECOND-ID")
      _ = try await fixtureResponse("mac-stage/paste-invitation")
      try pasteMacInvitation(invitation, in: app)
      _ = try await fixtureResponse("mac-stage/confirm-second-server")
      confirmPairing(pairingButton(in: app), in: app)
      _ = try await fixtureResponse("mac-stage/select-second-session")
      activate(app.descendants(matching: .any)["session-" + otherSession].firstMatch)
      activate(input)
      input.typeText("independent-mac\n")
      XCTAssertTrue(
        app.descendants(matching: .any).matching(
          NSPredicate(format: "identifier == %@ AND value CONTAINS %@",
                      "session-terminal", "second:independent-mac")
        ).firstMatch.waitForExistence(timeout: 12))
      _ = try await fixtureResponse("mac-stage/second-session-echo")
    #endif
    _ = try await fixtureResponse("switch/" + participant)
    if participant == "phone" {
      activate(input)
      input.typeText("independent-phone\n")
    }
    #if !os(macOS)
      let marker = app.descendants(matching: .any).matching(
        NSPredicate(
          format: "identifier == %@ AND value CONTAINS %@", "session-terminal", "out:independent-phone")
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
            format: "identifier == %@ AND value CONTAINS %@", "session-terminal", "out:independent-phone")
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
    if !terminal && !eof && ProcessInfo.processInfo.environment["CLISTREAM_PARTICIPANT"] == nil {
      let ready = try await fixtureResponse("prepare-pipe")
      XCTAssertEqual(ready, "ready")
    }
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
    #if os(macOS)
      captureWelcomeIfEmpty(app)
    #endif
    let link = try await invitation()
    #if os(macOS)
      if app.state == .notRunning { app.launch() }
      if ProcessInfo.processInfo.environment["CLISTREAM_PARTICIPANT"] != nil {
        try pasteMacInvitation(link, in: app)
      } else {
        try await enterInvitation(link, in: app, captureEmpty: capturePairing)
      }
    #else
      app.open(try XCTUnwrap(URL(string: link)))
    #endif
    let pair = pairingButton(in: app)
    confirmPairing(pair, in: app)
    #if os(macOS)
      XCTAssertTrue(session.waitForExistence(timeout: 15))
      session.click()
    #else
      if !session.waitForExistence(timeout: 4) {
        let sidebar = app.buttons["Show Sidebar"].firstMatch
        if sidebar.isHittable { sidebar.tap() }
      }
      if !session.waitForExistence(timeout: 15) {
        let screen = XCTAttachment(screenshot: app.screenshot())
        screen.name = "mobile-paired-list-window"
        screen.lifetime = .keepAlways
        add(screen)
        XCTFail("The servers list did not show the paired supervisor")
        return app
      }
      activate(session)
    #endif
    return app
  }
  private func invitation() async throws -> String { try await fixtureResponse("QR") }
  @MainActor func confirmPairing(_ pair: XCUIElement, in app: XCUIApplication,
                                 file: StaticString = #filePath, line: UInt = #line) {
    XCTAssertTrue(pair.waitForExistence(timeout: 12), file: file, line: line)
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
      // Report the actual gesture in the shareable XCTest results. Element
      // descriptions may include invitation values and terminal input.
      tapLocation = XCTSourceCodeLocation(filePath: file.description, lineNumber: Int(line))
      defer { tapLocation = nil }
      element.tap()
    #endif
  }
}

extension XCTestCase {
  @MainActor func pairingButton(in app: XCUIApplication) -> XCUIElement {
    #if os(macOS)
      return app.buttons["Pair"].firstMatch
    #else
      return app.buttons["workspace-confirm"].firstMatch
    #endif
  }

  #if os(macOS)
    @MainActor private static var inspectedFirstLaunch = false

    @MainActor func captureWelcomeIfEmpty(_ app: XCUIApplication) {
      guard ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1",
        !Self.inspectedFirstLaunch else { return }
      Self.inspectedFirstLaunch = true
      if app.state == .notRunning { app.launch() }
      if app.staticTexts["Add a server to see its CLI apps."].firstMatch.waitForExistence(timeout: 3) {
        let appearance = XCUIDevice.shared.appearance
        defer { XCUIDevice.shared.appearance = appearance }
        for dark in [false, true] {
          XCUIDevice.shared.appearance = dark ? .dark : .light
          captureCurrent(dark ? "welcome-dark" : "welcome", app)
        }
      }
    }
  #endif

  #if os(iOS)
    @MainActor func openTerminalKeys(in app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
      if !app.keyboards.firstMatch.exists {
        let show = app.buttons["Show Keyboard"].firstMatch
        XCTAssertTrue(show.waitForExistence(timeout: 12), file: file, line: line)
        show.tap()
      }
      XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 12), file: file, line: line)
    }

    @MainActor func touchTerminalKey(_ name: String, in app: XCUIApplication, file: StaticString = #filePath,
                                     line: UInt = #line) {
      if name == "Show Keyboard" { openTerminalKeys(in: app, file: file, line: line); return }
      openTerminalKeys(in: app, file: file, line: line)
      let strip = app.scrollViews["terminal-key-strip"].firstMatch
      let key = app.buttons[name == "Control-C" ? "Control Key" : name].firstMatch
      if !key.isHittable && strip.exists {
        strip.swipeRight()
        for _ in 0..<4 where !key.isHittable { strip.swipeLeft() }
      }
      XCTAssertTrue(key.wait(for: \.isHittable, toEqual: true, timeout: 8), file: file, line: line)
      key.tap()
      if name == "Control-C" { app.typeText("c") }
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
