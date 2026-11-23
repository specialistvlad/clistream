# Native Apple clients

CLIStream has a dedicated macOS application and a universal iPhone and iPad application. Both target OS 27. Build with Xcode 27.1 or later for iPhone Duo. The supervisor runs on the computer that owns the program. Its server can run on another Mac or Linux machine and route sessions from several hosts.

## Where to change the interface

`apple/Desktop` owns AppKit window tabbing and the Finder-style browser. `apple/Mobile` owns compact and split-view navigation. The shared views in `apple/App` display a session, edit commands and forward user actions to a `Connection` scoped to a server and supervisor. Each connection owns its terminal renderer, stream and port forwards. Closing one tab releases only that tab’s resources.

`Core.swift` translates actions into requests to the Go bridge in `apple/Bridge`. The bridge owns native handles. Supervisor discovery, run output and QUIC attachments live in `internal/client`, where the Go CLI can use them as well. Layout changes belong in Swift and use the same actions as existing controls. The macOS design target, with measurements from Apple's macOS 27 UI Kit, is in the [macOS design reference](design/macos/README.md).

## Pair and discover

Open the daemon’s invitation or paste it into Pair. Review the advertised server address before confirming enrollment. Keep the saved identity across launches. A pairing error lets you dismiss an invalid or expired link. For a revoked device, obtain a new invitation from the server operator. Successful pairing displays the server’s supervisors, including stopped CLI apps. Pair additional servers with **Add Server**. Under **View**, choose **Tree** or **List**, then group by **Server**, **Host**, or **CLI Name**. An offline server offers **Retry**.

## Mac windows

Server sections contain hosts and their CLIs. Click a CLI to work in its terminal. Type a program name in Search to filter the sidebar. Choose tree, list, or grouping from the toolbar or View menu. Secondary-click a CLI to open it in a tab or separate window, manage its run, inspect its ports, or open its workspace in Finder. Keyboard commands apply to the front window. The terminal subtitle identifies the host and server. The status bar shows connection state, measured round-trip latency and I/O mode. Both Apple apps follow the system’s Light or Dark appearance.

Use **File → Paste Invitation** to review the address and identity before pairing. Select a server and press Return to rename it inline. Return saves the name and Escape cancels. **Server → Remove Server…** forgets that pairing on this Mac and removes its Finder entries. Server-side revocation is an operator action. After revocation, an open terminal keeps its last output and offers **Pair Again…**.

## Touch-only operation on iPhone and iPad

Choose a session in the sidebar when space permits or navigate from the compact session list. Switching preserves open attachments. Choose **Session → Detach** to close the selected attachment. The terminal follows the system appearance and scales its monospaced text with Dynamic Type. In an attached terminal, open **Session → Keys** for Escape, arrows, Control-letter combinations, and the onscreen keyboard. The selected run stays open across keyboard changes, rotation, iPad resizing, and Duo folding. Use hardware keyboard and pointer input where available.

## Terminal and session controls

Pipe and PTY sessions both open in SwiftTerm. Keystrokes go straight to the child, including before Return. Pipe sessions display stdout and stderr together in arrival order, send LF on Return, and let the child control echo. PTYs receive terminal resize events. **Send EOF** closes pipe input for every viewer. Detaching leaves the program running; a stopped managed program exposes its most recent retained result.

On Mac, use the window toolbar for lifecycle actions. On iPhone and iPad, open **Session**. **Restart…** and **Start…** present the current executable and arguments together in a highlighted command field. Use quotes for spaces and `""` for an empty argument. The Go client and native apps use the same parser. Cancel keeps the current run; confirming launches the reviewed command and attaches to its replacement. **Stop…** identifies the affected program before ending it for all viewers. One-shot supervisors offer attachment only.

**Ports** opens a trailing inspector on Mac and wide mobile layouts, or a sheet in compact layouts. Toggle a named TCP or UDP export, then copy its local listening address. Closing the inspector leaves active listeners running. Each tab or window shows the forwards for its selected session. Windows showing the same session share its switch state and local listener address.

## Finder workspaces

To enable CLIStream on this Mac, select **CLIStream** under **Locations** in the Finder sidebar and click **Enable**. You can also open **System Settings › General › Login Items & Extensions**, select **By Category**, click the info button beside **File Providers**, and turn on **CLIStream**. Return to your session and choose **Show in Finder**. [Apple’s guide to extension settings](https://support.apple.com/guide/mac-help/mtusr003/mac)

**Show in Finder** opens the selected program under **CLIStream › server › host › program**. The system downloads file contents on demand. Finder and ordinary document apps can create, save, move, rename and remove items in a shared folder. Host-side edits are picked up by the provider. Workspaces belong to the supervisor, so stopping and restarting a managed child preserves the folder. Legacy peers and supervisors started with `--files=FALSE` omit this action. The Go CLI continues to provide its own workspace commands on Linux and macOS. [Apple File Provider](https://developer.apple.com/documentation/fileprovider)

Finder checks paired servers independently. If one goes offline, its cached entries remain visible and you can continue opening and editing workspaces on connected servers.

While the extension is running, it checks previously browsed folders for host-side changes about every ten seconds. It also refreshes on enumeration, including after reconnecting. Concurrent document saves preserve the remote original and a conflicted copy, or return a version conflict when the editing app requests one.

After removing the last file, Finder can delete the empty folder even if its displayed version predates that file removal. The provider verifies that the folder still has the same identity and sends a version-checked, nonrecursive delete. A new child on the host prevents the deletion.

## Future file access

The iPhone, iPad and Duo Files extension is deferred. These apps use the shared terminal and session controls described above.

## Acceptance

Drive multi-server pairing, grouping, tab and session switching, terminal input and output, lifecycle requests, completed runs and port forwards through the production user interfaces. Include simultaneous clients, touch-only navigation, keyboard and pointer input, accessibility, and live resizing on Mac, iPhone, iPad and iPhone Duo. Record observed results and outstanding scenarios in the verification record.
