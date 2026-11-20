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

Supervisors launched with `-no-remote-restart` or `-once` use `register` for a single attach-only run. Lifecycle requests target managed supervisor registrations.

## Negotiated supervisor controls

A peer probes with `{"Op":"capabilities"}`. An enrolled peer receives an `ok` with `Capabilities` containing `managed-supervisors-v1`. A legacy operation rejection selects the existing register/list/open paths. Those records retain their original fields.

| Operation | Request and response |
|---|---|
| `publish` | The supervisor sends `Supervisor` with its 24-hex-character lifetime `ID`, increasing `Revision` and session descriptor `Info`. A running child has its own 24-hex-character `Info.ID`. An empty run ID marks a stopped supervisor, with optional `Exit` or `Failed`. Reconnection republishes the current revision on the new connection. |
| `supervisors` | The broker returns `Supervisors` sorted by supervisor ID. A legacy registration has revision zero, indicating Attach only. Ordinary `list` returns running session descriptors. |
| `command-supervisors` | A broker advertising `edit-command-v1` lists per-supervisor `EditableCommand` support. The legacy listing omits this field. Publishers announce the capability on registration. |
| `command`, `run-command` | For a capable managed supervisor, fetch or submit the complete `Command` vector (JSON array of base64 byte strings, executable first). Both requests require supervisor ID, expected revision and current run. A successful `run-command` returns the replacement `Run`. A preflight failure preserves the child. A launch failure returns the stopped revision. Refresh after a lost acknowledgement and never automatically repeat the request. |
| `arguments` | Fetch the current argument vector in `Args` when the operator opens the editor. This legacy operation edits arguments only. An omitted vector represents zero arguments. |
| `start`, `stop`, `restart`, `apply` | Supply controller `ID`, expected `Revision` and current `Run` (empty while stopped). Only `apply` supplies replacement `Args`. The supervisor validates the snapshot and serializes transitions. An error or missing acknowledgement requires a fresh listing before another operator decision. |

The broker removes a connection's registrations on disconnect. It publishes changes to its running-session and supervisor views together. Attachment, file and tunnel streams continue to use the run ID. The supervisor closes old channels before launching a replacement. The activity journal records `supervisor_publish`, `supervisor_list` and `supervisor_request`. The latter includes the action, requesting device and connection, target supervisor and current run where available, duration and outcome. A missing upstream reply records an `unconfirmed` outcome.

## Channel operations

- **stdin** accepts data, resize(Cols, Rows) or eof. Each request receives an acknowledgment. Closing the attachment leaves shared stdin open. Explicit eof closes a pipe for all participants.
- **stdout, stderr, pty** stream data followed by exit(Code). The receiver acknowledges exit. PTY output combines both process descriptors. Each new subscriber replays up to the last MiB or 4096 chunks of its channel. A subscriber overtaken by the history buffer must reattach. Concurrent input is ordered by arrival at the process. The last resize sets shared terminal dimensions.
- **port/name** exchanges data records. The supervisor chooses the endpoint from local launch flags. TCP uses stream FIN for half-close. Each UDP record carries one complete datagram over reliable QUIC streams.
- **fs/list** streams entry metadata followed by end. **fs/get** streams data followed by end. **fs/put** accepts data followed by explicit end, then replies ok after publishing the file. FIN alone aborts an incomplete upload. A destination must be new. All three use paths relative to the session's shared folder. The supervisor determines that folder locally. A client sends `.` to list its contents in either startup mode. [Session folder selection](requirements.md#session-folder-selection)

An error on one logical stream can end that operation independently. Disconnecting a client detaches its streams. A supervisor connection owns its registration. Broker restart removes live registrations. One-shot supervisors reconnect with backoff and receive a fresh session ID. Managed supervisors republish their stable supervisor ID and the ID of the surviving child. Refresh the client list and attach again.

## Broker activity events

Logging uses information already visible to the v1 broker. Each accepted QUIC connection gets a random `connection_id` and an initial role of `unknown`. A successful registration identifies a supervisor, and a list or open request identifies client activity. Pairing alone leaves the role unknown. A shared device key can therefore appear on concurrent connections with different roles. Connections rejected during the TLS handshake precede broker acceptance.

`daemon_start`, `listener_ready` and `daemon_stop` bound daemon activity. `admin_request` records the action and outcome, including invitation issuance (`invite`) and replacement (`display`). The broker records `pair`, `session_register`, `session_list`, `session_removed`, and `request_rejected` for malformed or unknown requests. Failed channel opens generate `channel_rejected`. Successful opens generate `channel_open` and `channel_close` with a QUIC stream ID scoped to the connection.

A file-channel outcome of `confirmed` means the broker observed the supervisor's final acknowledgement or end record. It makes no claim about saving a download on the client. Output channels report `exited` and the child’s `exit_code` when the supervisor sends an exit record. Other channel outcomes include `closed`, `interrupted` and `failed`. Named exports are described only as TCP or UDP. No logging metadata is transmitted on the wire.
