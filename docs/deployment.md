# Deployment

Build with `make build`. Install the three binaries to a directory on PATH. The server needs an inbound UDP route. Supervisors and clients initiate outbound connections. The default is loopback. Configure a public or VPN-reachable hostname with `-advertise`. The local admin socket lives inside the daemon state directory. On Unix systems keep this path short enough for a Unix socket (under roughly 100 bytes).

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

## Apple client and TestFlight

Install full Xcode, Go and XcodeGen. Run `make apple` and open apple/CLIStream.xcodeproj. Select CLIStream-iOS for iPhone and iPad, or CLIStream-macOS for Mac. The static Go library contains arm64 device, arm64/x86_64 simulator, and universal macOS slices. Simulator and local builds can use separate signing settings. The application uses SwiftTerm for PTY rendering.

Create or select an App Store Connect record matching the bundle identifier and platforms in apple/project.yml. Configure ASC_KEY_PATH, ASC_KEY_ID, ASC_ISSUER_ID and DEVELOPMENT_TEAM in the release environment. Run `apple/testflight.sh iOS` and `apple/testflight.sh macOS` to archive and upload. Build processing, encryption-compliance declarations and internal tester assignment take place in App Store Connect. Keep signing keys, profiles, invitation links and TestFlight credentials outside this repository.

Use `ARCHIVE_ONLY=1` to create a signed local archive. The native Mac app is inside its Products/Applications directory.
