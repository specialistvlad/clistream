# Prompt: implement the CLIStream macOS redesign

Give this file to a coding agent working in this repository.

---

You are implementing the macOS 27 redesign of the CLIStream Mac app. The design is finished and approved by the owner. Your job is to make the shipping app match it, using Apple's system components, and to keep the iPhone and iPad apps working unchanged.

## Revision of November 24, 2026

The first implementation of this design has shipped. The owner then revised it, and spec.md, tokens.json and the boards hold the revised design. Implement these changes. The Mac interface now lives in `apple/Desktop` (`DesktopApp`, `DesktopCommands`, `DesktopSidebar`, `DesktopSheets`, `DesktopTerminal`), with session models and the command field in `apple/App`.

1. **Run controls act at once.** Restart stops the run and starts it again with the same arguments. Start launches a stopped program with its last arguments. Stop ends the run with no alert. Their labels lose the ellipsis in the toolbar, menus and the stopped strip.
2. **Edit Arguments…** (`pencil`) opens the command sheet, now titled `Edit Arguments for “NAME”`. Confirming restarts a running program or starts a stopped one. It follows Stop in the toolbar group, sits beside Start on a stopped run, and appears in the CLI context menu and in Session as ⌘<.
3. **One command set.** The CLI context menu and the Session menu list the toolbar's commands in toolbar order with its symbols. Show Ports moves from View to Session, keeping ⌥⌘I.
4. **Sidebar toolbar.** Add Server and Layout and grouping move into the sidebar's toolbar area, before the sidebar toggle.
5. **GitHub link.** A **CLIStream on GitHub** circle before Search opens `https://github.com/specialistvlad/clistream`.
6. **Open port.** The Ports inspector adds **Open** beside Copy for enabled TCP forwards.
7. **Visible invitation.** The Pair sheet shows the invitation link in SF Mono.

Follow steps 2, 4, 5 and 7 below for these changes. Update the UI cases that confirm the Stop alert or reach the command sheet through Restart, and add cases for Edit Arguments…, Open and the GitHub link. [Native Apple clients](../../apple-client-design.md) already describes the revised behavior.

## What exists

The design was built on a canvas with Apple's macOS 27 UI Kit, then written into this folder:

