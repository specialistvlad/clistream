# Implementation and acceptance plan

1. Establish TLS identities, single-use role-bound invitations and mandatory bounded compression.
2. Connect the daemon and its local administration socket to a session registry and multiplexed QUIC relay.
3. Implement process drivers, terminal fan-out, local interaction and explicit port exports.
4. Share the client library between the Unix CLI and a native Apple terminal.
5. Implement descriptor-rooted workspace browsing, no-replace uploads and streamed downloads. Cover interruption, symlinks, hard links, traversal and a renamed root.
6. Package checksummed build artifacts for amd64 and arm64 Linux and macOS. Sign both Apple archives, attempt TestFlight upload and record external release blockers.
7. Exercise both platforms, two simultaneous clients, unauthorized access, replay, port exports and terminal behavior. Record executed checks and release gates in verification.md.
8. Run app-hosted and UI tests on iPhone and iPad simulators and native macOS. Verify both target-specific drivers, repeatable file-picker round trips, terminal rendering and forwarded sockets.
9. Attach the three Apple clients and a CLI client concurrently to one child. Exercise packet loss, delay, address changes and a lost handshake. Record the commands, results and remaining provider and distribution gates in the verification record.
10. Implement and verify the native Apple session and workspace navigation. Exercise keyboard focus, terminal mouse reporting, Finder and Files interactions, adaptive layout and accessibility on each supported form factor. Run every mobile workflow by touch alone on both iPhone and iPad simulators and record the gesture outcomes. [Design acceptance criteria](apple-client-design.md#design-acceptance)

## One-command pairing and reconnect

1. Sign short-lived role-bound invitations and share redemption across selected listeners.
2. Display a copyable CLI command and QR code in an interactive terminal. Support replacement with **p** and the local administration socket.
3. Reconnect from the saved device profile and wait for, attach to, or select a session. Use the same invitation decoder for Apple confirmation.
4. Exercise concurrent redemption, tampering, expiry, terminal interaction and iPhone/iPad handoff. Verify tap-to-open from Messages on a physical iPhone with an updated build before claiming that workflow.

Track acceptance against the ten functional requirements and the engineering checks in the [requirements audit](test-results/requirements-audit.md).
