import Foundation
import XCTest

/// The fixture mints a short-lived invitation only after the test app is ready.
func fixtureResponse(_ purpose: String) async throws -> String {
  let env = ProcessInfo.processInfo.environment
  if let link = env["CLISTREAM_" + purpose + "_LINK"] {
    // The concurrent harness issues each device its own invitation after boot.
    return link
  }
  let base = try XCTUnwrap(env["CLISTREAM_FIXTURE"])
  let endpoint = try XCTUnwrap(URL(string: base))
  var request = URLRequest(url: endpoint.appendingPathComponent(purpose))
  request.timeoutInterval = 45
  let (data, response) = try await URLSession.shared.data(for: request)
  XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
  return try XCTUnwrap(String(data: data, encoding: .utf8)).trimmingCharacters(
    in: .whitespacesAndNewlines)
}
