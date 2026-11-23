# CLIStream interactive terminal client and supervisor controls

Operator guide for the Go client and supervisor on macOS and Linux.

## What the operator sees

Running `clistream` in a terminal on Linux or macOS opens a full-screen application immediately. **The top row always says `CLIStream Client`. The second row shows the available keys. Both rows form a highlighted header.** Before attachment, the title identifies the server. During attachment, it identifies the program and its host. The operator can tell where the next keystroke will go.

| Screen | Behavior |
|---|---|
| Connecting | Draw the header before dialing. Show a spinner and `Connecting to HOST`. On failure, show the reason with `R Retry` and `Q Quit`. With no saved server, show the existing pairing command. |
| Supervisors | Show a live table with **Program**, **Host**, **State**, **Mode**, and **ID**. Open this screen even when exactly one supervisor is available. With zero rows, show `Connected · Waiting for supervisors`. Preserve selection by supervisor ID as the table refreshes. |
| Attached | Show `CLIStream Client │ Attached to PROGRAM on HOST` above the remote terminal. Ctrl-] and Ctrl-[ cycle forward and backward through running apps in table order, wrapping at either end. Ctrl-G returns home to the supervisor table. All apps keep running. When a text-mode child exits, inspect its retained output and exit status, then press Enter to return home. An alternate-screen program returns directly to the table. |
| Connection lost | Immediately stop accepting remote input and show `Disconnected from HOST`. Retry returns to the table. The operator explicitly chooses where to attach again. |

The table can show running and stopped programs together.

```text
CLIStream Client │ Supervisors │ Connected to devbox:7443
↑↓ Select   Enter Attach   R Restart   S Stop   E Edit command   Q Quit
  Program     Host          State             Mode    ID
› codex       build-mac     Running           PTY     4a91bc
  worker      linux-01      Stopped · exit 0  Pipe    97bd20
```

On a stopped row, **R** opens the Start command editor. **Enter** opens the retained output when available. On a narrow terminal, retain the client identity, target, state, and home key first. Show enough of each ID to distinguish visible rows.

The client requests distinct keyboard codes for Ctrl-[ and Escape. On terminals that encode them identically, the key reaches the app as Escape. With only one running app, either switch key keeps the current attachment. Pasted controls remain app input.

## Supervisor actions

| Action | Result |
|---|---|
| **Attach** | Open the selected running child. Inside the terminal, ordinary letters, arrows, function keys, paste, and `Ctrl-C` go to that child. Dashboard shortcuts apply only on the dashboard. |
| **Stop** | Ask `Stop PROGRAM on HOST?` with **Cancel** selected. On confirmation, stop the child, end its attachments, and keep the supervisor available in the table. |
| **Restart / Start / Edit command** | R or E opens one horizontally scrolling line with the current executable and all arguments. The header identifies the target. Enter validates, submits and attaches to the acknowledged run. Escape discards edits. A preflight error preserves the running child. A launch failure after stopping keeps the draft available for correction. |

**Managed operation becomes the supervisor default.** A natural exit, failed launch, or remote Stop leaves the supervisor available for another start. A local termination signal shuts down the supervisor and its child. Add `clistreams --once` for scripts that need today’s behavior of exiting with the child’s status. Edited arguments last for the lifetime of that supervisor, including broker reconnects. Launching the supervisor afresh uses the arguments supplied locally.

## Implementation design

- **Terminal rendering.** Keep the existing `creack/pty` on the supervisor, where it starts the child and changes its terminal size. **Bubble Tea v2** owns the Go client’s event loop, terminal modes, keyboard, mouse, resize events and screen updates. The **Bubbles textinput** widget edits and horizontally scrolls the restart command and supplies its cursor position. Lip Gloss draws the focused field’s border when space permits. Use **`charmbracelet/x/vt`** to interpret remote output inside a bounded terminal viewport. An Ultraviolet buffer composes sanitized emulator cells and the header into the Bubble Tea view. A remote program can clear its screen or enter alternate-screen mode while the CLIStream header stays visible. The tested versions are pinned in `go.mod`. [Go PTY library](https://github.com/creack/pty) · [Ultraviolet documentation](https://pkg.go.dev/github.com/charmbracelet/ultraviolet) · [VT emulator documentation](https://pkg.go.dev/github.com/charmbracelet/x/vt)
- **Interactive entry points.** Use the framed interface for `clistream`, `clistream PAIRING-LINK`, and interactive `clistream attach ID`. Explicit attach opens the named session directly and returns its exit status on completion. Preserve redirected JSON listing, byte-for-byte scripted attachment, separate stdout/stderr, `--send-eof`, and existing file and forwarding commands. Pipe sessions remain usable inside the frame. The client does not spawn a local shell.
- **Input and display boundaries.** Send the viewport dimensions to the supervisor after reserving the two header rows. Translate mouse coordinates into that viewport. Treat pasted text as data, including text matching a hotkey. Render remote output and remote-supplied labels within their assigned cells. Restore the original terminal mode, cursor, and screen on exit or error.
- **Process ownership.** Give each managed supervisor a stable ID for its lifetime and each child run a fresh session ID. The dashboard selects a supervisor. Its terminal input, replay, CLI file commands, and forwards use the child run ID. Finder uses a separate supervisor-scoped workspace capability. Close the old run’s channels and reap its process group before starting a replacement. Serialize lifecycle actions at the supervisor, and reject requests based on a stale run or state revision. After a lost acknowledgement, the client refreshes supervisor state and waits for the operator’s next action.
- **Protocol and compatibility.** Add capability discovery, supervisor listing, argument retrieval, and lifecycle requests to the authenticated Go protocol. Keep the existing session list limited to attachable running children. Existing decoders reject unknown fields, so use separately negotiated operations and preserve existing message shapes on legacy paths. One-shot registrations appear as **Attach only**. Previous brokers return retired fields in session listings, which the current client rejects. Upgrade the client, daemon and supervisor together. An older client can list and attach to current sessions. [Mixed-version tests](../tests/e2e/compatibility_test.go)
- **Permissions and logging.** Apply the existing enrolled-device trust model to management requests. Recheck enrollment and the target supervisor before dispatch. Record the requesting device, action, supervisor and run IDs, and outcome in the daemon activity log. Keep argument values out of listings, logs, and error text. The editor sends argument values directly as an argument vector without shell expansion.
- **Apple clients.** The native Mac, iPhone, and iPad apps also start, stop, restart, and edit managed commands. Their highlighted command field uses the Go CLI’s quoting and escaping rules. Stopping or replacing a run affects every attached viewer. [Apple session controls](apple-client-design.md#terminal-and-session-controls)

## Supervisor terminal

On Linux and macOS, the supervisor defaults to a timestamped activity feed on stderr. With `--interactive` and terminal stdin and stdout, it opens directly on its local process. Bubble Tea renders a two-row header above the same bounded emulator viewport as the client. The first row identifies the supervisor, program and broker. The second shows the child state independently of the broker connection, for example **Running · Reconnecting in 4s**. After a managed exit the last screen stays visible with its exit status. A replacement starts in a fresh viewport. Local input goes directly to the child during network outages. In read-only mode remote viewers can still interact, and Ctrl-C exits locally.

## Launch examples

Use `clistreams /bin/sh` to keep a shell available for another start after exit. Use `clistreams --once /bin/sh` to share a single shell. Its row reads **Attach only**. **Ctrl-G Disconnect** returns one viewer to the table while the shell keeps running. Ctrl-D reaches the shell. If it exits, all attachments end, the row disappears, and the supervisor exits with the shell’s status. A new run requires a local launch.

In the command line, arrows move by character, Ctrl-A/E move to either end, Ctrl-W erases a word, and Ctrl-U/K erase to the beginning or end. Paste inserts text. Separate arguments with spaces, use single or double quotes for grouping, and use `""` for an empty argument. Double quotes accept escapes such as `\n` and `\xFF`. Shell metacharacters are literal.

On older managed peers, R retains its original lifecycle action and E opens the argument editor. In that editor, ↑ and ↓ select entries, ← and → move the cursor, Enter inserts an empty argument, and Ctrl-D removes the selected entry. Ctrl-R opens the start or restart confirmation. Tab selects the action, Enter confirms, and Escape cancels.

The Go CLI accepts scripting commands after the lifetime session ID. The dashboard sends its usable dimensions on each remote launch. After restarting a short-lived command it retrieves the acknowledged run’s bounded output even if attachment raced with exit. The interactive client keeps its own terminal frame for both pipes and PTYs. It renders live child output below the header and presents completed text in a scrollable viewport with the exit status. Enter returns to the session table. A stopped session’s Enter action opens the latest retained result. Alternate-screen runs return directly to the table.
