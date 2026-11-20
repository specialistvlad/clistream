#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
make build
cd apple
export GITHUB_ACTIONS=true
platform=${1:-iOS}
case "$platform" in
  macOS) unset SIMULATOR_ID; destination="platform=macOS,arch=$(uname -m)"; dd=build/accept-mac ;;
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
case "$platform" in
  macOS) hosted=CLIStream-MacTests; ui=CLIStream-MacUITests
         test_limits=(-test-timeouts-enabled YES -default-test-execution-time-allowance 300 -maximum-test-execution-time-allowance 360) ;;
  iOS) hosted=CLIStream-iOSTests; ui=CLIStream-UITests; test_limits=() ;;
esac
suites="hosted files terminal"
if [[ "$platform" == iOS ]]; then
  suites+=" touch scroll"
  runtime=$(xcrun simctl getenv "$SIMULATOR_ID" SIMULATOR_RUNTIME_VERSION)
  if (( ${runtime%%.*} >= 27 )); then suites+=" voiceover"; fi
fi
results=$(mktemp -d "$PWD/build/results-$platform-XXXXXXXX")
echo "XCTest results in $results"
failed=0
for suite in ${CLISTREAM_TEST_SUITES:-$suites}; do
  case "$suite" in
    hosted) test="$hosted" ;;
    files) test="$ui/SessionTests/testPairAndUseSession" ;;
    terminal) test="$ui/SessionTests/testTerminalInput" ;;
    scroll) test="$ui/SessionTests/testScrollSessionsAndTerminalHistory" ;;
    touch) test="$ui/SessionTests/testTouchNavigationAndEditing" ;;
    voiceover) test="$ui/SessionTests/testVoiceOverWorkspaceOrder" ;;
    *) echo "Unknown suite $suite" >&2; exit 2 ;;
  esac
  echo "Running $platform $suite"
  python3 test-fixture.py "$suite" xcodebuild -quiet test-without-building "${options[@]}" \
    -resultBundlePath "$results/$suite.xcresult" -only-testing:"$test" -parallel-testing-enabled NO -collect-test-diagnostics never \
    ${test_limits[@]+"${test_limits[@]}"} || failed=1
done
exit "$failed"
