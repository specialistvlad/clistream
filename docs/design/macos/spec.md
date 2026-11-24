# CLIStream for macOS 27: interface specification

This specification defines the target design for the macOS app's main window, sheets, alerts and menus. Measurements and colors come from Apple's macOS 27 UI Kit and live in [tokens.json](tokens.json). Each section names the SwiftUI or AppKit component that produces the element, so the system draws its chrome and the app draws only the terminal and the content of its rows.

## Principles

1. **System chrome, app content.** NavigationSplitView, toolbar items, `List` selection, menus, sheets, alerts and the inspector come from the system. The app customizes the terminal surface and the content inside rows.
2. **Two levels in the sidebar.** Servers are section headers. Hosts are rows that disclose their CLI rows. Every row pairs an SF Symbol with a label.
3. **One command, three places.** Every toolbar action also appears in the menu bar and in the clicked row's context menu.

## Window

| Element | Specification | Component |
|---|---|---|
| Window | Unified toolbar with title, sidebar and content. System corner radius. Default size 1200 × 740. | `WindowGroup` + `NavigationSplitView` |
| Traffic lights | 14 pt, centers at x 25, 48, 71 and y 26, inside the sidebar. | System |
| Sidebar toggle | 36 pt glass circle at the trailing edge of the sidebar's titlebar area, symbol `sidebar.left`. | System (`SidebarCommands`) |
| Sidebar toolbar | Capsule with **Add Server** (`plus`) and the **Layout and grouping** pull-down (`list.bullet.indent`), 6 pt before the sidebar toggle. It hides with the sidebar; File › Add Server… and the View menu keep both commands. | `ToolbarItem` in the sidebar column |
| Toolbar | 52 pt tall, no background bar, faint scroll-edge line above content. | `.toolbar` |
| Title | Program name, SF Pro Bold 15, label color. | `.navigationTitle` |
| Subtitle | `HOST — SERVER`, SF Pro Medium 11, tertiary label. | `.navigationSubtitle` |
| Content | Terminal surface, opaque, inset 10 pt vertically and 16 pt horizontally. | `LiveTerminal` (SwiftTerm) |
| Status bar | 26 pt with a top separator. Leading: 8 pt dot plus `Connected · 2 ms` or `Disconnected`. Trailing: `PTY`, `Pipe` or `Pipe · Stopped`. | App view below the content |

The window title names the content. With no server paired, the title is `Servers` and the subtitle is `None paired`.

## Sidebar

Width 256 pt (210–400). Rows sit 10 pt from both sidebar edges. Use `List(selection:)` with `.listStyle(.sidebar)`, so the system draws selection, focus and row height.

### Rows

| Row | Content | Specification |
|---|---|---|
| Server section header | Server display name, trailing latency (`2 ms`) | SF Pro Bold 11, tertiary label, 18 pt. A disclosure chevron appears on hover. `Section(isExpanded:)` |
| Offline server | `wifi.slash`, `Offline`, small **Retry** button | Replaces the host rows while the server is unreachable. |
| Revoked server | `wifi.slash`, `Access revoked`, small **Pair Again…** button | Shown when the server rejects this device's key. |
| Host row | Disclosure chevron, `laptopcomputer` for macOS hosts or `desktopcomputer` for others, host name, trailing OS name (`macOS`, `Linux`) | Level 0, text at x 50. `DisclosureGroup` |
| CLI row | `terminal` symbol, program name, trailing caption | Level 1, text at x 62, 32 pt (medium size). |

CLI row states:

| State | Symbol color | Trailing caption |
|---|---|---|
| Running | Accent | none |
| Stopped | Gray (`stoppedSymbol`) | `Exit N` |
| Attach only (`--once`) | Accent | `Attach only` |
| Server revoked | Gray | none |
| Duplicate name on one host | Accent | first 8 characters of the supervisor ID |

