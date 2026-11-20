import XCTest

final class SessionTests: XCTestCase {
  func testPairAndUseSession() throws {
    let env = ProcessInfo.processInfo.environment
    let name = try XCTUnwrap(env["CLISTREAM_FILENAME"])
    let app = try pairedApp(session: "CLISTREAM_SESSION")
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
    #if os(macOS)
      input.typeText("echo\n")
    #else
      for letter in "echo" { tap(app.keyboards.keys[String(letter)]) }
      tap(app.buttons["Send"])
    #endif
    for ch in ["out:", "err:"] {
      XCTAssertTrue(
        app.descendants(matching: .any).matching(
          NSPredicate(
            format: "label CONTAINS %@ OR value CONTAINS %@", ch + "echo",
            ch + "echo")
        ).firstMatch.waitForExistence(timeout: 10))
    }
    tap(app.buttons["Files"])
    openEntry(name + ".txt", in: app)
    // Save the remote file through the system document picker, then re-import it
    // from Recents into another remote directory. The fixture checks the bytes.
    #if os(macOS)
      let saved = try XCTUnwrap(env["CLISTREAM_SAVE_DIR"])
      choosePath(saved, in: app)
      tap(app.buttons["OKButton"])
    #else
      tap(app.buttons["Save"])
    #endif
    openEntry("inbox", in: app)
    openEntry("nested", in: app)
    tap(app.buttons["Done"])
    tap(app.buttons["Files"])
    XCTAssertTrue(app.buttons["Upload"].waitForExistence(timeout: 8))
    tap(app.buttons["Upload"])
    tap(app.buttons["Cancel"])
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
    #if os(macOS)
      tap(app.buttons["Up"])
    #else
      let browse = app.navigationBars["nested"]
      XCTAssertTrue(browse.waitForExistence(timeout: 8))
      browse.buttons.element(boundBy: 0).tap()
    #endif
    tap(app.buttons["Done"])
    XCTAssertTrue(
      app.descendants(matching: .any).matching(
        NSPredicate(
          format: "label CONTAINS %@ OR value CONTAINS %@", "out:workspace-finished",
          "out:workspace-finished")
      ).firstMatch.waitForExistence(timeout: 10))
    tap(app.buttons["Detach"])
  }

  func testTerminalInput() throws {
    let app = try pairedApp(session: "CLISTREAM_UI_PTY")
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    XCTAssertTrue(view.waitForExistence(timeout: 10), app.debugDescription)
    let origin = view.coordinate(withNormalizedOffset: .zero)
    let cell = origin.withOffset(CGVector(dx: 4, dy: 4))
    #if os(macOS)
      cell.click()
    #else
      cell.tap()
      tap(app.buttons["Keys"])
      tap(app.buttons["Up Arrow"])
      tap(app.buttons["Keys"])
      tap(app.buttons["Control Key"])
      tap(app.buttons["Control-C"])
      XCUIDevice.shared.orientation = .landscapeLeft
      defer { XCUIDevice.shared.orientation = .portrait }
      view.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 4, dy: 4)).tap()
    #endif
    tap(app.buttons["Detach"])
  }

  #if os(iOS)
    func testTouchNavigationAndEditing() throws {
      let app = try pairedApp(session: "CLISTREAM_SESSION", largeText: true)
      let output = app.textViews["Standard output"]
      XCTAssertTrue(output.waitForExistence(timeout: 10))
      let word = output.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 37, dy: 20))
      word.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Copy"].firstMatch)
      let input = app.textFields["session-input"]
      input.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Paste"].firstMatch)
      tap(app.buttons["Send"])
      tap(app.buttons["Files"])
      openEntry("inbox", in: app)
      openEntry("nested", in: app)
      let list = app.collectionViews.firstMatch
      XCTAssertTrue(list.waitForExistence(timeout: 5))
      let edge = list.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.5))
      edge.press(
        forDuration: 0.05, thenDragTo: edge.withOffset(CGVector(dx: 22, dy: 0)),
        withVelocity: .slow, thenHoldForDuration: 0.5)
      XCTAssertTrue(app.navigationBars["nested"].exists)
      list.swipeUp()
      XCTAssertTrue(app.navigationBars["nested"].exists)
      edge.press(
        forDuration: 0.05,
        thenDragTo: list.coordinate(
          withNormalizedOffset:
            CGVector(dx: 0.85, dy: 0.5)), withVelocity: .fast, thenHoldForDuration: 0)
      XCTAssertTrue(app.navigationBars["inbox"].waitForExistence(timeout: 5))
      try app.performAccessibilityAudit(for: [.sufficientElementDescription])
      tap(app.buttons["Done"])
      tap(app.buttons["Detach"])
    }
  #endif

  private func pairedApp(session: String, largeText: Bool = false) throws -> XCUIApplication {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let link = try XCTUnwrap(env["CLISTREAM_LINK"].flatMap(URL.init(string:)))
    let id = try XCTUnwrap(env[session])
    let app = XCUIApplication()
    if largeText {
      app.launchArguments += [
        "-AppleInterfaceStyle", "Dark", "-UIPreferredContentSizeCategoryName",
        "UICTContentSizeCategoryAccessibilityL",
      ]
    }
    app.launch()
    app.open(link)
    tap(app.buttons["Pair device"])
    tap(app.descendants(matching: .any)["session-" + id].firstMatch)
    return app
  }

  private func openEntry(_ name: String, in app: XCUIApplication) {
    #if os(macOS)
      let entry = app.staticTexts[name].firstMatch
      XCTAssertTrue(entry.waitForExistence(timeout: 10), app.debugDescription)
      entry.doubleClick()
    #else
      tap(app.descendants(matching: .any)["entry-" + name].firstMatch)
    #endif
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
