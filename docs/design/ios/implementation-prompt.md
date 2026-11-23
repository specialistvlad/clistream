# Prompt: implement the CLIStream iOS redesign

Give this file to a coding agent working in this repository.

---

You are implementing the iOS 27 redesign of the CLIStream app on its three targets: iPhone Pro, iPad and iPhone Duo. The design is approved by the owner. Your job is to make the universal iOS app match it with Apple's system components, keeping the Mac app as it is.

## What exists

| Source | Contents | Read it |
|---|---|---|
| [spec.md](spec.md) | Every screen and state per target, with exact copy, measurements and the SwiftUI component for each element. | First, end to end. It is the source of truth. |
| [tokens.json](tokens.json) | Device sizes, light and dark colors, type, metrics and SF Symbol names, tagged with their iOS 27 UI Kit source. | When checking rendered values or picking a symbol. |
| [boards/Phone.dc.html](boards/Phone.dc.html), [boards/Pad.dc.html](boards/Pad.dc.html) | Canvas sources. Their `renderVals()` data holds every state; the props table in [README.md](README.md) lists them. | When spec.md leaves a measurement or layout detail open. |
| [../macos/spec.md](../macos/spec.md) | The Mac design, which shares commands, wording and confirmations with iOS. | When a command's wording or behavior needs confirming. |
| [Design canvas](https://claude.ai/artifact/KFDbtxBvYkSZN6o9KpHJCD) | Page **iOS · iPhone, iPad and Duo**: interactive boards, shipping-app screenshots and iOS 27 kit renders. Owner access only. | When you have Claude's Artifact tool and want to see a board rendered. |
| `docs/screenshots/iphone`, `ipad`, `iphone-duo` | The shipping iOS app, captured by CI. Local and gitignored; they may be absent. | To compare before and after. |

## Where the current interface lives

| File | Draws |
|---|---|
| `apple/Mobile/MobileApp.swift` | iOS `NavigationSplitView`, compact and regular navigation, empty state |
| `apple/App/WorkspaceBrowser.swift` | `WorkspaceSidebar` (shared with the Mac), offline Retry button for iOS, `WorkspaceStatusBar` |
| `apple/App/SessionPane.swift` | Terminal pane and the connection status text under it |
| `apple/App/SessionControls.swift` | iOS Session menu with the Keys submenu, command sheet, stop alert, Ports sheet and inspector |
| `apple/App/Terminal.swift` | SwiftTerm terminal and its keyboard accessory |
| `apple/App/Pairing.swift` | Pair sheet, `clistream://` link handling (`onOpenURL`) and pairing errors |
| `apple/Info.plist`, `apple/project.yml` | One Info.plist shared by the iOS and macOS targets; XcodeGen project definition |
| `apple/UITests/SessionTests.swift`, `NativeSessionTests.swift`, `DuoPoseTests.swift` | UI suites the hosted matrix runs on iPhone, iPad and Duo |

`apple/App` is shared with the Mac app, which already ships its own redesign. Put iOS-only changes behind `#if os(iOS)` or into `apple/Mobile`, so the Mac keeps its behavior.

## Rules that shape every change

- **System components first.** Build each element with the component spec.md names. Hardcode only values marked `appDrawn` in tokens.json.
- **Copy is exact.** Use the strings from spec.md word for word, including curly quotes and ellipses.
- **System rendering wins.** When the rendered app differs from a kit value and the element is a system component, keep the system's rendering.
- **Size classes, not devices.** Drive layout from horizontal size class: compact for iPhone Pro and the Duo outer screen, regular for iPad and the Duo inner screen. Keep toolbar items clear of the Dynamic Island and the Duo camera regions.
- **Touch-only operation.** Every action works by touch alone, as [Native Apple clients](../../apple-client-design.md#touch-only-operation-on-iphone-and-ipad) requires; hardware keyboards and pointers add to it.
- **Stable test hooks.** Keep the accessibility identifiers and labels the UI suites query, such as `session-<id>`, `session-terminal`, `workspace-add-server`, `workspace-view-options`, `session-ports`, `retry-server`, `server-name-field`, `Hide Keyboard`, `Show Keyboard`, `Control Key` and `Control-C`. When a control moves, for example the Keys submenu into the key bar, keep its label so tests find it, and give new controls identifiers in the same style.
- **Repository process.** [AGENTS.md](../../../AGENTS.md) governs plans, UI automation and delivery. Apple UI tests run only on the hosted runners; local work is compilation and Go tests.
- **Shared `main`.** Other agents commit to `main` while you work, often in the same Apple files. Rebase onto `origin/main` before each push and keep each commit to your own change.
- **Owner's working tree.** `apple/Info.plist` may hold the owner's uncommitted edits. Before you add the camera usage string to it, show the owner the diff and ask how to commit it. Leave every other unrelated change out of your commits.

## Steps

1. **Map the design to the code.** Read spec.md and AGENTS.md. For each row of spec.md's Screens table, note the view that draws it today and the component it becomes. Done when every row has both.
2. **Write the plan.** Add a dated section to `docs/implementation-plan.md` covering documentation, every platform and tests as AGENTS.md requires. State why the Go daemon, supervisor, CLI and the Mac app need no changes. Present it to the owner. Done when the owner approves it.
3. **Ask the one open question.** **Show in Files** needs the iOS Files extension that [Native Apple clients](../../apple-client-design.md#finder-workspaces) lists as deferred; the Mac's Finder provider already exists. Ask the owner whether to build the iOS extension in this change or ship the item hidden until it exists. Done when the answer is recorded in the plan.
4. **Build in slices.** Land each slice as a compiling, working increment, in this order:
   1. Servers list: inset grouped server sections, host disclosure rows, CLI rows with states, offline Retry, bottom search, swipe actions.
   2. Sidebar for regular width: 44 pt rows, pill selection, search field, Add Server and layout capsule, Hide Sidebar.
   3. Terminal toolbar per target: inline title and subtitle, Ports and Session on iPhone, capsule groups on iPad and the Duo inner screen, stacked circles on the Duo outer screen. On iPad and the Duo inner screen, connection status and PTY size move into the navigation subtitle and the bottom status line goes away.
   4. Key bar: the floating glass capsule above the keyboard, the Show Keyboard circle, horizontal overflow for `~ | / -` and F1–F8.
   5. Session menu: control row and actions, replacing the Keys submenu.
   6. Sheets and inspector: Ports (sheet on compact, inspector on regular), Add Server with Paste and Scan QR Code, Restart and Start.
   7. Scan QR Code: the VisionKit scanner sheet, its unavailable and denied states, the camera usage string, and the hand-off to Add Server.
   8. Alerts and empty state: Stop, Remove Server, pairing error, first launch.
   9. Show in Files per step 3.

   Done for each slice when the iOS app builds locally:

   ```sh
   make apple
   cd apple && xcodebuild -scheme CLIStream-iOS -destination 'generic/platform=iOS Simulator' build
   ```

   Build the macOS scheme the same way after any change to shared code.
5. **Update the tests.** Adjust `SessionTests`, `NativeSessionTests` and `DuoPoseTests` to the new list, toolbar, key bar and menu. Add cases for swipe Restart and Stop, the key bar keys, search filtering, Paste, the Ports inspector on regular width and the Duo outer toolbar column. Simulators have no camera, so cover Scan QR Code through its button, the `Camera Unavailable` state and a test hook that hands a scanned link to Add Server. Done when every spec.md feature has a test that would fail without it.
6. **Update the docs.** Bring `docs/apple-client-design.md` (pairing, touch-only operation and session controls), the README feature table, `docs/requirements.md` where behavior changes, and `docs/implementation-plan.md` in line with the shipped behavior. Change spec.md only where the owner approves a deviation. Done when every document describes what the app now does.
7. **Verify on the hosted matrix.** Push to `main` as AGENTS.md describes. The Test workflow runs macOS, iPhone, iPad, Duo and the shared four-client suite. Rerun single devices or cases with **Actions › Test › Run workflow**. Compare the iPhone, iPad and Duo gallery screenshots with each row of spec.md's Screens table, light and dark, including the Duo's closed, partially open and open poses. Record commands, run links and results in `docs/verification.md`. Done when all five matrix jobs pass and every Screens row has a matching screenshot.