Selection is the system's rounded highlight (radius 8): gray while the terminal has keyboard focus, accent while the sidebar has it. The secondary-click target shows the system's 2 pt accent ring.

### Layout and grouping

The sidebar toolbar's layout pull-down and the View menu set two remembered preferences.

| Preference | Values | Result |
|---|---|---|
| Group by | Server (default), Host, CLI Name | Section headers name the chosen group. |
| Layout | Tree (default), List | Tree adds the disclosure level below each section. List shows CLI rows directly under the section, with the other grouping value as a trailing caption. |

### Context menus

No keyboard shortcuts, unavailable items hidden, and an ellipsis on items that need more input. The CLI menu offers the toolbar's session commands in toolbar order with the toolbar's symbols, on every item of a group: Restart (`arrow.counterclockwise`), Stop (`stop`) and Edit Arguments… (`pencil`), then Show in Finder (`folder`) and Show Ports (`sidebar.right`). Remove Server… uses `trash`, the standard Delete icon. The tab and window group and the other server items are plain text.

| Target | Items |
|---|---|
| CLI row | Open in New Tab · Open in New Window — Restart (Start when stopped) · Stop · Edit Arguments… — Show in Finder · Show Ports |
| Server header | Rename… · Reconnect — Remove Server… |

**Rename…** turns the header into an inline text field with the system focus ring. Return saves and Escape cancels. Return on a selected server header starts rename, as in Finder.

## Toolbar items

Toolbar items are 36 pt glass circles or capsules drawn by the system. Prefer symbols over text inside groups.

| Position | Item | Visible when |
|---|---|---|
| Trailing group | Capsule: **Restart** (`arrow.counterclockwise`), **Stop** (`stop`) and **Edit Arguments…** (`pencil`) | Running managed run |
| Trailing group | **Send EOF** (`text.append`) in the same capsule | Pipe run |
| Trailing | **Start**, tinted accent with `play.fill`, the window's one prominent action, followed by an **Edit Arguments…** circle (`pencil`) | Stopped run |
| Trailing | `Attach only`, plain tertiary text | One-shot run |
| Trailing | **Reconnect** circle (`arrow.clockwise`) | Server revoked or attachment lost |
| Trailing | Capsule: **Show in Finder** (`folder`) and **Ports** (`sidebar.right`, pressed while the inspector is open) | Any session. Show in Finder hides when the supervisor runs with `--files=FALSE` or access is revoked. |
| Trailing | **CLIStream on GitHub** circle (`chevron.left.forwardslash.chevron.right`), tooltip `Open the CLIStream project on GitHub`, opens `https://github.com/specialistvlad/clistream` | Always |
| Trailing edge | **Search CLIs** field, 160 × 36, placeholder `Search`, filters the sidebar | Always |

Restart stops the run and starts it again with the same arguments. Start launches a stopped program with its last arguments. Stop ends the run for every viewer. All three act at once, so their labels carry no ellipsis. **Edit Arguments…** opens the Edit Arguments sheet.

## Terminal content

| State | Content |
|---|---|
| Running | Live terminal with a block cursor. |
| Stopped | Retained output of the last run. A bottom strip shows `stop`, `Exited with status N · showing the last run’s output` and a small **Start** button. |
| Revoked | The last output stays visible without a cursor. A bottom strip shows `exclamationmark.triangle.fill` in the warning color, `This device’s access to SERVER was revoked. Pair again with a fresh invitation.` and **Pair Again…**. The status bar reads `Disconnected`. |

## Ports inspector

A trailing inspector, 280 pt (230–400), toggled by the Ports toolbar item and **Session › Show Ports**. It has no Done button. Use `.inspector` with a grouped `Form`.

