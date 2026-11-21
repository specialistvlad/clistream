# Native Apple clients

CLIStream has a dedicated macOS application and a universal iPhone and iPad application. Both target OS 27. Build with Xcode 27.1 or later for iPhone Duo. The Go daemon and supervisor run on the hosts that own the CLI apps.

## Where to change the interface

`apple/Desktop` owns AppKit window tabbing and the Finder-style browser. `apple/Mobile` owns compact and split-view navigation. The shared views in `apple/App` display a session, edit commands and forward user actions to a per-tab `Connection`. Each connection owns its draft, terminal renderer, stream and port forwards. Closing one tab releases only that tab’s resources.

`Core.swift` translates actions into requests to the Go bridge in `apple/Bridge`. The bridge owns native handles. Supervisor discovery, run output and QUIC attachments live in `internal/client`, where the Go CLI can use them as well. Layout changes belong in Swift and use the same actions as existing controls.

## Pair and discover

Open the daemon’s invitation or paste it into Pair. Review the advertised server address before confirming enrollment. Keep the saved identity across launches. An expired invitation, revoked identity, or unreachable server needs a visible explanation. Successful pairing displays the server’s supervisors, including stopped CLI apps.

## Mac windows

The Servers sidebar separates the broker from the computers running supervisors. Select a CLI to work in its terminal. Change the sidebar layout with View or open a CLI in its own tab or window through the row’s context menu. The terminal subtitle identifies its host and server. The status bar shows connection state and measured round-trip latency to the selected broker.

## Touch-only operation on iPhone and iPad

Choose a session in the sidebar when space permits or navigate from the compact session list. Switching preserves open attachments. Detach closes the selected attachment. Keep touch controls reachable while typing and preserve the selected run across keyboard changes, rotation, iPad resizing and Duo folding. Use hardware keyboard and pointer input where available.

## Terminal and session controls

PTY sessions use native SwiftTerm views. The terminal sends input and valid grid sizes to the supervised child. Pipe sessions offer a text field, Send, explicit EOF and separately selectable stdout and stderr. Managed supervisors support stop, start, restart, and editing the executable and its arguments. Each argument has its own editable field so spaces and empty arguments survive the round trip. Stopped sessions expose their most recent retained result. Attach-only sessions expose terminal interaction and detach.

Only exported TCP and UDP services appear as forwards. Starting a forward displays its local loopback address. Stopping it releases the listener without ending the session.

## Future file access

The Go CLI supports remote workspace operations today. Apple file access is deferred to a replicated File Provider exposing a location in Finder and Files. [Apple File Provider](https://developer.apple.com/documentation/fileprovider)

## Acceptance

Drive multi-server pairing, grouping, tab and session switching, terminal input and output, lifecycle requests, completed runs and port forwards through the production user interfaces. Include simultaneous clients, touch-only navigation, keyboard and pointer input, accessibility, and live resizing on Mac, iPhone, iPad and iPhone Duo. Record observed results and outstanding scenarios in the verification record.
