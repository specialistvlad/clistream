# macOS design reference

The design target for the CLIStream Mac app on macOS 27, in formats that people and coding agents can read directly.

| File | Contents |
|---|---|
| [spec.md](spec.md) | Every screen, state, menu, sheet and alert, with copy, measurements and the SwiftUI component that draws each element. |
| [tokens.json](tokens.json) | Colors for light and dark, typography, metrics and SF Symbol names, each tagged with its source in Apple's UI Kit. |

## Using this reference

1. Build each element with the system component that [spec.md](spec.md) names. The system then draws its glass, selection, focus and spacing on its own.
2. Use [tokens.json](tokens.json) to check the rendered app. Hardcode only the values marked `appDrawn`: the terminal surface, the status dot and row status colors.
3. Compare the hosted UI suite's screenshots with the screens listed at the end of [spec.md](spec.md). The system rendering on macOS 27 is the final reference when it differs from a value here.

## Sources

- **Apple macOS 27 UI Kit**, Sketch 2026.3.1: [Sketch](https://sketch.com/s/57153a31-3379-4737-8ac6-dbfd6525f052) · [Figma](https://www.figma.com/community/file/1651309434229735362/macos-27) · [Apple Design Resources](https://developer.apple.com/design/resources/). Symbol names in `tokens.json` match the kit's layer names.
- **Human Interface Guidelines:** [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars) · [Toolbars](https://developer.apple.com/design/human-interface-guidelines/toolbars) · [Materials](https://developer.apple.com/design/human-interface-guidelines/materials) · [Windows](https://developer.apple.com/design/human-interface-guidelines/windows) · [Menus](https://developer.apple.com/design/human-interface-guidelines/menus) · [Context menus](https://developer.apple.com/design/human-interface-guidelines/context-menus) · [What's new](https://developer.apple.com/design/whats-new/)
- **Design canvas** (owner access): [CLIStream macOS Redesign](https://claude.ai/artifact/KFDbtxBvYkSZN6o9KpHJCD). The canvas holds the interactive boards, the shipping app's screenshots and the kit renders.
- **Shipping app:** `docs/screenshots/macos`, captured on macOS 27.2 by the Apple UI matrix in the Test workflow. The folder stays local.

The interaction requirements behind these screens are in [Native Apple clients](../../apple-client-design.md) and [Product requirements](../../requirements.md).
