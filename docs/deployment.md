# Deployment

Build with `make build`. Install the three binaries to a directory on PATH. The server needs an inbound UDP route. Supervisors and clients initiate outbound connections. The default is loopback. Select network interfaces by repeating `--interface NAME` (use `loopback` to include localhost). Alternatively use `--listen` with `--advertise` for a reachable address, including a numeric IP. Allow inbound UDP on that port. The local admin socket lives inside the daemon state directory. On Unix systems keep this path short enough for a Unix socket (under roughly 100 bytes).

## Connect to a running daemon

Run `clistreamd` in a terminal under the service account to display its pairing commands and QR codes. Administration commands use the service account’s configuration directory. Existing custom directories are no longer selected. Before upgrading, stop the old service and back up its private state. To keep its identity and enrollments, move its complete state into an unused account-default server directory with the same owner and private permissions. For a custom client or supervisor identity, pair under the intended OS account or migrate its saved profile into an unused default device directory. Use the localhost link on the daemon host or the network link on a host that can reach that address. For scripts, `clistreamd invite` prints one unquoted `clistream://HOST/?token=TOKEN` link and prefers a network address when available.

On the program host, run `clistreams LINK PROGRAM [ARG ...]`. On the same OS account, `clistream` then connects with the saved key. On a second host, run `clistream LINK` within three minutes. Press **p** at the daemon terminal to replace outstanding links across all interfaces. For an upgrade, review previously enrolled supervisor keys before restarting the service, since those keys will gain control of every session. [Upgrade permissions](security.md)

## Choose the supervisor lifetime

On Linux and macOS, `clistreams PROGRAM [ARG ...]` keeps the supervisor registered after the child exits or fails to launch. Select its row in `clistream` to start it again. Stop and Restart terminate the managed child’s process group with SIGKILL. Use the program’s own exit command when it needs to save work before quitting. Enrolled operators can change the executable and arguments. The supervisor resolves the command on its host with the original I/O mode setting. Restarting the supervisor itself restores its locally supplied command. Send SIGTERM to the supervisor to end supervision and its running child. For a single attach-only run, use `clistreams --once PROGRAM [ARG ...]`. Viewers can disconnect and reattach. When the child exits, the supervisor returns its status and the session disappears. Start the next run on the supervisor host. A supervisor connecting to an older daemon reports its one-shot fallback. [Terminal controls](terminal-client-design.md)

### Connection recovery

A paired supervisor waits for broker registration before launching its first child. After a transient failure it waits 1, 2, 4, 8, 16, then at most 30 seconds between connection attempts. A successful registration resets the delay. An existing child keeps running locally during recovery. A managed child that exits offline is listed as stopped when the broker returns. An attach-only supervisor exits with its child’s status even while reconnecting. A rejected identity or mismatched server key ends automatic retries. Check the server address and pair with a fresh invitation from the intended daemon.

## Supervisor activity logs

Plain `clistreams PROGRAM` runs with a readable stderr feed and empty stdout, including under a service manager. The feed shows registration, connection and retry status, child starts and exits, remote lifecycle requests, terminal channel boundaries, file operations and forwards. Pressing Ctrl-G in the client closes that viewer’s terminal channels. The supervisor reports each channel as closed and the program continues running. Keep using the client for child input and output. Local stdin may close without ending the child. Use `--interactive` before the program name to opt scripts into child streams or a terminal into the framed local view.

Each supervisor process writes UTC JSON lines to `logs/supervisor-<instance-id>.log` under its account identity directory. The feed shows the location at startup. The interactive view shows it while waiting to start. The same journal follows replacements and broker reconnects. Each file has mode 0600 and rotates at 10 MiB with five backups per instance. Journals from previous supervisor processes remain in the directory. A journal failure appears locally and supervision continues.

## Server activity logs

The serving daemon writes each activity event as one JSON line to both standard error and `server.log`. Successful supervisor-table refreshes are omitted from both destinations. Foreground operators see actions as they happen. Under a service manager, the console copy goes to the configured stderr destination. On Linux the default location is `${XDG_CONFIG_HOME:-$HOME/.config}/clistream/server/server.log`. On macOS it is `~/Library/Application Support/clistream/server/server.log`. Files have mode 0600. Before the next record would exceed 10 MiB, the daemon rotates the active log to `server.log.1` and keeps up to five previous files.

Use `clistreamd --log-file=FALSE` for stderr-only logging. Set the flag on the serving process. Use `--log-file=PATH` for a custom file. Relative paths start in the daemon’s working directory. The parent directory must exist. TRUE selects the default private file. For systemd append it to `ExecStart` and read events with `journalctl --user -u clistream`. For launchd add `<string>--log-file=FALSE</string>` to `ProgramArguments` and configure `StandardErrorPath` if you want launchd to collect the output. If the daemon cannot open, write or rotate its own file, it reports the error and uses standard error for the rest of that run. Normal startup and diagnostic text can share standard error with JSON events.

