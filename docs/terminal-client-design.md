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
↑↓ Select   Enter Attach   R Restart   S Stop   E Edit arguments   Q Quit
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
| **Restart / Start** | Restart a running child with its current arguments, or start a stopped child. Confirm replacement of a running child and identify the affected program and host. Existing viewers receive the end of the old run. |
| **Edit arguments + restart** | Fetch the arguments only when the editor opens. Present one editable entry per argument, including empty entries and arguments containing spaces. Keep the executable fixed. Show **Restart with these arguments** or **Start with these arguments** according to state. Cancel leaves the current run intact. |

**Managed operation becomes the supervisor default.** A natural exit, failed launch, or remote Stop leaves the supervisor available for another start. A local termination signal shuts down the supervisor and its child. Add `clistream-supervise -once` for scripts that need today’s behavior of exiting with the child’s status. Edited arguments last for the lifetime of that supervisor, including broker reconnects. Launching the supervisor afresh uses the arguments supplied locally.

## Implementation design

- **Terminal rendering.** Keep the existing `creack/pty` on the supervisor, where it starts the child and changes its terminal size. Use Charm’s **Ultraviolet** for the Go client’s outer screen, keyboard events, paste, mouse, and resizing. Use **`charmbracelet/x/vt`** to interpret remote output inside a bounded terminal viewport. Ultraviolet draws the cells from the emulator. A remote program can clear its screen or enter alternate-screen mode while the CLIStream header stays visible. The tested versions are pinned in `go.mod`. [Go PTY library](https://github.com/creack/pty) · [Ultraviolet documentation](https://pkg.go.dev/github.com/charmbracelet/ultraviolet) · [VT emulator documentation](https://pkg.go.dev/github.com/charmbracelet/x/vt)
- **Interactive entry points.** Use the framed interface for `clistream`, `clistream PAIRING-LINK`, and interactive `clistream attach ID`. Explicit attach opens the named session directly and returns its exit status on completion. Preserve redirected JSON listing, byte-for-byte scripted attachment, separate stdout/stderr, `-eof`, and existing file and forwarding commands. Pipe sessions remain usable inside the frame. The client does not spawn a local shell.
- **Input and display boundaries.** Send the viewport dimensions to the supervisor after reserving the two header rows. Translate mouse coordinates into that viewport. Treat pasted text as data, including text matching a hotkey. Render remote output and remote-supplied labels within their assigned cells. Restore the original terminal mode, cursor, and screen on exit or error.
- **Process ownership.** Give each managed supervisor a stable ID for its lifetime and each child run a fresh session ID. The dashboard selects a supervisor. Terminal input, replay, files, and forwards remain bound to a specific run. Close the old run’s channels and reap its process group before starting a replacement. Serialize lifecycle actions at the supervisor, and reject requests based on a stale run or state revision. After a lost acknowledgement, the client refreshes supervisor state and waits for the operator’s next action.
- **Protocol and compatibility.** Add capability discovery, supervisor listing, argument retrieval, and lifecycle requests to the authenticated Go protocol. Keep the existing session list limited to attachable running children. Existing decoders reject unknown fields, so use separately negotiated operations and preserve existing message shapes on legacy paths. Older supervisors appear as **Attach only**. Against an older daemon, the new client can still display and attach to its running sessions. A new supervisor reports the legacy limitation and uses one-shot behavior there.
- **Permissions and logging.** Apply the existing enrolled-device trust model to management requests. Recheck enrollment and the target supervisor before dispatch. Record the requesting device, action, supervisor and run IDs, and outcome in the daemon activity log. Keep argument values out of listings, logs, and error text. The editor sends argument values directly as an argument vector without shell expansion.
- **Apple clients.** The first release delivers management controls in the Go terminal client on Linux and macOS. Native Mac, iPhone, and iPad clients continue to list and attach to running sessions. A stop or restart ends their old attachment, and the replacement appears as a new running session.

## Launch examples

Use `clistream-supervise /bin/sh` to keep a shell available for another start after exit. Use `clistream-supervise -once /bin/sh` when a script needs the exit status of that invocation.

In the argument editor, ↑ and ↓ select entries, ← and → move the cursor, Enter inserts an empty argument, and Ctrl-D removes the selected entry. Ctrl-R opens the start or restart confirmation. Tab selects the action, Enter confirms, and Escape cancels.
