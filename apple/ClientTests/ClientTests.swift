import Darwin
import Foundation
import SwiftTerm
import XCTest

@testable import CLIStream

/// Runs in the built app on macOS or in the iOS simulator. All calls use its linked Go client.
final class ClientTests: XCTestCase {
  func profile(_ fixture: String) async throws -> String {
    let link = try await testInvitation(fixture)
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
    let fields = ["Op": "pair", "Dir": dir.path, "Link": link]
    let paired = await Core.request(fields)
    XCTAssertNil(paired.Error)
    let replay = await Core.request(fields)
    XCTAssertNotNil(replay.Error)
    return dir.path
  }

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

  func testWorkspaceTransfers() async throws {
    let dir = try await profile("FILES")
    let id = try session()
    let path = "inbox/apple.bin"
    let local = URL(fileURLWithPath: dir).appendingPathComponent("source")
    let download = URL(fileURLWithPath: dir).appendingPathComponent("download")
    let bytes = Data((0..<240019).map { UInt8(truncatingIfNeeded: $0) })
    try bytes.write(to: local)
    let put: [String: Any] = ["ID": id, "Dir": dir, "Path": path, "Local": local.path]
    _ = try await request("put", put)
    let duplicate = await Core.request(put.merging(["Op": "put"]) { _, new in new })
    XCTAssertNotNil(duplicate.Error)
    let listing = try await request("ls", ["ID": id, "Dir": dir, "Path": "inbox"])
    XCTAssertEqual(
      listing.Entries?.first(where: { $0.Name == "apple.bin" })?.Size, Int64(bytes.count))
    for escaped in ["../outside", "escape/outside"] {
      let bad = await Core.request([
        "Op": "get", "ID": id, "Dir": dir, "Path": escaped, "Local": download.path,
      ])
      XCTAssertNotNil(bad.Error)
      XCTAssertFalse(FileManager.default.fileExists(atPath: download.path))
    }
    _ = try await request("get", ["ID": id, "Dir": dir, "Path": path, "Local": download.path])
    XCTAssertEqual(try Data(contentsOf: download), bytes)
  }

  func testCLISpecificDrivers() async throws {
    let env = ProcessInfo.processInfo.environment
    try XCTSkipIf(
      env["CLISTREAM_CLAUDE_SESSION"] == nil,
      "Run with CLISTREAM_APP_DRIVERS=1 to start both installed model CLIs.")
    let dir = try await profile("DRIVERS")
    let list = try await request("list", ["Dir": dir])
    for name in ["claude", "codex"] {
      let id = try session(name.uppercased() + "_SESSION")
      let item = try XCTUnwrap(list.Sessions?.first { $0.id == id })
      XCTAssertEqual(item.Driver, name)
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

  func testPortForwarding() async throws {
    let dir = try await profile("PORTS")
    let id = try session()
    let list = try await request("list", ["Dir": dir])
    let item = try XCTUnwrap(list.Sessions?.first { $0.id == id })
    XCTAssertEqual(item.Ports, ["tcp": "tcp", "udp": "udp"])
    for proto in ["tcp", "udp"] {
      let r = try await request("forward", ["ID": id, "Dir": dir, "Name": proto])
      let handle = try XCTUnwrap(r.Handle)
      defer { _ = Core.call(["Op": "close", "Handle": handle]) }
      let address = try XCTUnwrap(r.Address?.components(separatedBy: ":").last.flatMap(UInt16.init))
      try exchange(port: address, datagram: proto == "udp")
    }
  }

  private func exchange(port: UInt16, datagram: Bool) throws {
    let sock = Darwin.socket(AF_INET, datagram ? SOCK_DGRAM : SOCK_STREAM, 0)
    guard sock >= 0 else { throw POSIXError(.ENOTSOCK) }
    defer { Darwin.close(sock) }
    var timeout = timeval(tv_sec: 6, tv_usec: 0)
    for option in [SO_RCVTIMEO, SO_SNDTIMEO] {
      XCTAssertEqual(
        setsockopt(
          sock, SOL_SOCKET, option, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout))), 0)
    }
    var address = sockaddr_in()
    address.sin_family = sa_family_t(AF_INET)
    address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
    address.sin_addr.s_addr = inet_addr("127.0.0.1")
    address.sin_port = port.bigEndian
    let connected = withUnsafePointer(to: &address) { ptr in
      ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) {
        Darwin.connect(sock, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
      }
    }
    XCTAssertEqual(connected, 0)
    let payload = Array((0..<1300).map { UInt8(truncatingIfNeeded: $0) })
    XCTAssertEqual(
      payload.withUnsafeBytes { send(sock, $0.baseAddress, $0.count, 0) }, payload.count)
    if !datagram { XCTAssertEqual(shutdown(sock, SHUT_WR), 0) }
    var received = [UInt8]()
    var buffer = [UInt8](repeating: 0, count: 4096)
    repeat {
      let n = buffer.withUnsafeMutableBytes { recv(sock, $0.baseAddress, $0.count, 0) }
      guard n > 0 else { break }
      received.append(contentsOf: buffer.prefix(n))
    } while !datagram && received.count < payload.count
    XCTAssertEqual(received, Array(payload.reversed()))
  }

  func testSharedPipe() async throws {
    let dir = try await profile("PIPE")
    let r = try await request("attach", ["ID": try session(), "Dir": dir])
    let key = try XCTUnwrap(r.Handle)
    defer { _ = Core.call(["Op": "close", "Handle": key]) }
    let participant = ProcessInfo.processInfo.environment["CLISTREAM_PARTICIPANT"] ?? "apple"
    let names = participant == "apple" ? ["apple"] : ["iphone", "ipad", "mac", "cli"]
    let payload = Data("hosted-\(participant)-echo\n".utf8)
    _ = try await request("input", ["Handle": key, "Data": payload.base64EncodedString()])
    var channels = [String: Data]()
    try await consume(key, seconds: names.count == 1 ? 12 : 100) { event in
      if let ch = event.Channel, let data = event.Data {
        channels[ch, default: Data()].append(data)
      }
      return names.allSatisfy { name in
        let stdout = Data("out:hosted-\(name)-echo\n".utf8)
        let stderr = Data("err:hosted-\(name)-echo\n".utf8)
        return channels["stdout"]?.range(of: stdout) != nil
          && channels["stderr"]?.range(of: stderr) != nil
      }
    }
  }

  @MainActor
  func testPTYRenderingKeyboardResizeAndExit() async throws {
    let model = Connection()
    let link = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_PTY_LINK"])
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
  private func consume(_ key: String, seconds: Int = 12, until complete: (Reply) -> Bool)
    async throws
  {
    let timer = Task {
      try await Task.sleep(for: .seconds(seconds))
      _ = await Core.request(["Op": "close", "Handle": key])
    }
    defer { timer.cancel() }
    while true {
      let event = try await request("next", ["Handle": key])
      if complete(event) { return }
      if event.Exit != nil { throw POSIXError(.EPIPE) }
    }
  }
}
