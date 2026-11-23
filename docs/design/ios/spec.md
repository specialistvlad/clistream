# CLIStream for iOS 27: interface specification

This specification defines the target design for the universal iPhone and iPad app on its three targets: iPhone Pro, iPad and iPhone Duo. Measurements and colors come from Apple's iOS 27 UI Kit and live in [tokens.json](tokens.json). Each section names the SwiftUI component that produces the element, so the system draws its glass, navigation, lists, sheets and keyboard, and the app draws only the terminal and the content of its rows.

## Principles

1. **System chrome, app content.** `NavigationSplitView`, `List`, toolbars, menus, sheets, alerts, swipe actions and the keyboard come from the system. The app customizes the terminal surface and the key bar's contents.
2. **One layout per size class.** Compact width (iPhone Pro, Duo outer screen) navigates from the servers list to the terminal. Regular width (iPad, Duo inner screen) shows the sidebar and terminal together, with Ports as an inspector.
3. **Same commands as the Mac.** Restart…, Stop…, Send EOF, Ports, Show in Files and Detach carry the same wording, order and confirmations as the macOS design in [../macos/spec.md](../macos/spec.md).

## Layout by target

| Target | Size | Layout |
|---|---|---|
| iPhone Pro | 402 × 874 | `NavigationSplitView` collapsed to a stack: Servers list, then the terminal. |
| iPad Pro 13-inch | 1376 × 1032 landscape | Sidebar 330 pt, terminal, and an optional 360 pt Ports inspector. |
| iPhone Duo, outer | 466 × 678 | iPhone layout. An 84 pt trailing column holds the camera, time and Wi-Fi, and toolbar buttons stack vertically below them. Inline titles at the top leading edge. |
| iPhone Duo, inner | 951 × 669 landscape, 669 × 951 portrait | iPad layout with a 272 pt sidebar and a 300 pt inspector. The top-trailing camera corner stays clear, so toolbar groups end before it. |

The selected run stays open across rotation, iPad resizing and Duo folding.

## Servers list (compact)

| Element | Specification | Component |
|---|---|---|
| Title | `Servers`, Large Title 34 Bold. On the Duo outer screen, an inline Headline title. | `.navigationTitle` |
| Toolbar | Trailing glass capsule with **Scan QR Code** (`qrcode.viewfinder`), **Add Server** (`plus`) and **Layout and grouping** (`list.bullet.indent`). Stacked circles in the same order on the Duo outer screen. | `.toolbar` |
| Sections | One inset grouped section per server: header in Headline, secondary color, with trailing latency (`2 ms`). Rows 52 pt, radius 26, 16 pt margins. | `List` with `.insetGrouped` style |
| Host row | `laptopcomputer` for macOS, `desktopcomputer` for others, host name, trailing OS name, disclosure chevron. | `DisclosureGroup` |
| CLI row | Indented 44 pt. `terminal` symbol, program name, trailing caption, navigation chevron. | `NavigationLink` |
| Offline server | `wifi.slash`, `Offline` in secondary color, trailing **Retry** in accent. | Row button |
| Search | `Search` field with microphone, at the bottom of the screen, 48 pt. Filters CLI rows by name. | `.searchable` |
| Swipe actions | Trailing swipe on a CLI row: **Restart** (accent) and **Stop** (red), 66 pt rounded actions. Stopped runs offer **Start**. | `.swipeActions` |

CLI row states match the macOS spec: running rows use the accent symbol, stopped rows show a gray symbol and `Exit N`, and one-shot runs show `Attach only`.

## Sidebar (regular)

The iPad and Duo inner sidebar uses the same sections and rows at 44 pt with 17 pt labels and accent symbols. The top has the capsule with Scan QR Code, Add Server and layout, plus the **Hide Sidebar** circle (`sidebar.left`). A 40 pt search field sits below them. Section headers carry a disclosure chevron. Selection is the system pill (radius 22). Use `List(selection:)` with `.listStyle(.sidebar)`.

## Terminal

| Element | iPhone Pro | iPad and Duo inner | Duo outer |
|---|---|---|---|
| Back | 44 pt glass circle, `chevron.left` | Sidebar navigation | Glass circle at the top leading edge |
| Title | Inline, centered: program name in Headline, `HOST — SERVER` in Caption 1 | Leading: program name 17 Bold, subtitle Footnote | Leading, beside the back button |
| Toolbar | Glass capsule: **Ports** (`network`, opens a sheet) and **Session** (`ellipsis`, opens the menu) | Capsules: Restart… and Stop…; Show in Files and Ports (`sidebar.right`, toggles the inspector); More (`ellipsis`) | Stacked circles: Ports, Session, Show Keyboard |
| Terminal | SF Mono 12, 14 pt horizontal inset | SF Mono 13, 24 pt inset | SF Mono 12 |
| Status line | 8 pt dot, `Connected · 2 ms`, trailing `PTY` in Footnote | Same, trailing `PTY · 132 × 38` | Same |

