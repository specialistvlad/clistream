import SwiftUI

@main
struct CLIStreamApp: App {
    var body: some Scene { WindowGroup { BrowserView() } }
}

private struct Pairing: Identifiable { let id = UUID(); var link: URL? }

struct BrowserView: View {
    @StateObject private var connection = Connection()
    @State private var invitation = ""
    @State private var browsing = false
    @State private var pairing: Pairing?
    @State private var line = ""
    var body: some View {
        NavigationStack {
            VStack(alignment: .leading) {
                if let session = connection.selected {
                    if session.Mode == "pty" { LiveTerminal(connection: connection).id(session.id) }
                    else {
                        stream("Standard output", connection.stdout)
                        stream("Standard error", connection.stderr)
                        HStack {
                            TextField("Input", text: $line).onSubmit(submit)
                            Button("Send", action: submit)
                            Button("EOF") { connection.send(["Op":"eof"]) }
                        }.padding()
                    }
                    if let ports = session.Ports, !ports.isEmpty {
                        ForEach(ports.keys.sorted(), id: \.self) { name in
                            HStack {
                                Button(connection.forwarding[name] == nil ? "Forward \(name)" : "Stop \(name)") {
                                    Task { await connection.forward(name, item: session) }
                                }
                                if let address = connection.forwarding[name] { Text(address).textSelection(.enabled) }
                            }.padding(.horizontal)
                        }
                    }
                } else {
                    List(connection.sessions) { item in
                        Button {
                            Task { await connection.attach(item) }
                        } label: {
                            VStack(alignment: .leading) {
                                Text(item.Name).font(.headline)
                                Text("\(item.Host) · \(item.Mode.uppercased()) · \(item.Driver) \(item.Version)").font(.caption)
                            }
                        }.buttonStyle(.plain)
                    }.overlay { if connection.sessions.isEmpty {
                        ContentUnavailableView("No sessions", systemImage: "terminal", description: Text("Pair with your server, then start a supervisor."))
                    } }
                }
                if !connection.status.isEmpty { Text(connection.status).font(.callout).padding(.horizontal).textSelection(.enabled) }
            }
            .navigationTitle(connection.selected?.Name ?? "Sessions")
            .toolbar {
                if connection.selected != nil {
                    Button("Files") { browsing = true }; Button("Detach") { connection.leave(); Task { await connection.refresh() } }
                } else {
                    Button("Refresh") { Task { await connection.refresh() } }
                    Button("Pair") { pairing = Pairing() }
                }
            }
        }
        .sheet(isPresented: $browsing) { if let session = connection.selected { WorkspaceBrowser(connection: connection, session: session) } }
        .task { await connection.refresh() }
        .onOpenURL { url in if url.scheme == "clistream", url.host == "pair" { pairing = Pairing(link: url) } }
        .sheet(item: $pairing, onDismiss: { invitation = "" }) { request in
            VStack(alignment: .leading, spacing: 16) {
                Text("Pair a server").font(.title2)
                if let url = request.link {
                    let fields = URLComponents(string: "?" + (url.fragment ?? ""))?.queryItems
                    Text(fields?.first(where: { $0.name == "address" })?.value ?? "Server invitation")
                } else { SecureField("Pairing link", text: $invitation).textFieldStyle(.roundedBorder) }
                HStack {
                    Button("Cancel") { pairing = nil }
                    Button("Pair device") {
                        let link = request.link?.absoluteString ?? invitation.trimmingCharacters(in: .whitespacesAndNewlines)
                        pairing = nil
                        Task { await connection.pair(link) }
                    }.disabled(request.link == nil && invitation.isEmpty)
                }
            }.padding(24).frame(minWidth: 290).presentationDetents([.medium])
        }
    }

    func submit() {
        let data = Data((line + "\n").utf8)
        connection.send(["Op":"input", "Data":data.base64EncodedString()])
        line = ""
    }

    func stream(_ title: String, _ data: Data) -> some View {
        VStack(alignment: .leading) {
            Text(title).font(.caption).foregroundStyle(.secondary)
            ScrollView { Text(String(decoding: data, as: UTF8.self)).font(.system(.body, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
        }.padding(.horizontal)
    }
}

