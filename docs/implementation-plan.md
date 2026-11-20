# Implementation and acceptance plan

1. Establish TLS identities, reusable three-minute invitations and mandatory bounded compression.
2. Connect the daemon and its local administration socket to a session registry and multiplexed QUIC relay.
3. Implement process drivers, terminal fan-out, local interaction and explicit port exports.
4. Share the client library between the Unix CLI and a native Apple terminal.
5. Add the optional shared-folder path to supervisor startup. Use the supplied folder for browsing and transfers, defaulting to the supervisor's working directory at launch. The file service already supports streamed downloads and no-replace uploads. Test explicit and default folder selection from the same launch directory on macOS and Linux, and verify the files through the CLI and Apple clients.
6. Package checksummed build artifacts for amd64 and arm64 Linux and macOS. Sign both Apple archives, attempt TestFlight upload and record external release blockers.
7. Exercise both platforms, two simultaneous clients, unauthorized access, replay, port exports and terminal behavior. Record executed checks and release gates in verification.md.
8. Run app-hosted and UI tests on iPhone and iPad simulators and native macOS. Verify both target-specific drivers, repeatable file-picker round trips, terminal rendering and forwarded sockets.
9. Attach the three Apple clients and a CLI client concurrently to one child. Exercise packet loss, delay, address changes and a lost handshake. Record the commands, results and remaining provider and distribution gates in the verification record.
10. Implement and verify the native Apple session and workspace navigation. Exercise keyboard focus, terminal mouse reporting, Finder and Files interactions, adaptive layout and accessibility on each supported form factor. Run every mobile workflow by touch alone on both iPhone and iPad simulators and record the gesture outcomes. [Design acceptance criteria](apple-client-design.md#design-acceptance)

## Server activity logs

The Linux and macOS daemon records broker events to standard error and a bounded private JSON-lines file by default. Global flag `-no-log-file` selects standard error alone. The broker assigns connection IDs, observes roles from session operations and waits for close records on shutdown. Supervisors, Go clients and Apple clients use their existing wire operations. All operator controls belong to the daemon. The Apple interaction design and client UI therefore require no logging controls.

Compare default file and stderr records byte for byte and verify foreground pairing with live console events. Exercise both destinations, concurrent records, rotation, write failure and payload privacy with race-enabled Go and CLI tests on both systems. Use the four-container deployment for separate Linux supervisors and the shared-session harness for simultaneous native Mac, iPhone, iPad and CLI participants. [Operator guide](deployment.md#server-activity-logs) · [Execution record](verification.md)

## Pairing and launch status on November 20, 2026

The daemon and Go CLIs implement reusable v2 invitations and a shared default identity. Real-daemon tests cover both pairing directions, concurrent first use, profile migration, conflicting saved servers, revocation and restart. The generic supervisor runs the supplied program directly. The operator can opt into version-aware Claude or Codex behavior.

Apple v2 acceptance remains open. Update the remaining mobile UI single-use assertions, then verify opening and pasting the same invitation on iPhone and iPad and enrolling the native Mac client. Record the server address shown for confirmation and verify session input and output after pairing. Physical-camera scanning and opening a link from Messages require a device run. [Verification record](verification.md)

Track acceptance against the eleven functional requirements and the engineering checks in the [requirements audit](test-results/requirements-audit.md).

## Interactive terminal and managed supervision · November 20, 2026

The Linux and macOS daemon, supervisor and CLI share the registry, lifecycle protocol, Unix process-group handling and terminal implementation. The Go client uses Bubble Tea v2 for terminal interaction, Bubbles for the command field, and x/vt for child output and input encoding. Native Mac, iPhone and iPad clients use the existing running-session protocol and receive an exit event for the old run. Management controls are delivered in the Go client. [Approved feature specification](terminal-client-design.md)

Run `make test e2e` on Linux and macOS, the four-host Linux deployment suite, and the compatibility harness with the previous binaries. Exercise terminal clear, alternate screen, resize, input, stale actions, argument round trips, loss of a restart acknowledgement, reconnection and one-shot status. Run `apple/test-shared.py` with both simulators and native macOS. All four viewers must observe the first exit and exchange input again on the distinct replacement run. [Execution record](verification.md)

The header and app-switching update is shared Go CLI code on Linux and macOS. Each switch uses the existing discovery and attachment operations, so the daemon, supervisor and native Apple apps require no changes. Run the renderer checks and the real-PTY `test_cycle_running_apps` in both platform suites. Verify next, previous, wraparound, input destination, Escape and return to the supervisor table.

The single-run flag aliases `-once` in the shared Linux and macOS supervisor. The Go client labels its detach action **Disconnect** for attach-only rows. The broker and native Mac, iPhone and iPad clients use existing session operations. Verify two viewers, reconnect to the same run, child EOF and status, and rejection of lifecycle requests. The following command editor extends managed Start and Restart.

## Editable restart command

Deliver the single-line editor in the shared Linux and macOS Go dashboard. Negotiate support in the broker and supervisor, validate the executable and driver on the supervisor host, and bind automatic attachment to the acknowledged run. Retain legacy managed actions and single-run disconnect behavior. Native macOS, iPhone and iPad use their existing run-bound attachments. Verify quoting, cursor editing, stale requests, launch errors and recovery, mixed-version peers, both Go platform suites, Linux multi-host deployment and four-client replacement on Apple.

Use the same widget on Linux and macOS. The daemon, supervisor and native Apple clients continue using the existing run protocol. Verify incremental redraws in a real pseudo-terminal, cursor position after each character, grapheme deletion, resizing and the argument vector received by the restarted child. Run both Go platform suites, mixed-version compatibility and the shared Apple session harness.

## Supervisor live terminal and reconnection

Use the shared Go viewport on Linux and macOS. Route local child output through the supervisor TUI only when stdin and stdout are terminals. Handle input, resize, running and stopped transitions independently of registration. Both supervisor modes retry temporary outages with backoff, publish before first launch, and retain the local process across broker restarts. The existing broker operations carry the state. Verify timing with a controlled clock and exercise outages, redirected I/O, permanent rejection, restricted input and legacy peers with real Go binaries. Native Apple implementation and verification are deferred at the operator’s request.
