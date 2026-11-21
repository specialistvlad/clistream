# Verification record

The acceptance suites launch the compiled daemon, supervisor and client with temporary device keys, a private administration socket and real QUIC connections. The Apple tests run the built app and its linked Go client. Test processes use a separate bundle identifier.

The audit maps requirements to existing checks and identifies pending coverage. [Requirements audit](test-results/requirements-audit.md) · [Case-by-case execution evidence](test-results/requirements-audit.json)

## README walkthrough and animated demo

On macOS and Debian Linux arm64, `make test e2e` passed. `TestTwoViewersSwitchIndependently` used two Go clients and two supervised CLI apps. Both clients saw `18 passed` and `19 passed`. After one switched back and typed `TODO` into the editor, the other received `20 passed` from the watcher. The cross-host deployment suite also passed.

A separate smoke check on each OS enrolled the supervisor using `clistreamd invite`, launched `/bin/sh`, exchanged shell output, uploaded and downloaded a file, compared its contents and exercised start and stop. The smoke checks used an ephemeral loopback port. The 18-second GIF was inspected at 640 and 360 pixels wide. The iPad is an illustration. Native app updates and runtime verification remain deferred.

## Interactive client frame with pipe children

On macOS and Linux arm64, `make test e2e` passed the race-enabled package tests, vet and 46 end-to-end cases. A real client PTY attached to a child that verified its stdin and stdout were pipes. The terminal capture showed the client header during streaming, after exit, while scrolling to the final stdout and stderr lines, and when reopening the result. The client stayed in the alternate screen throughout those transitions. The cross-host deployment suite also passed. Native app work and verification remain deferred.

## Session commands and completed output · November 20, 2026

On macOS arm64 and Linux arm64, `make test e2e` passed the race-enabled package tests, vet, requirements trace and 45 end-to-end cases. Both systems exercised immediate command replacement, stable session IDs, copying and renaming inside the workspace, atomic concurrent replacement, custom logging, and a fast command that produced 120 lines and exited with status 7. The macOS run also launched the installed Codex CLI with `--help`. The dashboard displayed the result and returned to an interactive shell in the same session.

`make test-deployment` passed across four Linux containers and with a native macOS CLI stopping, starting, restarting, editing and attaching to a Linux session. The historical-peer compatibility suite passed on macOS. Native app changes and verification on Mac, iPhone and iPad were deferred.

## Supervisor rename, grouped help and workspace controls · November 20, 2026

On macOS and Linux arm64, `make test e2e` passed the race-enabled package tests, vet, requirements trace and 43 CLI cases. The CLI cases checked help with an unpaired account, rejected retired spellings and invalid operands, and executed both pairing commands printed by the daemon. Mixed-version checks on both systems enrolled through the historical supervisor executable and launched `clistreams` with that account’s existing identity.

Two supervisors started from the same directory shared different files under the same remote name. Downloads and uploads reached the selected workspace, the child used the original launch directory, and the selected folder survived a managed restart and broker reconnect. Missing folders, regular files, FIFOs and conflicting options failed before launch. With files disabled, listing, upload and download failed explicitly while a client exchanged terminal input and fetched a page through a forwarded TCP port.

The four-host Linux deployment and macOS client to Linux supervisor checks passed. All four checksummed release archives contained the license, README and three executables (`clistream`, `clistreamd`, `clistreams`). Both native arm64 archives passed the 43-case acceptance suite. The amd64 archives were cross-built and inspected.

Native macOS, iPhone and iPad changes and tests remain deferred. The Apple fixture still invokes the retired supervisor name and flags. Its migration and native workspace verification remain pending.

## Hostname pairing URLs · November 20, 2026

The macOS and Linux arm64 runs of `make test e2e` passed. The new identity checks covered DNS, IPv4, IPv6, scoped IPv6 and custom ports. They verified the earlier v2 link form and rejected a modified host or token. The terminal test redeemed the displayed quoted link, replaced it with the daemon’s **p** command, rejected the superseded invitation and exchanged input with the paired supervisor. The four-container Linux deployment, native macOS CLI to Linux supervisor deployment, and macOS mixed-version suite passed. Native Apple pairing was deferred.

## Go command and tooling housekeeping · November 20, 2026

