import XCTest

final class SessionTests: XCTestCase {
  func testPairAndUseSession() throws {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let link = try XCTUnwrap(env["CLISTREAM_LINK"].flatMap(URL.init(string:)))
    let id = try XCTUnwrap(env["CLISTREAM_SESSION"])
    let name = try XCTUnwrap(env["CLISTREAM_FILENAME"])
    let app = XCUIApplication()
    app.launch()
    app.open(link)
    tap(app.buttons["Pair device"])
    tap(app.buttons["session-" + id])
    for port in ["tcp", "udp"] {
      tap(app.buttons["Forward " + port])
      XCTAssertTrue(
        app.staticTexts.matching(
          NSPredicate(
            format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "127.0.0.1:", "127.0.0.1:")
        ).firstMatch.waitForExistence(timeout: 8))
      tap(app.buttons["Stop " + port])
    }
    let input = app.textFields["session-input"]
    tap(input)
    input.typeText("apple-client-echo\n")
    for ch in ["out:", "err:"] {
      XCTAssertTrue(
        app.staticTexts.matching(
          NSPredicate(
            format: "label CONTAINS %@ OR value CONTAINS %@", ch + "apple-client-echo",
            ch + "apple-client-echo")
        ).firstMatch.waitForExistence(timeout: 10))
    }
    tap(app.buttons["Files"])
    tap(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", name)).firstMatch)
    // Save the remote file through the system document picker, then re-import it
    // from Recents into another remote directory. The fixture checks the bytes.
    #if os(macOS)
      let saved = try XCTUnwrap(env["CLISTREAM_SAVE_DIR"])
      choosePath(saved, in: app)
      tap(app.buttons["OKButton"])
    #else
      tap(app.buttons["Save"])
    #endif
    tap(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "inbox")).firstMatch)
    tap(app.buttons["Upload"])
    #if os(macOS)
      choosePath(saved + "/" + name + ".txt", in: app)
      tap(app.buttons["OKButton"])
    #else
      let recents = app.buttons["Recents"]
      if recents.waitForExistence(timeout: 3) { recents.tap() }
      let pickerFile = app.descendants(matching: .any).matching(
        NSPredicate(format: "label BEGINSWITH %@", name)
      ).firstMatch
      tap(pickerFile)
      let open = app.buttons["Open"]
      if open.exists { open.tap() }
    #endif
    XCTAssertTrue(
      app.staticTexts.matching(
        NSPredicate(
          format: "label == %@ OR value == %@", "Uploaded " + name + ".txt",
          "Uploaded " + name + ".txt")
      ).firstMatch.waitForExistence(timeout: 15), app.debugDescription)
    tap(app.buttons["Up"])
    tap(app.buttons["Done"])
    tap(app.buttons["Detach"])
    tap(app.buttons["Refresh"])
    XCTAssertTrue(app.buttons["session-" + id].waitForExistence(timeout: 10))
  }

  #if os(macOS)
    private func choosePath(_ path: String, in app: XCUIApplication) {
      app.typeKey("g", modifierFlags: [.command, .shift])
      app.typeText(path)
      app.typeKey(.return, modifierFlags: [])
    }
  #endif

  private func tap(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
    XCTAssertTrue(
      element.waitForExistence(timeout: 12), XCUIApplication().debugDescription, file: file,
      line: line)
    #if os(macOS)
      element.click()
    #else
      element.tap()
    #endif
  }
}
