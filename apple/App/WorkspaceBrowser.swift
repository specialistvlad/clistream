import SwiftUI
import UniformTypeIdentifiers

struct Download: Transferable {
  let url: URL
  static var transferRepresentation: some TransferRepresentation {
    FileRepresentation(exportedContentType: .data) { file in SentTransferredFile(file.url) }
      .suggestedFileName { $0.url.lastPathComponent }
  }
}

struct WorkspaceLocation {
  var path = [String]()
  var selections = [String: String]()
  var folder: String { path.last ?? "." }
}

struct WorkspaceBrowser: View {
  @ObservedObject var connection: Connection
  let session: SessionInfo
  @Environment(\.dismiss) private var dismiss
  @Environment(\.colorSchemeContrast) private var contrast
  @Binding var location: WorkspaceLocation
  @State private var listings = [String: [WorkspaceEntry]]()
  private var entries: [WorkspaceEntry] { listings[location.folder] ?? [] }
  @State private var message = ""
  @State private var importer = false
  @State private var exporter = false
  @State private var download: Download?
  @State private var active = false
  private func child(_ name: String) -> String {
    location.folder == "." ? name : location.folder + "/" + name
  }

  var body: some View {
    observedNavigation
      .fileImporter(isPresented: $importer, allowedContentTypes: [.data], onCompletion: imported)
      .fileExporter(
        isPresented: $exporter, item: download, contentTypes: [.data],
        defaultFilename: download?.url.lastPathComponent,
        onCompletion: exported, onCancellation: cleanDownload)
  }

  private func imported(_ result: Result<URL, Error>) {
    if case .success(let url) = result { Task { await put(url) } }
  }

  private func exported(_ result: Result<URL, Error>) {
    switch result {
    case .success(let url): message = "Downloaded " + url.lastPathComponent
    case .failure(let error): message = error.localizedDescription
    }
    cleanDownload()
  }

  @ViewBuilder private var navigation: some View {
    #if os(macOS)
      NavigationStack { contents(folder: location.folder) }.frame(minWidth: 520, minHeight: 340)
    #else
      NavigationStack(path: $location.path) {
        contents(folder: ".").navigationDestination(for: String.self) { folder in
          contents(folder: folder)
        }
      }
    #endif
  }

  private var observedNavigation: some View {
    navigation
      .interactiveDismissDisabled(active)
      .onChange(of: location.path) { old, path in
        if path.count > old.count, let child = path.last {
          location.selections[old.last ?? "."] = URL(fileURLWithPath: child).lastPathComponent
        }
      }
      .onChange(of: location.folder) { _, folder in
        AccessibilityNotification.Announcement(folder == "." ? session.Name + " Files" : folder)
          .post()
      }
      .onChange(of: message) { _, text in
        if !text.isEmpty { AccessibilityNotification.Announcement(text).post() }
      }
      .task(id: location.folder) { await refreshFolder() }
  }

  private func refreshFolder() async {
    let folder = location.folder
    listings = listings.filter { $0.key == "." || location.path.contains($0.key) }
    message = ""
    while !Task.isCancelled {
      let r = await connection.workspace(["Op": "ls", "Path": folder], session: session)
      guard !Task.isCancelled, location.folder == folder else { return }
      if let error = r.Error {
        message = error
        return
      }
      let updated = (r.Entries ?? []).sorted {
        $0.Name.localizedStandardCompare($1.Name) == .orderedAscending
      }
      if listings[folder] != updated { listings[folder] = updated }
      do { try await Task.sleep(for: .seconds(2)) } catch { return }
    }
  }

