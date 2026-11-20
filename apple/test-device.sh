#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
make build
cd apple
export GITHUB_ACTIONS=true
platform=${1:-iOS}
case "$platform" in
  macOS) destination="platform=macOS,arch=$(uname -m)"; dd=build/accept-mac ;;
  iOS) : "${SIMULATOR_ID:?Set an iPhone or iPad simulator UDID}"
       destination="platform=iOS Simulator,id=$SIMULATOR_ID"; dd=build/accept-ios
       xcrun simctl bootstatus "$SIMULATOR_ID" -b ;;
  *) echo "expected iOS or macOS" >&2; exit 2 ;;
esac
swiftc decode-test-qr.swift -o build/test-qr
options=(CLISTREAM_APP_ID=com.vlk.clistream.acceptance -scheme "CLIStream-$platform" -destination "$destination" -derivedDataPath "$dd")
xcodebuild -quiet build-for-testing "${options[@]}"
case "$platform" in
  macOS) notice="$dd/Build/Products/Debug/CLIStream.app/Contents/Resources/LICENSE" ;;
  iOS) notice="$dd/Build/Products/Debug-iphonesimulator/CLIStream.app/LICENSE" ;;
esac
cmp ../LICENSE "$notice"
python3 test-fixture.py xcodebuild -quiet test-without-building "${options[@]}" -parallel-testing-enabled NO
