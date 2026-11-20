import XCTest
@testable import CLIStream

final class ClientTests: XCTestCase {
    func testPairedAppClient() async throws {
        let link = try XCTUnwrap(ProcessInfo.processInfo.environment["CLISTREAM_LINK"])
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let dir = directory.path
        let pair = await Core.request(["Op":"pair","Dir":dir,"Link":link])
        XCTAssertNil(pair.Error)
        let list = await Core.request(["Op":"list","Dir":dir])
        let item = try XCTUnwrap(list.Sessions?.first)
        let attach = await Core.request(["Op":"attach","Dir":dir,"ID":item.id])
        let handle = try XCTUnwrap(attach.Handle)
        defer { _ = Core.call(["Op":"close","Handle":handle]) }
        let payload = Data("apple-client-echo\n".utf8)
        let send = await Core.request(["Op":"input","Handle":handle,"Data":payload.base64EncodedString()])
        XCTAssertNil(send.Error)
        let echo = await Core.request(["Op":"next","Handle":handle])
        XCTAssertEqual(echo.Data, payload)
        let local = directory.appendingPathComponent("upload")
        try payload.write(to: local)
        let put = await Core.request(["Op":"put","Dir":dir,"ID":item.id,"Path":"from-mac","Local":local.path])
        XCTAssertNil(put.Error)
        let download = directory.appendingPathComponent("download")
        let get = await Core.request(["Op":"get","Dir":dir,"ID":item.id,"Path":"from-mac","Local":download.path])
        XCTAssertNil(get.Error)
        XCTAssertEqual(try Data(contentsOf: download), payload)
    }
}
