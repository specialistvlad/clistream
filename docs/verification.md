# Verification record

The acceptance suites launch the compiled daemon, supervisor and client with temporary device keys, a private administration socket and real QUIC connections. The Apple tests run the built app and its linked Go client. Test processes use a separate bundle identifier.

Each requirement has an executable check and a recorded outcome for the environments exercised. [Requirements audit](test-results/requirements-audit.md) · [Case-by-case execution evidence](test-results/requirements-audit.json)

## One-command pairing on November 20, 2026

| Environment | Checks |
|---|---|
| macOS CLI | A fresh interactive daemon printed a shell-safe command. A second terminal replaced its invitation with **p**. The CLI rejected the old link, enrolled with the replacement, waited for the first session, reconnected using its saved key, and offered a numbered choice with two sessions. Background startup kept invitations out of redirected output. |
| Broker and Linux deployment | Race-enabled Go tests checked signature tampering, expiry against the server clock and concurrent redemption of two signed endpoints for one device slot. Four Linux containers exercised loopback and a selected network interface with the compiled clients and supervisors. |
| iPhone 17 Pro and iPad mini simulators, iOS 26.4.1 | XCTest opened a link decoded from the daemon’s QR code, checked the displayed server address, canceled, pasted the same link and paired. Reusing the link failed. A separate test waited out the full three-minute lifetime before attempting enrollment with an unused link. |
| Native macOS | App-hosted XCTest exercised the Go bridge and device enrollment against a running daemon. |

Run `make test e2e` for the Go and CLI checks and `make test-deployment` for Linux. For each mobile form factor, run `make apple` and then `SIMULATOR_ID="$DEVICE_ID" CLISTREAM_TEST_SUITES="pairing pairing-expiry" apple/test-device.sh iOS`. Use `CLISTREAM_TEST_SUITES=hosted apple/test-device.sh macOS` for the native Mac checks.

Opening a custom link directly on a simulator exercises the app’s URL handler. Tapping a message containing that link and scanning with a physical camera still require a device with the updated app installed.

## Requirements audit on November 19, 2026

