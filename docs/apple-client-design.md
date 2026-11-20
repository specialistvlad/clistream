# Apple client interaction design

A Mac user should be able to pair with a server, select a running CLI, type at its prompt, browse the files it produced and return to the same prompt. On iPhone or iPad, the same tasks should feel familiar to someone who uses the Files app.

Use the longstanding Apple conventions of consistency, direct manipulation, visible feedback and user control. A selected row should look selected. Back should take the user to the place they just left. Use the system controls and appearance supplied by each supported OS. [Apple’s original design principles](https://developer.apple.com/library/archive/referencelibrary/GettingStarted/RoadMapiOS-Legacy/chapters/RM_iHIG_Station/Fundamentals/Fundamentals.html) · [Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines/design-principles)

## Pair and open a session

On first launch, offer **Pair**. On a Mac, the user can paste an invitation or open its link directly. On iPhone or iPad, scanning the server’s QR code opens the same confirmation sheet. Show the server address with **Pair device** and **Cancel**. If the invitation has expired, explain that the user can request a fresh one from the running server.

After pairing, show all sessions registered on that server. Use the CLI name as the row title and the host and mode as secondary text so two sessions running the same utility are distinguishable. Selecting a session attaches to its existing process. Keep the session name visible in both **Terminal** and **Files**. Distinguish a connected server with no running sessions from a server the client cannot reach.

| Platform | Navigation |
|---|---|
| Mac | Use a resizable window with a session sidebar and a detail area. Keep the selected row visible. Put frequent commands in the toolbar and expose them in the menu bar for keyboard users. |
| iPhone | Use a navigation stack from sessions to the selected session. Offer Terminal and Files within that session. Give each folder a title and a standard Back button. Keep input and primary actions reachable when the software keyboard is visible. |
| iPad | Show the session list alongside its detail when space permits. Collapse to stack navigation at compact widths. Preserve the selected session and folder when the window resizes, including while using a hardware keyboard or trackpad. |

Apple’s Food Truck sample demonstrates how a SwiftUI split view and navigation stack can share a selection across form factors. Use that pattern for sessions and their detail. [Apple multiplatform sample](https://developer.apple.com/documentation/swiftui/food-truck-building-a-swiftui-multiplatform-app) · [Designing for macOS](https://developer.apple.com/design/human-interface-guidelines/designing-for-macos)

## Interact with a running CLI

- **Keyboard.** Focus the terminal on attach. In PTY mode, send text, arrows, Escape and control keys to the running program. For example, Control-C goes to the child while Command-C copies a local text selection. Keep app commands available through standard menus. In pipe mode, provide a labeled input field and distinct, selectable stdout and stderr.
- **Mouse and trackpad.** In an ordinary terminal, clicking focuses the view, dragging selects text and scrolling moves through output. When a terminal application enables mouse reporting, translate supported clicks, drags and wheel events into terminal input at the correct row and column. A click in a full-screen CLI’s list should select the item the user pointed at. Keep toolbar and file-browser events local.
- **Touch and focus.** Tapping the terminal on iPhone or iPad should bring up the keyboard. Make terminal-specific keys such as Escape and Control reachable from the onscreen keyboard. Opening Files moves focus into the browser. Returning to Terminal restores input focus and shows output received while the user was browsing.

Switching views keeps the attachment alive. **Detach** leaves the session and its child running for other clients. [Apple keyboard guidance](https://developer.apple.com/design/human-interface-guidelines/keyboards/) · [Apple pointing-device guidance](https://developer.apple.com/design/human-interface-guidelines/pointing-devices)

## Touch-only operation on iPhone and iPad

Every user-facing action offered on iPhone or iPad must be reachable and usable by touch alone, including with the onscreen keyboard open. This covers pairing and cancellation, listing and opening sessions, both terminal modes, file navigation and transfers, starting and stopping port forwarding, error recovery and detaching. Actions available through a shortcut or pointer must also have a discoverable touch path, such as a labeled button or a native menu item. Follow the platform’s standard gestures and keep Back and Cancel available where a gesture can dismiss a view. [Apple gesture guidance](https://developer.apple.com/design/human-interface-guidelines/gestures)

| Interaction | Expected result |
|---|---|
| Tap, scroll and go back | Tapping a control activates it once and gives visible feedback. A swipe through sessions, files or terminal history scrolls the intended view without activating the row or cell under the finger. In a navigation stack, the edge-swipe and Back button return to the same parent. Cancelling a partial back swipe preserves the current view. |
| Enter and select text | Tap to focus, show or dismiss the keyboard, enter text and send it. In PTY mode, arrows, Escape and Control combinations are available onscreen. In a text-selection context, touch and hold to select output, drag the selection handles and use the system Copy action. Paste into the intended input using the system edit menu. |
| Interact with a full-screen CLI | When the child requests mouse reporting, a deliberate tap on a terminal item targets that item’s row and column, including after rotation or a keyboard resize. Gestures in app controls, sheets and file pickers stay local. |
| Reach controls across layouts | Keep touch targets comfortably sized and separated. The keyboard, safe areas and adjacent controls must leave actions reachable in portrait, landscape and compact iPad windows. Touch users can dismiss a sheet, cancel a picker or recover from an error and resume the same session. |

## Browse the session workspace

**Files** opens at the supervisor’s workspace root on the first visit. Keep the current folder and selection on later visits to the same session. Show the session name and current folder so the user can identify the remote destination before transferring a file.

| Task | Mac, following Finder | iPhone and iPad, following Files |
|---|---|---|
| Navigate | Use a native list or table. Single-click selects, double-click opens a folder, and arrow keys move the selection. Offer keyboard commands to open the selected folder and go to its parent. | Use folder rows with standard symbols. Tap to enter a folder and use Back to return to its parent. A wider iPad window can keep the folder hierarchy visible. |
| Identify an item | Show the name and, where space permits, size and modification date in aligned columns. Preserve selection as directory contents refresh. | Prioritize the filename and folder indicator. Show secondary metadata where it fits and allow larger text to reflow. |
| Transfer | **Upload** opens the system Open panel. **Download** on a selected remote file opens the Save panel with its filename filled in. | **Upload** uses the system document picker. **Download** lets the user choose where to save through the system export interface. Keep both actions discoverable by touch. |

Parent navigation ends at the workspace root. Keep the local picker’s location separate from the remote folder. Show transfer activity, report completion only after success and leave the user in the same remote folder after an error or picker cancellation. [Finder user guide](https://support.apple.com/guide/mac-help/organize-your-files-in-the-finder-mchle9f0a1b2/mac) · [Files user guide](https://support.apple.com/guide/iphone/find-and-view-files-and-folders-iphe4bff8827/ios)

## Appearance and accessibility

Use native SwiftUI, AppKit or UIKit lists, toolbars, menus, sheets and pickers. Use system text styles for navigation and filenames, and a legible monospaced face for terminal content. Use SF Symbols with accessible labels for familiar commands. Inherit system spacing, selection colors and focus indicators. Support light and dark appearance, increased contrast and Dynamic Type on iPhone and iPad. VoiceOver should announce the selected session, the current folder and transfer outcomes. Pair a status color with text or a symbol. [Apple color guidance](https://developer.apple.com/design/human-interface-guidelines/color) · [Apple VoiceOver guidance](https://developer.apple.com/design/human-interface-guidelines/voiceover)

## Design acceptance

1. On Mac, complete pairing, select a registered session, enter a command, open Files and return to Terminal using only the keyboard. Repeat with a mouse or trackpad. Verify a mouse-aware test CLI receives a click at the selected cell after resizing the window.
2. On iPhone and iPad simulators, navigate into two nested folders, download a file, upload it to the other folder and compare its contents on the supervisor. Cancel a second transfer in the picker and verify the current folder and selection. At the root, verify parent navigation cannot expose host files.
3. Keep a second client attached. While the Apple user browses Files, send output from the running child. On return to Terminal, verify the same session shows that output and accepts the next keystroke.
4. Review the session list, terminal and file browser in light and dark appearances, at large text sizes and in a narrow window. Check keyboard focus and VoiceOver labels and reading order. Record outcomes by platform, including which interactions were checked manually.
5. On **both iPhone and iPad simulators**, disconnect hardware keyboard input and exercise the touch-only workflows above through the visible UI. Include swipes, a cancelled back gesture, long-press selection, handle dragging, copy/paste, onscreen terminal keys, port controls and picker cancellation. Check the resulting screen state and the input received by the child. Repeat terminal-cell targeting after a layout change. Record each workflow’s outcome separately on each device, including any gesture requiring manual verification.

These are acceptance criteria for the intended design. The verification record identifies the interactions already exercised in a build. [Executed tests](verification.md)
