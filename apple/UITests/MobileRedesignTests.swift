#if os(iOS)
import UIKit
import XCTest

extension NativeSessionTests {
  @MainActor func testGroupAndRemoveServer() async throws {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let id = try XCTUnwrap(env["CLISTREAM_PIPE_ID"])
    let address = try XCTUnwrap(env["CLISTREAM_ENDPOINT"])
    let app = XCUIApplication()
    app.launchArguments = ["--ui-test-first-launch"]
    app.launch()
    defer { app.terminate() }
    let invitation = try await fixtureResponse("QR")
    app.open(try XCTUnwrap(URL(string: invitation)))
    confirmPairing(pairingButton(in: app), in: app)
    let row = app.descendants(matching: .any)["session-" + id].firstMatch
    XCTAssertTrue(row.waitForExistence(timeout: 15))
    let options = app.buttons["workspace-view-options"].firstMatch
    func choose(_ option: String, from picker: String) {
      options.tap()
      let choice = app.buttons[option].firstMatch
      if !choice.isHittable && app.buttons[picker].firstMatch.isHittable {
        app.buttons[picker].firstMatch.tap()
      }
      XCTAssertTrue(choice.wait(for: \.isHittable, toEqual: true, timeout: 5), option)
      choice.tap()
      XCTAssertTrue(row.waitForExistence(timeout: 8))
    }
    for layout in ["List", "Tree"] {
      choose(layout, from: "Layout")
      for group in ["Host", "CLI Name", "Server"] {
        choose(group, from: "Group by")
        let prefix = group == "Server" ? "workspace-host-" : "workspace-group-" + group + "-"
        let heading = app.descendants(matching: .any).matching(
          NSPredicate(format: "identifier BEGINSWITH %@", prefix)).firstMatch
        if group == "Server" && layout == "List" {
          XCTAssertTrue(heading.waitForNonExistence(timeout: 5))
        } else {
          XCTAssertTrue(heading.waitForExistence(timeout: 5))
        }
      }
    }
    let header = app.descendants(matching: .any).matching(
      NSPredicate(format: "label CONTAINS %@", "Server " + address)).firstMatch
    let remove = app.buttons["Remove Server…"].firstMatch
    let alert = app.alerts["Remove “" + address + "”?"].firstMatch
    for confirmation in ["Cancel", "Remove"] {
      XCTAssertTrue(header.wait(for: \.isHittable, toEqual: true, timeout: 10))
      header.press(forDuration: 0.8)
      XCTAssertTrue(remove.waitForExistence(timeout: 5))
      remove.tap()
      XCTAssertTrue(alert.waitForExistence(timeout: 5))
      XCTAssertTrue(alert.staticTexts["This device forgets the server and its pairing. The device stays enrolled until the server operator revokes it."].exists)
      alert.buttons[confirmation].tap()
      if confirmation == "Cancel" { XCTAssertTrue(row.exists) }
    }
    XCTAssertTrue(row.waitForNonExistence(timeout: 8))
    XCTAssertTrue(app.staticTexts["No Servers"].firstMatch.exists)
  }

  @MainActor func testScanAndPasteInvitationReview() async throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launchArguments = ["--ui-test-first-launch"]
    app.launch()
    addTeardownBlock { @MainActor in
      if self.testRun?.hasSucceeded == false && app.state == .runningForeground {
        let screen = XCTAttachment(screenshot: app.screenshot())
        screen.name = "mobile-invitation-window"
        screen.lifetime = .keepAlways
        self.add(screen)
      }
      _ = try await fixtureResponse("pasteboard/clear")
      app.terminate()
    }
    addUIInterruptionMonitor(withDescription: "Paste from the simulator clipboard") { alert in
      let allow = alert.buttons["Allow Paste"].firstMatch
      guard allow.exists else { return false }
      allow.tap()
      return true
    }
    func putOnClipboard(_ source: String) async throws {
      let result = try await fixtureResponse("pasteboard/" + source)
      XCTAssertEqual(result, "ready")
    }
    let scan = app.buttons["workspace-scan"].firstMatch
    XCTAssertTrue(scan.waitForExistence(timeout: 12))
    scan.tap()
    XCTAssertTrue(app.staticTexts["Camera Unavailable"].waitForExistence(timeout: 10))
    try await capture("scan-qr", app)
    try await putOnClipboard("invitation")
    app.buttons["Paste Invitation"].firstMatch.tap()
    allowPaste(in: app)
    let confirm = pairingButton(in: app)
    XCTAssertTrue(confirm.wait(for: \.isEnabled, toEqual: true, timeout: 12))
    XCTAssertTrue(app.staticTexts["pairing-identity"].firstMatch.exists)
    try await capture("add-server", app)
    app.buttons["workspace-cancel"].tap()
    XCTAssertTrue(app.staticTexts["No Servers"].waitForExistence(timeout: 10))