With Go 1.27.4 on macOS arm64 and Debian Linux arm64, `make test e2e` passed the race-enabled package tests, vet, requirements trace and 41 end-to-end cases. The command tests exercised double-dash options, help, retired option rejection, direct argument passing, automatic and explicit I/O modes, and account-default identity migration. The session listing omitted both retired metadata keys.

`make test-compatibility` ran on both platforms against binaries from `bd2d7ab` and `50cbcaa`. A previous Go client listed and attached to a current session. A current broker rejected an older supervisor’s descriptor, and a current CLI rejected an older daemon’s nonempty listing. A previous client exchanged data with the current supervisor through each older daemon. Deploy matching Go binaries for full functionality.

`make test-deployment` passed with four isolated Linux containers and with a native macOS CLI attaching to a Linux supervisor through a Linux daemon. Two builds of the four macOS and Linux amd64 and arm64 archives produced identical SHA-256 manifests. `make test-dist` verified each native arm64 archive and ran all 41 cases against its extracted binaries. The amd64 archives were cross-built and checksum-verified.

Native macOS, iPhone and iPad work was explicitly deferred. Their current session decoder requires the removed keys for a nonempty listing. The shared test runner imports the retired Python harness, the optional provider fixture imports the retired emulator, and the fixtures call removed options. Native verification requires corresponding decoder and test-tooling updates. The earlier Apple entries below describe their original builds.

## Returning home from an attachment · November 20, 2026

The Go dashboard attached to a live child, returned home with Ctrl-G and attached again. In PTY and pipe modes, the supervisor feed and journal recorded normal channel closures, and the broker journal reported matching outcomes. The reattached client exchanged input and output with the running child. The regression runs in `test_home_closes_terminal_channels`. A separate dashboard test observed a supervisor appear and disappear on successive refreshes, then checked that publication was logged and successful polls were absent from both stderr and the journal.

## Default supervisor activity feed · November 20, 2026

Real terminal and pipe tests verified that plain launches showed readable operations, kept child output off the host console, and left the child available for remote input after local typing and EOF. A foreground Ctrl-C check verified orderly shutdown and terminal restoration. The tests compared a file-download event in the feed and private journal, checked that payload and path markers were absent, verified distinct journals for concurrent supervisors, and continued supervision when the journal directory was unavailable. Interactive replacement and broker reconnects reused the same journal. The shared writer’s rotation and failure cases passed under the race detector.

The Linux deployment and mixed-version Go compatibility suites passed. Both CLI platform suites exercised 48 cases with two optional skips on macOS and five on Linux. Native Apple testing remains deferred.

## Local supervisor console and connection recovery · November 20, 2026

Real PTYs exercised cold startup with an unavailable broker, cancellation before launch, two broker outages, input while disconnected, reattachment to the same run, and publication of a child’s offline exit. The child’s launch record contained exactly one PID. Other cases verified editable remote restart in a read-only local view, clear and alternate-screen sequences, translated mouse input, paste, Ctrl-C, Ctrl-D, resize, attach-only exit during backoff, and rejection of a wrong server pin. A controlled Go clock checked the retry schedule through the 30-second cap, reset and cancellation.

The Go race and vet checks and CLI suites ran on macOS and Linux. The Linux deployment suite and mixed-version CLI checks covered multiple hosts and older peers. Native Apple implementation and testing remain deferred.

## Server console activity · November 20, 2026

The daemon test compared every default stderr event byte for byte with the file journal, including timestamps, from startup through shutdown. A foreground PTY showed a live pairing event. Closing a stderr pipe still allowed a client request and a clean shutdown with a final file record. Rotation, concurrent writers, file opt-out, file failure and payload privacy were also exercised. Both Go platform suites passed (39 CLI cases, with two optional skips on macOS and five on Linux), as did the Linux multi-host deployment and the shared Go, Mac, iPhone and iPad simulator session.

## Bubble Tea dashboard and command widget · November 20, 2026

A real PTY test typed ` --help` into a prefilled `codex` command and decoded successive terminal updates to verify the visible text and cursor after each key. A second PTY test corrected a character in the middle, resized and submitted the command. The replacement reported one argument, `--help`. The Go race tests also checked grapheme deletion, horizontal scrolling, the fixed header and emulator output. The macOS and Linux CLI suites each ran 38 cases, with two optional skips on macOS and five on Linux. Mixed-version compatibility and the shared Mac, iPhone simulator, iPad simulator and Go session passed.

