# CLIStream

Share one running terminal between your Mac, Linux machine, iPhone and iPad. A Go supervisor launches the command locally. Enrolled devices can attach to that same process, exchange terminal input, browse its workspace and forward explicitly exported ports.

## Build and try it

Install Go 1.27.4 or newer. Linux filesystem transfers use openat2 or, when unavailable, verify mount IDs through /proc/self/fdinfo. Run `make build` to produce three binaries in `bin/`. Put the absolute bin directory on PATH before changing to a session's working directory.

```sh
export PATH="$PWD/bin:$PATH"
clistreamd
```

In another terminal, enroll a supervisor and a client. Each invocation issues a fresh invitation. These pipelines are for a same-machine demonstration.

```sh
clistreamd -qr=false invite supervisor | clistream-supervise -pair
clistreamd -qr=false invite client | clistream pair
mkdir -p ~/clistream-demo
cd ~/clistream-demo
clistream-supervise -mode pty -- /bin/sh
```

From a third terminal run `clistream list`, copy the session ID and run `clistream attach ID`. Type into either terminal. The shell exits for everyone when you run `exit`. To detach a remote CLI without affecting the shared process, press Ctrl-] in an interactive PTY attachment. In pipe mode, `clistream -eof attach ID` closes the child's shared stdin when the client's input ends.

With Python 3.11+, `make dist` builds checksummed CLI archives in `dist/` for Linux and macOS on amd64 and arm64. Extract the matching archive on each host.

## Connect another machine

Start the daemon with a reachable advertised hostname and allow inbound UDP on the selected port. For example, replace `server.example.org` with your server.

```sh
clistreamd -listen :7443 -advertise server.example.org:7443
```

Run `clistreamd` a second time as the same user to display a client enrollment QR and link. On iPhone or iPad, scan it with Camera, open CLIStream and tap **Pair device**. The Apple client also accepts a pasted link. For another CLI or supervisor, run its pairing command, paste its role-specific link on standard input and press Return. Start supervisors on the machines where the target executables are installed.

```sh
clistream-supervise -- claude
clistream-supervise -- codex
clistream-supervise -mode pipe -- your-command --your-option
clistream-supervise -local-input=false -- /bin/cat
```

The version-aware drivers select PTY for Claude Code **2.1.329** and Codex CLI **0.170.0**. Explicit `-mode pipe` or `-mode pty` overrides selection. Unrecognized versions require `-driver generic`. The generic driver defaults to pipes. Terminal and process credentials come from the machine running the supervisor.

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
