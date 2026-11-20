# CLIStream wire protocol v1

Each supervisor and client initiates a QUIC connection to the broker. TLS 1.3 negotiates ALPN `clistream/1+snappy`. ALPN selects both protocol and compression. There is no uncompressed mode or 0-RTT session resumption.

## Device identity and enrollment

The broker requests a TLS client certificate. TLS CertificateVerify proves possession of the private key. The broker identifies a device by hex-encoded SHA-256 of the certificate's DER SubjectPublicKeyInfo.

The local administration interface issues invitations valid for three minutes. The version in `clistream://pair/v2/` selects the invitation encoding. Enrolled peers continue to use wire protocol v1 and ALPN `clistream/1+snappy`. Encode the concatenated payload and signature as one unpadded base64url string. The payload consists of the 32-byte Ed25519 public key, eight-byte big-endian Unix expiry in seconds, 32-byte random invitation ID and remaining UTF-8 host:port. The 64-byte Ed25519 signature covers ASCII `clistream/pair/v2`, one zero byte and the payload. The client verifies the signature and pins the TLS peer to the SHA-256 digest of the signer's DER SubjectPublicKeyInfo.

The client sends the complete signed link as the `pair` token over the pinned connection. On every enrollment request the broker verifies the signature, its own pin, outstanding ID and server-clock expiry before enrolling the peer key. Multiple devices may use a link concurrently. Re-enrolling an enrolled key succeeds. Displayed endpoints share an ID. Replacing the display invalidates outstanding IDs, and restarting clears all invitations.

Every enrolled key can publish and control sessions, including keys enrolled with a client or supervisor role before the upgrade. Revocation disconnects active connections and removes the enrollment. Updated clients can also redeem signed v1 role-specific links and original unsigned fragment links on older daemons. For a fragment link, the client sends the opaque token over the pinned connection. Those servers retain their original role and single-use checks.

The broker terminates both TLS connections and can read relayed data. Enrolled clients share all sessions on this daemon. Use separate daemons for separate trust domains. Invitation possession authorizes enrollment and the custom URL scheme requires trust in the app handling the link.

## Records

Every application record, including enrollment, filesystem and tunnel data, is UTF-8 JSON compressed independently as a Snappy block, preceded by a four-byte big-endian compressed length. Decoders verify both compressed and expanded limits before allocation. The maximum JSON size is 131072 bytes. Independent records share no compression dictionary. Binary fields use JSON base64. Lengths and traffic timing are visible on the network. Field names and types are specified by `internal/wire.Message`. Unknown fields, malformed records and unsupported operations fail. Each request opens a QUIC bidirectional stream. Its first response is `ok` or `error`.

| First operation | Role | Purpose |
|---|---|---|
| pair(Token) | Invited | Enroll the presented key |
| list | Enrolled device | Session descriptors in Sessions |
| register(Info) | Enrolled device | Assign a session ID for this connection |
| open(ID, Channel, Path?) | Enrolled device | Broker opens matching stream back to the supervisor |

## Channel operations

- **stdin** accepts data, resize(Cols, Rows) or eof. Each request receives an acknowledgment. Closing the attachment leaves shared stdin open. Explicit eof closes a pipe for all participants.
- **stdout, stderr, pty** stream data followed by exit(Code). The receiver acknowledges exit. PTY output combines both process descriptors. Each new subscriber replays up to the last MiB or 4096 chunks of its channel. A subscriber overtaken by the history buffer must reattach. Concurrent input is ordered by arrival at the process. The last resize sets shared terminal dimensions.
- **port/name** exchanges data records. The supervisor chooses the endpoint from local launch flags. TCP uses stream FIN for half-close. Each UDP record carries one complete datagram over reliable QUIC streams.
- **fs/list** streams entry metadata followed by end. **fs/get** streams data followed by end. **fs/put** accepts data followed by explicit end, then replies ok after publishing the file. FIN alone aborts an incomplete upload. A destination must be new. All three use paths relative to the session's shared folder. The supervisor determines that folder locally. A client sends `.` to list its contents in either startup mode. [Session folder selection](requirements.md#session-folder-selection)

An error on one logical stream can end that operation independently. Disconnecting a client detaches its streams. A supervisor connection owns its registration. Broker restart removes live registrations. The supervisor reconnects with backoff, registers a fresh session ID and keeps its child process alive. Refresh the client list and attach again.