### Keyboard and key bar

With the onscreen keyboard up, the terminal key bar floats 8 pt above the system keyboard as one 44 pt glass capsule, as iOS 27 floats input accessories. Its keys, in order: `esc`, `ctrl` (latches), Tab (`arrow.right.to.line`), the four arrows, Pointer mode (`hand.point.up.left`, accent while on), and Hide Keyboard (`keyboard.chevron.compact.down`). The bar scrolls horizontally to reveal `~`, `|`, `/`, `-` and, on iPad, F1–F8. With the keyboard down, a 48 pt **Show Keyboard** circle (`keyboard`) sits at the bottom trailing edge. Build the bar as an `inputAccessoryView` on the terminal.

## Session menu

The Session button opens a glass menu, 250 pt wide with radius 28, anchored below the toolbar.

| Group | Items |
|---|---|
| Control row | **Restart…**, **Stop…** (red), **Send EOF** (dimmed for a PTY). Icon above a 13 pt label. Stopped runs show **Start…**. |
| Actions | **Ports** (`network`), **Show in Files** (`folder`) |
| Session | **Detach** (`rectangle.portrait.and.arrow.right`) |

Every item carries an icon, as iOS menus do. Use `Menu` with a `ControlGroup` for the top row.

## Sheets

Sheets follow the kit: inset 8 pt from the screen edges, radius 40, grabber, a glass **Close** circle (`xmark`) at the leading edge, a centered Headline title and an accent **Confirm** circle (`checkmark`) at the trailing edge. Content uses inset grouped sections. Use `.sheet` with `.presentationDetents`.

| Sheet | Detent | Content |
|---|---|---|
| Ports | Medium | One row per export: name, `TCP · 127.0.0.1:8080` subtitle, 64 × 28 toggle. An enabled forward adds a row with the local address in SF Mono and a **Copy** circle. Footer: `Forwards keep running when you close this sheet.` No Confirm button. |
| Add Server | Large | **Invitation** section: masked link field, **Paste Invitation** (`doc.on.clipboard`) and **Scan QR Code** (`qrcode.viewfinder`). Footer: `Scan or paste the invitation from clistreamd. Invitations last three minutes.` **Server** section after a valid link: Address and Identity (SF Mono fingerprint). Footer: `Check the server before pairing. This device can then control every CLI on it.` Confirm reads **Pair**. |
| Restart or Start | Medium | **Command** section with the SF Mono command field. Footer: `Quote arguments that contain spaces. Use "" for an empty argument. Restarting ends the current run for everyone connected.` Confirm reads **Restart** or **Start**. |

### Scan QR Code

Scan QR Code opens a large-detent sheet titled `Scan QR Code`, with Close and no Confirm button, laid out like the Home app's Add Accessory scanner.

| Element | Specification |
|---|---|
| Camera view | Square, full sheet width minus 16 pt margins (354 pt on iPhone Pro), radius 32. Live camera feed. |
| Code frame | Four white corner brackets, 40 pt long and 4 pt thick with a 20 pt radius, framing a 228 pt square in the center. They follow the detected code. |
| Instruction | `Point the camera at the QR code in the clistreamd terminal.` in Headline, centered. Below it, in Subheadline secondary: `Open clistreamd in a terminal to show the code. Invitations last three minutes.` |
| Fallback | **Paste Invitation** (`doc.on.clipboard`) as a 50 pt glass capsule at the bottom. |
| Detected code | A valid `clistream://` link closes the scanner and opens the Add Server sheet with the invitation filled in and the Server section showing, ready for review and **Pair**. Any other code leaves the scanner open. |
| No camera | When scanning is unavailable, the camera view shows `ContentUnavailableView` with `qrcode.viewfinder`, title `Camera Unavailable` and `Paste the invitation instead.` When access is denied, the title is `Allow Camera Access` with `Turn on camera access for CLIStream in Settings to scan QR codes.` and an **Open Settings** button. |

Build the camera view with VisionKit's `DataScannerViewController`, recognizing `.barcode(symbologies: [.qr])`, in a `UIViewControllerRepresentable`. Check `isSupported` and `isAvailable` before starting. The iOS target's `NSCameraUsageDescription` reads `CLIStream uses the camera to scan pairing codes from clistreamd.`

