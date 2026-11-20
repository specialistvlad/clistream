# CLIStream product and engineering requirements

## Product and scope

An operator starts a CLI utility once, on the machine that has the executable and its working files. The utility can run unmodified. From a Mac, a Linux terminal, an iPhone or an iPad, the operator can then interact with that same running process. Several authorized clients can participate at once. The target machine continues to own the process, its terminal state and its workspace.

The initial Unix targets are macOS and Linux. Delivery consists of three compiled Go programs and an Apple client for macOS, iPhone and iPad. The source, implementation plan, protocol documentation, operating instructions and verification evidence belong in the public MIT-licensed repository.

| Component | Responsibility |
|---|---|
| Server daemon, `clistreamd` | Authenticate participants, issue enrollment invitations through a local administration interface, track live sessions and route streams between clients and supervisors. |
| Supervisor, `clistream-supervise` | Run on the target host. Accept an executable path, arguments and a server endpoint or paired configuration. Start and supervise the process, own its pipes or pseudo-terminal (PTY), and serve its workspace and configured ports. |
| Command-line client, `clistream` | Enroll, discover sessions on the server, attach, interact, transfer files and use forwarded ports from macOS or Linux. |
| Apple client | Provide those same user workflows through a native interface on Mac, iPhone and iPad. Open pairing links, render interactive terminals and use the platform file pickers. Deliver testable builds through TestFlight. |

## Functional requirements

The acceptance criteria below translate the requested behaviors into checks. The verification record lists the checks that have been run and their outcomes. [Verification record](verification.md)

| ID | Required behavior | Observable acceptance criterion |
|---|---|---|
| F1 | Run the server as a daemon. A second local invocation must recognize and administer the existing process and let the operator display a pairing QR code or equivalent link. The interactive daemon also prints a link for each selected address at startup and replaces the displayed set on **p**. | Start the daemon, invoke it again, decode the QR and verify that the invitation belongs to the original server. The original sessions stay available. |
| F2 | Enroll clients using the server address and authorization material in the QR or link. Scanning on iPhone must open the installed app and present a pairing confirmation. The CLI must accept the printed link as its positional argument and reconnect with its saved profile when started without arguments. V1 authenticates enrolled devices by proof of private-key possession and offers no username/password login. | A newly enrolled Apple client and CLI can list sessions. An unknown key, a mismatched server identity and an expired or replaced invitation are rejected. Each device retains its own private key. |
| F3 | Launch `clistream-supervise [PAIRING-LINK] PROGRAM [ARG ...]` on the program’s host. Enroll on first use, reuse the saved server on later launches, and automatically select terminal or pipe I/O. Supervisors on different hosts must be able to register independently. | Start two supervisors with different programs and workspaces against one server. An enrolled client can distinguish the sessions and send input to the intended child. |
| F4 | Let multiple CLI and Apple clients list, attach to and control the same live session concurrently, alongside the local operator. Detaching one viewer must leave the child and the other viewers running. | Attach from iPhone, iPad, Mac and a separate CLI. Each sends a distinct line to one child. Every attached client sees the resulting output, and the remaining clients can continue after one detaches. |
| F5 | Support a pipe driver for standard input, output and error, and a PTY driver for interactive terminal applications. Render the session in the mode the supervisor selected. | Pipe mode preserves binary data and separate stdout and stderr. PTY mode carries keystrokes, resize events and terminal control sequences and renders a full-screen text UI. Clients receive the child’s exit status. |
| F6 | Launch any executable through the generic driver by default, without a version probe. Offer version-aware Claude Code and Codex CLI drivers by explicit selection. Each specialized driver must declare its supported versions and capabilities. | Run a generic command and each supported named CLI. Verify detection, selected mode and an actual interaction. An unknown version produces a clear compatibility result and an explicit generic option. |
| F7 | Use QUIC for bidirectional, multiplexed communication. Define logical streams for system and session information, input, stdout, stderr and PTY traffic, plus file operations and tunnels as needed. Compression is mandatory for all application traffic. | During an attachment, exchange input and output while performing a file operation. Verify channel boundaries. Reject peers that omit mandatory compression and reject an uncompressed application record on an authenticated connection. |
| F8 | Forward TCP and UDP services made available by the supervisor through the session’s encrypted transport. Clients must be able to select an exported service. | Reach a TCP service and a UDP service from both client interfaces. Check TCP half-close, UDP datagram boundaries and rejection of an undeclared destination. |
| F9 | Expose a live view of the session workspace, rooted at the directory where its supervisor started. Authorized clients can browse, observe changes, download and upload. File operations must stay inside that root. | Create a file locally and observe it from an attached client. Round-trip binary content and compare bytes. Test two distinct roots, traversal, escaping links, interrupted transfers and concurrent uploads. |
| F10 | Provide a native Apple workflow for pairing, opening any registered CLI and switching between Terminal and Files within the same session. Support keyboard, mouse and trackpad on Mac, touch on iPhone and iPad, and attached keyboards and pointers on iPad. Follow Finder conventions on Mac and Files conventions on mobile. | Pair from the Mac, use the keyboard and a mouse-aware terminal application, browse nested folders, transfer a file and return to the live process. Exercise the equivalent mobile workflow and verify focus, selection and workspace confinement. |

