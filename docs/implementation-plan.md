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
