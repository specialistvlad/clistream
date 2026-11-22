#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
platform=${1:-iOS}
case "$platform" in
  macOS)
    destination="platform=macOS,arch=$(uname -m)"
    buildDestination="generic/platform=macOS"
    dd=accept-mac
    hosted=CLIStream-MacTests
    suite=CLIStream-MacUITests ;;
  iOS)
    : "${SIMULATOR_ID:?Set SIMULATOR_ID to an iOS 27 or later simulator UDID}"
    destination="platform=iOS Simulator,id=$SIMULATOR_ID"
    buildDestination="generic/platform=iOS Simulator"
    dd=accept-ios
    hosted=CLIStream-iOSTests
    suite=CLIStream-UITests ;;
  *) echo "expected macOS or iOS" >&2; exit 2 ;;
esac
extra=()
if [[ $platform == iOS ]] && [[ $(xcrun simctl list devices --json | jq -r --arg id "$SIMULATOR_ID" '.devices[][] | select(.udid == $id) | .deviceTypeIdentifier') == *iPhone-Duo ]]; then
  export CLISTREAM_DUO_NAME=$(xcrun simctl list devices --json | jq -r --arg id "$SIMULATOR_ID" '.devices[][] | select(.udid == $id) | .name')
  extra+=("-only-testing:$suite/DuoContinuityTests")
fi
make build
# Compile before booting iOS. Simulator services and the Swift compiler otherwise
# compete for memory on a cold hosted Mac.
result=$(mktemp -d "$PWD/apple/build/results-$platform-XXXXXXXX")
products="$result/CLIStream.xctestproducts"
(
  cd apple
  xcodebuild -quiet -scheme "CLIStream-$platform" -derivedDataPath "build/$dd" \
    -destination "$buildDestination" -jobs 3 CLISTREAM_APP_ID=com.vlk.clistream.acceptance \
    -testProductsPath "$products" build-for-testing
)
if [[ $platform == iOS ]]; then apple/boot-simulator.sh "$SIMULATOR_ID"; fi
export GITHUB_ACTIONS=true CLISTREAM_HOSTED_TESTS=1
go run ./scripts/apple-ui xcodebuild -quiet -testProductsPath "$products" -destination "$destination" \
  -parallel-testing-enabled NO -collect-test-diagnostics never -resultBundlePath "$result/native.xcresult" \
  -only-testing:"$hosted" -only-testing:"$suite/SessionTests" -only-testing:"$suite/NativeSessionTests" ${extra[@]+"${extra[@]}"} -skip-testing:"$suite/NativeSessionTests/testConcurrentViewers" test-without-building
go run ./scripts/apple-report "$result/native.xcresult"
case "$platform" in
  macOS) notice="Debug/CLIStream.app/Contents/Resources/LICENSE" ;;
  iOS) notice="Debug-iphonesimulator/CLIStream.app/LICENSE" ;;
esac
cmp LICENSE "apple/build/$dd/Build/Products/$notice"
