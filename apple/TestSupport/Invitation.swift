import Foundation
import XCTest

/// The fixture mints a short-lived invitation only after the test app is ready.
func testInvitation(_ purpose: String) async throws -> String {
  let env = ProcessInfo.processInfo.environment
  if let base = env["CLISTREAM_FIXTURE"], let endpoint = URL(string: base) {
    let (data, response) = try await URLSession.shared.data(
      from: endpoint.appendingPathComponent(purpose))
    XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
    return try XCTUnwrap(String(data: data, encoding: .utf8)).trimmingCharacters(
      in: .whitespacesAndNewlines)
  }
  // The four-client test coordinates one pre-issued invite per participant.
  return try XCTUnwrap(env["CLISTREAM_" + purpose + "_LINK"])
}