For F10, every user-facing action offered on iPhone or iPad must work by touch alone. Verify the complete workflows on both simulators using the onscreen keyboard and the visible controls. [Touch-only interaction requirements](apple-client-design.md#touch-only-operation-on-iphone-and-ipad)

The current driver list includes Claude Code 2.1.329 and Codex CLI 0.170.0. Both default to PTY mode. Keep the supported-version list with the driver so a new CLI release can be qualified independently. [Supported versions and mode selection](../README.md)

### Filesystem isolation

The requested boundary is that remote access must not expose files outside the supervisor’s launch directory. There are two routes to a file in this product. The file browser can request a download, and a user controlling a shell can ask the child process to read a file. They need separate acceptance tests.

The current file service enforces a directory boundary. The child process inherits the supervisor user’s OS permissions. To satisfy the broader requirement that *anything controlled through the session* is confined, deployment must also isolate the supervisor and child, for example inside a VM or container exposing only that workspace as host data. Verify this by placing a sentinel file outside the root and attempting to read and modify it through both routes. Whether CLIStream must provision that isolation itself is an open deployment decision. [Current security model](security.md)

## Nonfunctional and engineering requirements

| ID | Quality | Engineering requirement |
|---|---|---|
| N1 | Security and trust | Use modern authenticated encryption, verify the server identity and require possession of an enrolled private key. Document who can read relayed data, how invitations expire, how to remove a device and where credentials are stored. The current design uses QUIC with TLS 1.3, pinned server identity, per-device Ed25519 keys and a trusted broker that can read session data. Keep device keys, live invitations and Apple signing or upload credentials out of source control and published test evidence. |
| N2 | Responsiveness and data integrity | Stream incrementally in both directions so an interactive CLI feels usable across machines. Preserve byte order within each stream, file contents and UDP message boundaries. Measure keystroke-to-echo latency under stated network conditions. No numerical latency, throughput or concurrency target was specified. |
| N3 | Reliability and resource use | Define what happens on detach, child exit, broker restart, interrupted upload and a slow consumer. Bound buffering and decompression. An incomplete upload must not appear as a successful file. Exercise these behaviors with loss, delay and disconnection. |
| N4 | Modularity and maintainability | Keep transport and compression, authentication and authorization, session discovery, process I/O, CLI-specific drivers, workspace operations, tunnels and client presentation behind focused interfaces. Adding a future authentication strategy or CLI driver should preserve callers of the existing contracts. Keep OS-specific PTY and filesystem code behind their platform interfaces. |
| N5 | Protocol discipline | Maintain a versioned reference for each component boundary. Specify messages, stream roles, capabilities, size limits, lifecycle and errors. Both endpoints must enforce the contract and handle unsupported versions and malformed records predictably. The wire specification records current protocol and compression choices. [Wire protocol](protocol.md) |
| N6 | Portability and delivery | Build the server, supervisor and CLI client for supported macOS and Linux targets. Maintain one Apple product experience across all three form factors. Provide reproducible build and launch instructions, signed Apple builds and a repeatable TestFlight upload process using the owner’s Apple Developer account. |
| N7 | Verification and ownership | Test during implementation. Use idiomatic Go, formatting, static checks, race detection and automated integration tests. Keep the implementation plan, test commands, platform and version matrix, results, skips and external blockers in the repository. Publish source under MIT and ship its notice with the deliverables. |

## Definition of done

1. **An operator can run the complete workflow from the documentation.** Start the daemon, enroll a supervisor, launch a utility, pair a client, list the resulting session and use it from another machine.
2. **The feature suite exercises real applications.** Launch the compiled daemon, supervisor and CLI against a real QUIC connection. Run app-hosted and UI suites on iPhone and iPad simulators and native macOS. Include the four-client shared-session scenario, both drivers, both I/O modes, both tunnel protocols, workspace operations, mandatory compression and rejection cases.
3. **Results identify what was actually proven.** Record commands, versions, environment and outcomes. Distinguish a real executable using an emulated provider from a live provider request. Distinguish simulator deep-link testing, a signed archive and a successful TestFlight installation.
4. **The owner can deploy and test the release.** Provide runnable binaries and server deployment instructions. For Apple beta delivery, the owner must be able to install the build through TestFlight, pair to a reachable server and interact with a supervised CLI.

The [requirements audit](test-results/requirements-audit.md) maps each requirement to executable checks. The [verification record](verification.md) identifies the platforms tested, completed workflows, external provider limits and TestFlight status. See the [implementation plan](implementation-plan.md) and [deployment instructions](deployment.md).
