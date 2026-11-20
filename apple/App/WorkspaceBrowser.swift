import SwiftUI
import UniformTypeIdentifiers

struct Download: Transferable {
    let url: URL
    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .data) { file in SentTransferredFile(file.url) }
    }
}

struct WorkspaceBrowser: View {
    @ObservedObject var connection: Connection
    let session: SessionInfo
    @Environment(\.dismiss) private var dismiss
    @State private var path = [String]()
    @State private var entries = [WorkspaceEntry]()
    @State private var message = ""
    @State private var importer = false
    @State private var exporter = false
    @State private var download: Download?
    @State private var active = false
    private var location: String { path.isEmpty ? "." : path.joined(separator: "/") }
    private func child(_ name: String) -> String { (path + [name]).joined(separator: "/") }

    var body: some View {
        NavigationStack {
            List(entries) { entry in
                Button {
                    if entry.Directory { path.append(entry.Name) }
                    else { Task { await get(entry) } }
                } label: {
                    HStack {
                        Label(entry.Name, systemImage: entry.Directory ? "folder" : "doc")
                        Spacer()
                        if !entry.Directory { Text(ByteCountFormatter.string(fromByteCount: entry.Size, countStyle: .file)).foregroundStyle(.secondary) }
                    }
                }.disabled(active)
            }
            .navigationTitle(location)
            .toolbar {
                Button("Up") { path.removeLast() }.disabled(path.isEmpty || active)
                Button("Upload") { importer = true }.disabled(active)
                Button("Done") { dismiss() }
            }
            if active { ProgressView() }
            if !message.isEmpty { Text(message).font(.caption).textSelection(.enabled) }
        }
        .task(id: location) {
            let folder = location
            while !Task.isCancelled {
                do {
                    let r = await Core.request(["Op":"ls", "Dir":try connection.directory, "ID":session.id, "Path":folder])
                    guard !Task.isCancelled else { return }
                    if let error = r.Error { message = error; return }
                    entries = (r.Entries ?? []).sorted { $0.Name.localizedStandardCompare($1.Name) == .orderedAscending }
                    try await Task.sleep(for: .seconds(2))
                } catch { return }
            }
        }
        .fileImporter(isPresented: $importer, allowedContentTypes: [.data]) { result in
            if case .success(let url) = result { Task { await put(url) } }
        }
        .fileExporter(isPresented: $exporter, item: download, contentTypes: [.data], defaultFilename: download?.url.lastPathComponent) { result in
            if case .failure(let error) = result { message = error.localizedDescription }
            cleanDownload()
        } onCancellation: { cleanDownload() }
    }

    func cleanDownload() {
        if let download { try? FileManager.default.removeItem(at: download.url.deletingLastPathComponent()) }
        download = nil
    }

    func get(_ entry: WorkspaceEntry) async {
        guard entry.Name == URL(fileURLWithPath: entry.Name).lastPathComponent, entry.Name != ".", entry.Name != ".." else { return }
        active = true
        defer { active = false }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let file = dir.appendingPathComponent(entry.Name)
            let r = await Core.request(["Op":"get","Dir":try connection.directory,"ID":session.id,"Path":child(entry.Name),"Local":file.path])
            if let error = r.Error { message = error; try? FileManager.default.removeItem(at: dir); return }
            download = Download(url: file)
            exporter = true
        } catch { message = error.localizedDescription }
    }

    func put(_ url: URL) async {
        active = true
        defer { active = false }
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        do {
            let r = await Core.request(["Op":"put","Dir":try connection.directory,"ID":session.id,"Path":child(url.lastPathComponent),"Local":url.path])
            message = r.Error ?? "Uploaded \(url.lastPathComponent)"
        } catch { message = error.localizedDescription }
    }
}

