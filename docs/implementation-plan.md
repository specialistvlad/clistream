# Implementation and acceptance plan

## Delivery status

The clients support live sessions, managed commands, retained results, and named port forwarding. The Mac app provides writable Finder workspaces. The owner approved writable iOS Files workspaces as part of the mobile redesign. [Native client guide](apple-client-design.md) · [Verification record](verification.md)

## Apple design revision · November 24, 2026

Approved scope follows steps 2, 4, 5 and 7 of the [Mac handoff](design/macos/implementation-prompt.md) and [mobile handoff](design/ios/implementation-prompt.md). Use DesktopApp and DesktopSidebar for the Mac window, menus and first launch, DesktopTerminal for stopped and revoked output and Ports, and DesktopSheets and DesktopCommands for pairing, command review and key-window commands. Use MobileServers for iPhone, iPad and Duo navigation and first launch, MobileTerminal and TerminalKeyBar for adaptive terminal controls, and MobileSheets, MobileInvitations and MobileScanner for Ports, pairing and scanning.

Build sidebar and toolbars, immediate lifecycle and separate command editing, TCP Open, then sheets and alerts. Use the existing Swift connection and command parser. The Go daemon, supervisor, CLI and bridge on Linux and macOS already implement the required operations. The protocol, deployment guide and terminal-client behavior need no changes. Preserve existing Finder and Files behavior. Signed Files and standalone Files verification remain outstanding.

Compile each slice and both schemes after shared changes. Update native lifecycle, pairing, Ports, Mac command and adaptive mobile cases. Verify clicked-row and key-window routing, saved arguments, edits, TCP browser targets, and the shared native and Go session on hosted runners. Capture each revised board in both appearances, including Duo poses. Reconcile README, native guide, requirements and security coverage and record the hosted results. Push to main after local verification.

The follow-up audit identified four interface corrections. Render standard title-and-icon labels in Mac menus, use a native button for Open in Mac Ports, give GitHub a separate placement in the iPad sidebar toolbar, and hide managed run controls for mobile attach-only or revoked sessions. The iPad and Duo inner sidebars use the titleless toolbar in the design board. The iPhone and Duo outer layouts retain their Servers title. These view changes reuse the existing shared connections and URL handling. Linux and macOS Go binaries, protocol, credentials, deployment and workspace behavior require no changes.

Compile both Apple schemes and exercise the Mac Session and CLI contextual menus, Mac TCP and UDP forwarding controls, iPad sidebar and attach-only terminal, and compact Phone and Duo layouts on hosted runners. Use the Mac gallery to compare rendered menu symbols and the iPad gallery to compare toolbar groups. Keep the current native guide, README and requirements behavior and record the verification results.

Compact iPhone and Duo navigation now opens a CLI through an explicit row action. This avoids relying on split-view selection changes to navigate when the same row is selected again. Regular-width iPad and Duo keep the native selection link. macOS, the Go daemon, supervisor and CLI on macOS and Linux, protocol and deployment need no changes. The hosted two-server case checks that the restored terminal contains the first server’s output before sending a new marker to it. Record the focused and matrix results in the verification record.

The CI follow-up moves mobile keyboard focus to terminal mounting, scales Retry explicitly with Dynamic Type, and improves hosted synchronization for Mac window changes, shared PTY focus, iPad server selection and scanner dismissal. Go commands, daemon, supervisor, deployment and protocol on Linux and macOS keep the same policies. Current feature guides already describe the affected user flows. Compile both Apple schemes, run focused Mac and mobile regressions on GitHub, and record remaining Files and simulator failures in the verification record. Keep the mobile TestFlight packaging without Files while App Group signing is unavailable.

