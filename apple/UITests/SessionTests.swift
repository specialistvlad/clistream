import XCTest

#if os(iOS)
  import UIKit
#endif

final class SessionTests: XCTestCase {
  #if os(iOS)
    @MainActor func testExpiredPairingLink() async throws {
      continueAfterFailure = false
      let app = XCUIApplication()
      app.launch()
      let link = try await fixtureResponse("EXPIRED")
      let url = try XCTUnwrap(URL(string: link))
      app.open(url)
      tap(app.buttons["Pair device"])
      let alert = app.alerts["Couldn’t pair this device"]
      XCTAssertTrue(alert.waitForExistence(timeout: 10))
      XCTAssertTrue(
        alert.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "fresh")).firstMatch
          .exists)
    }

    @MainActor func testSignedPairingLink() async throws {
      continueAfterFailure = false
      let env = ProcessInfo.processInfo.environment
      let address = try XCTUnwrap(env["CLISTREAM_ENDPOINT"])
      let session = try XCTUnwrap(env["CLISTREAM_SESSION"])
      let app = XCUIApplication()
      app.launch()
      let invitation = try await fixtureResponse("QR")
      XCTAssertTrue(invitation.hasPrefix("clistream://"))
      let url = try XCTUnwrap(URL(string: invitation))
      app.open(url)
      XCTAssertTrue(app.staticTexts[address].waitForExistence(timeout: 10))
      tap(app.buttons["Cancel"])
      tap(app.buttons["Add Server"])
      let field = app.secureTextFields["Pairing link"]
      XCTAssertTrue(field.waitForExistence(timeout: 10))
      captureCurrent("add-server", app)
      field.tap()
      field.typeText(invitation)
      XCTAssertTrue(app.staticTexts[address].waitForExistence(timeout: 10))
      tap(app.buttons["Pair device"])
      XCTAssertTrue(
        app.descendants(matching: .any)["session-" + session].firstMatch.waitForExistence(
          timeout: 12))
      captureCurrent("servers-tree", app)
      app.open(url)
      tap(app.buttons["Pair device"])
      XCTAssertTrue(
        app.descendants(matching: .any)["session-" + session].firstMatch.waitForExistence(
          timeout: 12))
    }
  #endif

  @MainActor func testTerminalInput() async throws {
    #if os(iOS)
      let initialOrientation = XCUIDevice.shared.orientation
      let duo = !(ProcessInfo.processInfo.environment["CLISTREAM_DUO_NAME"] ?? "").isEmpty
      defer {
        if !duo && initialOrientation != .unknown && XCUIDevice.shared.orientation != initialOrientation {
          XCUIDevice.shared.orientation = initialOrientation
        }
      }
    #endif
    _ = try await fixtureResponse("RESET")
    let app = try await pairedApp(session: "CLISTREAM_UI_PTY")
    let view = app.descendants(matching: .any)["session-terminal"].firstMatch
    XCTAssertTrue(view.waitForExistence(timeout: 10), app.debugDescription)
    let rendered = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "value CONTAINS %@", "COPYME terminal"), object: view)
    XCTAssertEqual(XCTWaiter.wait(for: [rendered], timeout: 10), .completed)
    let cell = view.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4))
    #if os(macOS)
      cell.click()
      let window = app.windows.firstMatch
      let corner = window.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 1))
        .withOffset(CGVector(dx: -2, dy: -2))
      corner.click(forDuration: 0.1, thenDragTo: corner.withOffset(CGVector(dx: -96, dy: -48)))
      view.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 4, dy: 4)).click()
    #else
      try await verifyPointer { cell.tap() }
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
      // A windowed iPad can keep its horizontal size class through rotation.
      // Start with the software keyboard up and measure both grid dimensions.
      tap(app.buttons["Keys"])
      tap(app.buttons["Show Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 10))
      let portrait = view.frame.size
      let firstGrid = try await fixtureResponse("GRID")
      if duo {
        let opened = try await fixtureResponse("pose/Open")
        XCTAssertEqual(opened, "applied")
      } else {
        XCUIDevice.shared.orientation = .landscapeLeft
      }
      tap(app.buttons["Keys"])
      tap(app.buttons["Hide Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 10))
      let resized = XCTNSPredicateExpectation(
        predicate: NSPredicate { _, _ in
          let size = view.frame.size
          return abs(size.width - portrait.width) > 1 || abs(size.height - portrait.height) > 1
        }, object: view)
      XCTAssertEqual(XCTWaiter.wait(for: [resized], timeout: 15), .completed)
      let resizedGrid = try await fixtureResponse("RESIZED")
      XCTAssertNotEqual(firstGrid, resizedGrid, "The child PTY kept its original grid")
      let rotated = view.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4))
      try await verifyPointer { rotated.tap() }
      if UIDevice.current.userInterfaceIdiom == .pad {
        try await verifyPointer { rotated.click() }
      }
      if duo {
        let closed = try await fixtureResponse("pose/Closed")
        XCTAssertEqual(closed, "applied")
      }
    #endif
    #if os(macOS)
      app.typeKey("w", modifierFlags: .command)
    #else
      tap(app.buttons["Session"])
      tap(app.buttons["Detach"])
    #endif
  }

  #if os(iOS)
    #if compiler(>=6.4)
      @available(iOS 27.0, *)
      @MainActor func testVoiceOverSessionControls() async throws {
        let voice = XCUIDevice.shared.voiceOverService
        let initiallyEnabled = voice.isEnabled
        addTeardownBlock { @MainActor in
          if initiallyEnabled && !voice.isEnabled { try voice.enable() }
          if !initiallyEnabled && voice.isEnabled { try voice.disable() }
        }
        if initiallyEnabled { try voice.disable() }
        let app = try await pairedApp(session: "CLISTREAM_SESSION")
        tap(app.buttons["Keys"])
        tap(app.buttons["Hide Keyboard"])
        XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 10))
        try voice.enable()
        XCTAssertTrue(voice.isEnabled)
        // Start in the toolbar and read forward into the session, as a VoiceOver user would.
        // A single exploratory touch moves VoiceOver focus without activating the button.
        app.buttons["session-ports"].coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        var readings = [String]()
        let focus = XCTNSPredicateExpectation(
          predicate: NSPredicate { _, _ in
            guard let speech = try? voice.currentSpeech() else { return false }
            return !speech.utterance.isEmpty
          }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [focus], timeout: 12), .completed)
        readings.append(try voice.currentSpeech().utterance)
        var deliveryRetries = 0
        for _ in 0..<20 {
          do {
            readings.append(try voice.moveForward().utterance)
          } catch let error as NSError
            where error.domain == "AXVoiceOverAutomationErrorDomain"
            && error.code == 3 && deliveryRetries < 3
          {
            deliveryRetries += 1
            try await Task.sleep(for: .milliseconds(500))
            continue
          }
          if ["Terminal", "Ports"].allSatisfy({ label in
            readings.contains { $0.localizedCaseInsensitiveContains(label) }
          }) {
            break
          }
        }
        for label in ["Terminal", "Ports"] {
          XCTAssertTrue(
            readings.contains { $0.localizedCaseInsensitiveContains(label) },
            "Missing \(label) in VoiceOver focus order: \(readings)")
        }
      }
    #endif

    @MainActor private func verifyPointer(_ gesture: () -> Void) async throws {
      let initial = try await fixtureResponse("POINTERS")
      gesture()
      // Read the real child's input. Its echo may already be above the viewport
      // after a software keyboard or rotation changes the terminal height.
      for _ in 0..<40 {
        let observed = try await fixtureResponse("POINTERS")
        if let count = Int(observed), let previous = Int(initial), count > previous { return }
        try await Task.sleep(for: .milliseconds(250))
      }
      XCTFail("The child did not receive a terminal pointer press")
    }

    @MainActor func testScrollSessionsAndTerminalHistory() async throws {
      _ = try await fixtureResponse("RESET")
      let app = try await pairedApp(session: "CLISTREAM_UI_PTY", scrollList: true)
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      assertText("scroll-ready", in: terminal)
      tap(app.buttons["Keys"])
      tap(app.buttons["Hide Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 12))
      let started = try await fixtureResponse("HISTORY")
      XCTAssertEqual(started, "released")
      let tail = NSPredicate(format: "value CONTAINS %@", "history-end")
      XCTAssertEqual(
        XCTWaiter.wait(
          for: [XCTNSPredicateExpectation(predicate: tail, object: terminal)],
          timeout: 20), .completed, "The live terminal did not display the final history marker")
      let start = terminal.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.2))
      let end = terminal.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.85))
      start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.2)
      let history = NSPredicate(
        format: "value CONTAINS %@ AND NOT (value CONTAINS %@)", "history-", "history-end")
      XCTAssertEqual(
        XCTWaiter.wait(
          for: [XCTNSPredicateExpectation(predicate: history, object: terminal)],
          timeout: 15), .completed, "A downward drag did not expose earlier terminal lines")
      tap(app.buttons["Keys"])
      tap(app.buttons["Show Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 6))
      tap(app.buttons["Session"])
      tap(app.buttons["Detach"])
    }

    @MainActor func testTouchNavigationAndEditing() async throws {
      let app = try await pairedApp(session: "CLISTREAM_TOUCH_SESSION", largeText: true)
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      assertText("copyword selection", in: terminal)
      let word = terminal.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 16, dy: 8))
      word.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Select"].firstMatch)
      captureCurrent("text-selection", app)
      tap(app.descendants(matching: .any)["Copy"].firstMatch)
      word.press(forDuration: 1)
      tap(app.descendants(matching: .any)["Paste"].firstMatch)
      terminal.typeText("\n")
      assertText("out:copyword", in: terminal)
      tap(app.buttons["Keys"])
      tap(app.buttons["Hide Keyboard"])
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 10))
      try app.performAccessibilityAudit(for: [.sufficientElementDescription, .contrast, .dynamicType]) { issue in
        let element = issue.element
        let identifiers = ["session-terminal", "session-ports", "desktop-session-status"]
        let identifier = identifiers.first { $0 == element?.identifier } ?? "unidentified"
        let role = element?.elementType.rawValue ?? 0
        let detail = XCTAttachment(string: issue.compactDescription)
        detail.name = "audit-\(issue.auditType.rawValue)-\(role)-\(identifier)"
        detail.lifetime = .keepAlways
        self.add(detail)
        return false
      }
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
    #if os(macOS)
      app.launchArguments += ["-ApplePersistenceIgnoreState", "YES"]
      if app.state != .notRunning { app.terminate() }
    #else
      app.launch()
      if (env["CLISTREAM_DUO_NAME"] ?? "").isEmpty && XCUIDevice.shared.orientation != .portrait {
        XCUIDevice.shared.orientation = .portrait
      }
    #endif
    let invitation = try await fixtureResponse("QR")
    app.open(try XCTUnwrap(URL(string: invitation)))
    tap(app.buttons["Pair device"])
    let target = app.descendants(matching: .any)["session-" + id].firstMatch
    XCTAssertTrue(target.waitForExistence(timeout: 15))
    if scrollList {
      #if os(iOS)
        let list = app.collectionViews.firstMatch
        for _ in 0..<12 where !target.isHittable { list.swipeUp() }
      #endif
    }
    tap(target)
    return app
  }

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
