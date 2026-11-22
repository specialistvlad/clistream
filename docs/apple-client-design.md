# Native Apple clients

CLIStream has a dedicated macOS application and a universal iPhone and iPad application. Both target OS 27. Build with Xcode 27.1 or later for iPhone Duo. The supervisor runs on the computer that owns the program. Its server can run on another Mac or Linux machine and route sessions from several hosts.

## Where to change the interface

`apple/Desktop` owns AppKit window tabbing and the Finder-style browser. `apple/Mobile` owns compact and split-view navigation. The shared views in `apple/App` display a session, edit commands and forward user actions to a `Connection` scoped to a server and supervisor. Each connection owns its terminal renderer, stream and port forwards. Closing one tab releases only that tab’s resources.

`Core.swift` translates actions into requests to the Go bridge in `apple/Bridge`. The bridge owns native handles. Supervisor discovery, run output and QUIC attachments live in `internal/client`, where the Go CLI can use them as well. Layout changes belong in Swift and use the same actions as existing controls.

## Pair and discover

Open the daemon’s invitation or paste it into Pair. Review the advertised server address before confirming enrollment. Keep the saved identity across launches. A pairing error lets you dismiss an invalid or expired link. For a revoked device, obtain a new invitation from the server operator. Successful pairing displays the server’s supervisors, including stopped CLI apps. Pair additional servers with **Add Server**. Under **View**, choose **Tree** or **List**, then group by **Server**, **Host**, or **CLI Name**. An offline server offers **Retry**.

## Mac windows

The Servers sidebar separates the broker from the computers running supervisors. Select a CLI to work in its terminal. Change the sidebar layout with View or open a CLI in its own tab or window through the row’s context menu. Use the server row’s context menu to rename it, or choose a server under View → Rename Server. The terminal subtitle identifies its host and server. The status bar shows connection state and measured round-trip latency to the selected broker. Both Apple apps follow the system’s Light or Dark appearance.

## Touch-only operation on iPhone and iPad

Choose a session in the sidebar when space permits or navigate from the compact session list. Switching preserves open attachments. Detach closes the selected attachment. In an attached terminal, open **Keys** for Escape, arrows, Control-letter combinations, and the onscreen keyboard. The selected run stays open across keyboard changes, rotation, iPad resizing, and Duo folding. Use hardware keyboard and pointer input where available.

## Terminal and session controls

Pipe and PTY sessions both open in SwiftTerm. Keystrokes go straight to the child, including before Return. Pipe sessions display stdout and stderr together in arrival order, send LF on Return, and let the child control echo. PTYs receive terminal resize events. **Send EOF** closes pipe input for every viewer. Detaching leaves the program running; a stopped managed program exposes its most recent retained result.

On Mac, use the window toolbar for lifecycle actions. On iPhone and iPad, open **Session**. **Restart…** and **Start…** present the current executable and arguments together in a highlighted command field. Use quotes for spaces and `""` for an empty argument. The Go client and native apps use the same parser. Cancel keeps the current run; confirming launches the reviewed command and attaches to its replacement. **Stop…** identifies the affected program before ending it for all viewers. One-shot supervisors offer attachment only.

**Ports** opens a trailing inspector on Mac and wide mobile layouts, or a sheet in compact layouts. Toggle a named TCP or UDP export, then copy its local listening address. Closing the inspector leaves active listeners running. Each tab or window shows the forwards for its selected session.

## Future file access

The Go CLI supports remote workspace operations today. Apple file access is deferred to a replicated File Provider exposing a location in Finder and Files. [Apple File Provider](https://developer.apple.com/documentation/fileprovider)

## Acceptance

Drive multi-server pairing, grouping, tab and session switching, terminal input and output, lifecycle requests, completed runs and port forwards through the production user interfaces. Include simultaneous clients, touch-only navigation, keyboard and pointer input, accessibility, and live resizing on Mac, iPhone, iPad and iPhone Duo. Record observed results and outstanding scenarios in the verification record.
