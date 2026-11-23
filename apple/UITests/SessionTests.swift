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
      tap(app.buttons["workspace-confirm"])
      let alert = app.alerts["Couldn’t Pair This Device"]
      XCTAssertTrue(alert.waitForExistence(timeout: 10))
      XCTAssertTrue(
        alert.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "new one")).firstMatch
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
      tap(app.buttons["workspace-cancel"])
      tap(app.buttons["workspace-add-server"])
      let field = app.secureTextFields["pairing-link"]
      XCTAssertTrue(field.waitForExistence(timeout: 10))
      field.tap()
      field.typeText(invitation)
      XCTAssertTrue(app.staticTexts[address].waitForExistence(timeout: 10))
      tap(app.buttons["workspace-confirm"])
      XCTAssertTrue(
        app.descendants(matching: .any)["session-" + session].firstMatch.waitForExistence(
          timeout: 12))
      captureCurrent("servers-tree", app)
      app.open(url)
      tap(app.buttons["workspace-confirm"])
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
      func selectKey(_ name: String, on panel: String = "outer") async throws {
        let keyboardVisibility = ["Show Keyboard": true, "Hide Keyboard": false][name]
        let keyboard = app.keyboards.firstMatch
        touchTerminalKey(name, in: app)
        if let keyboardVisibility {
          XCTAssertTrue(keyboard.wait(for: \.exists, toEqual: keyboardVisibility, timeout: 10))
        }
      }
      // On the closed Duo the software keyboard leaves only a few rows for a menu.
      // Dismiss it before browsing all the arrow commands.
      if app.keyboards.firstMatch.exists {
        if duo {
          // A fresh system keyboard can present its one-time QuickPath tutorial.
          // Its keyboard overlay intercepts input intended for the native menu.
          let tutorial = NSPredicate(format: "label BEGINSWITH %@", "Speed up your typing by sliding")
          for surface in [app, XCUIApplication(bundleIdentifier: "com.apple.springboard")] {
            let message = surface.staticTexts.matching(tutorial).firstMatch
            if message.waitForExistence(timeout: 1) {
              tap(surface.buttons["Continue"].firstMatch)
              XCTAssertTrue(message.waitForNonExistence(timeout: 8))
              break
            }
          }
        }
        try await selectKey("Hide Keyboard")
      }
      var keyChecks = ["Up Arrow", "Down Arrow", "Right Arrow", "Left Arrow", "Escape", "Control-C", "Tab", "~", "|", "/", "-"]
      if UIDevice.current.userInterfaceIdiom == .pad { keyChecks += (1...8).map { "F" + String($0) } }
      for name in keyChecks {
        let label = ["~": "tilde", "|": "pipe", "/": "slash", "-": "dash"][name] ?? name
        let route = "key-count/" + label
        let raw = try await fixtureResponse(route)
        let before = try XCTUnwrap(Int(raw))
        try await selectKey(name)
        var count = before
        for _ in 0..<12 {
          let value = try await fixtureResponse(route)
          if let observed = Int(value) { count = observed }
          if count != before { break }
          try await Task.sleep(for: .milliseconds(150))
        }
        XCTAssertEqual(count, before + 1, name + " must reach the child exactly once")
      }
      // A windowed iPad can keep its horizontal size class through rotation.
      // Start with the software keyboard up and measure both grid dimensions.
      if !app.keyboards.firstMatch.exists { try await selectKey("Show Keyboard") }
      let portrait = view.frame.size
      let firstGrid = try await fixtureResponse("GRID")
      if duo {
        addTeardownBlock {
          let restored = try await fixtureResponse("pose/Closed")
          XCTAssertEqual(restored, "applied")
        }
        try await moveDuo(to: "Open", scenario: "input", in: app)
      } else {
        XCUIDevice.shared.orientation = .landscapeLeft
      }
      try await selectKey("Hide Keyboard", on: "inner")
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
        func setVoiceOver(_ enabled: Bool, line: UInt = #line) async {
          var lastError: NSError?
          for _ in 0..<3 {
            if voice.isEnabled == enabled { return }
            do {
              if enabled { try voice.enable() } else { try voice.disable() }
            } catch { lastError = error as NSError }
            if voice.isEnabled == enabled { return }
            try? await Task.sleep(for: .seconds(1))
          }
          XCTFail("VoiceOver state change failed (code \(lastError?.code ?? 0))", line: line)
        }
        addTeardownBlock { @MainActor in await setVoiceOver(initiallyEnabled) }
        await setVoiceOver(false)
        let app = try await pairedApp(session: "CLISTREAM_SESSION")
        touchTerminalKey("Hide Keyboard", in: app)
        XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 10))
        await setVoiceOver(true)
        XCTAssertTrue(voice.isEnabled)
        // Explore a control by touch, then navigate away and back with the VoiceOver cursor.
        // This exercises both toolbar layouts without assuming which side the terminal occupies.
        func spoken(_ label: String, near element: XCUIElement) async throws -> Bool {
          guard element.isHittable else { return false }
          element.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
          let focus = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
            (try? voice.currentSpeech().utterance.localizedCaseInsensitiveContains(label)) == true
          }, object: nil)
          let foundByTouch = await XCTWaiter.fulfillment(of: [focus], timeout: 3) == .completed
          var readings = [String]()
          for forward in [true, false, false, true] {
            do {
              let output = try forward ? voice.moveForward() : voice.moveBackward()
              readings.append(output.utterance)
            } catch {
              // On a short toolbar a move can hit the edge of its container. Continue
              // with the opposite direction and still require spoken output.
              try await Task.sleep(for: .milliseconds(500))
            }
            if readings.contains(where: { $0.localizedCaseInsensitiveContains(label) }) {
              return true
            }
          }
          return foundByTouch && readings.contains(where: { !$0.isEmpty })
        }
        let ports = try await spoken("Ports", near: app.buttons["session-ports"])
        XCTAssertTrue(ports, "VoiceOver could not reach Ports")
        let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
        let content = try await spoken("Terminal", near: terminal)
        XCTAssertTrue(content, "VoiceOver could not reach the terminal")
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
      touchTerminalKey("Hide Keyboard", in: app)
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
      openTerminalKeys(in: app)
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
      touchTerminalKey("Hide Keyboard", in: app)
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 10))
      try app.performAccessibilityAudit(for: [.sufficientElementDescription, .contrast, .dynamicType]) { issue in
        let element = issue.element
        let identifiers = ["session-terminal", "session-ports", "desktop-session-status", "retry-server"]
        let identifier = identifiers.first { $0 == element?.identifier } ?? "unidentified"
        let role = element?.elementType.rawValue ?? 0
        let reason = (issue.compactDescription + " " + issue.detailedDescription).lowercased()
        let categories = ["clip", "truncat", "scal", "overlap", "small", "font", "contrast"]
          .filter { reason.contains($0) }.joined(separator: "-")
        let detail = XCTAttachment(string: issue.compactDescription)
        detail.name = "audit-\(issue.auditType.rawValue)-\(role)-\(identifier)-\(categories)"
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
      // Use the user's system setting so the subsequent audit can exercise other sizes.
      let original = try await fixtureResponse("content-size/current")
      addTeardownBlock {
        let restored = try await fixtureResponse("content-size/" + original)
        XCTAssertEqual(restored, original)
      }
      let applied = try await fixtureResponse("content-size/accessibility-large")
      XCTAssertEqual(applied, "accessibility-large")
      app.launchArguments += ["-AppleInterfaceStyle", "Dark"]
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
    tap(pairingButton(in: app))
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
