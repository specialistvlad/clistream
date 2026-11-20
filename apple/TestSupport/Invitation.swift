import Foundation
import XCTest

/// The fixture mints a short-lived invitation only after the test app is ready.
func fixtureResponse(_ purpose: String, at point: CGPoint? = nil) async throws -> String {
  let env = ProcessInfo.processInfo.environment
  let base = try XCTUnwrap(env["CLISTREAM_FIXTURE"])
  let endpoint = try XCTUnwrap(URL(string: base))
  var url = endpoint.appendingPathComponent(purpose)
  if let point {
    url.append(queryItems: [
      URLQueryItem(name: "x", value: String(Double(point.x))),
      URLQueryItem(name: "y", value: String(Double(point.y))),
    ])
  }
  var request = URLRequest(url: url)
  request.timeoutInterval = 45
  let (data, response) = try await URLSession.shared.data(for: request)
  XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
  return try XCTUnwrap(String(data: data, encoding: .utf8)).trimmingCharacters(
    in: .whitespacesAndNewlines)
}
