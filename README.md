# CLIStream

Share one running terminal between your Mac, Linux machine, iPhone and iPad. A Go supervisor launches the command locally. Enrolled devices can attach to that same process, exchange terminal input, browse its workspace and forward explicitly exported ports.

## Build and try it

Install Go 1.27.4 or newer. Linux filesystem transfers use openat2 or, when unavailable, verify mount IDs through /proc/self/fdinfo. Run `make build` to produce three binaries in `bin/`. Put the absolute bin directory on PATH before changing to a session's working directory.

```sh
export PATH="$PWD/bin:$PATH"
clistreamd
```

The daemon prints a pairing command and QR code. Press **p** in its terminal to replace the displayed invitation. In another terminal with the same PATH, start the program you want to share. This example requests a one-time supervisor link from the local daemon.

```sh
mkdir -p ~/clistream-demo
cd ~/clistream-demo
clistream-supervise "$(clistreamd -qr=false invite supervisor)" /bin/sh
```

The supervisor saves its enrollment. Next time, run `clistream-supervise /bin/sh`. To supervise on a different machine, run `clistreamd -qr=false invite supervisor` on the server, then pass the printed link as the first argument on that machine, followed by the program and its arguments. Existing scripts can still pipe the link to `clistream-supervise -pair`.

In a third terminal, paste the entire printed `clistream clistream://pair/v1/…` command. On later visits run just `clistream`. It attaches to the sole session, offers a numbered choice for several, or waits for a supervisor. When input or output is redirected it prints the session list as JSON. Explicit `list`, `attach ID`, and `pair` (invitation on stdin) remain available to scripts. Type into either terminal. The shell exits for everyone when you run `exit`. To detach a remote CLI without affecting the shared process, press Ctrl-] in an interactive PTY attachment. In pipe mode, `clistream -eof attach ID` closes the child's shared stdin when the client's input ends.

With Python 3.11+, `make dist` builds checksummed CLI archives in `dist/` for Linux and macOS on amd64 and arm64. Extract the matching archive on each host.

## Connect another machine

Choose the network interfaces to serve when starting the daemon. For example, on a Mac whose Wi-Fi interface is `en0`, run:

```sh
clistreamd -interface loopback -interface en0
```

Use a network link printed for an IP address reachable from the phone. The default loopback link works only on the server machine. Scan the QR code, open the link in CLIStream, or paste it into **Pair**, then tap **Pair device**. All endpoints in a displayed set enroll one device. The set expires after three minutes. Press **p** for another device. With a background daemon, run `clistreamd` as the same user in a terminal to open the pairing display. Start supervisors on the machines where the target executables are installed.

```sh
clistream-supervise claude
clistream-supervise codex
clistream-supervise -mode pipe -- your-command --your-option
clistream-supervise -local-input=false -- /bin/cat
```

The version-aware drivers select PTY for Claude Code **2.1.329** and Codex CLI **0.170.0**. Explicit `-mode pipe` or `-mode pty` overrides selection. Unrecognized versions require `-driver generic`. Commands beginning with `claude -p`, `claude --print` or `codex exec` select pipes automatically. Generic programs get a PTY when both supervisor input and output are terminals and pipes when either is redirected. Terminal and process credentials come from the machine running the supervisor.

Inspect the supported versions, default execution mode and available modes with `clistream-supervise -drivers`. The command prints JSON and works before enrollment.

## Workspace files and forwarded ports

```sh
clistream ls ID .
clistream put ID ./local.txt remote.txt
clistream get ID remote.txt ./download.txt
clistream-supervise -export web=tcp:127.0.0.1:8080 -export echo=udp:127.0.0.1:9000 -- your-command
clistream forward ID web 127.0.0.1:8081
```

The file service roots paths at the directory where the supervisor started. Transfers stream in chunks and preserve existing destination files. In the Apple app, attach and select **Files** to browse, upload or save a file. The visible directory refreshes every two seconds. A session's **Forward** button displays the loopback address where an exported service is available on that device.

## Administration and development

`clistreamd devices` lists enrolled key fingerprints and roles. `clistreamd revoke FINGERPRINT` removes that device and closes active connections. Global flags precede subcommands. `-state DIR` chooses a separate private identity directory for any binary.

- [Product requirements and acceptance criteria](docs/requirements.md)
- [Apple client interaction design](docs/apple-client-design.md)
- [Deployment and Apple build instructions](docs/deployment.md)
- [Security model and workspace boundary](docs/security.md)
- [Wire protocol](docs/protocol.md)
- [Implementation plan](docs/implementation-plan.md) and [verification record](docs/verification.md)

Run `make test e2e` for race-enabled Go tests, vet and multi-process acceptance tests. Use `make apple` on macOS with Xcode and XcodeGen to build the shared Go XCFramework and generate the Xcode project.

[MIT license](LICENSE)
