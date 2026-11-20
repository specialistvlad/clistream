import Foundation

/// Compiled only into the simultaneous-client simulator test host.
@MainActor
enum SharedParticipant {
  static func run(_ connection: Connection) async {
    let env = ProcessInfo.processInfo.environment
    guard let name = env["CLISTREAM_PARTICIPANT"], let session = env["CLISTREAM_SESSION"],
      let raw = env["CLISTREAM_FIXTURE"], let invitation = URL(string: raw)
    else { return }

    let report = FileManager.default.temporaryDirectory.appendingPathComponent("shared-\(session).json")
    func write(_ fields: [String: String]) throws {
      try JSONEncoder().encode(fields).write(to: report, options: .atomic)
    }
    func send(_ line: String) {
      connection.send(["Op": "input", "Data": Data((line + "\n").utf8).base64EncodedString()])
    }
    var stage = "pairing"
    do {
      let (data, response) = try await URLSession.shared.data(from: invitation.appendingPathComponent("PIPE"))
      guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
      await connection.pair(String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines))
      guard let item = connection.sessions.first(where: { $0.id == session }) else {
        throw URLError(.resourceUnavailable)
      }
      stage = "attaching to the session"
      let reader = Task { await connection.attach(item) }
      defer { connection.leave(); reader.cancel() }
      try await wait { connection.selected?.id == session }
      send("ready-\(name)")
      stage = "waiting for the CLI"
      try await wait { connection.stdout.range(of: Data("out:hosted-cli-echo\n".utf8)) != nil }
      send("hosted-\(name)-echo")
      stage = "receiving all writers"
      try await wait {
        ["mac", "iphone", "ipad", "cli"].allSatisfy {
          connection.stdout.range(of: Data("out:hosted-\($0)-echo\n".utf8)) != nil
            && connection.stderr.range(of: Data("err:hosted-\($0)-echo\n".utf8)) != nil
        }
      }
      try write(["stdout": String(decoding: connection.stdout, as: UTF8.self),
                 "stderr": String(decoding: connection.stderr, as: UTF8.self)])
    } catch {
      try? write(["failedStage": stage])
    }
  }

  private static func wait(until complete: () -> Bool) async throws {
    let deadline = ContinuousClock.now + .seconds(1200)
    while !complete() {
      guard ContinuousClock.now < deadline else { throw URLError(.timedOut) }
      try await Task.sleep(for: .milliseconds(100))
    }
  }
}