To follow one connection, select its `connection_id` from `connection_open` and find the matching `connection_close`. The close event includes the observed role, reason (`outcome`) and `duration_ms`. Use the device fingerprint to follow a reconnect, `supervisor_id` to follow lifecycle requests, and `session_id` to find clients of one run. An orderly daemon stop drains connection and channel events before `daemon_stop`. An abrupt daemon kill or host power loss can leave an open event without a close.

## Linux user service

For binaries installed in /usr/local/bin, save this as ~/.config/systemd/user/clistream.service and substitute the endpoint. Run `systemctl --user daemon-reload` and `systemctl --user enable --now clistream`. Use `journalctl --user -u clistream` for diagnostics.

```ini
[Unit]
Description=CLIStream QUIC broker
After=network.target
[Service]
ExecStart=/usr/local/bin/clistreamd --listen :7443 --advertise server.example.org:7443
Restart=on-failure
UMask=0077
NoNewPrivileges=true
[Install]
WantedBy=default.target
```

## macOS user service

Save the property list below as ~/Library/LaunchAgents/com.vlk.clistreamd.plist. Substitute the installed binary path and address. Load it using `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.vlk.clistreamd.plist`. Run clistreamd again from the owner's terminal to administer the running broker.

```xml
<plist version="1.0"><dict>
<key>Label</key><string>com.vlk.clistreamd</string>
<key>ProgramArguments</key><array><string>/usr/local/bin/clistreamd</string><string>--listen</string><string>:7443</string><string>--advertise</string><string>server.example.org:7443</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
```

## Choose the folder a supervisor shares

Start the current supervisor in the folder whose contents clients should see. For example, with the supervisor enrolled and on PATH, this shares `~/clistream-demo` and runs the shell installed at `/bin/sh`.

```sh
mkdir -p ~/clistream-demo
cd ~/clistream-demo
clistreams --io-mode pty -- /bin/sh
```

Use `clistreams --workspace DIR PROGRAM` to share another folder. Relative paths start in the launch directory. The supervisor retains the open folder through restarts and reconnects. Use `--files=FALSE` to disable all file operations. Combining those options or choosing an invalid folder fails before launch. The child uses the host account's normal permissions. [Session folder selection](requirements.md#session-folder-selection)

The optional Docker test, `make test-deployment`, starts a broker, two independent supervisors with different working directories, and a viewer on a private network. Its temporary deployment uses in-memory identities and cleans up its own containers and network. Production supervisors need private persistent state or re-enrollment after replacement. [Deployment test](../tests/deployment/deployment_test.go)

## Apple client and TestFlight

Fresh enrollment requires a client build that accepts the daemon’s hostname-and-token links. Existing enrolled devices can reconnect with their saved keys. The Go CLIs also accept invitations from older daemons. The native apps accept hostname-and-token invitations through **Add Server** or by opening the link. Confirm the advertised server address on the device. When upgrading a running daemon, restart the process to activate the new terminal display. If the saved network address changes, enroll with a link containing a reachable address.

Install full Xcode, Go and XcodeGen. Run `make apple` and open apple/CLIStream.xcodeproj. Select CLIStream-iOS for iPhone and iPad, or CLIStream-macOS for Mac. The static Go library contains arm64 device, arm64/x86_64 simulator, and universal macOS slices. Simulator and local builds can use separate signing settings. The application uses SwiftTerm for PTY rendering.

