import Foundation

struct SessionInfo: Codable, Identifiable {
  let ID, Name, Mode, Host, OS: String
  let Ports: [String: String]?
  var supervisor: String?
  var revision: UInt64?
  var editable: Bool?
  var hasResult: Bool?
  var id: String { supervisor ?? self.ID }
  var running: Bool { !self.ID.isEmpty }

}

struct SupervisorInfo: Decodable {
  let ID: String
  let Revision: UInt64
  let Info: SessionInfo
  let EditableCommand: Bool?
  let RunResult: Bool?
}

struct Reply: Decodable {
  var Supervisors: [SupervisorInfo]?
  var Command: [Data]?
  var Run: String?
  var Error: String?
  var Handle: String?
  var Sessions: [SessionInfo]?
  var Address: String?
  var Data: Foundation.Data?
  var Channel: String?
  var Exit: Int?
}
