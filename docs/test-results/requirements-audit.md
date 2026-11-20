# Requirements audit

Use the case names below to trace a product requirement to the executable that verifies it. The Apple UI suites launch the shipping interface under a separate bundle identifier. The fixture verifies output on the supervisor side, including terminal bytes and the contents of uploaded files. A green UI assertion alone cannot pass a fixture.

| Requirement | Automated evidence | What the check establishes |
|---|---|---|
| F1. Administer an existing daemon | Apple fixture, `testPairAndUseSession` | A second invocation of the daemon renders an invitation. Core Image decodes it and a client pairs to the broker that already owns the sessions. |
| F2. Device-key enrollment | `TestServerRequiresCertificateAndCompressionALPN`, `TestEnrollmentAndCompressedStreams`, `test_authorization`, `test_active_revocation` | Key possession, role checks, pinned broker identity, expiring single-use invitations and termination of an actively revoked device. |
| F3. Register independent remote supervisors | `make test-deployment` | Four network namespaces, two hostnames and two workspace mounts. A viewer addresses the intended child and retrieves that host's file. |
| F4. Share a running process | `test_shared_session`, `test_detach_and_replay`, `apple/test-shared.py` | Two CLI viewers and local input share one process. The separate Apple harness requires all three Apple apps and a fourth CLI to observe each participant's output. |
| F5. Pipes and terminals | `test_large_binary_stdio`, `test_pty_resize`, `testPTYRenderingKeyboardResizeAndExit`, `testTerminalInput` | Separate binary stdout and stderr, PTY control sequences, resize, rendered cells and process exit. UI terminal input is captured at the child. |
| F6. Versioned CLI drivers | `test_driver_catalog`, `test_detected_drivers`, `test_interactive_driver_terminals`, `test_unsupported_version_and_generic_override`, `testCLISpecificDrivers` | The supervisor publishes supported versions and execution modes. Both installed interactive CLIs produce PTY output. An unsupported version fails explicitly and the generic driver remains usable. |
| F7. Compressed multiplexed QUIC | `TestRecords`, `TestReject`, `TestEnrollmentAndCompressedStreams`, `test_files` | Record limits, mandatory compression on an authenticated stream and continued operation on another stream. A CLI stays attached across upload, download and observation of a local file change. |
| F8. Named TCP and UDP services | `test_ports`, `testPortForwarding`, `testPairAndUseSession` | Both protocols carry actual payloads. TCP supports half-close, UDP preserves datagrams, undeclared exports fail, and the Apple UI starts and stops listeners. |
| F9. Confined workspace transfers | `test_files`, `test_concurrent_uploads`, `TestPartialUploadIsRemoved`, `TestRootFollowsOriginalDirectory`, `testWorkspaceTransfers`, `make test-deployment` | Binary equality, no overwrites, interrupted transfer cleanup, distinct roots, descriptor anchoring, and rejection of escaping paths and links. The deployment test also probes the child shell inside an isolated Linux container. |
| F10. Native Apple interaction | `testPairAndUseSession`, `testTerminalInput`, `testTouchNavigationAndEditing` | Files and port controls, restoring a nested folder, picker cancellation, output from another viewer, touch selection and paste, folder back gestures, accessibility labels and PTY input. Consult the platform outcomes for individual executions. |

## Engineering checks

| Quality | Evidence and interpretation |
|---|---|
| Authentication and modularity | The broker accepts an `identity.Authenticator`. Version-specific mode selection lives in `internal/driver`. The client and Apple bridge share the transport, file and forwarding implementations. Go race tests, vet and end-to-end tests exercise these component boundaries. |
| Responsiveness and reliability | `test_keystroke_latency` measures median echo time. `test_network_path` injects loss, delay and a source-port change. `test_retry_lost_handshake` and `test_reconnect` check recovery. `TestSlowSubscriberCanReattachWithinBoundedHistory` checks replay after a subscriber falls behind. |
| Distribution | `make dist` creates four archives. Verify the checksums, unpack each one and run the acceptance suite against its three executables using `CLISTREAM_BIN`. Both signed Apple apps must pass code-signature and bundled MIT notice verification. |
| Apple delivery | Simulator deep links exercise the app after the camera has handed off a URL. A physical camera and installation through TestFlight require separate evidence. The previous distribution attempt returned Apple's missing-agreement error. |
| Source and evidence | The repository carries the MIT license. Published XCTest results contain case names and outcomes. Keep raw result bundles local because a diagnostic attachment may contain a short-lived pairing URL. |

## Reproduce a failed check

Run `make test e2e` for Go and CLI coverage and `make test-deployment` for the four-container deployment. On a Mac, run `make apple` once. Set `SIMULATOR_ID` to the iPhone or iPad simulator and run `apple/test-device.sh`. Use `apple/test-device.sh macOS` on a Mac desktop. Each suite starts its own broker, devices and child processes. The script prints a new result directory and checks the actual workspace files separately from XCTest.

To isolate a failing scenario set `CLISTREAM_TEST_SUITES` to `hosted`, `files`, `terminal` or, on iOS, `touch`. Set `CLISTREAM_APP_DRIVERS=1` to include the installed model clients. The four-client harness and live-provider options are documented in the [verification record](../verification.md).
