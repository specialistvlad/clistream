import FileProvider

final class WorkspaceEnumerator: NSObject, NSFileProviderEnumerator {
  private let container: NSFileProviderItemIdentifier
  private let provider: WorkspaceProvider
  private var invalidated = false

  init(container: NSFileProviderItemIdentifier, provider: WorkspaceProvider) {
    self.container = container
    self.provider = provider
  }
  func invalidate() { provider.queue.async { self.invalidated = true } }

  private func refresh() throws -> [WorkspaceNode] {
    guard !invalidated else { throw CocoaError(.userCancelled) }
    let index = provider.index
    try index.load()
    let prior = index.sequence
    let items: [WorkspaceNode]
    if container == .workingSet {
      try index.refreshWorkingSet()
      items = index.all
    } else {
      if index.cached(container.rawValue) == nil { try index.discover() }
      let node = try index.require(container.rawValue)
      try index.refresh(node)
      items = node.kind == .file ? [try index.require(node.id)] : index.children(of: node.id)
    }
    try provider.persist(after: prior)
    return items
  }

  func enumerateItems(for observer: NSFileProviderEnumerationObserver, startingAt page: NSFileProviderPage) {
    provider.queue.async {
      do {
        let items = try self.refresh()
        for start in stride(from: 0, to: items.count, by: 200) {
          observer.didEnumerate(items[start..<min(start + 200, items.count)].map(WorkspaceItem.init))
        }
        observer.finishEnumerating(upTo: nil)
      } catch { observer.finishEnumeratingWithError(error) }
    }
  }
  func enumerateChanges(for observer: NSFileProviderChangeObserver, from anchor: NSFileProviderSyncAnchor) {
    provider.queue.async {
      do {
        _ = try self.refresh()
        let index = self.provider.index
        let id = self.container.rawValue
        let changes = try index.changes(since: anchor)
        let affected = Set(changes.filter {
          self.container == .workingSet || $0.parent == id || $0.oldParent == id || $0.id == id
        }.map(\.id))
        var removed = [NSFileProviderItemIdentifier]()
        var updated = [WorkspaceItem]()
        for key in affected {
          if let node = index.cached(key),
             self.container == .workingSet || node.parent == id || node.id == id {
            updated.append(WorkspaceItem(node))
          } else {
            removed.append(.init(key))
          }
        }
        if !removed.isEmpty { observer.didDeleteItems(withIdentifiers: removed) }
        if !updated.isEmpty { observer.didUpdate(updated) }
        observer.finishEnumeratingChanges(upTo: index.anchor, moreComing: false)
      } catch { observer.finishEnumeratingWithError(error) }
    }
  }
  func currentSyncAnchor(completionHandler: @escaping (NSFileProviderSyncAnchor?) -> Void) {
    provider.queue.async {
      do {
        try self.provider.index.load()
        completionHandler(self.provider.index.anchor)
      } catch { completionHandler(nil) }
    }
  }
}
