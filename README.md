# CLIStream

Share one running terminal between your Mac, Linux machine, iPhone and iPad. A Go supervisor launches the command locally. Enrolled devices can attach to that same process, exchange terminal input, browse its workspace and forward explicitly exported ports.

## Build and try it

Install Go 1.27.4 or newer. Linux filesystem transfers use openat2 or, when unavailable, verify mount IDs through /proc/self/fdinfo. Run `make build` to produce three binaries in `bin/`. Put the absolute bin directory on PATH before changing to a session's working directory.

```sh
export PATH="$PWD/bin:$PATH"
clistreamd
```

The daemon prints two commands and a QR code for each listening address. Both commands use the same three-minute link. Replace `LINK` below with the link for the machine where the program will run.

```sh
mkdir -p ~/clistream-demo
cd ~/clistream-demo
clistream-supervise LINK /bin/sh
```

In a third terminal on the same OS account, run `clistream`. The two Go tools share a device key and saved server, so you can also pair with `clistream LINK` first and then run `clistream-supervise /bin/sh`. On a different machine, use the same invitation before it expires. Press **p** in the daemon terminal to invalidate outstanding invitations and display a fresh link for each address.

For scripts, `clistreamd invite` prints a single link. The older spellings `clistreamd supervisor`, `clistreamd invite supervisor` and `clistreamd invite client` also issue universal invitations. Both CLIs still accept invitations on stdin through `clistream pair` and `clistream-supervise -pair`.

Run `clistream` to open the supervisor table. Select a running program with the arrow keys and press Enter to attach. While attached, **Ctrl-]** selects the next running app, **Ctrl-[** selects the previous app, and **Ctrl-G** returns home to the supervisor table. If your terminal sends Escape for Ctrl-[, use **Ctrl-^** for previous. Switching keeps every app running. Use **S** to stop, **R** to restart or start, and **E** to edit arguments. The highlighted header identifies the target and shows the available keys. When input or output is redirected it prints the session list as JSON. Explicit `list`, `attach ID`, and `pair` (invitation on stdin) remain available to scripts. Type into either terminal. The shell exits for every viewer when you run `exit`. Its supervisor stays available for another start. Use `clistream-supervise -once PROGRAM` in scripts that wait for the child’s status. In pipe mode, `clistream -eof attach ID` closes the child's shared stdin when the client's input ends.

With Python 3.11+, `make dist` builds checksummed CLI archives in `dist/` for Linux and macOS on amd64 and arm64. Extract the matching archive on each host.

## Connect another machine

Choose the network interfaces to serve when starting the daemon. For example, on a Mac whose Wi-Fi interface is `en0`, run:

```sh
clistreamd -interface loopback -interface en0
```

Use a network link printed for an IP address reachable from the phone. The default loopback link works only on the server machine. Scan the QR code, open the link in CLIStream, or paste it into **Pair**, then tap **Pair device**. Each device can redeem the invitation during its three-minute lifetime. Fresh enrollment requires software that understands v2 invitations. With a background daemon, run `clistreamd` as the same user in a terminal to open the pairing display. Start supervisors on the machines where the target executables are installed.

```sh
clistream-supervise LINK claude
clistream-supervise codex
clistream-supervise -mode pipe -- your-command --your-option
clistream-supervise -local-input=false -- /bin/cat
```

Use the first command to pair a new account and launch Claude. Later launches on that account can omit the link. By default, the supervisor runs the supplied program directly and passes through its arguments. It uses a PTY when both input and output are terminals and pipes when either is redirected. Any version can run this way, including Claude and Codex. Use `-mode pipe` or `-mode pty` to override I/O selection.

For application-specific behavior, opt in with `clistream-supervise -driver claude claude` or `clistream-supervise -driver codex codex`. These drivers recognize Claude Code **2.1.329** and Codex CLI **0.170.0**. They default to PTY and select pipes for commands beginning with `claude -p`, `claude --print` or `codex exec`. Terminal and process credentials come from the machine running the supervisor.

Inspect the supported versions, default execution mode and available modes with `clistream-supervise -drivers`. The command prints JSON and works before enrollment.

## Workspace files and forwarded ports

```sh
clistream ls ID .
clistream put ID ./local.txt remote.txt
clistream get ID remote.txt ./download.txt
clistream-supervise -export web=tcp:127.0.0.1:8080 -export echo=udp:127.0.0.1:9000 -- your-command
clistream forward ID web 127.0.0.1:8081
```

The current supervisor shares the contents of its working directory at startup. In the example above, `clistream ls ID .` lists the contents of `~/clistream-demo`. To share a different folder with this build, change into that folder before starting the supervisor. The revised requirements also allow the operator to supply the folder directly at startup. That option is planned. [Session folder selection](docs/requirements.md#session-folder-selection)

Transfers stream in chunks and preserve existing destination files. In the Apple app, attach and select **Files** to browse, upload or save a file. On iPhone and iPad, Upload imports a copy of the selected local document. Canceling the picker keeps the current remote folder. The visible directory refreshes every two seconds. A session's **Forward** button displays the loopback address where an exported service is available on that device.

## Administration and development

The daemon automatically appends JSON activity events to `server.log` in its private state directory. Follow it with `tail -F /path/to/state/server.log`. Start the daemon with `clistreamd -no-log-file` to send events to standard error. The log records connections, disconnects, sessions, file operations and forwards. Terminal and file contents stay private. [Server activity logs](docs/deployment.md#server-activity-logs)

`clistreamd devices` lists enrolled key fingerprints. `clistreamd revoke FINGERPRINT` removes that device and closes active connections. On upgrade, previously enrolled supervisors gain permission to control every session. A holder of a valid invitation can enroll again after revocation. Press **p** to replace outstanding invitations. Global flags precede subcommands. `-state DIR` chooses a separate private identity directory for any binary.

On upgrade, the Go tools reuse the sole saved profile, or the client profile when both legacy profiles have the same address and server pin. If the servers differ, each tool keeps its existing connection until you explicitly pair with the shared server. Older daemons retain their original enrollment rules.

- [Product requirements and acceptance criteria](docs/requirements.md)
- [Interactive terminal client and supervisor controls](docs/terminal-client-design.md)
- [Apple client interaction design](docs/apple-client-design.md)
- [Deployment and Apple build instructions](docs/deployment.md)
- [Security model and file-service permissions](docs/security.md)
- [Wire protocol](docs/protocol.md)
- [Implementation plan](docs/implementation-plan.md) and [verification record](docs/verification.md)

Run `make test e2e` for race-enabled Go tests, vet and multi-process acceptance tests. Use `make apple` on macOS with Xcode and XcodeGen to build the shared Go XCFramework and generate the Xcode project.

[MIT license](LICENSE)
