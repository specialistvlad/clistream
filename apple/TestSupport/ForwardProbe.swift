import Darwin
import XCTest

func exchange(port: UInt16, datagram: Bool) throws {
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
