import SwiftTerm
import UIKit

/// UIKit hosts this view above the software keyboard, including after a display change.
final class TerminalKeyBar: UIView {
  private weak var terminal: TerminalView?
  private let keys = UIStackView()
  private var control: UIButton!
  private var pointer: UIButton!
  override var intrinsicContentSize: CGSize { CGSize(width: UIView.noIntrinsicMetric, height: 52) }

  init(terminal: TerminalView) {
    self.terminal = terminal
    super.init(frame: CGRect(x: 0, y: 0, width: terminal.bounds.width, height: 52))
    autoresizingMask = [.flexibleWidth]
    let effect = UIGlassEffect()
    effect.isInteractive = true
    let glass = UIVisualEffectView(effect: effect)
    glass.cornerConfiguration = .capsule()
    let scroll = UIScrollView()
    scroll.showsHorizontalScrollIndicator = false
    scroll.alwaysBounceHorizontal = false
    addSubview(glass)
    glass.contentView.addSubview(scroll)
    scroll.addSubview(keys)
    for view in [glass, scroll, keys] { view.translatesAutoresizingMaskIntoConstraints = false }
    NSLayoutConstraint.activate([
      glass.leadingAnchor.constraint(equalTo: safeAreaLayoutGuide.leadingAnchor, constant: 6),
      glass.trailingAnchor.constraint(equalTo: safeAreaLayoutGuide.trailingAnchor, constant: -6),
      glass.topAnchor.constraint(equalTo: topAnchor),
      glass.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -8),
      scroll.leadingAnchor.constraint(equalTo: glass.contentView.leadingAnchor),
      scroll.trailingAnchor.constraint(equalTo: glass.contentView.trailingAnchor),
      scroll.topAnchor.constraint(equalTo: glass.contentView.topAnchor),
      scroll.bottomAnchor.constraint(equalTo: glass.contentView.bottomAnchor),
      keys.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor),
      keys.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor),
      keys.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor),
      keys.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor),
      keys.heightAnchor.constraint(equalTo: scroll.frameLayoutGuide.heightAnchor)
    ])
    addKey("Escape", text: "esc") { $0.send(txt: "\u{1b}") }
    control = addKey("Control Key", text: "ctrl") { [weak self] view in
      view.controlModifier.toggle()
      self?.refreshModifiers()
    }
    let interrupt = UIAction(title: "Control-C") { [weak terminal] _ in
      terminal?.controlModifier = false
      terminal?.send(txt: "\u{03}")
    }
    control.menu = UIMenu(children: [interrupt])
    control.accessibilityCustomActions = [UIAccessibilityCustomAction(name: "Control-C") { [weak terminal] _ in
      guard let terminal else { return false }
      terminal.controlModifier = false
      terminal.send(txt: "\u{03}")
      return true
    }]
    addKey("Tab", symbol: "arrow.right.to.line") { $0.send(txt: "\t") }
    for (name, suffix) in [("Left","D"), ("Down","B"), ("Up","A"), ("Right","C")] {
      addKey(name + " Arrow", symbol: "arrow." + name.lowercased()) { view in
        view.send(txt: (view.getTerminal().applicationCursor ? "\u{1b}O" : "\u{1b}[") + suffix)
      }
    }
    pointer = addKey("Pointer mode", symbol: "hand.point.up.left") { [weak self] view in
      view.allowMouseReporting.toggle()
      self?.refreshModifiers()
    }
    addKey("Hide Keyboard", symbol: "keyboard.chevron.compact.down") { _ = $0.resignFirstResponder() }
    for key in ["~", "|", "/", "-"] { addKey(key, text: key) { $0.insertText(key) } }
    if traitCollection.userInterfaceIdiom == .pad || UIDevice.current.userInterfaceIdiom == .pad {
      for (index, code) in ["OP","OQ","OR","OS","[15~","[17~","[18~","[19~"].enumerated() {
        let label = "F\(index + 1)"
        addKey(label, text: label) { $0.send(txt: "\u{1b}" + code) }
      }
    }
    NotificationCenter.default.addObserver(self, selector: #selector(refreshModifiers),
      name: .terminalViewControlModifierReset, object: terminal)
    refreshModifiers()
  }
  required init?(coder: NSCoder) { nil }
  @objc private func refreshModifiers() {
    for (button, enabled) in [(control, terminal?.controlModifier), (pointer, terminal?.allowMouseReporting)] {
      button?.tintColor = enabled == true ? .systemBlue : .label
      button?.accessibilityValue = enabled == true ? "On" : "Off"
    }
  }
  @discardableResult private func addKey(_ name: String, text: String? = nil, symbol: String? = nil,
                                          action: @escaping (TerminalView) -> Void) -> UIButton {
    let button = UIButton(type: .system)
    var config = UIButton.Configuration.plain()
    config.title = text
    config.image = symbol.flatMap { UIImage(systemName: $0) }
    config.contentInsets = .zero
    button.configuration = config
    button.tintColor = .label
    button.accessibilityLabel = name
    button.widthAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
    button.addAction(UIAction { [weak terminal] _ in
      if let terminal { action(terminal) }
    }, for: .primaryActionTriggered)
    keys.addArrangedSubview(button)
    return button
  }
}
