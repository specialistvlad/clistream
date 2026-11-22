#if os(macOS)
  import XCTest

  /// Drives Device Hub's real device-pose controls while the mobile XCTest stays attached.
  final class DuoPoseTests: XCTestCase {
    @MainActor func testSetDevicePose() throws {
      continueAfterFailure = false
      let env = ProcessInfo.processInfo.environment
      let pose = try XCTUnwrap(env["CLISTREAM_DUO_POSE"])
      let device = try XCTUnwrap(env["CLISTREAM_DUO_NAME"])
      let id = try XCTUnwrap(env["CLISTREAM_DUO_ID"])
      let hubPath = try XCTUnwrap(env["CLISTREAM_DEVICE_HUB"])
      let hub = XCUIApplication(url: URL(fileURLWithPath: hubPath))
      hub.activate()
      let button = hub.buttons[pose].firstMatch
      let window = hub.windows.matching(NSPredicate(format: "title CONTAINS %@", device)).firstMatch
      if !window.exists || !button.exists {
        let sidebar = hub.buttons["Show Sidebar"].firstMatch
        if sidebar.exists && sidebar.isHittable { sidebar.click() }
        let row = hub.descendants(matching: .any).matching(
          NSPredicate(format: "identifier == %@ OR label == %@ OR value == %@",
                      "TableRow.Device." + id, device, device)
        ).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 60), "Duo simulator missing from Device Hub")
        row.click()
        let screen = hub.buttons["View Screen"].firstMatch
        if !button.exists && screen.waitForExistence(timeout: 10) { screen.click() }
      }
      guard button.waitForExistence(timeout: 30) else {
        XCTContext.runActivity(named: "Device Hub controls") { activity in
          activity.add(XCTAttachment(string: hub.debugDescription))
        }
        XCTFail("Missing Device Hub pose control")
        return
      }
      button.click()
    }
  }
#endif

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
      }
      XCTAssertNotEqual(widths.first, widths[2])
      XCTAssertEqual(widths.first, widths.last)
    }
  }
#endif
