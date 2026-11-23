import CryptoKit
import FileProvider
import Foundation
import OSLog

final class WorkspaceProvider: NSObject, NSFileProviderReplicatedExtension {
  let queue = DispatchQueue(label: "com.vlk.clistream.finder.index", qos: .utility)
  let index = WorkspaceIndex()
  let manager: NSFileProviderManager?
  private var poll: DispatchSourceTimer?
  private let log = Logger(subsystem: "com.vlk.clistream", category: "FileProvider")

  required init(domain: NSFileProviderDomain) {
    manager = NSFileProviderManager(for: domain)
    super.init()
    // The supervisor can also be edited by programs on the host, without an app notification.
    let timer = DispatchSource.makeTimerSource(queue: queue)
    timer.schedule(deadline: .now() + 5, repeating: 10, leeway: .seconds(2))
    timer.setEventHandler { [weak self] in
      guard let self else { return }
      do {
        try index.load()
        let prior = index.sequence
        try index.refreshWorkingSet()
        try persist(after: prior)
      } catch { log.error("Could not refresh Finder metadata: \(error.localizedDescription, privacy: .public)") }
    }
    poll = timer
    timer.resume()
  }

  func invalidate() { poll?.cancel() }

  func persist(after sequence: Int) throws {
    let changed = index.changedContainers(after: sequence)
    try index.save()
    guard !changed.isEmpty else { return }
    for id in changed { manager?.signalEnumerator(for: .init(id)) { _ in } }
    manager?.signalEnumerator(for: .workingSet) { _ in }
  }

  private func perform<T>(_ work: @escaping (String) throws -> T,
                          complete: @escaping (Result<T, Error>) -> Void) -> Progress {
    let progress = Progress(totalUnitCount: 1)
    let handle = UUID().uuidString
    progress.cancellationHandler = {
      DispatchQueue.global(qos: .userInitiated).async {
        _ = try? WorkspaceService.call(["Op": "workspace-cancel", "Handle": handle])
      }
    }
    queue.async {
      let result = Result {
        guard !progress.isCancelled else { throw CocoaError(.userCancelled) }
        try self.index.load()
        let prior = self.index.sequence
        do {
          let value = try work(handle)
          try self.persist(after: prior)
          return value
        } catch {
          try self.persist(after: prior)
          if progress.isCancelled { throw CocoaError(.userCancelled) }
          throw error
        }
      }
      progress.completedUnitCount = 1
      complete(result)
    }
    return progress
  }

  func item(for identifier: NSFileProviderItemIdentifier, request: NSFileProviderRequest,
            completionHandler: @escaping (NSFileProviderItem?, Error?) -> Void) -> Progress {
    perform({ _ in
      if self.index.cached(identifier.rawValue) == nil { try self.index.discover() }
      let node = try self.index.require(identifier.rawValue)
      return WorkspaceItem(node)
    }) { result in
      switch result {
      case .success(let item): completionHandler(item, nil)
      case .failure(let error): completionHandler(nil, error)
      }
    }
  }

  func fetchContents(for itemIdentifier: NSFileProviderItemIdentifier, version requestedVersion: NSFileProviderItemVersion?,
                     request: NSFileProviderRequest,
                     completionHandler: @escaping (URL?, NSFileProviderItem?, Error?) -> Void) -> Progress {
    perform({ handle in
      let node = try self.index.require(itemIdentifier.rawValue)
      guard node.kind == .file, let manager = self.manager else { throw NSFileProviderError(.noSuchItem) }
      let file = try manager.temporaryDirectoryURL().appendingPathComponent(UUID().uuidString)
      let version = requestedVersion.map { String(decoding: $0.contentVersion, as: UTF8.self) }
      do {
        let entry = try WorkspaceService.request("fetch", parent: node, path: self.index.path(of: node),
                                                 local: file, version: version, requestID: handle).entry()
        var current = entry.node(in: try self.index.require(node.parent))
        guard current.id == node.id else { throw NSFileProviderError(.noSuchItem) }
        current.name = node.name
        self.index.store(current)
        return (file, WorkspaceItem(current))
      } catch {
        try? FileManager.default.removeItem(at: file)
        if (error as NSError).code == NSFileProviderError.Code.localVersionConflictingWithServer.rawValue {
          throw NSFileProviderError(.versionNoLongerAvailable)
        }
        throw error
      }
    }) { result in
      switch result {
      case .success(let (url, item)): completionHandler(url, item, nil)
      case .failure(let error): completionHandler(nil, nil, error)
      }
    }
  }

  private func existing(at path: String, in parent: WorkspaceNode) throws -> WorkspaceNode {
    let entry = try WorkspaceService.request("stat", parent: parent, path: path).entry()
    let node = entry.node(in: parent)
    index.store(node)
    return node
  }

