# Verification record

Tests used isolated temporary state, enrolled device keys and live QUIC connections. The black-box harness starts the compiled daemon, supervisor and clients, then cleans up its processes and temporary files.

| Check | Observed result |
|---|---|
| Multi-client terminal | The original terminal and two CLI attachments wrote to one child. Both clients received the expected stdout, stderr and exit status 7. |
| PTY | Escape sequences and carriage returns survived a round trip through the child and relay. |
| Reconnection | After restarting the broker, the supervisor registered its existing child under a fresh session ID. The new attachment replayed the output written before the restart. |
| Authorization | Unenrolled key, incorrect server pin, replayed invitation, incorrect role and revoked key were rejected. Enrollment survived broker restart. |
| Files | Binary upload/download matched exactly. Traversal, absolute paths, escaping symlinks, hard links and an existing upload destination were rejected. Unit tests covered interrupted upload cleanup and replacement of the original directory pathname. |
| Forwarding | TCP request/response survived a half-close. UDP records preserved empty, one-byte and 1400-byte datagrams. |
| Compression | A 30,800-byte repetitive log sample occupied 2,047 application wire bytes. One byte of input occupied 33 wire bytes. Malformed, uncompressed, oversized and unknown-field records were rejected. |
| Driver detection | The locally installed Claude Code 2.1.329 and Codex CLI 0.170.0 ran through their version-aware PTY wrappers with --version. This check uses no paid model requests. |
| Apple | Native iOS and macOS targets compiled and produced signed archives. An iPhone 17 Pro simulator followed a pairing link, entered text, saw the echoed output and opened the live workspace directory. |
| Distribution | Both App Store Connect uploads reached app-record lookup and received HTTP 403 REQUIRED_AGREEMENTS_MISSING_OR_EXPIRED. TestFlight installation and physical-device testing await account-holder action. |

## Reproduction

```sh
make test e2e
go test -v ./internal/wire
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
make apple
SIMULATOR_ID=your-simulator-udid apple/test-device.sh
apple/test-device.sh macOS
```

The Go suite and black-box tests were run on Apple Silicon macOS. Linux arm64 binaries were executed in Docker, with the same black-box harness. The Go 1.27.4 vulnerability scan reported zero reachable known vulnerabilities. Module-level advisories outside the reachable call graph are reported by the scanner. These local checks cover loopback operation and do not measure Internet latency, prolonged load or reachability through a production firewall.

