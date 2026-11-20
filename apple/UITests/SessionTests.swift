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
      let address = app.staticTexts["forward-address-" + port]
      assertText("127.0.0.1:", in: address)
      tap(app.buttons["Stop " + port])
    }
    let input = app.textFields["session-input"]
    #if os(macOS)
      input.typeText("echo\n")
    #else
      tap(input)
      for letter in "echo" { tap(app.keyboards.keys[String(letter)]) }
      tap(app.buttons["Send"])
    #endif
    func output(_ title: String) -> XCUIElement {
      #if os(macOS)
        return app.descendants(matching: .any)["pipe-" + title].firstMatch
      #else
        return app.textViews[title]
      #endif
    }
    assertText("out:echo", in: output("Standard output"))
    assertText("err:echo", in: output("Standard error"))
    #if os(macOS)
      app.typeKey("e", modifierFlags: [.command])
      XCTAssertTrue(app.buttons["Upload"].waitForExistence(timeout: 10))
      app.typeKey(.escape, modifierFlags: [])
      input.typeText("keyboard-return\n")
      app.typeKey("e", modifierFlags: [.command])
    #else
      tap(app.buttons["Files"])
    #endif
    openEntry(name + ".txt", in: app)
    // Save through the system document picker, then re-import the same local
    // file into another remote directory. The fixture checks the bytes.
    #if os(macOS)
      let saved = try XCTUnwrap(env["CLISTREAM_SAVE_DIR"])
      choosePath(saved, in: app)
      tap(app.buttons["OKButton"])
    #else
      let storage = UIDevice.current.userInterfaceIdiom == .pad ? "On My iPad" : "On My iPhone"
      let storageRow = app.cells[storage].firstMatch
      let usedSidebar = storageRow.waitForExistence(timeout: 3) && storageRow.isHittable
      if usedSidebar { storageRow.tap() }
      tap(app.buttons["Save"])
    #endif
    let completion = "Downloaded " + name + ".txt"
    XCTAssertTrue(
      app.staticTexts.matching(
        NSPredicate(format: "label == %@ OR value == %@", completion, completion)
      )
      .firstMatch.waitForExistence(timeout: 12))
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
    #if os(macOS)
      XCTAssertTrue(app.buttons["OKButton"].firstMatch.waitForExistence(timeout: 10))
      app.typeKey(.escape, modifierFlags: [])
    #else
      tap(app.buttons["Cancel"])
    #endif
    tap(app.buttons["Upload"])
    #if os(macOS)
      choosePath(saved + "/" + name + ".txt", in: app)
      tap(app.buttons["OKButton"])
    #else
      let pickerFile = app.descendants(matching: .any).matching(
        NSPredicate(format: "label BEGINSWITH %@", name)
      ).firstMatch
      if usedSidebar {
        tap(storageRow)
      } else if !pickerFile.waitForExistence(timeout: 2) {
        let browse = app.buttons["Browse"].firstMatch
        if browse.waitForExistence(timeout: 4) { browse.tap() }
        if !pickerFile.waitForExistence(timeout: 2) {
          if !storageRow.exists && browse.isHittable { browse.tap() }
          tap(storageRow)
        }
      }
      tap(pickerFile)
      let open = app.buttons["Open"]
      if open.exists { open.tap() }
    #endif
    XCTAssertTrue(
      app.staticTexts.matching(
        NSPredicate(
          format: "label == %@ OR value == %@", "Uploaded " + name + ".txt",
          "Uploaded " + name + ".txt")
      ).firstMatch.waitForExistence(timeout: 15),
      "Upload completion was missing in the restored nested folder")
    #if os(macOS)
      tap(app.buttons["Up"])
    #else
      let browse = app.navigationBars["nested"]
      XCTAssertTrue(browse.waitForExistence(timeout: 8))
      browse.buttons.element(boundBy: 0).tap()
    #endif
    tap(app.buttons["Done"])
    assertText("out:workspace-finished", in: output("Standard output"))
    tap(app.buttons["EOF"])
    XCTAssertTrue(app.staticTexts["Process exited with status 0"].waitForExistence(timeout: 12))
    tap(app.buttons["Detach"])
  }

  @MainActor func testTerminalInput() async throws {
    let app = try await pairedApp(session: "CLISTREAM_UI_PTY")
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    XCTAssertTrue(view.waitForExistence(timeout: 10), app.debugDescription)
    let rendered = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "COPYME terminal"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [rendered], timeout: 10), .completed)
    let origin = view.coordinate(withNormalizedOffset: .zero)
    let cell = origin.withOffset(CGVector(dx: 4, dy: 4))
    #if os(macOS)
      cell.click()
      let window = app.windows.firstMatch
      let corner = window.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 1))
        .withOffset(CGVector(dx: -2, dy: -2))
      corner.click(forDuration: 0.1, thenDragTo: corner.withOffset(CGVector(dx: -96, dy: -48)))
      view.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 4, dy: 4)).click()
    #else
      cell.tap()
      if UIDevice.current.userInterfaceIdiom == .pad {
        let result = try await fixtureResponse("HID")
        XCTAssertEqual(result, "delivered")
      }
      for name in ["Up Arrow", "Down Arrow", "Right Arrow", "Left Arrow", "Escape"] {
        tap(app.buttons["Keys"])
        tap(app.buttons[name])
      }
      tap(app.buttons["Keys"])
      tap(app.buttons["Control Key"])
      tap(app.buttons["Control-C"])
      let portraitWidth = view.frame.width
      XCUIDevice.shared.orientation = .landscapeLeft
      defer { XCUIDevice.shared.orientation = .portrait }
      let resized = XCTNSPredicateExpectation(
        predicate: NSPredicate { _, _ in abs(view.frame.width - portraitWidth) > 1 }, object: view)
      XCTAssertEqual(XCTWaiter.wait(for: [resized], timeout: 15), .completed)
      let rotated = view.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 4, dy: 4))
      rotated.tap()
      if UIDevice.current.userInterfaceIdiom == .pad {
        rotated.click()
      }
    #endif
    tap(app.buttons["Detach"])
  }

  #if os(iOS)
    #if compiler(>=6.4)
      @available(iOS 27.0, *)
      @MainActor func testVoiceOverWorkspaceOrder() async throws {
        let voice = XCUIDevice.shared.voiceOverService
        let initiallyEnabled = voice.isEnabled
        addTeardownBlock { @MainActor in
          if initiallyEnabled && !voice.isEnabled { try voice.enable() }
          if !initiallyEnabled && voice.isEnabled { try voice.disable() }
        }
        if initiallyEnabled { try voice.disable() }
        let app = try await pairedApp(session: "CLISTREAM_SESSION")
        tap(app.buttons["Files"])
        openEntry("inbox", in: app)
        XCTAssertTrue(app.navigationBars["inbox"].waitForExistence(timeout: 8))
        try voice.enable()
        XCTAssertTrue(voice.isEnabled)
        // VoiceOver may enable with no focused element. Start by exploring
        // the folder heading, then use the screen reader's next action.
        app.navigationBars["inbox"].staticTexts["inbox"].firstMatch.tap()
        var readings = [String]()
        if let initial = try? voice.currentSpeech() { readings.append(initial.utterance) }
        for _ in 0..<20 {
          readings.append(try voice.moveForward().utterance)
          if ["nested", "Upload", "Done"].allSatisfy({ label in
            readings.contains { $0.localizedCaseInsensitiveContains(label) }
          }) {
            break
          }
        }
        for label in ["nested", "Upload", "Done"] {
          XCTAssertTrue(
            readings.contains { $0.localizedCaseInsensitiveContains(label) },
            "Missing \(label) in VoiceOver focus order: \(readings)")
        }
      }
    #endif

    @MainActor func testScrollSessionsAndTerminalHistory() async throws {
      let app = try await pairedApp(session: "CLISTREAM_UI_PTY", scrollList: true)
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      let tail = NSPredicate(format: "value CONTAINS %@", "history-end")
      XCTAssertEqual(
        XCTWaiter.wait(
          for: [XCTNSPredicateExpectation(predicate: tail, object: terminal)],
          timeout: 10), .completed)
      terminal.swipeDown()
      let history = NSPredicate(
        format: "value CONTAINS %@ AND NOT (value CONTAINS %@)", "history-", "history-end")
      XCTAssertEqual(
        XCTWaiter.wait(
          for: [XCTNSPredicateExpectation(predicate: history, object: terminal)],
          timeout: 8), .completed)
      tap(app.buttons["Keys"])
      tap(app.buttons["Hide Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 6))
      tap(app.buttons["Keys"])
      tap(app.buttons["Show Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 6))
      tap(app.buttons["Detach"])
    }

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

  @MainActor private func pairedApp(
    session: String, largeText: Bool = false, scrollList: Bool = false
  ) async throws
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
    let invitation = try await fixtureResponse("QR")
    let link = try XCTUnwrap(URL(string: invitation))
    if largeText {
      app.open(link)
      tap(app.buttons["Cancel"])
    }
    #if os(macOS)
      if session == "CLISTREAM_SESSION" {
        app.activate()
        app.typeKey("k", modifierFlags: [.command])
        let field = app.secureTextFields["Pairing link"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 12), "Pairing link field did not appear")
        field.typeText(invitation)
      } else {
        app.open(link)
      }
      app.activate()
      XCTAssertTrue(app.buttons["Pair device"].waitForExistence(timeout: 12))
      app.typeKey(.return, modifierFlags: [])
    #else
      app.open(link)
      tap(app.buttons["Pair device"])
    #endif
    if largeText {
      XCTAssertTrue(
        app.descendants(matching: .any)["session-" + id].firstMatch.waitForExistence(timeout: 10))
      app.open(link)
      tap(app.buttons["Pair device"])
      tap(app.alerts.buttons["OK"])
    }
    let target = app.descendants(matching: .any)["session-" + id].firstMatch
    #if os(macOS)
      XCTAssertTrue(
        target.waitForExistence(timeout: 30), "New server sessions failed to load after pairing")
      app.typeKey("r", modifierFlags: [.command])
    #else
      tap(app.buttons["Refresh"])
    #endif
    #if os(iOS)
      if scrollList {
        let list = app.collectionViews.firstMatch
        let rows = list.buttons.matching(
          NSPredicate(format: "identifier BEGINSWITH %@", "session-"))
        XCTAssertTrue(rows.firstMatch.waitForExistence(timeout: 10))
        func visible() -> Set<String> {
          Set(rows.allElementsBoundByIndex.filter(\.isHittable).map(\.identifier))
        }
        let before = visible()
        list.swipeUp()
        XCTAssertFalse(before.isEmpty)
        XCTAssertNotEqual(before, visible())
        XCTAssertFalse(app.buttons["Files"].exists)
        for _ in 0..<4 where !target.isHittable { list.swipeDown() }
        for _ in 0..<12 where !target.isHittable { list.swipeUp() }
      }
    #endif
    #if os(macOS)
      if session == "CLISTREAM_SESSION" {
        // This fixture advertises one pipe so the first keyboard selection
        // identifies the same child that the independent CLI observes.
        XCTAssertTrue(target.waitForExistence(timeout: 12))
        app.typeKey("1", modifierFlags: [.command])
        app.typeKey(.downArrow, modifierFlags: [])
        XCTAssertTrue(app.textFields["session-input"].waitForExistence(timeout: 10))
      } else {
        tap(target)
      }
    #else
      tap(target)
    #endif
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

  private func assertText(
    _ text: String, in element: XCUIElement, file: StaticString = #filePath,
    line: UInt = #line
  ) {
    XCTAssertTrue(element.waitForExistence(timeout: 10), file: file, line: line)
    let predicate = NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@", text, text)
    let appeared = XCTNSPredicateExpectation(predicate: predicate, object: element)
    XCTAssertEqual(
      XCTWaiter.wait(for: [appeared], timeout: 12), .completed,
      "Missing " + text, file: file, line: line)
  }

  private func tap(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
    #if os(macOS)
      let app = XCUIApplication()
      if app.state != .runningForeground { app.activate() }
    #endif
    XCTAssertTrue(element.waitForExistence(timeout: 12), file: file, line: line)
    #if os(macOS)
      // Sheets and toolbar buttons can disappear as soon as mouse-down fires.
      // Resolve the hit point once so mouse-up can complete on the same window.
      let anchor = app.windows.firstMatch.coordinate(withNormalizedOffset: .zero)
      let target = element.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).screenPoint
      anchor.withOffset(
        CGVector(
          dx: target.x - anchor.screenPoint.x,
          dy: target.y - anchor.screenPoint.y)
      ).click()
    #else
      element.tap()
    #endif
  }
}
