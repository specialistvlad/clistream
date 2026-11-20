# Deployment

Build with `make build`. Install the three binaries to a directory on PATH. The server needs an inbound UDP route. Supervisors and clients initiate outbound connections. The default is loopback. Select network interfaces by repeating `-interface NAME` (use `loopback` to include localhost). Alternatively use `-listen` with `-advertise` for a reachable address, including a numeric IP. Allow inbound UDP on that port. The local admin socket lives inside the daemon state directory. On Unix systems keep this path short enough for a Unix socket (under roughly 100 bytes).

## Linux user service

For binaries installed in /usr/local/bin, save this as ~/.config/systemd/user/clistream.service and substitute the endpoint. Run `systemctl --user daemon-reload` and `systemctl --user enable --now clistream`. Use `journalctl --user -u clistream` for diagnostics.

```ini
[Unit]
Description=CLIStream QUIC broker
After=network.target
[Service]
ExecStart=/usr/local/bin/clistreamd -listen :7443 -advertise server.example.org:7443
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
<key>ProgramArguments</key><array><string>/usr/local/bin/clistreamd</string><string>-listen</string><string>:7443</string><string>-advertise</string><string>server.example.org:7443</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
```

## Isolated supervisor deployment

When the target is a shell or any utility that can open arbitrary files, its operating-system environment determines which host data the operator can reach. Put the supervisor and target in a dedicated VM or container. Mount the intended workspace at `/workspace`, make that the working directory, and put the supervisor identity in a separate private state directory. Give the container a read-only root, drop capabilities and enable `no-new-privileges`. Keep the host home directory, Docker socket, broker credentials and unrelated volumes outside the container.

Run `make test-deployment` with Docker available for a working example. The test starts a broker, two independent supervisors and a viewer on a private network. Its temporary deployment uses in-memory identities and cleans up its own containers and network. Production supervisors need private persistent state or re-enrollment after replacement. [Deployment test](../tests/deployment.py)

## Apple client and TestFlight

Fresh enrollment requires a client build that accepts the daemon’s signed `clistream://pair/v2/` links. Existing enrolled devices can reconnect with their saved keys. The updated app and CLI also accept invitations from older daemons. When upgrading a running daemon, restart the process to activate the new terminal display. If the saved network address changes, enroll with a link containing a reachable address.

Install full Xcode, Go and XcodeGen. Run `make apple` and open apple/CLIStream.xcodeproj. Select CLIStream-iOS for iPhone and iPad, or CLIStream-macOS for Mac. The static Go library contains arm64 device, arm64/x86_64 simulator, and universal macOS slices. Simulator and local builds can use separate signing settings. The application uses SwiftTerm for PTY rendering.

Create or select an App Store Connect record matching the bundle identifier and platforms in apple/project.yml. Configure ASC_KEY_PATH, ASC_KEY_ID, ASC_ISSUER_ID and DEVELOPMENT_TEAM in the release environment. Run `apple/testflight.sh iOS` and `apple/testflight.sh macOS` to archive and upload. Build processing, encryption-compliance declarations and internal tester assignment take place in App Store Connect. Keep signing keys, profiles, invitation links and TestFlight credentials outside this repository.

Use `ARCHIVE_ONLY=1` to create a signed local archive. The native Mac app is inside its Products/Applications directory.

### Install an internal beta

1. Sign in to the Apple Developer account for the team that owns `com.vlk.clistream`. If an upload reports `FORBIDDEN.REQUIRED_AGREEMENTS_MISSING_OR_EXPIRED`, the team's Account Holder can review any updated program terms on the account landing page. App Store Connect also displays agreement status under **Business → Agreements**. [Developer account](https://developer.apple.com/account/) · [Apple's agreement guide](https://developer.apple.com/help/app-store-connect/manage-agreements/sign-and-update-agreements/)
2. Under **App Store Connect → Apps**, open the record for that bundle identifier. If it is missing, use **+ → New App**, choose iOS and macOS, and enter the primary language, bundle ID and a unique SKU. One iOS build serves both iPhone and iPad. [App record instructions](https://developer.apple.com/help/app-store-connect/create-an-app-record/add-a-new-app/)
3. To upload an existing signed archive from the repository root, run `open apple/build/release/iOS-BUILD.xcarchive`, replacing `BUILD` with the archived build number. In Xcode Organizer, choose **Distribute App → TestFlight Internal Only → Distribute**. Repeat with `macOS-BUILD.xcarchive`. The project upload script above provides the command-line route. [Xcode distribution instructions](https://developer.apple.com/documentation/xcode/distributing-your-app-for-beta-testing-and-releases)
4. After Apple processes each upload, open **Apps → CLIStream → TestFlight**. If the build shows **Missing Compliance**, open **Manage** and review the encryption questions. The client includes the Go QUIC and TLS implementation. Under **Internal Testing**, select or create a group, add the two platform builds and invite the tester's App Store Connect account. Accept the invitation in TestFlight on each device. [Beta export compliance](https://developer.apple.com/help/app-store-connect/test-a-beta-version/provide-export-compliance-information-for-beta-builds/) · [Internal tester instructions](https://developer.apple.com/help/app-store-connect/test-a-beta-version/add-internal-testers/)
