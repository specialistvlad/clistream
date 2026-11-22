#if os(iOS)
  import XCTest

  final class DuoContinuityTests: XCTestCase {
    @MainActor func testFoldWhileAttached() async throws {
      continueAfterFailure = false
      let app = XCUIApplication()
      app.launch()
      captureWelcomeIfEmpty(app)
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
        let changed = try await fixtureResponse("pose/" + pose)
        XCTAssertEqual(changed, "applied")
        let layout = XCTNSPredicateExpectation(
          predicate: NSPredicate { _, _ in
            guard terminal.exists && terminal.isHittable else { return false }
            guard let closed = widths.first else { return true }
            if pose == "Open" { return abs(terminal.frame.width - closed) > 1 }
            if pose == "Closed" { return abs(terminal.frame.width - closed) < 1 }
            return true
          }, object: terminal)
        XCTAssertEqual(XCTWaiter.wait(for: [layout], timeout: 15), .completed)
        widths.append(terminal.frame.width)
        // Type into the same live PTY on each display and check the byte the child saw.
        terminal.tap()
        terminal.typeText(key)
        let echoed = XCTNSPredicateExpectation(
          predicate: NSPredicate(format: "value CONTAINS %@", marker), object: terminal)
        XCTAssertEqual(XCTWaiter.wait(for: [echoed], timeout: 10), .completed)
        if key != "d", ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" {
          let captured = try await fixtureResponse("pose-screenshot/" + pose)
          XCTAssertEqual(captured, "captured")
        }
      }
      XCTAssertNotEqual(widths.first, widths[2])
      XCTAssertEqual(widths.first, widths.last)
    }
  }
#endif