  private func digest(_ url: URL) throws -> SHA256.Digest {
    let stream = try FileHandle(forReadingFrom: url)
    defer { try? stream.close() }
    var hash = SHA256()
    while let data = try stream.read(upToCount: 256 * 1024), !data.isEmpty { hash.update(data: data) }
    return hash.finalize()
  }

  private func matches(_ local: URL, node: WorkspaceNode, handle: String) throws -> Bool {
    guard node.kind == .file, let manager, try local.resourceValues(forKeys: [.fileSizeKey]).fileSize == Int(node.bytes)
    else { return false }
    let copy = try manager.temporaryDirectoryURL().appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: copy) }
    _ = try WorkspaceService.request("fetch", parent: node, path: index.path(of: node), local: copy,
                                     version: node.version, requestID: handle)
    return try digest(local) == digest(copy)
  }

  func createItem(basedOn itemTemplate: NSFileProviderItem, fields: NSFileProviderItemFields,
                  contents url: URL?, options: NSFileProviderCreateItemOptions, request: NSFileProviderRequest,
                  completionHandler: @escaping (NSFileProviderItem?, NSFileProviderItemFields, Bool, Error?) -> Void) -> Progress {
    perform({ handle -> WorkspaceItem? in
      let parent = try self.index.require(itemTemplate.parentItemIdentifier.rawValue)
      let path = try self.index.childPath(itemTemplate.filename, in: parent)
      let type = itemTemplate.contentType ?? .data
      let folder = type.conforms(to: .directory)
      guard folder || (type.conforms(to: .data) && type != .symbolicLink) else {
        throw NSFileProviderError(.excludedFromSync)
      }
      var local = url
      var temporary: URL?
      defer { if let temporary { try? FileManager.default.removeItem(at: temporary) } }
      if !folder && local == nil {
        if options.contains(.mayAlreadyExist), !fields.contains(.contents) {
          do { return WorkspaceItem(try self.existing(at: path, in: parent)) }
          catch let err as NSError where err.code == NSFileProviderError.Code.noSuchItem.rawValue {
            // A reimported dataless placeholder carries no bytes to publish. A real, empty
            // document has content or a known zero byte size and may be created normally.
            if itemTemplate.documentSize??.int64Value != 0 { return nil }
          }
        }
        guard let manager = self.manager else { throw NSFileProviderError(.providerNotFound) }
        let empty = try manager.temporaryDirectoryURL().appendingPathComponent(UUID().uuidString)
        try Data().write(to: empty)
        temporary = empty
        local = empty
      }
      let requestID = itemTemplate.itemIdentifier.rawValue
      let replay = try self.index.beginCreation(requestID)
      do {
        let created = try WorkspaceService.request(folder ? "mkdir" : "create", parent: parent, path: path,
                                                   local: local, requestID: handle).entry().node(in: parent)
        self.index.store(created)
        return WorkspaceItem(created)
      } catch let e as NSError where e.code == NSFileProviderError.Code.filenameCollision.rawValue {
        let other = try self.existing(at: path, in: parent)
        if folder && other.kind == .directory && (replay || options.contains(.mayAlreadyExist)) { return WorkspaceItem(other) }
        if replay, let local, try self.matches(local, node: other, handle: handle) { return WorkspaceItem(other) }
        self.index.clearCreation(requestID)
        throw NSError.fileProviderErrorForCollision(with: WorkspaceItem(other))
      }
    }) { result in
      switch result {
      case .success(let item): completionHandler(item, [], false, nil)
      case .failure(let error): completionHandler(nil, [], false, error)
      }
    }
  }

  func modifyItem(_ item: NSFileProviderItem, baseVersion: NSFileProviderItemVersion,
                  changedFields: NSFileProviderItemFields, contents newContents: URL?,
                  options: NSFileProviderModifyItemOptions, request: NSFileProviderRequest,
                  completionHandler: @escaping (NSFileProviderItem?, NSFileProviderItemFields, Bool, Error?) -> Void) -> Progress {
    perform({ handle -> (WorkspaceItem, NSFileProviderItemFields, Bool) in
      let node = try self.index.require(item.itemIdentifier.rawValue)
      guard node.isRemote else { throw CocoaError(.fileWriteNoPermission) }
      let oldPath = try self.index.path(of: node)
      let parent = try self.index.require(item.parentItemIdentifier.rawValue)
      guard node.server == parent.server && node.workspace == parent.workspace else {
        throw CocoaError(.featureUnsupported)
      }
      let name = changedFields.contains(.filename) ? item.filename : node.remoteName
      let dest = try self.index.childPath(name, in: parent)
      let version = String(decoding: baseVersion.contentVersion, as: UTF8.self)
      do {
        if oldPath != dest {
          let moved = try WorkspaceService.request("move", parent: node, path: oldPath, destination: dest,
                                                   version: version, requestID: handle).entry().node(in: parent)
          self.index.store(moved)
          return (WorkspaceItem(moved), changedFields.intersection(.contents), false)
        }
        if changedFields.contains(.contents) {
          guard let newContents else { throw CocoaError(.fileReadNoSuchFile) }
          let result = try WorkspaceService.request("replace", parent: node, path: oldPath, local: newContents,
                                                    version: version, requestID: handle).entry().node(in: parent)
          self.index.store(result)
          return (WorkspaceItem(result), [], false)
        }
        return (WorkspaceItem(node), [], false)
      } catch let error as NSError where error.domain == NSFileProviderErrorDomain {
        if error.code == NSFileProviderError.Code.filenameCollision.rawValue {
          throw NSError.fileProviderErrorForCollision(with: WorkspaceItem(try self.existing(at: dest, in: parent)))
        }
        if error.code == NSFileProviderError.Code.noSuchItem.rawValue, oldPath != dest,
           let alreadyMoved = try? self.existing(at: dest, in: parent), alreadyMoved.id == node.id {
          return (WorkspaceItem(alreadyMoved), changedFields.intersection(.contents), false)
        }
        guard error.code == NSFileProviderError.Code.localVersionConflictingWithServer.rawValue,
              !options.contains(.failOnConflict), let newContents else { throw error }
        let latest = try self.existing(at: oldPath, in: self.index.require(node.parent))
        guard latest.id == node.id else { throw NSFileProviderError(.noSuchItem) }
        if try self.matches(newContents, node: latest, handle: handle) {
          return (WorkspaceItem(latest), [], false)
        }
        // Both editors keep their bytes. Publish a separate local revision before requesting the new remote original.
        let ext = (name as NSString).pathExtension
        var stem = (name as NSString).deletingPathExtension
        while stem.utf8.count + ext.utf8.count > 190 && !stem.isEmpty { stem.removeLast() }
        let suffix = try self.digest(newContents).prefix(8).map { String(format: "%02x", $0) }.joined()
        let conflictName = stem + " (conflicted copy " + suffix + ")" + (ext.isEmpty ? "" : "." + ext)
        let conflictPath = try self.index.childPath(conflictName, in: parent)
        do {
          let conflict = try WorkspaceService.request("create", parent: parent, path: conflictPath,
                                                      local: newContents, requestID: handle).entry().node(in: parent)
          self.index.store(conflict)
        } catch let collision as NSError where collision.code == NSFileProviderError.Code.filenameCollision.rawValue {
          let other = try self.existing(at: conflictPath, in: parent)
          guard try self.matches(newContents, node: other, handle: handle) else { throw collision }
        }
        return (WorkspaceItem(latest), [], true)
      }
    }) { result in
      switch result {
      case .success(let (item, pending, fetch)): completionHandler(item, pending, fetch, nil)
      case .failure(let error): completionHandler(nil, [], false, error)
      }
    }
  }

  func deleteItem(identifier: NSFileProviderItemIdentifier, baseVersion: NSFileProviderItemVersion,
                  options: NSFileProviderDeleteItemOptions, request: NSFileProviderRequest,
                  completionHandler: @escaping (Error?) -> Void) -> Progress {
    perform({ handle in
      guard let node = self.index.cached(identifier.rawValue) else { return }
      guard node.isRemote else { throw NSError.fileProviderErrorForRejectedDeletion(of: WorkspaceItem(node)) }
      let path = try self.index.path(of: node)
      do {
        _ = try WorkspaceService.request("remove", parent: node, path: path,
                                         version: String(decoding: baseVersion.contentVersion, as: UTF8.self),
                                         recursive: options.contains(.recursive), requestID: handle)
      } catch let error as NSError where error.domain == NSFileProviderErrorDomain {
        if error.code == NSFileProviderError.Code.localVersionConflictingWithServer.rawValue {
          let current = try self.existing(at: path, in: self.index.require(node.parent))
          throw NSError.fileProviderErrorForRejectedDeletion(of: WorkspaceItem(current))
        }
        if error.code != NSFileProviderError.Code.noSuchItem.rawValue { throw error }
      }
      self.index.remove(node.id)
    }) { result in
      switch result {
      case .success: completionHandler(nil)
      case .failure(let error): completionHandler(error)
      }
    }
  }

  func enumerator(for containerItemIdentifier: NSFileProviderItemIdentifier,
                  request: NSFileProviderRequest) throws -> NSFileProviderEnumerator {
    if containerItemIdentifier == .trashContainer { throw NSFileProviderError(.noSuchItem) }
    return WorkspaceEnumerator(container: containerItemIdentifier, provider: self)
  }
}