Remove the dedicated Paste button from Add Server on iPhone, iPad and Duo. Keep the editable invitation field and its system edit menu, the scanner’s Paste fallback, and explicit pairing confirmation. This only changes the mobile sheet, so macOS and the Go daemon, supervisor and CLI on macOS and Linux retain their current behavior. The iOS spec, canvas and native guide cover the revised entry points. Verify scanner Paste, manual entry and absence of the dialog action on hosted mobile runners. Record the matrix status and TestFlight build in the verification record.

## Universal iOS redesign and Files · November 23, 2026

| Screens in the design | Current view | Target |
| --- | --- | --- |
| iPhone Servers, dark swipe, Duo outer Servers | WorkspaceSidebar | Compact inset grouped list, searchable CLI names, server and host sections, lifecycle swipe actions |
| iPhone terminal, Duo outer terminal, iPad main, Duo inner main | WorkspaceTerminal, SessionPane, LiveTerminal | System navigation and adaptive toolbar, floating terminal input accessory, regular-width status in the navigation subtitle |
| Session menu, Restart, Stop | SessionActions and CommandSheet | ControlGroup, raw command sheet and run-specific confirmation |
| Compact Ports, iPad inspector, Duo inner inspector | PortsPresentation and PortList | System sheet or trailing inspector bound to the same live forwards |
| Add Server, QR scanner, first launch, pairing error | BrowserView and PairingPresentation | Native empty state, VisionKit scanner, masked invitation review and explicit Pair |

Build the compact list, regular sidebar, terminal toolbar, key bar, menu, sheets, scanner, alerts, then the mobile File Provider. Keep desktop behavior intact and reuse the existing signed-invitation, terminal, forwarding and workspace protocols. The daemon, supervisor and CLI on Linux and macOS require no wire or behavioral change. The iOS app and extension share enrollment through an App Group and preserve existing device keys. **Show in Files** uses the selected program’s path under Files → Browse → CLIStream when a public direct handoff is unavailable. The owner approved including the current formatting of the shared Info.plist with its camera usage string. On November 23 the owner approved Apple’s native **Paste** label on the welcome screen and both pairing sheets. Pasting still requires explicit review and **Pair**.

Build each slice locally, including the Mac scheme after shared changes. Run UI automation and screenshot capture exclusively in the hosted macOS, iPhone, iPad, Duo and shared-session suites. Check live terminal bytes, swipe target identity, forwarding, scanner review, Files operations and credential migration. Compare all 17 design screens in both appearances and the Duo across three poses. Record physical camera verification separately. Review the README, platform, protocol, security, deployment and requirements guides and publish verification evidence. [iOS reference](design/ios/spec.md)

## Native Mac workspace and Finder · November 23, 2026

Use the approved screens and exact copy on macOS. Build the sidebar, toolbar, terminal, inspector, commands, sheets and server actions in that order. Keep mobile presentation in its existing target. [Screen specification](design/macos/spec.md)

| Design screen | Existing implementation | Target |
| --- | --- | --- |
| Main window in light and dark appearance | DesktopBrowser, WorkspaceSidebar, WorkspaceTerminal and SessionActions | Native split view, selectable sidebar, unified toolbar, terminal and status bar, trailing Ports form |
| Server context menu and inline rename | WorkspaceSidebar disclosure label and rename sheet | Server section menu and focused inline text field |
| First launch | DesktopBrowser empty detail | ContentUnavailableView with Add Server and Paste Invitation |
| Access revoked | Workspace watcher and terminal connection | Retained output, disconnected status and Pair Again action |
| Pair sheet, empty and filled | PairingPresentation | Native sheet with masked invitation, server address and identity review |
| Pairing error | PairingPresentation alert | System alert with the invitation recovery message |
| Restart sheet and Stop alert | SessionActions and CommandSheet | Focused highlighted command field and destructive confirmation |
| Remove Server alert | New server action | Destructive confirmation, followed by local profile removal |
| Menu bar | DesktopApp sidebar commands | File, Edit, View, Session and Server menus targeting the key window |

