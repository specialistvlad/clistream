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
      var widths: [CGFloat] = []
      for (pose, key, marker) in [
        ("Closed", "A", "[01]"), ("Partially Open", "B", "[02]"),
        ("Open", "C", "[03]"), ("Closed", "D", "[04]"),
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
        app.buttons["Keys"].tap()
        app.buttons["Control Key"].tap()
        app.buttons["Control-" + key].tap()
        let echoed = XCTNSPredicateExpectation(
          predicate: NSPredicate(format: "value CONTAINS %@", marker), object: terminal)
        XCTAssertEqual(XCTWaiter.wait(for: [echoed], timeout: 10), .completed)
        if key != "D", ProcessInfo.processInfo.environment["CLISTREAM_CAPTURE_GALLERY"] == "1" {
          let captured = try await fixtureResponse("pose-screenshot/" + pose)
          XCTAssertEqual(captured, "captured")
        }
      }
      XCTAssertNotEqual(widths.first, widths[2])
      XCTAssertEqual(widths.first, widths.last)
    }
  }
#endif
