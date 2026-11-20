# CLIStream interactive terminal client and supervisor controls

Approved design · November 20, 2026

## What the operator sees

Running `clistream` in a terminal on Linux or macOS opens a full-screen application immediately. **The top row always says `CLIStream Client`. The second row shows the available keys. Both rows form a highlighted header.** Before attachment, the title identifies the server. During attachment, it identifies the program and its host. The operator can tell where the next keystroke will go.

| Screen | Behavior |
|---|---|
| Connecting | Draw the header before dialing. Show a spinner and `Connecting to HOST`. On failure, show the reason with `R Retry` and `Q Quit`. With no saved server, show the existing pairing command. |
| Supervisors | Show a live table with **Program**, **Host**, **State**, **Mode**, and **ID**. Open this screen even when exactly one supervisor is available. With zero rows, show `Connected · Waiting for supervisors`. Preserve selection by supervisor ID as the table refreshes. |
| Attached | Show `CLIStream Client │ Attached to PROGRAM on HOST` above the remote terminal. Ctrl-] and Ctrl-[ cycle forward and backward through running apps in table order, wrapping at either end. Ctrl-G returns home to the supervisor table. All apps keep running. When the child exits, return to the table with its exit status. |
| Connection lost | Immediately stop accepting remote input and show `Disconnected from HOST`. Retry returns to the table. The operator explicitly chooses where to attach again. |

The table can show running and stopped programs together.

```text
CLIStream Client │ Supervisors │ Connected to devbox:7443
↑↓ Select   Enter Attach   R Restart   S Stop   E Edit command   Q Quit
  Program     Host          State             Mode    ID
› codex       build-mac     Running           PTY     4a91bc
  worker      linux-01      Stopped · exit 0  Pipe    97bd20
```

On a stopped row, `R` reads **Start** and the header omits Attach and Stop. On a narrow terminal, retain the client identity, target, state, and home key first. Show enough of each ID to distinguish visible rows.

The client requests distinct keyboard codes for Ctrl-[ and Escape. On terminals that encode them identically, the key reaches the app as Escape. With only one running app, either switch key keeps the current attachment. Pasted controls remain app input.

## Supervisor actions

| Action | Result |
|---|---|
| **Attach** | Open the selected running child. Inside the terminal, ordinary letters, arrows, function keys, paste, and `Ctrl-C` go to that child. Dashboard shortcuts apply only on the dashboard. |
| **Stop** | Ask `Stop PROGRAM on HOST?` with **Cancel** selected. On confirmation, stop the child, end its attachments, and keep the supervisor available in the table. |
| **Restart / Start / Edit command** | R or E opens one horizontally scrolling line with the current executable and all arguments. The header identifies the target. Enter validates, submits and attaches to the acknowledged run. Escape discards edits. A preflight error preserves the running child. A launch failure after stopping keeps the draft available for correction. |

**Managed operation becomes the supervisor default.** A natural exit, failed launch, or remote Stop leaves the supervisor available for another start. A local termination signal shuts down the supervisor and its child. Add `clistream-supervise -once` for scripts that need today’s behavior of exiting with the child’s status. Edited arguments last for the lifetime of that supervisor, including broker reconnects. Launching the supervisor afresh uses the arguments supplied locally.

## Implementation design

- **Terminal rendering.** Keep the existing `creack/pty` on the supervisor, where it starts the child and changes its terminal size. **Bubble Tea v2** owns the Go client’s event loop, terminal modes, keyboard, mouse, resize events and screen updates. The **Bubbles textinput** widget edits and horizontally scrolls the restart command and supplies its cursor position. Lip Gloss draws the focused field’s border when space permits. Use **`charmbracelet/x/vt`** to interpret remote output inside a bounded terminal viewport. An Ultraviolet buffer composes sanitized emulator cells and the header into the Bubble Tea view. A remote program can clear its screen or enter alternate-screen mode while the CLIStream header stays visible. The tested versions are pinned in `go.mod`. [Go PTY library](https://github.com/creack/pty) · [Ultraviolet documentation](https://pkg.go.dev/github.com/charmbracelet/ultraviolet) · [VT emulator documentation](https://pkg.go.dev/github.com/charmbracelet/x/vt)
- **Interactive entry points.** Use the framed interface for `clistream`, `clistream PAIRING-LINK`, and interactive `clistream attach ID`. Explicit attach opens the named session directly and returns its exit status on completion. Preserve redirected JSON listing, byte-for-byte scripted attachment, separate stdout/stderr, `-eof`, and existing file and forwarding commands. Pipe sessions remain usable inside the frame. The client does not spawn a local shell.
- **Input and display boundaries.** Send the viewport dimensions to the supervisor after reserving the two header rows. Translate mouse coordinates into that viewport. Treat pasted text as data, including text matching a hotkey. Render remote output and remote-supplied labels within their assigned cells. Restore the original terminal mode, cursor, and screen on exit or error.
- **Process ownership.** Give each managed supervisor a stable ID for its lifetime and each child run a fresh session ID. The dashboard selects a supervisor. Terminal input, replay, files, and forwards remain bound to a specific run. Close the old run’s channels and reap its process group before starting a replacement. Serialize lifecycle actions at the supervisor, and reject requests based on a stale run or state revision. After a lost acknowledgement, the client refreshes supervisor state and waits for the operator’s next action.
- **Protocol and compatibility.** Add capability discovery, supervisor listing, argument retrieval, and lifecycle requests to the authenticated Go protocol. Keep the existing session list limited to attachable running children. Existing decoders reject unknown fields, so use separately negotiated operations and preserve existing message shapes on legacy paths. Older supervisors appear as **Attach only**. Against an older daemon, the new client can still display and attach to its running sessions. A new supervisor reports the legacy limitation and uses one-shot behavior there.
- **Permissions and logging.** Apply the existing enrolled-device trust model to management requests. Recheck enrollment and the target supervisor before dispatch. Record the requesting device, action, supervisor and run IDs, and outcome in the daemon activity log. Keep argument values out of listings, logs, and error text. The editor sends argument values directly as an argument vector without shell expansion.
- **Apple clients.** The first release delivers management controls in the Go terminal client on Linux and macOS. Native Mac, iPhone, and iPad clients continue to list and attach to running sessions. A stop or restart ends their old attachment, and the replacement appears as a new running session.

## Supervisor terminal

On Linux and macOS, the supervisor defaults to a timestamped activity feed on stderr. With `--interactive` and terminal stdin and stdout, it opens directly on its local process. Bubble Tea renders a two-row header above the same bounded emulator viewport as the client. The first row identifies the supervisor, program and broker. The second shows the child state independently of the broker connection, for example **Running · Reconnecting in 4s**. After a managed exit the last screen stays visible with its exit status. A replacement starts in a fresh viewport. Local input goes directly to the child during network outages. In read-only mode remote viewers can still interact, and Ctrl-C exits locally.

## Launch examples

Use `clistream-supervise /bin/sh` to keep a shell available for another start after exit. Use `clistream-supervise -no-remote-restart /bin/sh` (alias `-once`) to share a single shell. Its row reads **Attach only**. **Ctrl-G Disconnect** returns one viewer to the table while the shell keeps running. Ctrl-D reaches the shell. If it exits, all attachments end, the row disappears, and the supervisor exits with the shell’s status. A new run requires a local launch.

In the command line, arrows move by character, Ctrl-A/E move to either end, Ctrl-W erases a word, and Ctrl-U/K erase to the beginning or end. Paste inserts text. Separate arguments with spaces, use single or double quotes for grouping, and use `""` for an empty argument. Double quotes accept escapes such as `\n` and `\xFF`. Shell metacharacters are literal.

On older managed peers, R retains its original lifecycle action and E opens the argument editor. In that editor, ↑ and ↓ select entries, ← and → move the cursor, Enter inserts an empty argument, and Ctrl-D removes the selected entry. Ctrl-R opens the start or restart confirmation. Tab selects the action, Enter confirms, and Escape cancels.