| Executed check | Result |
|---|---|
| Go on macOS arm64 | Race-enabled package tests and vet passed. The trace check resolved test references for F1–F10 and N1–N7. |
| Real daemon, supervisor and CLI on macOS | 20 of 21 passed. The live Anthropic case was skipped in this run. A separate real provider request reached the account's weekly usage limit. Both interactive CLI executables, the emulated Claude exchange and live Codex passed. |
| Packaged executables | Each Darwin architecture passed 19 cases and skipped the two live-provider cases. Each Linux architecture passed 16 and skipped five optional model-client cases. All four archives matched SHA-256 and carried the MIT notice. Linux amd64 ran under emulation. |
| Network interference | 2,232 verified round trips in 120 seconds with 450 intentionally dropped datagrams and a mid-session source-port change. The separate loopback test measured a median key echo of 2.31 ms. |
| Four Linux containers | Two supervisors registered independently. Each returned its own workspace data. File-service traversal failed, the isolated shells could neither read nor modify the host sentinel, and the original bytes survived. |
| Native Mac app on macOS 27.2 | All seven cases passed across targeted runs. The driver case used the installed Claude Code and Codex executables. The two desktop UI cases exercised keyboard-only attachment, return from Files, native Open/Save panels, pointer input and window resizing. |
| Four simultaneous clients | Mac, iPhone, iPad and CLI each sent a distinct line to the same child. All clients observed each line on both output channels. This passed locally on the audit revision and on a GitHub-hosted Mac. [Hosted execution](https://github.com/specialistvlad/clistream/actions/runs/44677340259/job/136092198839) |
| Dependency scan | `govulncheck` reported zero reachable known vulnerabilities. It identified 11 module-level advisories outside the application's call paths. |
| Signed Apple build 2611200557 | Both archives passed deep, strict code-signature verification and contained the original MIT notice. The Mac executable contains arm64 and x86_64. |

The iPhone 17 Pro and iPad mini (A17 Pro) simulators on 26.4.1 each passed the five app-hosted cases, including both installed CLI drivers, plus the file, terminal, touch-editing and scroll scenarios across targeted runs. The scroll case registers enough real CLI processes to fill the session list. It verifies that a swipe changes the visible rows, that a terminal swipe exposes earlier numbered lines, and that showing and hiding the keyboard leaves the child's input empty.

On iOS 27.0, XCTest also enabled VoiceOver on iPhone and iPad and read the spoken labels for the nested folder, Upload and Done. On the compact iPhone layout, the file actions had to be part of the current navigation page for VoiceOver to reach them from the file list. [Apple VoiceOver testing API](https://developer.apple.com/documentation/xcuiautomation/xcuivoiceoverservice)

The Mac UI suite opens the pairing form with Command-K, enters the decoded invitation into its secure field, selects the first pipe from the sidebar using Command-1 and the down arrow, then sends a command. The supervisor receives a second command after Files is dismissed. A separate test opens the pairing link directly, clicks the PTY, resizes the window and checks the cell received by the child.

The iPhone and iPad scenarios use real system UI for copy/paste, keyboard input, file pickers and navigation. The touch test extends a selection using the system handle, sends the pasted text to the remote process, cancels a partial back swipe, completes a second swipe and runs Apple's accessibility audit. The fixture compares the two distinct pasted strings at the child. The terminal fixture checks the reported cell and records window sizes in the child process. The permissions of an ordinary supervised host process and those of the isolated Linux deployment differ as described in the [security model](security.md).

## Earlier baseline on November 19, 2026

| Suite | Environment | Result |
|---|---|---|
| Go tests with the race detector and `go vet` | Go 1.27.4, macOS 27.2, arm64 | Passed |
| Three-binary acceptance, live Codex and two-minute network test | macOS arm64 | 17 passed, 1 skipped |
| Packaged macOS binaries | arm64 and translated amd64 | 16 passed and 2 live-provider cases skipped on each |
| Packaged Linux binaries | arm64 Linux VM and amd64 binaries under emulation in the VM | 14 passed and 4 optional model-CLI cases skipped on each |

All four distribution archives passed SHA-256 verification and contained a byte-for-byte copy of the project's MIT license. The Linux containers ran with external networking disabled. Each broker, client, supervisor, child process and emulated network path communicated inside the container.

The iPhone 17 Pro and iPad mini (A17 Pro) simulators ran OS 26.4.1. Each passed all six Apple tests. The native macOS 27.2 application also passed all six. All three suites enabled both installed model CLIs. Xcode 27.1 recorded the device, case name, duration and outcome for each run. [Machine-readable test results](test-results/acceptance.json)

In a separate concurrent run, all three Apple apps and a fourth, command-line client attached to one supervised process. The CLI waited for input from all three Apple clients before sending its own line. Each Apple client checked all four lines on both output channels before detaching. The CLI and the supervisor checked the same bytes locally. [Concurrent test suite](../apple/test-shared.py)

A fresh Go vulnerability scan reported zero reachable known vulnerabilities. The scanner listed eleven advisories in modules in the dependency graph. Earlier interactive startup checks also captured the PTY screens of both supported CLI executables.

## Feature coverage

| Feature | Evidence |
|---|---|
| Pairing and authentication | The suite enrolls unique keys through role-specific invitations. It checks single use, expiration, server pin mismatch, unregistered keys, role restrictions, durable enrollment, and revocation during an active session while another viewer continues. Apple Core Image decodes the daemon's rendered QR, and the UI tests open that decoded URL and tap **Pair device**. |
| Pipe and PTY | The local terminal and remote clients exchange input, stdout and stderr with one child. The suite checks exit status, a 4 MiB binary round trip, PTY escape sequences, remote resize, detach, replay and reconnection after a broker restart. Loopback median single-key echo was 0.37 ms in one 21-sample run. |
| Compression | Negative tests exercise mandatory compression during TLS negotiation and again on an enrolled connection. A raw list request receives an error, then a compressed list succeeds on a different stream of the same connection. Codec tests also reject malformed, oversized and unknown-field messages. A 30,800-byte repetitive log sample used 2,047 application wire bytes. |
| Network interruption | For 120 seconds a UDP relay varied per-packet delay between 12 and 22 ms, dropped every 23rd datagram and changed the source port mid-session. The run checked 2,376 request/echo pairs byte-for-byte with 474 datagrams dropped. A separate test discarded all packets from the first handshake socket and verified recovery on a fresh socket. |
| Scoped file service | Tests compare uploaded and downloaded bytes, race two uploads to the same name, preserve an existing file and reject traversal, absolute paths, escaped symlinks and hard links. Filesystem tests exercise interrupted upload cleanup and replacement of the original directory pathname. |
| Port forwarding | TCP half-close and datagram boundaries, including empty and 1,400-byte UDP datagrams, are exercised through declared loopback exports. Apple-hosted tests exchange binary payloads through actual forwarded sockets. UI tests start and stop both forwarding controls. |
| Target-specific drivers | Claude Code 2.1.329 and Codex CLI 0.170.0 were detected by version. Remote clients supplied prompts, closed stdin, collected output and checked child exit status. Codex completed a live arithmetic request in a read-only ephemeral session. The real Claude executable completed nonce-based exchanges with a loopback Messages API emulator. A separate live Anthropic attempt reached the account's weekly usage limit. |
| Apple terminal and files | App-hosted tests drive SwiftTerm input and resize, inspect its rendered terminal buffer, and check exit status 23. A 240,019-byte upload/download is compared in the app and on the supervisor. The UI exports a random 150,013-byte file through the system picker and imports it into another directory. A separate CLI client verifies the Apple session output. |

The workspace boundary applies to file-service operations. The supervised command runs with its OS user's permissions. Use a VM or container with only the workspace mounted when the child process also requires filesystem isolation. The network results above describe an emulated path. QR decoding and deep-link handling were exercised without a physical camera.

## Reproduction

```sh
make test e2e
go test -v ./internal/wire ./internal/broker
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
CLISTREAM_LIVE_DRIVERS=1 CLISTREAM_SOAK_SECONDS=120 make e2e
make dist
(cd dist && shasum -a 256 -c SHA256SUMS)
```

Install the supported Claude Code version to enable the emulator test. Live Codex requires the supported CLI and a signed-in account. Set `CLISTREAM_LIVE_CLAUDE=1` to run the separate live Anthropic check when quota is available.

For iPhone and iPad UI checks, put AXe 2.1.1 on the test host’s PATH. The fixture taps a document row in the system Files picker and long-presses the rendered output through simulator touch input. CI verifies the release archive hash. The fixture sends USB keyboard usages for left Control and D to the designated simulator. The child must receive byte 0x04. [AXe simulator test tool](https://github.com/cameroncooke/AXe/releases/tag/v2.1.1)

On a Mac with Xcode, XcodeGen, Go and Python 3.10 or later, generate the project, list simulator IDs and run the suites. Setting `CLISTREAM_APP_DRIVERS=1` enables both installed CLI drivers in each Apple app. With that flag unset, the terminal, filesystem, forwarding and UI suites run independently of provider accounts.

```sh
make apple
xcrun simctl list devices available
CLISTREAM_APP_DRIVERS=1 SIMULATOR_ID="$IPHONE_SIMULATOR_ID" apple/test-device.sh
CLISTREAM_APP_DRIVERS=1 SIMULATOR_ID="$IPAD_SIMULATOR_ID" apple/test-device.sh
CLISTREAM_APP_DRIVERS=1 apple/test-device.sh macOS
IPHONE_SIMULATOR_ID="$IPHONE_SIMULATOR_ID" IPAD_SIMULATOR_ID="$IPAD_SIMULATOR_ID" python3 apple/test-shared.py
```

GitHub Actions selects the iPhone 17 Pro and 13-inch iPad Pro (M5) on the iOS 26.4 runtime for the device UI suites. The simultaneous-client job uses the iPhone and iPad mini (A17 Pro) to reduce the graphics memory required by two live simulators.

The device suite prints its result directory under `apple/build/results-*`. Each scenario has its own XCTest bundle. The simultaneous-client harness launches a test-only build of the app on both simulators. Each mobile app writes to the same session and records the stdout and stderr received by its view model. The harness checks all four writers on both channels and saves one JSON report per mobile device alongside the Mac XCTest bundle under `apple/build/shared-*`. Open the bundle with Xcode or run `xcrun xcresulttool get test-results tests --path PATH.xcresult` for the individual outcomes.

## Distribution

The signed iOS and universal macOS archives for build `2611200557` passed `codesign --verify --deep --strict`. The license resource in each app was byte-for-byte identical to the repository MIT license. The two earlier TestFlight upload attempts reached App Store Connect and returned HTTP 403 with `FORBIDDEN.REQUIRED_AGREEMENTS_MISSING_OR_EXPIRED`. TestFlight installation remains unverified. The Account Holder can review pending terms in the Apple Developer account and App Store Connect. [Apple agreement guidance](https://developer.apple.com/help/app-store-connect/manage-agreements/sign-and-update-agreements/)
