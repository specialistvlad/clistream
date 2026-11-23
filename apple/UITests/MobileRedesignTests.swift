#if os(iOS)
import UIKit
import XCTest

extension NativeSessionTests {
  @MainActor func testScanAndPasteInvitationReview() async throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launchArguments = ["--ui-test-first-launch"]
    app.launch()
    defer { UIPasteboard.general.items = []; app.terminate() }
    let scan = app.buttons["workspace-scan"].firstMatch
    XCTAssertTrue(scan.waitForExistence(timeout: 12))
    scan.tap()
    XCTAssertTrue(app.staticTexts["Camera Unavailable"].waitForExistence(timeout: 10))
    try await capture("scan-qr", app)
    UIPasteboard.general.string = try await fixtureResponse("QR")
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
    UIPasteboard.general.string = "https://example.invalid/"
    app.launch()
    scan.tap()
    allowPaste(in: app)
    XCTAssertTrue(app.staticTexts["Camera Unavailable"].waitForExistence(timeout: 10))
    XCTAssertFalse(confirm.exists)
    app.buttons["workspace-cancel"].tap()
    UIPasteboard.general.string = try await fixtureResponse("QR")
    scan.tap()
    allowPaste(in: app)
    XCTAssertTrue(confirm.wait(for: \.isEnabled, toEqual: true, timeout: 10))
    XCTAssertTrue(app.staticTexts["pairing-identity"].firstMatch.exists)
    confirm.tap()
    let pipe = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PIPE_ID"])
    XCTAssertTrue(app.descendants(matching: .any)["session-" + pipe].firstMatch.waitForExistence(timeout: 20))
  }

  @MainActor private func allowPaste(in app: XCUIApplication) {
    for surface in [app, XCUIApplication(bundleIdentifier: "com.apple.springboard")] {
      let allow = surface.alerts.buttons["Allow Paste"].firstMatch
      if allow.waitForExistence(timeout: 1) { allow.tap(); return }
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
