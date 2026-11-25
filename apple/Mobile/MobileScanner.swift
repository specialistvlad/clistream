import AVFoundation
import SwiftUI
import VisionKit

struct MobileScanner: View {
  @ObservedObject var flow: MobileInvitations
  @Environment(\.scenePhase) private var scenePhase
  @Environment(\.openURL) private var openURL
  private enum Camera { case checking, ready, denied, unavailable }
  @State private var camera = Camera.checking

  var body: some View {
    ScrollView {
      VStack(spacing: 16) {
        ZStack {
          switch camera {
          case .checking: ProgressView()
          case .ready:
            QRReader(detected: { _ = flow.scanned($0) }, unavailable: { camera = .unavailable })
          case .unavailable:
            ContentUnavailableView(
              "Camera Unavailable", systemImage: "qrcode.viewfinder",
              description: Text("Paste the invitation instead."))
          case .denied:
            ContentUnavailableView {
              Label("Allow Camera Access", systemImage: "qrcode.viewfinder")
            } description: {
              Text("Turn on camera access for CLIStream in Settings to scan QR codes.")
            } actions: {
              Button("Open Settings") {
                if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
              }
            }
          }
        }.aspectRatio(1, contentMode: .fit).clipShape(.rect(cornerRadius: 32))
        Text("Point the camera at the QR code in the clistreamd terminal.").font(.headline)
        Text("Open clistreamd in a terminal to show the code. An invitation lasts three minutes unless its server made it longer.")
          .font(.subheadline).foregroundStyle(.secondary)
      }.multilineTextAlignment(.center).padding()
    }
    .safeAreaInset(edge: .bottom) {
      InvitationPasteButton(review: flow.add)
        .accessibilityIdentifier("scanner-paste-invitation").buttonStyle(.glass).controlSize(.large).padding()
    }
    .navigationTitle("Scan QR Code").navigationBarTitleDisplayMode(.inline)
    .toolbar {
      ToolbarItem(placement: .cancellationAction) {
        Button("Close", systemImage: "xmark") { flow.page = nil }.labelStyle(.iconOnly)
          .accessibilityIdentifier("workspace-cancel")
      }
    }
    .task(id: scenePhase) {
      guard scenePhase == .active else { return }
      #if DEBUG && targetEnvironment(simulator)
        if CommandLine.arguments.contains("--ui-test-scanned-code") {
          let sample = FileManager.default.temporaryDirectory.appendingPathComponent("uitest-scanned-qr")
          if let value = try? String(contentsOf: sample, encoding: .utf8) {
            try? FileManager.default.removeItem(at: sample)
            if flow.scanned(value) { return }
          }
        }
        if CommandLine.arguments.contains("--ui-test-camera-denied") {
          camera = .denied
          return
        }
      #endif
      guard DataScannerViewController.isSupported else {
        camera = .unavailable
        return
      }
      var permission = AVCaptureDevice.authorizationStatus(for: .video)
      if permission == .notDetermined {
        _ = await AVCaptureDevice.requestAccess(for: .video)
        permission = AVCaptureDevice.authorizationStatus(for: .video)
      }
      guard !Task.isCancelled else { return }
      if permission != .authorized {
        camera = .denied
        return
      }
      camera = DataScannerViewController.isAvailable ? .ready : .unavailable
    }
  }
}

private struct QRReader: UIViewControllerRepresentable {
  var detected: (String) -> Void
  var unavailable: () -> Void

  func makeCoordinator() -> Coordinator { Coordinator(self) }
  func makeUIViewController(context: Context) -> DataScannerViewController {
    let vc = DataScannerViewController(
      recognizedDataTypes: [.barcode(symbologies: [.qr])],
      recognizesMultipleItems: false, isGuidanceEnabled: false, isHighlightingEnabled: false)
    vc.delegate = context.coordinator
    let frame = context.coordinator.frame
    vc.overlayContainerView.addSubview(frame)
    frame.translatesAutoresizingMaskIntoConstraints = false
    NSLayoutConstraint.activate([
      frame.topAnchor.constraint(equalTo: vc.overlayContainerView.topAnchor),
      frame.bottomAnchor.constraint(equalTo: vc.overlayContainerView.bottomAnchor),
      frame.leadingAnchor.constraint(equalTo: vc.overlayContainerView.leadingAnchor),
      frame.trailingAnchor.constraint(equalTo: vc.overlayContainerView.trailingAnchor),
    ])
    do { try vc.startScanning() } catch { Task { unavailable() } }
    return vc
  }
  func updateUIViewController(_ vc: DataScannerViewController, context: Context) {
    context.coordinator.parent = self
  }
  static func dismantleUIViewController(_ vc: DataScannerViewController, coordinator: Coordinator) {
    vc.stopScanning()
  }

  final class Coordinator: NSObject, DataScannerViewControllerDelegate {
    var parent: QRReader
    let frame = CodeFrame()
    init(_ parent: QRReader) { self.parent = parent }
    func dataScanner(
      _ scanner: DataScannerViewController, didAdd addedItems: [RecognizedItem],
      allItems: [RecognizedItem]
    ) { update(allItems) }
    func dataScanner(
      _ scanner: DataScannerViewController, didUpdate updatedItems: [RecognizedItem],
      allItems: [RecognizedItem]
    ) { update(allItems) }
    func dataScanner(
      _ scanner: DataScannerViewController, didRemove removedItems: [RecognizedItem],
      allItems: [RecognizedItem]
    ) { update(allItems) }
    func dataScanner(
      _ scanner: DataScannerViewController,
      becameUnavailableWithError error: DataScannerViewController.ScanningUnavailable
    ) { parent.unavailable() }
    private func update(_ items: [RecognizedItem]) {
      guard case .barcode(let code)? = items.first else {
        frame.code = nil
        return
      }
      let points = [
        code.bounds.topLeft, code.bounds.topRight, code.bounds.bottomLeft, code.bounds.bottomRight,
      ]
      frame.code = points.reduce(CGRect.null) { $0.union(CGRect(origin: $1, size: .zero)) }
      if let value = code.payloadStringValue { parent.detected(value) }
    }
  }
  final class CodeFrame: UIView {
    var code: CGRect? { didSet { setNeedsDisplay() } }
    init() {
      super.init(frame: .zero)
      isOpaque = false
      isUserInteractionEnabled = false
    }
    required init?(coder: NSCoder) { nil }
    override func draw(_ rect: CGRect) {
      let side = min(228, bounds.width * 0.8, bounds.height * 0.8)
      let box =
        code
        ?? CGRect(x: bounds.midX - side / 2, y: bounds.midY - side / 2, width: side, height: side)
      let length = min(40, min(box.width, box.height) / 2)
      guard length > 0 else { return }
      let radius = min(20, length)
      UIColor.white.setStroke()
      for (x, dx) in [(box.minX, CGFloat(1)), (box.maxX, CGFloat(-1))] {
        for (y, dy) in [(box.minY, CGFloat(1)), (box.maxY, CGFloat(-1))] {
          let line = UIBezierPath()
          line.move(to: CGPoint(x: x + dx * length, y: y))
          line.addLine(to: CGPoint(x: x + dx * radius, y: y))
          line.addQuadCurve(
            to: CGPoint(x: x, y: y + dy * radius), controlPoint: CGPoint(x: x, y: y))
          line.addLine(to: CGPoint(x: x, y: y + dy * length))
          line.lineWidth = 4
          line.lineCapStyle = .round
          line.stroke()
        }
      }
    }
  }
}