- Heading `Ports`, SF Pro Bold 13.
- A grouped box with one row per named export: name, `TCP · 127.0.0.1:8080` caption and a mini switch (36 × 16).
- When a forward is on, a second line shows the local address in SF Mono, a small **Copy** button and, for TCP, a small **Open** button that opens `http://` plus the local address in the default browser.
- Footer: `Forwards keep running when you close this inspector.`
- With no exports: `This session has no shared ports.`

## Menu bar

Standard order: app menu, File, Edit, View, Session, Server, Window, Help. The Session menu carries the same commands as the CLI context menu in the same order, with the toolbar's symbols; the Server menu matches the server context menu. Icons cover a whole group or none: Remove Server… uses `trash`, the system supplies the Dictation and Emoji icons, and the other groups are plain text. Main menus show shortcuts and dim unavailable commands. Every shortcut uses Command, so Control combinations reach the terminal.

| Menu | Items |
|---|---|
| File | New Window ⌘N · New Tab ⌘T — Add Server… ⌘K · Paste Invitation — Close Window ⌘W |
| Edit | Undo ⌘Z · Redo ⇧⌘Z — Cut ⌘X · Copy ⌘C · Paste ⌘V · Select All ⌘A — Start Dictation… · Emoji & Symbols |
| View | Hide Sidebar ⌃⌘S — as Tree ⌘1 · as List ⌘2 · Group By ▸ (Server, Host, CLI Name) — Enter Full Screen |
| Session | Open in New Tab · Open in New Window — Restart ⌘R (Start when stopped) · Stop ⌘. · Edit Arguments… ⌘< · Send EOF · Reconnect — Show in Finder ⌥⌘R · Show Ports ⌥⌘I |
| Server | Rename… · Reconnect — Remove Server… ⌘⌫ |

Restart ⌘R, Stop ⌘. and Edit Arguments… ⌘< follow Xcode's Run, Stop and Edit Scheme…. Implement with `.commands`: `CommandGroup` for File and View, `CommandMenu("Session")` and `CommandMenu("Server")`, routed to the key window through focused scene values.

## Sheets

Sheets follow the kit's dialog: 440 pt wide, radius 24, title SF Pro Bold 13, message SF Pro Medium 11. Form labels align trailing in an 84 pt column. Text fields are 24 pt with the 3.5 pt accent focus ring. Buttons are 24 pt rounded rectangles (radius 6, minimum width 76) at the trailing edge, with Cancel as a gray fill and the default action as an accent fill. The window behind dims. Use `.sheet` with a `Form`.

| Sheet | Title and message | Rows | Buttons |
|---|---|---|---|
| Pair, empty | `Pair with a server` · `Paste the invitation from clistreamd. Invitations last three minutes.` | Invitation: focused field, placeholder `clistream://host/?token=…` | Cancel · **Pair** (disabled until the link is valid) |
| Pair, link pasted | `Pair with a server` · `Check the server before pairing. This device can then control every CLI on it.` | Invitation: the link in SF Mono, `clistream://192.168.1.20:7443/?token=…` · Server: `192.168.1.20:7443` · Identity: fingerprint in SF Mono | Cancel · **Pair** |
| Edit Arguments | `Edit Arguments for “NAME”` · `Saving restarts “NAME” for everyone connected.` (stopped: `Saving starts “NAME” with this command.`) | Command: focused SF Mono field, prefilled with the current command. Help below: `Quote arguments that contain spaces. Use "" for an empty argument.` | Cancel · **Restart** or **Start** |

## Alerts

Alerts follow the kit's side-by-side alert: 260 pt wide, 22 pt padding, radius 18, title SF Pro Bold 13, message SF Pro Regular 13, left aligned. Buttons are 28 pt capsules sharing the width. When the main action is destructive, it uses the red-tinted style and Cancel is the default accent button. Use `.alert` or `.confirmationDialog`.

| Alert | Title | Message | Buttons |
|---|---|---|---|
| Remove server | `Remove “SERVER”?` | `This Mac forgets the server and its pairing. The device stays enrolled until the server operator revokes it.` | Remove (destructive) · **Cancel** |
| Pairing error | `Couldn’t pair this device` | `The invitation expired or was replaced. Ask the server operator for a new one with clistreamd invite.` | **OK**, full width |

