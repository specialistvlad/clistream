# Implementation and acceptance plan
1. Establish TLS identities, single-use role-bound invitations and mandatory bounded compression.
2. Connect the daemon and its local administration socket to a session registry and multiplexed QUIC relay.
3. Implement process drivers, terminal fan-out, local interaction and explicit port exports.
4. Share the client library between the Unix CLI and a native Apple terminal.
5. Exercise both platforms, two simultaneous clients, unauthorized access, replay, port exports and terminal behavior. Record executed checks and release gates in verification.md.

