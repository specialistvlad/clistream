# CLIStream

**Share a live terminal across your devices.** Run a shell, coding agent, or build on one machine and pick it up from a Mac, Linux terminal, iPhone, or iPad. Paired devices see the same output and can type into the same process through a server you control. The program keeps running when you disconnect.

![Codex and Claude Code run on the laptop. A “Typing here” marker identifies the second Mac. As it types “Run the tests.”, the same letters appear in Codex on the laptop and iPad. The Mac switches to Claude Code to draft release notes. The iPad stays with Codex and sees “18 tests passed.”](docs/media/clistream-demo.gif)

*Watch the Mac type into Codex. Each keystroke appears on the laptop and iPad at the same time. When the Mac switches to Claude Code, the iPad keeps showing the test run.*

## What you can do

| Task | Where it works |
| --- | --- |
| Share a live terminal or a pipe-backed process with several viewers. Send input, see output, and detach independently. | All clients |
| Start, stop, or restart a managed program, edit its executable and arguments, and read its retained output after exit. | All clients |
| Forward a named TCP or UDP service to a local port on the viewing device. | All clients |
| Pair and rename servers, see connection latency, and browse programs in a tree or list grouped by server, host, or CLI name. | Native Mac, iPhone, and iPad apps |
| Scan or paste a daemon invitation, check its address and fingerprint, and pair. Search CLI names or swipe a managed CLI to restart or stop it. | iPhone, iPad, and both Duo displays |
| Search CLI names, activate a terminal in one click, and open sessions in native tabs or separate windows. | Mac app |
| Use touch terminal keys, select and paste text, and keep a session across size or fold changes. | iPhone, iPad, and iPhone Duo apps |
| List, copy, move, or remove files and manage directories in the supervisor’s shared workspace. | Go CLI on macOS and Linux |
| Browse, edit, move, or delete files in a program’s remote workspace. | Finder on Mac with a workspace-capable daemon and supervisor. On iPhone, iPad, and Duo, the Files extension is in testing. [Verification](docs/verification.md) |

