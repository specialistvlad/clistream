# Implementation plans

Every CLIStream implementation plan must cover all three areas below.

- **Documentation.** Review the README and all repository documentation. Identify and update every document affected by the change, including requirements, implementation plans, protocol and security references, platform guides, and verification records.
- **Code on every supported platform.** Describe how the change will work in the Go daemon, supervisor, and CLI on Linux and macOS, and in the native apps on macOS, iPhone, and iPad. Identify which behavior belongs in shared code and which requires platform-specific changes.
- **Tests.** Specify the tests to add or update, the suites to run on each affected platform, and the expected results. Include cross-platform end-to-end coverage when a change affects how components interact.

If an area or platform requires no changes, explain why. Record any work the user explicitly defers and identify the remaining verification.

# Delivery

Commit and push requested changes to the current branch's upstream as soon as verification passes. Keep unrelated working-tree changes out of the commit.
