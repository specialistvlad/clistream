import Darwin
import Foundation
import SwiftTerm
import XCTest

@testable import CLIStream

/// Runs in the built app on macOS or in the iOS simulator. All calls use its linked Go client.
final class ClientTests: XCTestCase {
  func testForwardBrowserURL() {
    XCTAssertEqual(ForwardLink.url(protocolName: "tcp", address: "127.0.0.1:49152")?.absoluteString,
                   "http://127.0.0.1:49152")
    XCTAssertNil(ForwardLink.url(protocolName: "udp", address: "127.0.0.1:49152"))
    XCTAssertNil(ForwardLink.url(protocolName: "tcp", address: "example.com:80"))
    XCTAssertNil(ForwardLink.url(protocolName: "tcp", address: "127.0.0.1:49152/unexpected"))
  }

  func testCommandLookupRetry() async {
    var target = SessionInfo(ID: "child", Name: "sh", Mode: "pipe", Host: "mac", OS: "darwin", Ports: nil)
    target.supervisor = "supervisor"
    target.revision = 17
    let timeout = Reply(Error: "deadline exceeded", ErrorKind: "transport-timeout")
    let command = Reply(Command: [Data("/bin/sh".utf8)])
    func perform(_ action: SessionAction, _ first: Reply, then second: Reply = Reply(),
                 cancel: Bool = false) async -> (Int, Reply) {
      var calls = 0
      let reply = await Core.action(action, directory: "/fixture", target: target,
                                    arguments: nil, columns: nil, rows: nil) { fields in
        XCTAssertEqual(fields["Op"] as? String, action.rawValue)
        XCTAssertEqual(fields["Dir"] as? String, "/fixture")
        XCTAssertEqual(fields["ID"] as? String, "supervisor")
        XCTAssertEqual(fields["Run"] as? String, "child")
        XCTAssertEqual(fields["Revision"] as? UInt64, 17)
        calls += 1
        if cancel { withUnsafeCurrentTask { $0?.cancel() } }
        return calls == 1 ? first : second
      }
      return (calls, reply)
    }
    let (successAttempts, recovered) = await perform(.command, timeout, then: command)
    XCTAssertEqual(successAttempts, 2)
    XCTAssertEqual(recovered.Command, command.Command)
    let (attempts, exhausted) = await perform(.command, timeout, then: timeout)
    XCTAssertEqual(attempts, 2)
    XCTAssertEqual(exhausted.ErrorKind, "transport-timeout")
    for action: SessionAction in [.launch, .restart, .start, .stop, .result] {
      let (attempts, _) = await perform(action, timeout)
      XCTAssertEqual(attempts, 1, action.rawValue)
    }
    for reply in [command, Reply(Error: "deadline exceeded"),
                  Reply(Error: "stale supervisor"), Reply(Error: "client authorization required")] {
      let (attempts, _) = await perform(.command, reply)
      XCTAssertEqual(attempts, 1)
    }
    let cancelled = await Task { await perform(.command, timeout, cancel: true) }.value
    XCTAssertEqual(cancelled.0, 1)
  }

  func profile(_ fixture: String) async throws -> String {
    let link = try await fixtureResponse(fixture)
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
    let fields = ["Op": "pair", "Dir": dir.path, "Link": link]
    let paired = await Core.request(fields)
    XCTAssertNil(paired.Error)
    let replay = await Core.request(fields)
    XCTAssertNil(replay.Error)
    return dir.path
  }

  @MainActor func testAdoptsExistingEnrollment() async throws {
    let dir = try await profile("PIPE")
    let workspace = Workspace(directory: dir)
    let key = URL(fileURLWithPath: dir).appendingPathComponent("key.pem")
    let original = try Data(contentsOf: key)
    workspace.load()
    workspace.load()
    XCTAssertEqual(workspace.profiles.map(\.directory), [dir])
    let link = try await fixtureResponse("PIPE")
    await workspace.enroll(link)
    XCTAssertEqual(workspace.profiles.map(\.directory), [dir])
    XCTAssertEqual(try Data(contentsOf: key), original)
  }