## Empty window

`ContentUnavailableView` with the `network` symbol (48 pt, tertiary), title `Pair with a server` (SF Pro Bold 17), message `Open an invitation from clistreamd, or paste one. Invitations last three minutes.`, and two push buttons: **Paste Invitation** (gray) and **Add Server…** (accent). The sidebar shows `Add a server to see its CLI apps.`

## Features this design introduces

| Feature | Behavior |
|---|---|
| Search CLIs | Filters sidebar rows by program name. Servers and hosts without matches hide while a query is active. |
| Paste Invitation | Reads a `clistream://` link from the pasteboard and opens the Pair sheet with it filled in. |
| Remove Server… | Deletes the local server profile and its enrollment directory after the alert confirms. Server-side revocation stays with the operator. |
| Show in Finder | Opens the session's shared workspace in Finder through the CLIStream File Provider domain, at `CLIStream › SERVER › HOST › PROGRAM`. Requires the replicated File Provider described under [Future file access](../../apple-client-design.md#future-file-access). |
| Context menu lifecycle actions | Restart, Stop, Edit Arguments…, Show in Finder and Show Ports activate the clicked row, then run the same flow as the toolbar. |
| Edit Arguments… | Edits the command line in a sheet, then restarts a running program or starts a stopped one. Restart and Stop act at once with the current arguments. |
| Open port | Opens an enabled TCP forward in the default browser from the Ports inspector. |
| GitHub link | Opens the CLIStream project on GitHub from the toolbar. |
| Menu bar commands | The Session, Server and View items above, enabled by the key window's selection. |

## Screens

Each screen below is a board on the design canvas. Shipping screenshots with the same subject live in `docs/screenshots/macos`.

| Screen | Key state | Shipping screenshot |
|---|---|---|
| Main window, light | `sh` running, PTY | `servers-tree.png` |
| Main window, dark | Ports inspector open, CLI context menu on `codex` | `port-forwards-dark.png`, `open-session-menu.png` |
| Server context menu | Search field focused with query `co` | none |
| Inline server rename | Dark, stopped `worker` selected | `rename-server.png` |
| First launch | No servers | `welcome.png` |
| Access revoked | Server rejected this device | `disconnected.png` |
| Pair sheet, empty | Invitation field focused | `add-server.png` |
| Pair sheet, link pasted | Server and identity shown | `confirm-server.png` |
| Pairing error | Expired invitation | `pairing-error.png` |
| Edit Arguments sheet | Command field focused | `edit-command.png` |
| Remove Server alert | Destructive confirmation | none |
| Menu bar | File, Edit, View, Session and Server open | none |

## Guidelines applied

| HIG page | Rule |
|---|---|
| [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars) | At most two levels. Symbols use the accent color. Show and hide from the View menu. Keep critical actions away from the sidebar's bottom edge. |
| [Toolbars](https://developer.apple.com/design/human-interface-guidelines/toolbars) | Items have no bezel. At most three groups. Title names the content. Every toolbar item is also a menu bar command. |
| [Materials](https://developer.apple.com/design/human-interface-guidelines/materials) | Liquid Glass for controls and navigation. The terminal stays in the content layer. |
| [Windows](https://developer.apple.com/design/human-interface-guidelines/windows) | System window chrome. The bottom bar carries only small related details. |
| [Context menus](https://developer.apple.com/design/human-interface-guidelines/context-menus) | No shortcuts, unavailable items hidden, items also reachable from the main interface. |
| [Menus](https://developer.apple.com/design/human-interface-guidelines/menus) | Icons sparingly and with purpose, on every item of a group or none. Title-style capitalization, an ellipsis for commands that ask for input, submenus one level deep. |
