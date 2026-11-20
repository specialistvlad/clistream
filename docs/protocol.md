# CLIStream wire protocol v1

Each supervisor and client initiates a QUIC connection to the broker. TLS 1.3 negotiates ALPN `clistream/1+snappy`. ALPN selects both protocol and compression. There is no uncompressed mode or 0-RTT session resumption.

## Identity and roles

The broker requests a TLS client certificate. TLS CertificateVerify proves possession of the private key. The broker identifies a device by hex-encoded SHA-256 of the certificate's DER SubjectPublicKeyInfo. Enrollment on a new connection requires a 256-bit, single-use, three-minute local invitation. The URL fragment contains address, server key pin and token. The requested role comes from the daemon's invitation table. Regular connections require an enrolled public key with the appropriate role. Each profile has one server and one device identity. Revocation disconnects the device.

The broker terminates both TLS connections and can read relayed data. Enrolled clients share all sessions on this daemon. Use separate daemons for separate trust domains. Invitation possession authorizes enrollment and the custom URL scheme requires trust in the app handling the link.

## Records

Every application record, including enrollment, filesystem and tunnel data, is UTF-8 JSON compressed independently as a Snappy block, preceded by a four-byte big-endian compressed length. Decoders verify both compressed and expanded limits before allocation. The maximum JSON size is 131072 bytes. Independent records share no compression dictionary. Binary fields use JSON base64. Lengths and traffic timing are visible on the network. Field names and types are specified by `internal/wire.Message`. Unknown fields, malformed records and unsupported operations fail. Each request opens a QUIC bidirectional stream. Its first response is `ok` or `error`.

| First operation | Role | Purpose |
|---|---|---|
| pair(Token) | Invited | Bind the presented key to the invitation role |
| list | Client | Session descriptors in Sessions |
| register(Info) | Supervisor | Assign a session ID for this connection |
| open(ID, Channel, Path?) | Client | Broker opens matching stream back to the supervisor |

## Channel operations

- **stdin** accepts data, resize(Cols, Rows) or eof. Each request receives an acknowledgment. Closing the attachment leaves shared stdin open. Explicit eof closes a pipe for all participants.
- **stdout, stderr, pty** stream data followed by exit(Code). The receiver acknowledges exit. PTY output combines both process descriptors. Each new subscriber replays up to the last MiB or 4096 chunks of its channel. A subscriber overtaken by the history buffer must reattach. Concurrent input is ordered by arrival at the process. The last resize sets shared terminal dimensions.
- **port/name** exchanges data records. The supervisor chooses the endpoint from local launch flags. TCP uses stream FIN for half-close. Each UDP record carries one complete datagram over reliable QUIC streams.
- **fs/list** streams entry metadata followed by end. **fs/get** streams data followed by end. **fs/put** accepts data followed by explicit end, then replies ok after publishing the file. FIN alone aborts an incomplete upload. A destination must be new. All three use relative paths rooted at the supervisor's launch directory. See security.md.

An error on one logical stream can end that operation independently. Disconnecting a client detaches its streams. A supervisor connection owns its registration. Broker restart removes live registrations. The supervisor reconnects with backoff, registers a fresh session ID and keeps its child process alive. Refresh the client list and attach again.