  #if os(macOS)
    @MainActor func testRemovingLegacyServerPreservesOtherPairings() async throws {
      let root = try await profile("PIPE")
      let workspace = Workspace(directory: root)
      workspace.load()
      let legacy = try XCTUnwrap(workspace.profiles.first)
      let invitation = try await fixtureResponse("SECOND")
      await workspace.enroll(invitation)
      let other = try XCTUnwrap(workspace.profiles.first { $0.id != legacy.id })
      let otherKey = URL(fileURLWithPath: other.directory).appendingPathComponent("key.pem")
      let identity = try Data(contentsOf: otherKey)

      workspace.rename(legacy, to: "Remove this server")
      try workspace.remove(legacy)
      XCTAssertFalse(FileManager.default.fileExists(atPath: root + "/key.pem"))
      XCTAssertFalse(FileManager.default.fileExists(atPath: root + "/server.json"))
      XCTAssertNil(UserDefaults.standard.string(forKey: "server-name-" + legacy.id))
      XCTAssertEqual(try Data(contentsOf: otherKey), identity)

      let reopened = Workspace(directory: root)
      reopened.load()
      XCTAssertEqual(reopened.profiles.map(\.id), [other.id])
      try reopened.remove(other)
      XCTAssertFalse(FileManager.default.fileExists(atPath: other.directory))
    }
  #endif

  func session(_ key: String = "SESSION") throws -> String {
    try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_" + key])
  }

  func request(_ op: String, _ fields: [String: Any]) async throws -> Reply {
    let reply = await Core.request(fields.merging(["Op": op]) { _, new in new })
    if let error = reply.Error {
      throw NSError(domain: "CLIStream", code: 1, userInfo: [NSLocalizedDescriptionKey: error])
    }
    return reply
  }

  func testCLISpecificDrivers() async throws {
    let env = ProcessInfo.processInfo.environment
    try XCTSkipIf(
      env["CLISTREAM_CLAUDE_SESSION"] == nil,
      "External Claude and Codex test sessions were not supplied.")
    let dir = try await profile("DRIVERS")
    let list = try await request("list", ["Dir": dir])
    for name in ["claude", "codex"] {
      let id = try session(name.uppercased() + "_SESSION")
      let item = try XCTUnwrap(list.Sessions?.first { $0.id == id })
      XCTAssertTrue(item.Name.contains(name))
      let open = try await request("attach", ["Dir": dir, "ID": id])
      let key = try XCTUnwrap(open.Handle)
      defer { _ = Core.call(["Op": "close", "Handle": key]) }
      var input = Data()
      let answer: String
      if name == "claude" {
        let challenge = try XCTUnwrap(env["CLISTREAM_CLAUDE_INPUT"])
        answer = try XCTUnwrap(env["CLISTREAM_CLAUDE_OUTPUT"])
        let message: [String: Any] = [
          "type": "user", "message": ["role": "user", "content": challenge],
          "parent_tool_use_id": NSNull(), "session_id": "",
        ]
        input = try JSONSerialization.data(withJSONObject: message)
      } else {
        input = Data("Compute 713 times 29. Reply with just the integer. Do not use tools.".utf8)
        answer = "20677"
      }
      input.append(10)
      _ = try await request("input", ["Handle": key, "Data": input.base64EncodedString()])
      _ = try await request("eof", ["Handle": key])
      var stdout = Data()
      try await consume(key, seconds: 90) { event in
        if event.Channel == "stdout", let data = event.Data { stdout.append(data) }
        if let code = event.Exit {
          XCTAssertEqual(code, 0)
          return true
        }
        return false
      }
      if name == "claude" {
        struct Frame: Decodable {
          let type: String
          let result: String?
        }
        let frames = try stdout.split(separator: 10).map {
          try JSONDecoder().decode(Frame.self, from: $0)
        }
        XCTAssertEqual(frames.filter { $0.type == "result" }.compactMap(\.result), [answer])
      } else {
        XCTAssertEqual(
          String(decoding: stdout, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines),
          answer)
      }
    }
  }

  @MainActor func testPortForwarding() async throws {
    let dir = try await profile("PORTS")
    let id = try session("PIPE_RUN")
    let list = try await request("list", ["Dir": dir])
    let item = try XCTUnwrap(list.Sessions?.first { $0.id == id })
    XCTAssertEqual(item.Ports, ["tcp": "tcp", "udp": "udp"])
    let model = Connection(profile: dir)
    let reader = Task { await model.attach(item) }
    defer {
      model.leave()
      reader.cancel()
    }
    try await waitUntil { model.attached }
    for proto in ["tcp", "udp"] {
      await model.setForward(proto, enabled: true, item: item)
      let listener = try XCTUnwrap(model.forwarding[proto])
      await model.setForward(proto, enabled: true, item: item)
      XCTAssertEqual(model.forwarding[proto], listener)
      let port = try XCTUnwrap(listener.components(separatedBy: ":").last.flatMap(UInt16.init))
      try exchange(port: port, datagram: proto == "udp")
      await model.setForward(proto, enabled: false, item: item)
      await model.setForward(proto, enabled: false, item: item)
      XCTAssertNil(model.forwarding[proto])
    }
  }