The Mac app embeds a Finder extension with the app’s bundle identifier plus `.finder`. Sign both targets with the same development team and configure their common App Group. Let XcodeGen derive both bundle identifiers from `CLISTREAM_APP_ID`. The acceptance app uses a separate domain from the shipping app. After installation, enable CLIStream under File Providers in System Settings if macOS asks, then pair a server and choose **Show in Finder**. The iOS archive embeds the Files extension at `com.vlk.clistream.files`. Register `group.com.vlk.clistream.workspace`. Under **Identifiers → App IDs**, open each of `com.vlk.clistream` and `com.vlk.clistream.files`. Choose **App Groups → Configure**, select the group, then confirm **Assign**. Both provisioning profiles must list this group before the app can be signed. The acceptance simulator uses a separate App Group. The release script verifies the extension and app signatures and their common group. [Register an App Group](https://developer.apple.com/help/account/identifiers/register-an-app-group/) · [Assign App Groups](https://developer.apple.com/help/account/identifiers/enable-app-capabilities)

Create or select an App Store Connect record matching the bundle identifier and platforms in apple/project.yml. Configure ASC_KEY_PATH, ASC_KEY_ID, ASC_ISSUER_ID and DEVELOPMENT_TEAM in the release environment. Run `apple/testflight.sh iOS` and `apple/testflight.sh macOS` to archive and upload. Build processing, encryption-compliance declarations and internal tester assignment take place in App Store Connect. Keep signing keys, profiles, invitation links and TestFlight credentials outside this repository.

Use `ARCHIVE_ONLY=1` to create a signed local archive. The native Mac app is inside its Products/Applications directory.

### Install an internal beta

**Testers:** Accept the internal testing invitation with your enrolled Apple Account. In TestFlight, open **CLIStream** and choose **Install** or **Update**. The same iOS build runs on iPhone, iPad, and Duo. Mac uses its own native build.

**Release operators:**

1. Sign in to the Apple Developer account for the team that owns `com.vlk.clistream`. If an upload reports `FORBIDDEN.REQUIRED_AGREEMENTS_MISSING_OR_EXPIRED`, the team's Account Holder can review any updated program terms on the account landing page. App Store Connect also displays agreement status under **Business → Agreements**. [Developer account](https://developer.apple.com/account/) · [Apple's agreement guide](https://developer.apple.com/help/app-store-connect/manage-agreements/sign-and-update-agreements/)
2. Under **App Store Connect → Apps**, open the record for that bundle identifier. If it is missing, use **+ → New App**, choose iOS and macOS, and enter the primary language, bundle ID and a unique SKU. One iOS build serves both iPhone and iPad. [App record instructions](https://developer.apple.com/help/app-store-connect/create-an-app-record/add-a-new-app/)
3. To upload an existing signed archive from the repository root, run `open apple/build/release/iOS-BUILD.xcarchive`, replacing `BUILD` with the archived build number. In Xcode Organizer, choose **Distribute App → TestFlight Internal Only → Distribute**. Repeat with `macOS-BUILD.xcarchive`. The project upload script above provides the command-line route. [Xcode distribution instructions](https://developer.apple.com/documentation/xcode/distributing-your-app-for-beta-testing-and-releases)
4. After Apple processes each upload, open **Apps → CLIStream → TestFlight**. If the build shows **Missing Compliance**, open **Manage** and review the encryption questions. The client includes the Go QUIC and TLS implementation. Under **Internal Testing**, select or create a group, add the two platform builds and invite the tester's App Store Connect account. Accept the invitation in TestFlight on each device. [Beta export compliance](https://developer.apple.com/help/app-store-connect/test-a-beta-version/provide-export-compliance-information-for-beta-builds/) · [Internal tester instructions](https://developer.apple.com/help/app-store-connect/test-a-beta-version/add-internal-testers/)

### Native client parity checks

The redesigned targets require macOS 27 or iOS/iPadOS 27. Build with Xcode 27.1 or later to exercise iPhone Duo. On an isolated CI runner, run `make apple`, then `apple/test-native.sh macOS` for the desktop UI or `SIMULATOR_ID="$SIMULATOR_ID" apple/test-native.sh` for a mobile simulator. The Go fixture creates an isolated broker and supervisor, serves fresh invitations to the XCTest runner, and writes an XCTest result bundle under `apple/build/results-*`.

For mobile UI tests, install AXe 2.1.1 on the Mac and put its executable on `PATH`. Select the same Xcode for building and simulator input with `DEVELOPER_DIR`. The Duo suite drives the simulated hinge with AXe and checks terminal input on each active display. The shared-session test uses XCTest to type into the native Mac, iPhone and iPad terminals while a Go CLI is attached to the same program. [Acceptance commands](verification.md)

On the isolated Mac, `apple/test-linux-host.sh macOS` runs the Finder round trip against a Linux supervisor. Use `SIMULATOR_ID="$SIMULATOR_ID" apple/test-linux-host.sh` for the mobile pairing case. Set `CLISTREAM_LINUX_CASE=testFilesWorkspaceRoundTrip` for the standalone Files round trip. The Docker VM must forward TCP for the test fixture and UDP for CLIStream's QUIC connection. The hosted workflow starts Colima with `--port-forwarder grpc` for both transports. [Colima port-forwarder configuration](https://github.com/abiosoft/colima/blob/v0.10.3/embedded/defaults/colima.yaml#L164-L170)

Pair additional brokers using **Add Server**. For a deployment with separate work and personal brokers, issue an invitation on each broker and redeem each in the app. The existing Apple enrollment remains available after upgrading.