## Single-line Start and Restart · November 20, 2026

The macOS and Linux `make test e2e` runs passed, including the Go race tests and vet. The CLI suite ran 37 cases per platform with two optional skips on macOS and five on Linux. The dashboard tests changed the executable, preserved empty and spaced arguments, reloaded the last command after broker reconnection, rejected a stale concurrent request and attached to the acknowledged replacement. Invalid syntax and a missing executable left the current run available. An executable with a missing interpreter failed after the old run ended, and correcting the draft successfully started another run.

The Linux multi-host deployment passed. Compatibility tests passed with pre-managed binaries from `bd2d7ab` and managed binaries from `50cbcaa`, exercising an older dashboard, supervisor and daemon independently. The four-client Apple harness observed the old exit on native macOS, iPhone and iPad simulators, and the Go CLI. The dashboard attached automatically and all four devices exchanged input on the replacement. [Apple execution record](test-results/acceptance.json)

## Single-run supervision · November 20, 2026

`make test e2e` passed on macOS arm64 and Debian Linux arm64. Of 37 CLI cases, macOS passed 35 with two optional provider skips, and Linux passed 32 with five optional driver or provider skips. The new two-viewer PTY case launched a shell with `-no-remote-restart`. One viewer disconnected, the other continued sending input, and the first reattached to the same run. Ctrl-D then ended the shell, both attachments ended, the supervisor returned status 23, and the session disappeared. A broker test verified that enrolled-device lifecycle requests against a single-run registration are rejected. Both previous-binary compatibility cases passed on macOS.

## Header and app switching · November 20, 2026

`make test e2e` passed on macOS arm64 and Debian Linux arm64. Of 36 CLI cases, macOS passed 34 with two optional provider skips. Linux passed 31 with five optional driver or provider skips. The real-PTY navigation case identified the receiving child after each switch among three running apps, checked wraparound, the previous-app shortcut, plain Escape delivery and Ctrl-G return to the table. The renderer test checked the two header rows during remote screen changes and verified output reached the bottom row. Both compatibility cases passed against binaries from `bd2d7ab`.

