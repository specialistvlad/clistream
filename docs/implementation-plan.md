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

## Pairing and launch status on November 20, 2026

The daemon and Go CLIs implement reusable v2 invitations and a shared default identity. Real-daemon tests cover both pairing directions, concurrent first use, profile migration, conflicting saved servers, revocation and restart. The generic supervisor runs the supplied program directly. The operator can opt into version-aware Claude or Codex behavior.

Apple v2 acceptance remains open. Update the older single-use assertions, then verify opening and pasting the same invitation on iPhone and iPad and enrolling the native Mac client. Record the server address shown for confirmation and verify session input and output after pairing. Physical-camera scanning and opening a link from Messages require a device run. [Verification record](verification.md)

Track acceptance against the ten functional requirements and the engineering checks in the [requirements audit](test-results/requirements-audit.md).