On iPad and the Duo inner screen, Ports opens as the trailing inspector instead of a sheet: heading `Ports` in Title 2, the same grouped rows, and the footer `Forwards keep running when you close this inspector.`

## Alerts

Alerts follow the kit: 300 pt wide, radius 34, left-aligned title in Headline Bold, message in Subheadline secondary, 48 pt capsule buttons. Use `.alert`.

| Alert | Title | Message | Buttons |
|---|---|---|---|
| Stop | `Stop “NAME”?` | `This ends the current run for everyone connected.` | Cancel · **Stop** (red text) |
| Remove server | `Remove “SERVER”?` | `This device forgets the server and its pairing. The device stays enrolled until the server operator revokes it.` | Cancel · **Remove** (red text) |
| Pairing error | `Couldn’t Pair This Device` | `The invitation expired or was replaced. Ask the server operator for a new one with clistreamd invite.` | **OK** (accent), full width |

## Empty state

`ContentUnavailableView`: `network` symbol in accent (56 pt), title `No Servers` (Title 2 Bold), message `Scan the QR code that clistreamd shows, or paste its invitation. Invitations last three minutes.` Two 50 pt capsule buttons at the bottom: **Paste Invitation** (glass, accent text) and **Scan QR Code** (accent fill, with `qrcode.viewfinder`). Add Server stays in the toolbar.

## Features this design introduces on iOS

| Feature | Behavior |
|---|---|
| Floating key bar | Replaces the Session › Keys submenu and the flat key row with one glass capsule above the keyboard. |
| Swipe actions | Restart and Stop from the servers list without opening the terminal. |
| Bottom search | Filters the servers list, as on the Mac. |
| Scan QR Code | Scans the QR code that clistreamd shows with the device camera and opens the Add Server sheet with the invitation, on iPhone, iPad and both Duo screens. Scanning with the system Camera app keeps working as before. |
| Paste Invitation | Reads a `clistream://` link from the pasteboard into the Add Server sheet. |
| Show in Files | Opens the session workspace in the Files app. Requires the iOS Files extension that [Native Apple clients](../../apple-client-design.md#finder-workspaces) lists as deferred. |
| Remove Server… | Long-press a server header, as on the Mac. |

## Screens

| Screen | Board | Shipping screenshot |
|---|---|---|
| Servers, light | `IPhoneList` | `iphone/servers-tree.png` |
| Servers, dark, swipe actions on `sh` | `IPhoneListDark` | none |
| Terminal with keyboard and key bar | `IPhoneTerminal` | `iphone/terminal.png` |
| Session menu, dark | `IPhoneMenu` | `iphone/terminal-keys.png` |
| Ports sheet | `IPhonePorts` | `iphone/port-forwards.png` |
| Add Server sheet, link pasted | `IPhonePair` | `iphone/add-server.png` |
| Scan QR Code | `IPhoneScan` | none |
| Restart sheet | `IPhoneCommand` | `iphone/edit-command.png` |
| Stop alert | `IPhoneStop` | `iphone/stop-confirmation.png` |
| First launch | `IPhoneEmpty` | `iphone/welcome.png` |
| Pairing error | `IPhonePairError` | `iphone/pairing-error.png` |
| iPad, sidebar and terminal | `IPadMain` | `ipad/terminal.png` |
| iPad, dark, Ports inspector | `IPadDark` | `ipad/port-forwards.png` |
| Duo outer, servers | `DuoOuterList` | `iphone-duo/duo-closed.png` |
| Duo outer, terminal, dark | `DuoOuterTerminal` | `iphone-duo/terminal.png` |
| Duo inner, sidebar and terminal | `DuoInner` | `iphone-duo/duo-open.png` |
| Duo inner, dark, Ports inspector | `DuoInnerPorts` | none |

## Guidelines applied

| HIG page | Rule |
|---|---|
| [Layout](https://developer.apple.com/design/human-interface-guidelines/layout) | Adapt to compact and regular widths. Keep controls clear of the Dynamic Island and the Duo camera regions. |
| [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars) | Two levels, accent symbols, a show and hide button. |
| [Lists and tables](https://developer.apple.com/design/human-interface-guidelines/lists-and-tables) | Inset grouped sections, swipe actions for frequent row actions. |
| [Sheets](https://developer.apple.com/design/human-interface-guidelines/sheets) | Detents sized to content, Close at the leading edge, Confirm at the trailing edge. |
| [Menus](https://developer.apple.com/design/human-interface-guidelines/menus) | Icons on every item, a control row for the most frequent actions, destructive items in red. |
| [Virtual keyboards](https://developer.apple.com/design/human-interface-guidelines/virtual-keyboards) | Custom input accessory above the system keyboard, with a dismiss key. |