After removing the alternate previous-app shortcut, the macOS renderer tests and real-PTY navigation test passed. The active child received the raw Ctrl-^ byte, and Ctrl-[ still selected the previous app.

## Framed terminal and managed supervisors · November 20, 2026

| Environment | Executed check | Result |
|---|---|---|
| macOS 27.2 arm64, Go 1.27.4 | `make test e2e` | Race-enabled Go tests, vet and requirements trace passed. 33 CLI cases passed and two optional live-provider cases skipped. |
| Debian Bookworm arm64, Go 1.27.4 | `make test e2e` | Go tests, vet and requirements trace passed. 30 CLI cases passed and five optional driver or provider cases skipped. |
| Four Linux containers | `make test-deployment` | Two managed supervisors registered on separate hosts. Their session-specific input, workspace downloads and activity records matched the selected host. |
| macOS, binaries from `bd2d7ab` | `CLISTREAM_LEGACY_BIN=/path/to/previous/bin python3 tests/compatibility.py` | Both directions passed. The new dashboard attached through the previous daemon, the new supervisor returned the child status there, and the previous CLI and supervisor interoperated with the new daemon. |
| Native Mac, iPhone 17 Pro and iPad Pro 11-inch (M5) simulators, Go CLI | `apple/test-shared.py`, Xcode 27.1 | Four enrolled viewers exchanged input and observed all writers. A dashboard restart ended each old attachment with status 128. All four attached to the new run and repeated the exchange. Closing pipe input ended the replacement with status 0. |

The PTY harness sent alternate-screen and clear sequences, rapid output, a pasted detach byte, Ctrl-C, a mouse click and a resize. The child received the expected bytes, viewport-relative mouse position and dimensions. A separate test decoded the outer renderer’s output and checked the title and footer cells before and after scrolling and detaching. A stale concurrent restart was rejected. Losing the requesting client while the old run was awaiting an exit acknowledgement produced exactly one replacement. The CLI recovered to the table after broker restart. Other checks covered edited empty and spaced arguments, launch failure, cancellation, old file and tunnel references, argument privacy and local termination. The four new CLI cases also passed against race-instrumented macOS binaries.

Apple evidence comes from the simulator test host and app-hosted Mac XCTest. The live-provider skips require credentials or quota. The Linux driver skips require the supported Claude Code and Codex CLI executables. These are the remaining skips from the requested suites. [Feature specification](terminal-client-design.md) · [Machine-readable audit](test-results/requirements-audit.json)

## Server activity logging on November 20, 2026

| Environment | Executed checks and outcomes |
|---|---|
| macOS arm64, Go 1.27.4 | `make test e2e` passed the race-enabled Go suites, vet, requirements trace and 29 of 31 CLI cases. The two optional live-provider cases were skipped. |
| Linux arm64, Go 1.27.4, Debian Bookworm container | The same command passed the Go suites, vet, requirements trace and 26 of 31 CLI cases. Five optional cases require installed model CLIs or live provider access. |
| Four Linux containers | `make test-deployment` passed. The broker log identified two distinct supervisor keys and connections. Confirmed download events carried the session ID of the supervisor that served each host's file. |
| Native Mac, iPhone 17 Pro and iPad mini simulators, and CLI | `apple/test-shared.py` passed. Four distinct enrolled keys opened input channels on the same child and the log recorded a client disconnect for each connection. The hosted Mac case also exercised repeat redemption of the v2 invitation. The mobile pairing UI and physical-camera checks remain separate. |

File tests verified append across restart, mode 0600, concurrent JSON records, five-backup rotation, and fallback on open, write and rotation failure. CLI tests exercised live file and terminal operations with a failed log destination, stderr opt-out with and without an existing log, and private values on both output destinations. Connection tests covered busy refusal, enrollment timeout, peer error, active revocation, reconnect under the same fingerprint, exit status and orderly shutdown. [Executable checks for F11](test-results/requirements-audit.md)

## Folder selection update on November 20, 2026

The recorded file-transfer tests exercised the supervisor's startup working directory. Revised F9 also allows an explicit folder at startup. Implementation and testing of that option are pending. The acceptance check will launch two sessions from one directory, give one session a different folder, and compare what the CLI and Apple clients list, download and upload against the contents of each host folder. [Revised requirement](requirements.md#session-folder-selection)

## Shared pairing links on November 20, 2026

At commit `4571796`, the local Go race tests and vet passed. The CLI suite passed 25 of 27 cases. The two skipped cases require live Claude or Codex provider access. The Go race tests exercised concurrent enrollment on multiple listeners, signature tampering, server-clock expiry and replacement. The CLI suite used the exact link from the daemon terminal to publish a process and exchange input and output from a separate identity. Tests also covered revocation, re-enrollment, restart, both pairing directions, concurrent first use, legacy migration, conflicting servers and explicit state directories.

A separate upgrade check enrolled the previous client and supervisor, restarted the upgraded daemon on the same state and reconnected both original binaries. The updated Go tools also paired against the previous daemon under its role-specific, single-use rules. Apple acceptance for v2 is pending. The Apple UI pairing case still expects a v1 URL and a rejection on its second redemption. At that revision the hosted enrollment test also expected reuse to fail. The server logging run above verifies the updated Mac assertion. The historical results below cover the earlier invitation rules.

The hosted Go jobs passed on Ubuntu and macOS for the same commit. The four Apple jobs were still running when this record was updated on November 20. [CI run for 4571796](https://github.com/specialistvlad/clistream/actions/runs/44800568684)

## Previous v1 pairing checks on November 20, 2026

| Environment | Checks |
|---|---|
| macOS CLI | A fresh interactive daemon printed a shell-safe command. A second terminal replaced its invitation with **p**. The CLI rejected the old link, enrolled with the replacement, waited for the first session, reconnected using its saved key, and offered a numbered choice with two sessions. Background startup kept invitations out of redirected output. |
| Broker and Linux deployment | Race-enabled Go tests checked signature tampering, expiry against the server clock and concurrent redemption of two signed endpoints for one device slot. Four Linux containers exercised loopback and a selected network interface with the compiled clients and supervisors. |
| iPhone 17 Pro and iPad mini simulators, iOS 26.4.1 | XCTest opened a link decoded from the daemon’s QR code, checked the displayed server address, canceled, pasted the same link and paired. Reusing the link failed. A separate test waited out the full three-minute lifetime before attempting enrollment with an unused link. |
| Native macOS | App-hosted XCTest exercised the Go bridge and device enrollment against a running daemon. |

Run `make test e2e` for current Go and CLI checks and `make test-deployment` for Linux. To reproduce the v1 mobile checks, check out the v1 pairing revision (`a633846`). For each mobile form factor, run `make apple` and then `SIMULATOR_ID="$DEVICE_ID" CLISTREAM_TEST_SUITES="pairing pairing-expiry" apple/test-device.sh iOS`. Use `CLISTREAM_TEST_SUITES=hosted apple/test-device.sh macOS` for the native Mac checks.

Opening a custom link directly on a simulator exercises the app’s URL handler. Tapping a message containing that link and scanning with a physical camera still require a device with the updated app installed.

## Requirements audit on November 19, 2026

| Executed check | Result |
|---|---|
| Go on macOS arm64 | Race-enabled package tests and vet passed. The trace check resolved test references for F1–F10 and N1–N7. |
| Real daemon, supervisor and CLI on macOS | 20 of 21 passed. The live Anthropic case was skipped in this run. A separate real provider request reached the account's weekly usage limit. Both interactive CLI executables, the emulated Claude exchange and live Codex passed. |
| Packaged executables | Each Darwin architecture passed 19 cases and skipped the two live-provider cases. Each Linux architecture passed 16 and skipped five optional model-client cases. All four archives matched SHA-256 and carried the MIT notice. Linux amd64 ran under emulation. |
| Network interference | 2,232 verified round trips in 120 seconds with 450 intentionally dropped datagrams and a mid-session source-port change. The separate loopback test measured a median key echo of 2.31 ms. |
| Four Linux containers | Two supervisors registered independently and each returned the files in its working directory. The fixture also verified that its container mounts protected a host sentinel. That check describes the test deployment. |
| Native Mac app on macOS 27.2 | All seven cases passed across targeted runs. The driver case used the installed Claude Code and Codex executables. The two desktop UI cases exercised keyboard-only attachment, return from Files, native Open/Save panels, pointer input and window resizing. |
| Four simultaneous clients | Mac, iPhone, iPad and CLI each sent a distinct line to the same child. All clients observed each line on both output channels. This passed locally on the audit revision and on a GitHub-hosted Mac. [Hosted execution](https://github.com/specialistvlad/clistream/actions/runs/44677340259/job/136092198839) |
| Dependency scan | `govulncheck` reported zero reachable known vulnerabilities. It identified 11 module-level advisories outside the application's call paths. |
| Signed Apple build 2611200557 | Both archives passed deep, strict code-signature verification and contained the original MIT notice. The Mac executable contains arm64 and x86_64. |

The iPhone 17 Pro and iPad mini (A17 Pro) simulators on 26.4.1 each passed the five app-hosted cases, including both installed CLI drivers, plus the file, terminal, touch-editing and scroll scenarios across targeted runs. The scroll case registers enough real CLI processes to fill the session list. It verifies that a swipe changes the visible rows, that a terminal swipe exposes earlier numbered lines, and that showing and hiding the keyboard leaves the child's input empty.

On iOS 27.0, XCTest also enabled VoiceOver on iPhone and iPad and read the spoken labels for the nested folder, Upload and Done. On the compact iPhone layout, the file actions had to be part of the current navigation page for VoiceOver to reach them from the file list. [Apple VoiceOver testing API](https://developer.apple.com/documentation/xcuiautomation/xcuivoiceoverservice)

The Mac UI suite opens the pairing form with Command-K, enters the decoded invitation into its secure field, selects the first pipe from the sidebar using Command-1 and the down arrow, then sends a command. The supervisor receives a second command after Files is dismissed. A separate test opens the pairing link directly, clicks the PTY, resizes the window and checks the cell received by the child.

The iPhone and iPad scenarios use real system UI for copy/paste, keyboard input, file pickers and navigation. The touch test extends a selection using the system handle, sends the pasted text to the remote process, cancels a partial back swipe, completes a second swipe and runs Apple's accessibility audit. The fixture compares the two distinct pasted strings at the child. The terminal fixture checks the reported cell and records window sizes in the child process.

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
| Pairing and authentication | The Go suite redeems one v2 link from independent devices, checks expiry and replacement, rejects mismatched pins and unknown keys, and verifies durable enrollment. Revoking an attached viewer disconnects it while another viewer continues. The November 20 v1 record above covers Apple QR decoding and confirmation. |
| Pipe and PTY | The local terminal and remote clients exchange input, stdout and stderr with one child. The suite checks exit status, a 4 MiB binary round trip, PTY escape sequences, remote resize, detach, replay and reconnection after a broker restart. Loopback median single-key echo was 0.37 ms in one 21-sample run. |
| Compression | Negative tests exercise mandatory compression during TLS negotiation and again on an enrolled connection. A raw list request receives an error, then a compressed list succeeds on a different stream of the same connection. Codec tests also reject malformed, oversized and unknown-field messages. A 30,800-byte repetitive log sample used 2,047 application wire bytes. |
| Network interruption | For 120 seconds a UDP relay varied per-packet delay between 12 and 22 ms, dropped every 23rd datagram and changed the source port mid-session. The run checked 2,376 request/echo pairs byte-for-byte with 474 datagrams dropped. A separate test discarded all packets from the first handshake socket and verified recovery on a fresh socket. |
| Scoped file service | Tests compare uploaded and downloaded bytes, race two uploads to the same name, preserve an existing file and reject traversal, absolute paths, escaped symlinks and hard links. Filesystem tests exercise interrupted upload cleanup and replacement of the original directory pathname. |
| Port forwarding | TCP half-close and datagram boundaries, including empty and 1,400-byte UDP datagrams, are exercised through declared loopback exports. Apple-hosted tests exchange binary payloads through actual forwarded sockets. UI tests start and stop both forwarding controls. |
| Target-specific drivers | Claude Code 2.1.329 and Codex CLI 0.170.0 were detected by version. Remote clients supplied prompts, closed stdin, collected output and checked child exit status. Codex completed a live arithmetic request in a read-only ephemeral session. The real Claude executable completed nonce-based exchanges with a loopback Messages API emulator. A separate live Anthropic attempt reached the account's weekly usage limit. |
| Apple terminal and files | App-hosted tests drive SwiftTerm input and resize, inspect its rendered terminal buffer, and check exit status 23. A 240,019-byte upload/download is compared in the app and on the supervisor. The UI exports a random 150,013-byte file through the system picker and imports it into another directory. A separate CLI client verifies the Apple session output. |

The network results above describe an emulated path. QR decoding and deep-link handling were exercised without a physical camera.

## Reproduction

```sh
make test e2e
go test -v ./internal/wire ./internal/broker
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
CLISTREAM_SOAK_SECONDS=120 make e2e
make dist test-dist
(cd dist && shasum -a 256 -c SHA256SUMS)
```

Set `CLISTREAM_LEGACY_BIN` and `CLISTREAM_MANAGED_V1_BIN` to binary directories built from `bd2d7ab` and `50cbcaa` to run `make test-compatibility`. Run `make test-deployment` with Docker for the four-container test and, on macOS, the native CLI to Linux supervisor check.

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

The terminal fixture reads the grid size from the child PTY before and after a resize. The hosted detach case checks that the child receives the final queued input. The transfer fixture compares files on the supervisor after native picker operations.

The device suite prints its result directory under `apple/build/results-*`. Each scenario has its own XCTest bundle. The simultaneous-client harness launches a test-only build of the app on both simulators. Each mobile app writes to the same session and records the stdout and stderr received by its view model. The harness checks all four writers on both channels and saves one JSON report per mobile device alongside the Mac XCTest bundle under `apple/build/shared-*`. Open the bundle with Xcode or run `xcrun xcresulttool get test-results tests --path PATH.xcresult` for the individual outcomes.

## Distribution

The signed iOS and universal macOS archives for build `2611200557` passed `codesign --verify --deep --strict`. The license resource in each app was byte-for-byte identical to the repository MIT license. The two earlier TestFlight upload attempts reached App Store Connect and returned HTTP 403 with `FORBIDDEN.REQUIRED_AGREEMENTS_MISSING_OR_EXPIRED`. TestFlight installation remains unverified. The Account Holder can review pending terms in the Apple Developer account and App Store Connect. [Apple agreement guidance](https://developer.apple.com/help/app-store-connect/manage-agreements/sign-and-update-agreements/)
