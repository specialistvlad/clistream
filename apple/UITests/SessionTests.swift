import XCTest

#if os(iOS)
  import UIKit
#endif

final class SessionTests: XCTestCase {
  @MainActor func testPairAndUseSession() async throws {
    let env = ProcessInfo.processInfo.environment
    let name = try XCTUnwrap(env["CLISTREAM_FILENAME"])
    let app = try await pairedApp(session: "CLISTREAM_SESSION")
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
    #if os(macOS)
      app.typeKey(.escape, modifierFlags: [])
      app.typeKey("e", modifierFlags: [.command])
    #else
      tap(app.buttons["Done"])
      tap(app.buttons["Files"])
    #endif
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

  @MainActor func testTerminalInput() async throws {
    let app = try await pairedApp(session: "CLISTREAM_UI_PTY")
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
    @MainActor func testTouchNavigationAndEditing() async throws {
      let app = try await pairedApp(session: "CLISTREAM_SESSION", largeText: true)
      let output = app.textViews["Standard output"]
      XCTAssertTrue(output.waitForExistence(timeout: 10))
      let word = output.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 37, dy: 20))
      word.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Copy"].firstMatch)
      let input = app.textFields["session-input"]
      input.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Paste"].firstMatch)
      tap(app.buttons["Send"])

      // Use UIKit's text layout for these coordinates, including Dynamic Type
      // and wrapping in a narrow split view. The OS owns the selection handles.
      word.press(forDuration: 1)
      let geometry = UITextView(frame: CGRect(origin: .zero, size: output.frame.size))
      geometry.font = UIFontMetrics(forTextStyle: .body).scaledFont(
        for: .monospacedSystemFont(ofSize: 17, weight: .regular),
        compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityLarge))
      geometry.text = "copyword selection"
      geometry.layoutIfNeeded()
      func point(after offset: Int, handle: Bool) throws -> XCUICoordinate {
        let pos = try XCTUnwrap(
          geometry.position(from: geometry.beginningOfDocument, offset: offset))
        let caret = geometry.caretRect(for: pos)
        return output.coordinate(withNormalizedOffset: .zero).withOffset(
          CGVector(dx: caret.midX, dy: handle ? caret.maxY + 3 : caret.midY))
      }
      let handle = try point(after: 8, handle: true)
      handle.press(
        forDuration: 0.15, thenDragTo: try point(after: 18, handle: false),
        withVelocity: .slow, thenHoldForDuration: 0.2)
      let copy = app.descendants(matching: .any)["Copy"].firstMatch
      if !copy.waitForExistence(timeout: 2) { word.tap() }
      tap(copy)
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
      try app.performAccessibilityAudit(for: [
        .sufficientElementDescription, .contrast, .dynamicType,
      ])
      tap(app.buttons["Done"])
      tap(app.buttons["Detach"])
    }
  #endif

  @MainActor private func pairedApp(session: String, largeText: Bool = false) async throws
    -> XCUIApplication
  {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let id = try XCTUnwrap(env[session])
    let app = XCUIApplication()
    if largeText {
      app.launchArguments += [
        "-AppleInterfaceStyle", "Dark", "-UIPreferredContentSizeCategoryName",
        "UICTContentSizeCategoryAccessibilityL",
      ]
    }
    app.launch()
    let invitation = try await testInvitation("QR")
    let link = try XCTUnwrap(URL(string: invitation))
    if largeText {
      app.open(link)
      tap(app.buttons["Cancel"])
    }
    app.open(link)
    tap(app.buttons["Pair device"])
    if largeText {
      XCTAssertTrue(
        app.descendants(matching: .any)["session-" + id].firstMatch.waitForExistence(timeout: 10))
      app.open(link)
      tap(app.buttons["Pair device"])
      tap(app.alerts.buttons["OK"])
    }
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
