# iOS design reference

The design target for the CLIStream app on iPhone Pro, iPad and iPhone Duo under iOS 27, in formats that people and coding agents can read directly. The Mac counterpart is in [../macos](../macos/README.md).

| File | Contents |
|---|---|
| [spec.md](spec.md) | Every screen and state on the three targets, with copy, measurements and the SwiftUI component for each element. |
| [tokens.json](tokens.json) | Device sizes, light and dark colors, type, metrics and SF Symbol names, tagged with their source in Apple's iOS 27 UI Kit. |
| [implementation-prompt.md](implementation-prompt.md) | Handoff prompt for a coding agent that implements this design. |
| [boards/](boards/) | Canvas sources: `Phone.dc.html` (iPhone Pro and Duo outer screen) and `Pad.dc.html` (iPad and Duo inner screen). |

## Using this reference

1. Build each element with the system component that [spec.md](spec.md) names, so the system draws glass, navigation, lists, sheets and the keyboard.
2. Use [tokens.json](tokens.json) to check the rendered app. Hardcode only the values marked `appDrawn`.
3. Compare the hosted UI suite's iPhone, iPad and Duo screenshots with the Screens table in [spec.md](spec.md). The system rendering on iOS 27 is the final reference when it differs from a value here.

## Implementation map

The handoff records the pre-redesign filenames at `e1890eb`. The mobile UI now lives in `apple/Mobile`. MobileServers draws both list layouts, MobileTerminal owns the terminal toolbar and status, and TerminalKeyBar hosts the keyboard accessory. MobileLifecycle and MobileSheets present run and port controls. MobileInvitations and MobileScanner handle pairing. Shared terminal I/O and enrollment live in `apple/App`. The native Mac and mobile Files extensions compile the same provider implementation.

## Board states

Each iOS board on the canvas renders one of the two components with these props. Unlisted props keep their defaults: light appearance, servers list, nothing open.

| Board | Component | Props |
|---|---|---|
| `IPhoneList` | Phone | defaults |
| `IPhoneListDark` | Phone | `appearance="dark"` `swipe="sh"` |
| `IPhoneTerminal` | Phone | `screen="terminal"` `keyboard` |
| `IPhoneMenu` | Phone | `screen="terminal"` `appearance="dark"` `menu="session"` |
| `IPhonePorts` | Phone | `screen="terminal"` `sheet="ports"` |
| `IPhonePair` | Phone | `sheet="pair"` |
| `IPhoneScan` | Phone | `sheet="scan"` |
| `IPhoneCommand` | Phone | `screen="terminal"` `sheet="command"` |
| `IPhoneStop` | Phone | `screen="terminal"` `alert="stop"` |
| `IPhoneEmpty` | Phone | `screen="empty"` |
| `IPhonePairError` | Phone | `screen="empty"` `alert="pairError"` |
| `DuoOuterList` | Phone | `device="duoOuter"` |
| `DuoOuterTerminal` | Phone | `device="duoOuter"` `screen="terminal"` `appearance="dark"` |
| `IPadMain` | Pad | defaults |
| `IPadDark` | Pad | `appearance="dark"` `portsOpen` |
| `DuoInner` | Pad | `device="duoInner"` |
| `DuoInnerPorts` | Pad | `device="duoInner"` `appearance="dark"` `portsOpen` |

## Sources

- **Apple iOS 27 UI Kit**, Sketch 2026.3.1: [Sketch](https://www.sketch.com/s/04c24d8b-38fb-4afb-8836-36617e022f02) · [Figma](https://www.figma.com/community/file/1651309003795292092/ios-and-ipados-27) · [Apple Design Resources](https://developer.apple.com/design/resources/). Names in `tokens.json` match the kit's layer names.
- **Human Interface Guidelines:** [Layout](https://developer.apple.com/design/human-interface-guidelines/layout) · [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars) · [Lists and tables](https://developer.apple.com/design/human-interface-guidelines/lists-and-tables) · [Sheets](https://developer.apple.com/design/human-interface-guidelines/sheets) · [Menus](https://developer.apple.com/design/human-interface-guidelines/menus) · [Virtual keyboards](https://developer.apple.com/design/human-interface-guidelines/virtual-keyboards)
- **Design canvas** (owner access): [CLIStream Apple Redesign](https://claude.ai/artifact/KFDbtxBvYkSZN6o9KpHJCD), page **iOS · iPhone, iPad and Duo**. It holds the interactive boards, the shipping iOS app's screenshots and the iOS 27 kit renders.
- **Shipping app:** `docs/screenshots/iphone`, `docs/screenshots/ipad` and `docs/screenshots/iphone-duo`, captured by the Apple UI matrix in the Test workflow. The folders stay local.