| Source | Contents | Read it |
|---|---|---|
| [spec.md](spec.md) | Every screen, state, menu, sheet and alert, with exact copy, measurements and the SwiftUI component for each element. | First, end to end. It is the source of truth. |
| [tokens.json](tokens.json) | Light and dark colors, type, metrics and SF Symbol names, tagged with their Apple UI Kit source. | When checking rendered values or picking a symbol. |
| [boards/Main.dc.html](boards/Main.dc.html) | Canvas source of the main window. Its `renderVals()` data holds every state; the props table in [README.md](README.md) lists them. | When spec.md leaves a measurement or layout detail open. |
| [boards/MenuBar.dc.html](boards/MenuBar.dc.html) | Canvas source of the menu bar, with every item and shortcut. | When building the `.commands`. |
| [Design canvas](https://claude.ai/artifact/KFDbtxBvYkSZN6o9KpHJCD) | Interactive boards on three pages: New design, Previous implementation (shipping-app screenshots and a comparison with Apple's guidance), Apple reference (kit renders). Owner access only. | When you have Claude's Artifact tool and want to see a board rendered. |
| `docs/screenshots/macos` | The shipping app on macOS 27.2, captured by CI. Local and gitignored; it may be absent. | To compare before and after. |

## Where the current interface lives

| File | Draws |
|---|---|
| `apple/Desktop/DesktopApp.swift` | Mac window, `NavigationSplitView`, tabs and separate windows, empty state |
| `apple/App/WorkspaceBrowser.swift` | `WorkspaceSidebar` (tree, list, grouping, rename sheet, offline Retry), `WorkspaceTerminal`, `WorkspaceStatusBar` |
| `apple/App/SessionControls.swift` | Toolbar session actions, command sheet, stop and restart alerts, Ports inspector |
| `apple/App/Pairing.swift` | Pair sheet and pairing errors |
| `apple/App/Workspace.swift` | Server profiles, rename, retry, attachments |
| `apple/App/SessionPane.swift`, `Terminal.swift` | Terminal surface and retained output |
| `apple/UITests/NativeSessionTests.swift`, `SessionTests.swift` | UI suites the hosted matrix runs |

The sidebar, sheets and controls in `apple/App` are shared with iPhone and iPad. Put Mac-only changes behind `#if os(macOS)` or into `apple/Desktop`, so the mobile layouts keep their current behavior.

## Rules that shape every change

- **System components first.** Build each element with the component spec.md names. The system draws glass, selection, focus, row height and spacing. Hardcode only values marked `appDrawn` in tokens.json.
- **Copy is exact.** Use the strings from spec.md word for word, including curly quotes and ellipses.
- **System rendering wins.** When the rendered app differs from a kit value and the element is a system component, keep the system's rendering.
- **Stable test hooks.** Keep the accessibility identifiers the UI suites query: `session-<id>`, `session-terminal`, `workspace-add-server`, `workspace-view-options`, `session-ports` (it now also closes the inspector, replacing the Done button's `dismiss-ports`), `session-send-eof`, `retry-server`, `server-name-field`, `desktop-session-status`, the `Server <name>` label on server headers, and the menu titles `Rename…`, `Open in New Tab` and `Open in New Window`. Give new controls identifiers in the same style.
- **Repository process.** [AGENTS.md](../../../AGENTS.md) governs plans, UI automation and delivery. Apple UI tests run only on the hosted runners; local work is compilation and Go tests.

## Steps

1. **Map the design to the code.** Read spec.md and AGENTS.md. For each row of spec.md's Screens table, note the view that draws it today and the component it becomes. Done when every row has both.
2. **Write the plan.** Add a dated section to `docs/implementation-plan.md` that covers documentation, every platform and tests as AGENTS.md requires. State why the Go daemon, supervisor and CLI need no changes, and how the iPhone and iPad apps stay unchanged. Present it to the owner. Done when the owner approves it.
3. **Ask the one open question.** **Show in Finder** opens the session's workspace through a File Provider domain, which [Future file access](../../apple-client-design.md#future-file-access) plans and the app lacks today. Ask the owner whether to build the File Provider in this change or ship the button hidden until it exists. Done when the answer is recorded in the plan.
4. **Build in slices.** Land each slice as a compiling, working increment, in this order:
   1. Sidebar: `List(selection:)`, server sections, host disclosure rows, CLI row states, context menus, inline rename on Return.
   2. Toolbar: title and subtitle, session group with Edit Arguments…, Start as the prominent action, Reconnect, Show in Finder and Ports capsule, GitHub circle, search field. Add Server and layout capsule in the sidebar's toolbar area.
   3. Content: terminal inset, stopped and revoked strips, status bar.
   4. Ports inspector: grouped `Form`, mini switches, no Done button.
   5. Menu bar: File, View, Session and Server commands routed to the key window through focused scene values.
   6. Sheets and alerts: Pair (empty and pasted), Edit Arguments, Remove Server, pairing error.
   7. New features: Search CLIs, Paste Invitation, Remove Server… (delete the profile, its enrollment directory and its saved name), Show in Finder per step 3.

   Done for each slice when the macOS app builds locally:

   ```sh
   make apple
   cd apple && xcodebuild -scheme CLIStream-macOS -destination 'generic/platform=macOS' build
   ```

   Build the iOS scheme the same way after any change to shared code.
5. **Update the tests.** Adjust `NativeSessionTests` and `SessionTests` to the new sidebar, toolbar and menus. Add cases for context-menu Restart and Stop, menu bar commands, search filtering, Paste Invitation, Remove Server and the revoked state. Done when every spec.md feature has a test that would fail without it.
6. **Update the docs.** Bring `docs/apple-client-design.md`, the README feature table, `docs/requirements.md` where behavior changes, and `docs/implementation-plan.md` in line with the shipped behavior. Change spec.md only where the owner approves a deviation. Done when every document describes what the app now does.
7. **Verify on the hosted matrix.** Push to `main` as AGENTS.md describes. The Test workflow runs macOS, iPhone, iPad, Duo and the shared four-client suite. Rerun single devices or cases with **Actions › Test › Run workflow**. Compare the macOS gallery screenshots with each row of spec.md's Screens table, light and dark. Record commands, run links and results in `docs/verification.md`. Done when all five matrix jobs pass and every Screens row has a matching screenshot.
