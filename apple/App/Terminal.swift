import SwiftUI
import SwiftTerm

final class TerminalDelegate: NSObject, TerminalViewDelegate {
    let connection: Connection
    init(_ connection: Connection) { self.connection = connection }
    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        let bytes = Data(data)
        MainActor.assumeIsolated { connection.send(["Op":"input", "Data":bytes.base64EncodedString()]) }
    }
    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        MainActor.assumeIsolated { connection.send(["Op":"resize", "Cols":newCols, "Rows":newRows]) }
    }
    func setTerminalTitle(source: TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func scrolled(source: TerminalView, position: Double) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String:String]) {}
    func bell(source: TerminalView) {}
    func clipboardCopy(source: TerminalView, content: Data) {}
    func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) {}
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

#if os(macOS)
struct LiveTerminal: NSViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeNSView(context: Context) -> TerminalView {
        let view = TerminalView(frame: .zero)
        view.terminalDelegate = context.coordinator
        connection.install(view)
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        return view
    }
    func updateNSView(_ view: TerminalView, context: Context) {}
}
#else
struct LiveTerminal: UIViewRepresentable {
    let connection: Connection
    func makeCoordinator() -> TerminalDelegate { TerminalDelegate(connection) }
    func makeUIView(context: Context) -> TerminalView {
        let view = TerminalView(frame: .zero)
        view.terminalDelegate = context.coordinator
        connection.install(view)
        return view
    }
    func updateUIView(_ view: TerminalView, context: Context) {}
}
#endif

