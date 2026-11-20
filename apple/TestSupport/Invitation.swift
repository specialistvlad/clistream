import Foundation
import XCTest

/// The fixture mints a short-lived invitation only after the test app is ready.
func testInvitation(_ purpose: String) async throws -> String {
  let env = ProcessInfo.processInfo.environment
  let base = try XCTUnwrap(env["CLISTREAM_FIXTURE"])
  let endpoint = try XCTUnwrap(URL(string: base))
  var request = URLRequest(url: endpoint.appendingPathComponent(purpose))
  // The shared-session fixture waits for the three real test hosts to be ready.
  request.timeoutInterval = 320
  let (data, response) = try await URLSession.shared.data(for: request)
  XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
  return try XCTUnwrap(String(data: data, encoding: .utf8)).trimmingCharacters(
    in: .whitespacesAndNewlines)
}
