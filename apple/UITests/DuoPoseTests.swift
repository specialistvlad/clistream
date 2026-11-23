#if os(iOS)
  import XCTest

  final class DuoContinuityTests: XCTestCase {
    @MainActor func testFoldWhileAttached() async throws {
      continueAfterFailure = false
      let app = XCUIApplication()
      app.launch()
      defer { app.terminate() }
      let link = try await fixtureResponse("QR")
      app.open(try XCTUnwrap(URL(string: link)))
      app.buttons["Pair device"].tap()
      let id = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_ID"])
      let row = app.descendants(matching: .any)["session-" + id].firstMatch
      XCTAssertTrue(row.waitForExistence(timeout: 15))
      row.tap()
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      XCTAssertTrue(terminal.waitForExistence(timeout: 15))
      addTeardownBlock {
        let restored = try await fixtureResponse("pose/Closed")
        XCTAssertEqual(restored, "applied")
      }
      var widths: [CGFloat] = []
      for (pose, key, marker) in [
        ("Closed", "a", "[61]"), ("Partially Open", "b", "[62]"),
        ("Open", "c", "[63]"), ("Closed", "d", "[64]"),
      ] {
        try await moveDuo(to: pose, scenario: "fold", in: app)
        let layout = XCTNSPredicateExpectation(
          predicate: NSPredicate { _, _ in
            guard terminal.exists && terminal.isHittable else { return false }
            guard let closed = widths.first else { return true }
            if pose == "Open" { return abs(terminal.frame.width - closed) > 1 }
            if pose == "Closed" { return abs(terminal.frame.width - closed) < 1 }
            return true
          }, object: terminal)
        let result = await XCTWaiter.fulfillment(of: [layout], timeout: 15)
        if result != .completed { recordDuoFailure("fold-layout", in: app) }
        XCTAssertEqual(result, .completed)
        widths.append(terminal.frame.width)
        // Type into the same live PTY on each display and check the byte the child saw.
        terminal.tap()
        terminal.typeText(key)
        let echoed = XCTNSPredicateExpectation(
          predicate: NSPredicate(format: "value CONTAINS %@", marker), object: terminal)
        let received = await XCTWaiter.fulfillment(of: [echoed], timeout: 10)
        XCTAssertEqual(received, .completed)
        if key != "d", ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" {
          let captured = try await fixtureResponse("pose-screenshot/" + pose)
          XCTAssertEqual(captured, "captured")
        }
      }
      XCTAssertNotEqual(widths.first, widths[2])
      XCTAssertEqual(widths.first, widths.last)
    }
  }

  extension XCTestCase {
    @MainActor func moveDuo(to pose: String, scenario: String, in app: XCUIApplication,
                            file: StaticString = #filePath, line: UInt = #line) async throws {
      struct Panel: Decodable { let Width, Height: Double }
      let reply = try await fixtureResponse("pose/" + scenario + "/" + pose, file: file, line: line)
      let panel = try JSONDecoder().decode(Panel.self, from: Data(reply.utf8))
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      let live = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
        guard app.state == .runningForeground, terminal.exists && terminal.isHittable else { return false }
        return app.windows.allElementsBoundByIndex.contains { window in
          let size = window.frame.size
          return abs(size.width - panel.Width) < 2 && abs(size.height - panel.Height) < 2
        }
      }, object: nil)
      let result = await XCTWaiter.fulfillment(of: [live], timeout: 30)
      if result != .completed { recordDuoFailure(scenario + "-scene", in: app) }
      XCTAssertEqual(result, .completed, "The terminal did not become active on the \(pose) display",
                     file: file, line: line)
    }

    @MainActor func recordDuoFailure(_ stage: String, in app: XCUIApplication) {
      guard !(ProcessInfo.processInfo.environment["CLISTREAM_DUO_NAME"] ?? "").isEmpty else { return }
      var state = ["appState=\(app.state.rawValue) appFrame=\(app.frame)"]
      for window in app.windows.allElementsBoundByIndex.prefix(4) {
        state.append("windowFrame=\(window.frame)")
      }
      let terminal = app.descendants(matching: .any)["session-terminal"].firstMatch
      for (name, item) in [("terminal", terminal), ("sessionMenu", app.buttons["Session"].firstMatch),
                           ("keys", app.buttons["Keys"].firstMatch), ("hideKeyboard", app.buttons["Hide Keyboard"].firstMatch),
                           ("keyboard", app.keyboards.firstMatch)] {
        state.append(item.exists ? "\(name) hittable=\(item.isHittable) frame=\(item.frame)" : "\(name) missing")
      }
      let geometry = XCTAttachment(string: state.joined(separator: "\n"))
      geometry.name = "failed-duo-" + stage + "-geometry"
      geometry.lifetime = .keepAlways
      add(geometry)
      let screen = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
      screen.name = "failed-duo-" + stage + "-window"
      screen.lifetime = .keepAlways
      add(screen)
    }
  }
#endif
