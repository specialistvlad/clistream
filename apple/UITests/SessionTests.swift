import XCTest

final class SessionTests: XCTestCase {
    func testPairAndUseSession() throws {
        continueAfterFailure = false
        let link = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_LINK"].flatMap(URL.init(string:)))
        let app = XCUIApplication()
        app.launch()
        app.open(link)
        let pair = app.buttons["Pair device"]
        XCTAssertTrue(pair.waitForExistence(timeout: 12))
        pair.tap()
        let session = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "cat")).firstMatch
        XCTAssertTrue(session.waitForExistence(timeout: 15))
        session.tap()
        let input = app.textFields["Input"]
        XCTAssertTrue(input.waitForExistence(timeout: 10))
        input.tap()
        input.typeText("apple-client-echo\n")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "apple-client-echo")).firstMatch.waitForExistence(timeout: 10))
        app.buttons["Files"].tap()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "fixture.txt")).firstMatch.waitForExistence(timeout: 10))
    }
}