    app.terminate()
    app.launchArguments += ["--ui-test-camera-denied"]
    app.launch()
    scan.tap()
    XCTAssertTrue(app.staticTexts["Allow Camera Access"].waitForExistence(timeout: 10))
    XCTAssertTrue(app.buttons["Open Settings"].exists)

    app.terminate()
    app.launchArguments = ["--ui-test-first-launch", "--ui-test-scanned-code"]
    try await putOnClipboard("invalid")
    app.launch()
    scan.tap()
    allowPaste(in: app)
    XCTAssertTrue(app.staticTexts["Camera Unavailable"].waitForExistence(timeout: 10))
    XCTAssertFalse(confirm.exists)
    app.buttons["workspace-cancel"].tap()
    try await putOnClipboard("invitation")
    scan.tap()
    allowPaste(in: app)
    XCTAssertTrue(confirm.wait(for: \.isEnabled, toEqual: true, timeout: 10))
    XCTAssertTrue(app.staticTexts["pairing-identity"].firstMatch.exists)
    confirm.tap()
    let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
    XCTAssertTrue(app.descendants(matching: .any)["session-" + pipe].firstMatch.waitForExistence(timeout: 20))
  }

  @MainActor func testAdaptiveTerminalToolbarAndInspector() async throws {
    continueAfterFailure = false
    let duo = !(ProcessInfo.processInfo.environment["CLISTREAM_DUO_NAME"] ?? "").isEmpty
    let device = XCUIDevice.shared
    let initialOrientation = device.orientation
    if UIDevice.current.userInterfaceIdiom == .pad { device.orientation = .landscapeLeft }
    defer { if !duo && initialOrientation != .unknown { device.orientation = initialOrientation } }
    if duo {
      _ = try await fixtureResponse("pose/Closed")
      addTeardownBlock { _ = try await fixtureResponse("pose/Closed") }
    }
    let app = try await openPipe()
    expectOutput("preview-ready", in: app)
    openTerminalKeys(in: app)
    try await capture("terminal-key-bar", app)
    touchTerminalKey("Hide Keyboard", in: app)
    let session = app.buttons["Session"].firstMatch
    XCTAssertTrue(session.waitForExistence(timeout: 10))
    if duo {
      XCTAssertTrue(app.descendants(matching: .any)["duo-terminal-tools"].firstMatch.exists)
      try await capture("duo-outer-terminal", app)
    }
    session.tap()
    XCTAssertTrue(app.buttons["Detach"].firstMatch.waitForExistence(timeout: 8))
    try await capture("session-menu", app)
    // Choosing Ports also verifies the action in the presented Session menu.
    app.buttons.matching(NSPredicate(format: "label == 'Ports' AND identifier != 'session-ports'")).firstMatch.tap()
    XCTAssertTrue(app.descendants(matching: .any)["forward-tcp"].firstMatch.waitForExistence(timeout: 12))
    app.buttons["dismiss-ports"].firstMatch.tap()
    if duo {
      try await moveDuo(to: "Partially Open", scenario: "fold", in: app)
      try await capture("duo-partially-open", app)
      try await moveDuo(to: "Open", scenario: "fold", in: app)
    }
    if duo || UIDevice.current.userInterfaceIdiom == .pad {
      let status = app.descendants(matching: .any).matching(NSPredicate(
        format: "label CONTAINS %@ AND label CONTAINS %@", "Connected", "Pipe")).firstMatch
      XCTAssertTrue(status.waitForExistence(timeout: 15))
      XCTAssertFalse(app.descendants(matching: .any)["desktop-session-status"].exists)
      try await capture(duo ? "duo-inner" : "ipad-main", app)
      app.buttons["session-ports"].firstMatch.tap()
      XCTAssertTrue(app.staticTexts["Forwards keep running when you close this inspector."].waitForExistence(timeout: 12))
      try await capture(duo ? "duo-inner-ports" : "ipad-ports", app)
    }
  }

  @MainActor private func allowPaste(in app: XCUIApplication) {
    for surface in [app, XCUIApplication(bundleIdentifier: "com.apple.springboard")] {
      let allow = surface.alerts.buttons["Allow Paste"].firstMatch
      if allow.waitForExistence(timeout: 4) { allow.tap(); return }
    }
  }

  @MainActor func testSearchAndSwipeLifecycle() async throws {
    continueAfterFailure = false
    _ = try await fixtureResponse("prepare-pipe")
    let env = ProcessInfo.processInfo.environment
    let pipe = try XCTUnwrap(env["CLISTREAM_PIPE_ID"])
    let other = try XCTUnwrap(env["CLISTREAM_PTY_ID"])
    let app = XCUIApplication()
    app.launch()
    let link = try await fixtureResponse("QR")
    app.open(try XCTUnwrap(URL(string: link)))
    confirmPairing(pairingButton(in: app), in: app)
    let row = app.descendants(matching: .any)["session-" + pipe].firstMatch
    let oneShot = app.descendants(matching: .any)["session-" + other].firstMatch
    XCTAssertTrue(row.waitForExistence(timeout: 15))
    try await capture("servers-tree", app)
    if !(env["CLISTREAM_DUO_NAME"] ?? "").isEmpty {
      XCTAssertTrue(app.descendants(matching: .any)["duo-server-tools"].firstMatch.exists)
      try await capture("duo-outer-list", app)
    }
    let search = app.searchFields.firstMatch
    if !search.isHittable && app.buttons["Search"].firstMatch.exists { app.buttons["Search"].firstMatch.tap() }
    XCTAssertTrue(search.waitForExistence(timeout: 8))
    search.tap()
    search.typeText("no-such-cli-for-search")
    XCTAssertTrue(row.waitForNonExistence(timeout: 8))
    let clear = search.buttons.firstMatch
    XCTAssertTrue(clear.exists)
    clear.tap()
    search.typeText("sh")
    XCTAssertTrue(row.waitForExistence(timeout: 8))
    XCTAssertFalse(oneShot.exists)
    search.typeText("\n")
    row.swipeLeft()
    let stop = app.buttons["Stop"].firstMatch
    XCTAssertTrue(stop.waitForExistence(timeout: 5))
    try await capture("swipe-actions", app)
    stop.tap()
    let stopAlert = app.alerts.firstMatch
    XCTAssertTrue(stopAlert.label.hasPrefix("Stop “"))
    stopAlert.buttons["Stop"].tap()
    XCTAssertTrue(row.waitForExistence(timeout: 12))
    row.swipeLeft()
    let start = app.buttons["Start"].firstMatch
    XCTAssertTrue(start.waitForExistence(timeout: 10))
    start.tap()
    let confirm = app.buttons["workspace-confirm"].firstMatch
    XCTAssertTrue(confirm.wait(for: \.isEnabled, toEqual: true, timeout: 15))
    confirm.tap()
    XCTAssertTrue(confirm.waitForNonExistence(timeout: 10))
    row.swipeLeft()
    app.buttons["Restart"].firstMatch.tap()
    XCTAssertTrue(app.textFields["command-line"].waitForExistence(timeout: 12))
    confirm.tap()
    XCTAssertTrue(confirm.waitForNonExistence(timeout: 10))
    row.tap()
    expectOutput("preview-ready", in: app)
  }
}
#endif