  func testSharedPipe() async throws {
    let dir = try await profile("PIPE")
    let r = try await request("attach", ["ID": try session("PIPE_RUN"), "Dir": dir])
    let key = try XCTUnwrap(r.Handle)
    defer { _ = Core.call(["Op": "close", "Handle": key]) }
    let participant = ProcessInfo.processInfo.environment["CLISTREAM_PARTICIPANT"] ?? "apple"
    let names = participant == "apple" ? ["apple"] : ["iphone", "ipad", "mac", "cli"]
    func send(_ line: String) async throws {
      _ = try await request(
        "input", ["Handle": key, "Data": Data((line + "\n").utf8).base64EncodedString()])
    }
    var sent = names.count == 1
    try await send(sent ? "hosted-\(participant)-echo" : "ready-\(participant)")
    var channels = [String: Data]()
    try await consume(key, seconds: names.count == 1 ? 12 : 1320) { event in
      if let ch = event.Channel, let data = event.Data {
        channels[ch, default: Data()].append(data)
      }
      if !sent, channels["stdout"]?.range(of: Data("out:hosted-cli-echo\n".utf8)) != nil {
        try await send("hosted-\(participant)-echo")
        sent = true
      }
      return names.allSatisfy { name in
        let stdout = Data("out:hosted-\(name)-echo\n".utf8)
        let stderr = Data("err:hosted-\(name)-echo\n".utf8)
        return channels["stdout"]?.range(of: stdout) != nil
          && channels["stderr"]?.range(of: stderr) != nil
      }
    }
    if let expected = ProcessInfo.processInfo.environment["CLISTREAM_EXPECT_EXIT"].flatMap(Int.init)
    {
      try await send("verified-\(participant)")
      try await consume(key, seconds: 90) { event in
        guard let code = event.Exit else { return false }
        XCTAssertEqual(code, expected)
        return true
      }
    }

  }

  @MainActor
  func testLastInputBeforeDetach() async throws {
    let model = Connection()
    let link = try await fixtureResponse("FLUSH")
    await model.pair(link)
    let id = try session()
    let item = try XCTUnwrap(model.sessions.first { $0.id == id })
    let reader = Task { await model.attach(item) }
    defer {
      model.leave()
      reader.cancel()
    }
    try await waitUntil { model.selected?.id == id }
    model.send(.bytes(Data("before-detach\n".utf8)))
    model.leave()
    let result = try await fixtureResponse("FLUSHED")
    XCTAssertEqual(result, "received")
  }

  @MainActor
  func testPTYRenderingKeyboardResizeAndExit() async throws {
    let model = Connection()
    let link = try await fixtureResponse("PTY")
    await model.pair(link)
    let id = try session("PTY_SESSION")
    let item = try XCTUnwrap(model.sessions.first { $0.id == id })
    let task = Task { await model.attach(item) }
    defer {
      model.leave()
      task.cancel()
    }
    try await waitUntil { model.selected?.id == id }
    let view = TerminalView(frame: CGRect(x: 0, y: 0, width: 700, height: 480))
    let delegate = TerminalDelegate(model)
    view.terminalDelegate = delegate
    model.install(view)
    delegate.sizeChanged(source: view, newCols: 117, newRows: 0)
    delegate.sizeChanged(source: view, newCols: 0, newRows: 39)
    delegate.sizeChanged(source: view, newCols: 117, newRows: 39)
    delegate.send(source: view, data: Array("measure\n".utf8)[...])
    try await waitUntil { model.status == "Process exited with status 23" }
    let screen = String(decoding: view.getTerminal().getBufferAsData(), as: UTF8.self)
    XCTAssertTrue(screen.contains("39 117"), screen)
    XCTAssertTrue(screen.contains("measure"), screen)
    XCTAssertFalse(screen.contains("\u{1B}[32m"), screen)
  }

  @MainActor
  private func waitUntil(_ ready: () -> Bool) async throws {
    let end = Date().addingTimeInterval(12)
    while !ready() {
      guard Date() < end else { throw POSIXError(.ETIMEDOUT) }
      try await Task.sleep(for: .milliseconds(50))
    }
  }

  /// Closing a handle unblocks a pending "next", including on a failed assertion.
  private func consume(
    _ key: String, seconds: Int = 12, until complete: (Reply) async throws -> Bool
  )
    async throws
  {
    let timer = Task {
      try await Task.sleep(for: .seconds(seconds))
      _ = await Core.request(["Op": "close", "Handle": key])
    }
    defer { timer.cancel() }
    while true {
      let event = try await request("next", ["Handle": key])
      if try await complete(event) { return }
      if event.Exit != nil { throw POSIXError(.EPIPE) }
    }
  }
}
