import SwiftUI

struct ProjectLink: View {
  var body: some View {
    Link(destination: URL(string: "https://github.com/specialistvlad/clistream")!) {
      Label("CLIStream on GitHub", systemImage: "chevron.left.forwardslash.chevron.right")
        .labelStyle(.iconOnly)
        .foregroundStyle(.primary)
    }
    .accessibilityIdentifier("project-github")
  }
}

enum ForwardLink {
  static func url(protocolName: String?, address: String) -> URL? {
    guard protocolName?.lowercased() == "tcp",
      let components = URLComponents(string: "http://" + address),
      components.host == "127.0.0.1", components.port != nil,
      components.path.isEmpty, components.query == nil, components.fragment == nil,
      components.user == nil, components.password == nil else { return nil }
    return components.url
  }
}