The owner selected read and write access in Finder for this release. Share the Go workspace operations between Linux and macOS, negotiate the provider capability with the daemon, and keep a managed workspace reachable across child exits. Preserve CLI behavior. Embed a replicated File Provider in the Mac app, migrate existing enrollments, and resolve each Show in Finder action to the selected workspace. Keep the mobile terminal, pairing, and layout on their existing paths.

Verify conditional writes, conflicts, interrupted transfers, confinement, revocation and old-peer interoperability in Go. Test each Mac design state in light and dark appearance and exercise the real Finder against both supported supervisor platforms. Run the iPhone, iPad, Duo and shared-client suites for regressions. Collect UI evidence only on isolated runners. Update the README, requirements, native guide, protocol, security, deployment, requirements audit and verification record as the changes land.

## Original implementation sequence

1. Establish TLS identities, reusable three-minute invitations and mandatory bounded compression.
2. Connect the daemon and its local administration socket to a session registry and multiplexed QUIC relay.
3. Implement pipe and PTY process execution, terminal fan-out, local interaction and explicit port exports.
4. Share the client library between the Unix CLI and a native Apple terminal.
5. Add the optional shared-folder path to supervisor startup. Use the supplied folder for browsing and transfers, defaulting to the supervisor's working directory at launch. The file service already supports streamed downloads and no-replace uploads. Test explicit and default folder selection from the same launch directory on macOS and Linux, and verify the files through the CLI and Apple clients.
6. Package checksummed build artifacts for amd64 and arm64 Linux and macOS. Sign both Apple archives, attempt TestFlight upload and record external release blockers.
7. Exercise both platforms, two simultaneous clients, unauthorized access, replay, port exports and terminal behavior. Record executed checks and release gates in verification.md.
8. Run app-hosted and UI tests on iPhone and iPad simulators and native macOS. Verify repeatable file-picker round trips, terminal rendering and forwarded sockets.
9. Attach the three Apple clients and a CLI client concurrently to one child. Exercise packet loss, delay, address changes and a lost handshake. Record the commands, results and remaining provider and distribution gates in the verification record.
10. Implement and verify the native Apple session and workspace navigation. Exercise keyboard focus, terminal mouse reporting, Finder and Files interactions, adaptive layout and accessibility on each supported form factor. Run every mobile workflow by touch alone on both iPhone and iPad simulators and record the gesture outcomes. [Design acceptance criteria](apple-client-design.md#acceptance)

## Server activity logs

The Linux and macOS daemon records broker events to standard error and a bounded private JSON-lines file by default. Global flag `--log-file=FALSE` selects standard error alone. The broker assigns connection IDs, observes roles from session operations and waits for close records on shutdown. Supervisors, Go clients and Apple clients use their existing wire operations. All operator controls belong to the daemon. The Apple interaction design and client UI therefore require no logging controls.

Compare default file and stderr records byte for byte and verify foreground pairing with live console events. Exercise both destinations, concurrent records, rotation, write failure and payload privacy with race-enabled Go and CLI tests on both systems. Use the four-container deployment for separate Linux supervisors and the shared-session harness for simultaneous native Mac, iPhone, iPad and CLI participants. [Operator guide](deployment.md#server-activity-logs) · [Execution record](verification.md)

## Pairing and launch status on November 20, 2026

The daemon and Go CLIs implement reusable v2 invitations and a shared default identity. Real-daemon tests cover both pairing directions, concurrent first use, profile migration, conflicting saved servers, revocation and restart. The supervisor runs the supplied program directly.

At this milestone Apple v2 acceptance was pending. Subsequent native UI runs verified reusable invitations on Mac, iPhone and iPad. The tests confirm the displayed server address and exchange input and output after pairing. Physical-camera scanning and opening a link from Messages require a device run. [Verification record](verification.md)

Track acceptance against the eleven functional requirements and the engineering checks in the [requirements audit](test-results/requirements-audit.md).

## Interactive terminal and managed supervision · November 20, 2026

The Linux and macOS daemon, supervisor and CLI share the registry, lifecycle protocol, Unix process-group handling and terminal implementation. The Go client uses Bubble Tea v2 for terminal interaction, Bubbles for the command field, and x/vt for child output and input encoding. Native Mac, iPhone and iPad clients use the existing running-session protocol and receive an exit event for the old run. Management controls are delivered in the Go client. [Approved feature specification](terminal-client-design.md)

Run `make test e2e` on Linux and macOS, the four-host Linux deployment suite, and the compatibility harness with the previous binaries. Exercise terminal clear, alternate screen, resize, input, stale actions, argument round trips, loss of a restart acknowledgement, reconnection and one-shot status. Run the shared-session acceptance harness with both simulators and native macOS. All four viewers must observe the first exit and exchange input again on the distinct replacement run. [Execution record](verification.md)

The header and app-switching update is shared Go CLI code on Linux and macOS. Each switch uses the existing discovery and attachment operations, so the daemon, supervisor and native Apple apps require no changes. Run the renderer checks and the real-PTY `TestCycleRunningApps` in both platform suites. Verify next, previous, wraparound, input destination, Escape and return to the supervisor table.

The single-run option is `--once` in the shared Linux and macOS supervisor. The Go client labels its detach action **Disconnect** for attach-only rows. The broker and native Mac, iPhone and iPad clients use existing session operations. Verify two viewers, reconnect to the same run, child EOF and status, and rejection of lifecycle requests. The following command editor extends managed Start and Restart.

## Editable restart command

Deliver the single-line editor in the shared Linux and macOS Go dashboard. Negotiate support in the broker and supervisor, validate the executable on the supervisor host, and bind automatic attachment to the acknowledged run. Retain legacy managed actions and single-run disconnect behavior. Native macOS, iPhone and iPad use their existing run-bound attachments. Verify quoting, cursor editing, stale requests, launch errors and recovery, mixed-version peers, both Go platform suites, Linux multi-host deployment and four-client replacement on Apple.

Use the same widget on Linux and macOS. The daemon, supervisor and native Apple clients continue using the existing run protocol. Verify incremental redraws in a real pseudo-terminal, cursor position after each character, grapheme deletion, resizing and the argument vector received by the restarted child. Run both Go platform suites, mixed-version compatibility and the shared Apple session harness.

## Supervisor live terminal and reconnection

Use the shared Go viewport on Linux and macOS. Route local child output through the supervisor TUI with `--interactive` when stdin and stdout are terminals. Handle input, resize, running and stopped transitions independently of registration. Both supervisor modes retry temporary outages with backoff, publish before first launch, and retain the local process across broker restarts. The existing broker operations carry the state. Verify timing with a controlled clock and exercise outages, redirected I/O, permanent rejection, restricted input and legacy peers with real Go binaries. Native Apple implementation and verification are deferred at the operator’s request.

## Supervisor activity feed

Use the shared Linux and macOS supervisor to suppress local child streams by default. Select the framed terminal or redirected child streams with `--interactive`. Reuse the daemon’s private rotating journal and record host-side operation boundaries in both modes. Verify local EOF and input isolation, remote access, concurrent journals, file failure, privacy, rotation, reconnect and legacy peers. The daemon and clients use the existing wire protocol. Native Apple changes and tests are deferred.

## Go housekeeping

Use a shared long-option parser and account-default identities for all three Linux and macOS commands. Use direct process execution for every executable and retire application-specific session metadata. Port Go-side acceptance, compatibility, deployment, requirements tracing and deterministic release tooling to Go and Make. Verify mixed-version boundaries, native packages, and a macOS CLI attached to a Linux supervisor. Native macOS, iPhone and iPad decoder and test-fixture repairs are deferred. The existing fixture imports and command invocations require migration before the Apple suites can run.

## Pairing link format

Generate the hostname-and-token URL in the shared Go identity package. The Linux and macOS daemon uses it for terminal commands, QR codes and scripted invitations. Both Go clients validate it, retain older invitation parsing and share the existing account enrollment flow. Verify DNS, IPv4, IPv6, custom ports, tampered tokens and host substitution, then exercise real enrollment from the displayed commands and across Linux and macOS. Native macOS, iPhone and iPad integration and verification remain deferred.

## Go command names, help and workspace controls

Ship `clistreamd`, `clistreams` and `clistream` on Linux and macOS. Group help by operator task and validate command operands before enrollment or connection. Open the chosen workspace once on the supervisor, default to the launch directory and reject file channels when disabled. Verify explicit and default roots, terminal and forwarded HTTP access with files disabled, release archive contents and saved identity reuse across the executable rename. Native macOS, iPhone and iPad tests and fixture migration remain deferred.

## Session-first Go commands

Use the existing revision-checked lifecycle requests on Linux and macOS. Resolve the session to a child run for file operations. Share atomic file publication and directory-descriptor checks between both Go builds. Negotiate completed-run retrieval separately, retain the most recent result in supervisor memory, and display finished text commands in a scrollable client viewport. Verify scripts, fast exits, explicit I/O modes, workspace boundaries, concurrent copies and custom logs in the Go suites and cross-host deployment. Native macOS, iPhone and iPad implementation and verification are deferred at the operator’s request.

## README walkthrough and animated demo

Use a drawn macOS desktop to show a Mac typing into Codex on a laptop and two clients receiving its output through the server. Switch the Mac to Claude Code while the iPad continues following Codex. Show direction with moving arrows on the connection lines. Place compatibility information beside the illustration. Review the quickstart and examples against the deployment, protocol, security and platform guides. Their runtime contracts and product requirements stay the same.

Exercise the documented commands on macOS and Linux and add a two-viewer case to the shared Go end-to-end suite. Both viewers should receive the same lines before one changes CLI apps. Run the package, end-to-end and cross-host deployment suites and inspect the GIF at desktop and phone widths. The walkthrough uses existing Go daemon, supervisor and client behavior. Native macOS, iPhone and iPad implementation and runtime verification are deferred.

## Native client parity

Use separate native macOS and universal iOS entry points, sharing the Go client, Apple bridge, session state and terminal rendering. Target OS 27. Selecting a CLI in the Mac sidebar activates its terminal. Its context menu can open it in a native tab or a separate window. Mobile adapts navigation to available space. Verify pairing, live input and output, independent switching, lifecycle, results, forwards and interruption through shipping UI on Mac, iPhone, iPad and iPhone Duo. Run Go acceptance on both supported hosts. Apple file handling is deferred to a File Provider extension. Preserve existing Go file operations and their security guarantees.

## Multi-server native workspace

Share enrolled profiles, catalog grouping and route selection across Apple platforms. The native Mac shell owns separate tabs and windows. The Go bridge reads active QUIC connection statistics. Existing daemon, supervisor and command-line behavior on Linux and macOS uses the same protocol. Exercise independent routing, offline recovery, saved profiles, window movement and display preferences in UI automation.

## Native terminal workspace and session controls

The desktop workspace placed sidebar and lifecycle actions inside the content area, presented command arguments as separate fields, and displayed pipe I/O as logs and a Send form. Move actions into the window toolbar, put port forwarding in an adaptive trailing inspector, and review a single highlighted command in a native sheet. Route both pipe and PTY attachments through the terminal renderer on all Apple platforms. Reuse the Go command grammar across macOS and Linux clients and the local Apple bridge. The daemon, supervisor, and wire operations already support these interactions.

Verify keyboard and touch input, return and paste, EOF, lifecycle review and cancellation, native windows, inspector toggling during active forwarding, and compact and wide layouts using the production apps. Compare shared pipe interactions with the Go CLI and refresh local screenshots after verification.