  private func contents(folder: String) -> some View {
    let entries = listings[folder] ?? []
    return VStack(alignment: .leading) {
      #if os(macOS)
        Table(
          entries,
          selection: Binding(
            get: { location.selections[folder] },
            set: { location.selections[folder] = $0 })
        ) {
          TableColumn("Name") { entry in
            Label(entry.Name, systemImage: entry.Directory ? "folder" : "doc")
          }
          TableColumn("Size") { entry in
            if !entry.Directory { Text(entry.Size.formatted(.byteCount(style: .file))) }
          }
          TableColumn("Modified") { entry in
            Text(
              Date(timeIntervalSince1970: Double(entry.Modified) / 1e9).formatted(
                date: .abbreviated, time: .shortened))
          }
        }
        .contextMenu(forSelectionType: String.self) { names in
          if let name = names.first { Button("Open") { open(name) } }
        } primaryAction: { names in
          if let name = names.first { open(name) }
        }
      #else
        List(entries) { entry in
          if entry.Directory {
            NavigationLink(value: folder == "." ? entry.Name : folder + "/" + entry.Name) {
              Label(entry.Name, systemImage: "folder")
            }.accessibilityIdentifier("entry-" + entry.Name)
              .accessibilityAddTraits(location.selections[folder] == entry.Name ? .isSelected : [])
          } else {
            let size = entry.Size.formatted(.byteCount(style: .file))
            Button {
              open(entry.Name)
            } label: {
              HStack {
                Label(entry.Name, systemImage: "doc")
                Spacer()
                Text(size)
                  .foregroundStyle(contrast == .increased ? Color.primary : Color.secondary)
              }
              .contentShape(Rectangle())
              .accessibilityElement(children: .ignore)
              .accessibilityLabel(Text(entry.Name))
              .accessibilityValue(Text(size))
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("entry-" + entry.Name)
            .accessibilityAddTraits(location.selections[folder] == entry.Name ? .isSelected : [])
          }
        }.disabled(active)
      #endif
      if active { ProgressView("Transferring file") }
      if !message.isEmpty {
        Text(message).font(.caption).textSelection(.enabled).padding(.horizontal)
      }
      #if os(iOS)
        // Keep the actions in the active navigation page so VoiceOver can
        // advance from its last file row to Upload and Done on compact screens.
        HStack {
          transferActions
        }.font(.body).buttonStyle(.plain).foregroundStyle(Color(uiColor: .label))
          .padding(.horizontal).frame(maxWidth: .infinity)
          .background(Color(uiColor: .systemBackground))
          .overlay(alignment: .top) { Divider() }
      #endif
    }
    .navigationTitle(
      folder == "." ? session.Name + " Files" : URL(fileURLWithPath: folder).lastPathComponent
    )
    #if os(iOS)
      .toolbarBackground(Color(uiColor: .systemBackground), for: .navigationBar)
      .toolbarBackground(.visible, for: .navigationBar)
    #endif
    #if os(macOS)
      .toolbar {
        Button("Up") { location.path.removeLast() }.keyboardShortcut(.upArrow, modifiers: .command)
        .disabled(location.path.isEmpty || active)
        if let name = location.selections[folder],
          let entry = entries.first(where: { $0.Name == name })
        {
          Button(entry.Directory ? "Open" : "Download") { open(name) }.keyboardShortcut("o")
        }
        transferActions
      }
    #endif
  }

  @ViewBuilder private var transferActions: some View {
    #if os(iOS)
      Button {
        importer = true
      } label: {
        Text("Upload").padding(.horizontal).frame(minHeight: 44)
      }
      .disabled(active)
      Spacer()
      Button {
        dismiss()
      } label: {
        Text("Done").padding(.horizontal).frame(minHeight: 44)
      }
      .keyboardShortcut(.cancelAction).disabled(active)
    #else
      Button("Upload") { importer = true }.disabled(active)
      Button("Done") { dismiss() }.keyboardShortcut(.cancelAction).disabled(active)
    #endif
  }

  private func open(_ name: String) {
    guard !active, let entry = entries.first(where: { $0.Name == name }) else { return }
    location.selections[location.folder] = name
    if entry.Directory { location.path.append(child(name)) } else { Task { await get(entry) } }
  }

  func cleanDownload() {
    if let download {
      try? FileManager.default.removeItem(at: download.url.deletingLastPathComponent())
    }
    download = nil
  }

  func get(_ entry: WorkspaceEntry) async {
    guard entry.Name == URL(fileURLWithPath: entry.Name).lastPathComponent, entry.Name != ".",
      entry.Name != ".."
    else { return }
    active = true
    defer { active = false }
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    do {
      try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
      let file = dir.appendingPathComponent(entry.Name)
      let r = await connection.workspace(
        ["Op": "get", "Path": child(entry.Name), "Local": file.path], session: session)
      if let error = r.Error {
        message = error
        try? FileManager.default.removeItem(at: dir)
        return
      }
      download = Download(url: file)
      exporter = true
    } catch { message = error.localizedDescription }
  }

  func put(_ url: URL) async {
    active = true
    defer { active = false }
    let destination = child(url.lastPathComponent)
    let access = url.startAccessingSecurityScopedResource()
    defer { if access { url.stopAccessingSecurityScopedResource() } }
    let r = await connection.workspace(
      ["Op": "put", "Path": destination, "Local": url.path], session: session)
    message = r.Error ?? "Uploaded \(url.lastPathComponent)"
  }
}