The **server** handles pairing and routes traffic. A **supervisor** runs beside each program on its **host** and keeps it available when viewers leave. A server can bring together programs from several hosts. The Apple apps require macOS 27 or iOS/iPadOS 27 and follow the system appearance. Upgrade the Go client, daemon, and supervisor together. Older native builds also need an update to display current sessions. [Native client guide](docs/apple-client-design.md) · [Build and TestFlight instructions](docs/deployment.md#apple-client-and-testflight)

In the native apps, type directly into either a pipe or a PTY session. **Start**, **Restart**, and **Stop** act at once, and **Edit Arguments…** lets you review the full command before replacing a run. Open **Ports** to manage that session’s forwards from a side panel or compact sheet. Use **Open** to load an enabled TCP forward in your browser.

## Try it with /bin/sh

Open three terminals in the repository directory on the same machine, using the same OS account. With Go 1.27.4 or newer installed, build the tools and start the server in terminal 1.

```sh
make build && ./bin/clistreamd
```

In terminal 2, start a shell in its own workspace. The supervisor enrolls using an invitation from the running server. Leave this terminal open to see its activity feed.

```sh
export PATH="$PWD/bin:$PATH"
mkdir -p "$HOME/clistream-demo" && cd "$HOME/clistream-demo" &&
  clistreams "$(clistreamd invite)" /bin/sh
```

In terminal 3, open the client. It shares the supervisor’s saved server and device key on this account.

```sh
./bin/clistream
```

Select the shell with the arrow keys and press **Enter**. Type `echo hello` in the attached client. Press **Ctrl-G** to return to the list while the shell keeps running. Attach again and type `exit` to finish the shell. **Ctrl-C** in the supervisor terminal ends supervision.

## Pair over the LAN

The default server listens on loopback. To connect a second machine, stop the demo server and start it with a LAN interface too. This example uses `en0` on a Mac. Substitute the server’s LAN interface on Linux or another Mac. Allow inbound UDP on the displayed port.

```sh
./bin/clistreamd --interface loopback --interface en0
```

On the second machine, install the Go CLI on your PATH and paste the **Network** client command printed by the server, including the single quotes around the link. Select the same shell to share its input and output. Invitations last three minutes. Press **p** in the server terminal to replace outstanding links. After pairing, reconnect with just `clistream`.

Each client and supervisor connects to the server using QUIC with TLS 1.3. The self-hosted server decrypts and relays traffic, so run it on a machine you trust. An enrolled device can control CLI apps and access their shared workspaces. [Security model](docs/security.md)

## Switch and control CLI apps

With the Go tools on your PATH, run `clistreams codex` or `clistreams claude` in another terminal on a paired host where that CLI app is installed. Add `--interactive` before the executable to use the CLI app from the supervisor terminal too.

| While using the Go client | Action |
| --- | --- |
| **Ctrl-]** / **Ctrl-[** while attached | Switch to the next / previous running CLI app on this client. Ctrl-[ needs a terminal that distinguishes it from Escape. |
| **Ctrl-G** while attached | Return to the supervisor list. Other viewers stay attached. |
| **S** in the list | Stop the selected CLI app for everyone. |
| **R** or **E** in the list | Edit the last command. Press Enter to launch it or Esc to cancel. |

For scripts, run `clistream list` and set `ID` to the supervisor’s `ID` field. It stays the same across restarts. For example, stop the shell, then start it again.

```sh
clistream "$ID" stop
clistream "$ID" start
```

Use `clistreams --once /bin/sh` to share a single run. When that shell exits, its supervisor exits too. [Terminal controls and launch examples](docs/terminal-client-design.md)

## Share files and forward a port

The supervisor shares its launch directory by default. To try a round trip in the demo workspace, run these commands from another directory on the client. The `local:` prefix identifies the client side of a copy.

```sh
printf 'demo note\n' > notes.txt
clistream "$ID" fs cp local:./notes.txt notes.txt
clistream "$ID" fs ls
clistream "$ID" fs cp notes.txt local:./download.txt
```

On Mac, select the program and choose **Session → Show in Finder** (⌥⌘R). Finder opens **CLIStream › server › host › program**. Saving a document there updates the file on the supervisor’s host. A managed program’s folder stays available while its child is stopped.

Use `clistreams --workspace ./reports /bin/sh` to share a different folder or `clistreams --files=FALSE /bin/sh` to disable file access. [Session folder selection](docs/requirements.md#session-folder-selection)

To share a web service already listening on port 8080 on the CLI app host, launch a supervisor with a named export.

```sh
clistreams --share-port web=tcp:127.0.0.1:8080 /bin/sh
```

On the client, set `ID` to the new supervisor’s ID from `clistream list`. Run the forward and open `http://127.0.0.1:8081` on the client while it runs.

```sh
clistream forward "$ID" web 127.0.0.1:8081
```

## Administration and development

Run `clistreamd devices` to list enrolled fingerprints and `clistreamd revoke FINGERPRINT` to revoke a device. The daemon writes activity events to stderr and its private rotating `server.log`. The supervisor prints its journal path at startup. [Deployment, migration and logs](docs/deployment.md)

Run `make test e2e` for race-enabled Go tests, vet and multi-process acceptance tests. Use `make dist` to build checksummed Linux and macOS archives for amd64 and arm64. On macOS with Xcode and XcodeGen, `make apple` builds the shared Go XCFramework and generates the native project.

- [Product requirements](docs/requirements.md)
- [Apple client interaction design](docs/apple-client-design.md) and [macOS design reference](docs/design/macos/README.md)
- [Wire protocol](docs/protocol.md)
- [Implementation plan](docs/implementation-plan.md) and [verification record](docs/verification.md)
- [MIT license](LICENSE)
